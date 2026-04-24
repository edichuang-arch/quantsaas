package ga

import (
	"context"
	"errors"
	"math/rand"
	"runtime"
	"sort"
	"sync"

	"github.com/edi/quantsaas/internal/quant"
)

// Config 引擎级超参（进化文档第 2 章默认值）。
type Config struct {
	PopSize                int
	MaxGenerations         int
	EliteCount             int
	TournamentSize         int
	MutationProbability    float64
	MutationScale          float64
	MutationProbabilityMax float64
	MutationScaleMax       float64
	MutationRampFactor     float64
	EarlyStopPatience      int
	EarlyStopMinDelta      float64
	InitialRandomRatio     float64 // 初始种群随机个体比例（默认 0.5）
	InitialEliteMutateRatio float64 // 初始种群"精英+强化变异"比例（默认 0.4）
	// 初始化路径固定的强化变异参数（进化文档 2.1）
	InitialBoostProb  float64
	InitialBoostScale float64
}

// DefaultConfig 引擎超参默认值。可整体改为单例或由调用方覆盖。
var DefaultConfig = Config{
	PopSize:                 300,
	MaxGenerations:          25,
	EliteCount:              8,
	TournamentSize:          3,
	MutationProbability:     0.15,
	MutationScale:           1.0,
	MutationProbabilityMax:  0.55,
	MutationScaleMax:        3.0,
	MutationRampFactor:      1.25,
	EarlyStopPatience:       5,
	EarlyStopMinDelta:       0.001,
	InitialRandomRatio:      0.5,
	InitialEliteMutateRatio: 0.4,
	InitialBoostProb:        0.15,
	InitialBoostScale:       1.5,
}

// EpochConfig 一次进化任务的运行时配置（由 HTTP 触发时生成）。
type EpochConfig struct {
	PopSize            int
	MaxGenerations     int
	LotStep            float64
	LotMin             float64
	InitialUSDT        float64
	MonthlyInject      float64
	Pair               string
	TemplateName       string
	SpawnPointOverride *quant.SpawnPoint // 覆盖冠军/默认的出生点
	SeedRNG            int64             // RNG seed，0 时用 time 作种（测试用）
	OnProgress         func(gen int, bestScore float64, mutProb, mutScale float64)
}

// EpochResult 一次 Epoch 的产出。
type EpochResult struct {
	BestGene       Gene
	BestScore      float64
	BestEvalResult EvalResult
	Generations    int
	Population     *Population
	CacheHits      int
}

// ElitesLoader 从 DB 读取精英基因（DB blob → Gene）。
// 在 engine_test 中可用 fake 实现绕过 DB。
type ElitesLoader interface {
	LoadElites(ctx context.Context, strategyID, symbol string, limit int) ([]Gene, error)
}

// Engine 主引擎。无状态（每次 RunEpoch 从零初始化种群）；可并发调用。
type Engine struct {
	Evolvable EvolvableStrategy
	Elites    ElitesLoader
	Cfg       Config
}

// NewEngine 构造 Engine。若 cfg.PopSize <= 0 则使用 DefaultConfig。
func NewEngine(ev EvolvableStrategy, elites ElitesLoader, cfg Config) *Engine {
	if cfg.PopSize <= 0 {
		cfg = DefaultConfig
	}
	return &Engine{Evolvable: ev, Elites: elites, Cfg: cfg}
}

// RunEpoch 驱动一次完整进化。
// 入参 plan 必须已经构建好（BuildEvaluablePlan）。
// ctx 取消时循环尽快退出。
func (e *Engine) RunEpoch(ctx context.Context, plan *EvaluablePlan, ec EpochConfig) (*EpochResult, error) {
	if e.Evolvable == nil {
		return nil, errors.New("evolvable nil")
	}
	if plan == nil || len(plan.Windows) == 0 {
		return nil, errors.New("plan/windows empty")
	}
	popSize := ec.PopSize
	if popSize <= 0 {
		popSize = e.Cfg.PopSize
	}
	maxGen := ec.MaxGenerations
	if maxGen <= 0 {
		maxGen = e.Cfg.MaxGenerations
	}

	seed := ec.SeedRNG
	if seed == 0 {
		seed = defaultSeed()
	}
	rng := rand.New(rand.NewSource(seed))

	// 1. 种群初始化
	pop, err := e.initializePopulation(ctx, plan, popSize, rng)
	if err != nil {
		return nil, err
	}

	// 2. 首次评估
	cache := &fingerprintCache{hits: map[uint64]float64{}}
	e.evaluatePopulation(ctx, plan, pop, cache)

	// 3. 主循环
	mutProb := e.Cfg.MutationProbability
	mutScale := e.Cfg.MutationScale
	var bestSoFar float64 = -1e18
	patience := 0
	lastGen := 0

	for gen := 0; gen < maxGen; gen++ {
		lastGen = gen
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		sortByFitnessDesc(pop)

		currentBest := pop.Fitness[0]
		if currentBest-bestSoFar >= e.Cfg.EarlyStopMinDelta {
			bestSoFar = currentBest
			patience = 0
		} else {
			patience++
		}

		// 变异斜坡
		if patience >= e.Cfg.EarlyStopPatience {
			atMax := mutProb >= e.Cfg.MutationProbabilityMax && mutScale >= e.Cfg.MutationScaleMax
			if atMax {
				break // Early Stop
			}
			mutProb *= e.Cfg.MutationRampFactor
			if mutProb > e.Cfg.MutationProbabilityMax {
				mutProb = e.Cfg.MutationProbabilityMax
			}
			mutScale *= e.Cfg.MutationRampFactor
			if mutScale > e.Cfg.MutationScaleMax {
				mutScale = e.Cfg.MutationScaleMax
			}
		}

		if ec.OnProgress != nil {
			ec.OnProgress(gen, currentBest, mutProb, mutScale)
		}

		// 产生下一代
		next := e.produceNextGen(pop, rng, mutProb, mutScale)
		pop = next
		e.evaluatePopulation(ctx, plan, pop, cache)
	}

	// 4. 排序取最优
	sortByFitnessDesc(pop)
	bestGene := pop.Genes[0]
	bestScore := pop.Fitness[0]

	// 对最优个体再跑一次 Evaluate 得到完整明细（用于写 DB 展示分数）
	bestEval := e.Evolvable.Evaluate(plan, bestGene)

	return &EpochResult{
		BestGene:       bestGene,
		BestScore:      bestScore,
		BestEvalResult: bestEval,
		Generations:    lastGen + 1,
		Population:     pop,
		CacheHits:      cache.hitCount,
	}, nil
}

// --- 种群初始化 ---

func (e *Engine) initializePopulation(ctx context.Context, plan *EvaluablePlan, size int, rng *rand.Rand) (*Population, error) {
	pop := NewPopulation(size)

	var elites []Gene
	if e.Elites != nil {
		loaded, err := e.Elites.LoadElites(ctx, e.Evolvable.StrategyID(), plan.Pair, size)
		if err == nil {
			elites = loaded
		}
	}

	// index 0 永远是"当前种子冠军"原样（进化文档 2.1）。
	// 有 elites 时取 elites[0]；无则用 DecodeElite(nil) → 默认种子。
	if len(elites) > 0 {
		pop.Genes[0] = elites[0]
	} else {
		pop.Genes[0] = e.Evolvable.DecodeElite(nil)
	}

	if size == 1 {
		return pop, nil
	}

	// 剩余个体按 10/40/50 比例分配：elite 原样 / elite+强化变异 / 完全随机
	remaining := size - 1
	var eliteCopyN, eliteMutateN int
	if len(elites) > 0 {
		eliteCopyN = int(float64(remaining) * 0.10) // 10% elite 原样
		eliteMutateN = int(float64(remaining) * e.Cfg.InitialEliteMutateRatio)
	}
	// 其余为随机
	randomN := remaining - eliteCopyN - eliteMutateN
	if randomN < 0 {
		randomN = 0
		eliteMutateN = remaining - eliteCopyN
	}

	idx := 1
	for i := 0; i < eliteCopyN && idx < size; i++ {
		pop.Genes[idx] = elites[i%len(elites)]
		idx++
	}
	for i := 0; i < eliteMutateN && idx < size; i++ {
		src := elites[rng.Intn(len(elites))]
		pop.Genes[idx] = e.Evolvable.Mutate(src, e.Cfg.InitialBoostProb, e.Cfg.InitialBoostScale, rng)
		idx++
	}
	for idx < size {
		pop.Genes[idx] = e.Evolvable.Sample(rng)
		idx++
	}
	return pop, nil
}

// --- 并发适应度评估 ---

type fingerprintCache struct {
	mu       sync.Mutex
	hits     map[uint64]float64
	hitCount int
}

func (c *fingerprintCache) get(fp uint64) (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.hits[fp]
	if ok {
		c.hitCount++
	}
	return v, ok
}

func (c *fingerprintCache) put(fp uint64, v float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits[fp] = v
}

func (e *Engine) evaluatePopulation(ctx context.Context, plan *EvaluablePlan, pop *Population, cache *fingerprintCache) {
	n := len(pop.Genes)
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers <= 0 {
		workers = 1
	}

	type job struct{ idx int }
	jobs := make(chan job, n)
	for i := 0; i < n; i++ {
		jobs <- job{idx: i}
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				fp := e.Evolvable.Fingerprint(pop.Genes[j.idx])
				if v, ok := cache.get(fp); ok {
					pop.Fitness[j.idx] = v
					continue
				}
				result := e.Evolvable.Evaluate(plan, pop.Genes[j.idx])
				pop.Fitness[j.idx] = result.ScoreTotal
				pop.FatalMap[j.idx] = result.Fatal
				cache.put(fp, result.ScoreTotal)
			}
		}()
	}
	wg.Wait()
}

// --- 产生下一代 ---

func (e *Engine) produceNextGen(prev *Population, rng *rand.Rand, mutProb, mutScale float64) *Population {
	size := len(prev.Genes)
	next := NewPopulation(size)
	// 精英保留
	elite := e.Cfg.EliteCount
	if elite > size {
		elite = size
	}
	for i := 0; i < elite; i++ {
		next.Genes[i] = prev.Genes[i]
	}
	// 剩余通过 tournament + crossover + mutate 生成
	for i := elite; i < size; i++ {
		p1 := tournamentSelect(prev, e.Cfg.TournamentSize, rng)
		p2 := tournamentSelect(prev, e.Cfg.TournamentSize, rng)
		child := e.Evolvable.Crossover(p1, p2, rng)
		child = e.Evolvable.Mutate(child, mutProb, mutScale, rng)
		next.Genes[i] = child
	}
	return next
}

func tournamentSelect(pop *Population, size int, rng *rand.Rand) Gene {
	if size <= 0 {
		size = 1
	}
	best := rng.Intn(len(pop.Genes))
	for i := 1; i < size; i++ {
		cand := rng.Intn(len(pop.Genes))
		if pop.Fitness[cand] > pop.Fitness[best] {
			best = cand
		}
	}
	return pop.Genes[best]
}

// sortByFitnessDesc 按 fitness 降序同时重排 Genes / Fitness / FatalMap。
func sortByFitnessDesc(pop *Population) {
	n := len(pop.Genes)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		return pop.Fitness[idx[i]] > pop.Fitness[idx[j]]
	})
	genes := make([]Gene, n)
	fit := make([]float64, n)
	fatal := make([]bool, n)
	for i, k := range idx {
		genes[i] = pop.Genes[k]
		fit[i] = pop.Fitness[k]
		fatal[i] = pop.FatalMap[k]
	}
	pop.Genes = genes
	pop.Fitness = fit
	pop.FatalMap = fatal
}

// BuildEvaluablePlan 根据 bars + 元数据构建 EvaluablePlan。
// 同时预计算每个窗口的 DCA 基线，代内复用。
func BuildEvaluablePlan(
	bars []quant.Bar,
	pair, templateName string,
	lotStep, lotMin, initialUSDT, monthlyInject float64,
	spawn *quant.SpawnPoint,
	warmupDays int,
) *EvaluablePlan {
	wins := quant.BuildCrucibleWindows(bars, warmupDays)
	baselines := make([]DCABaseline, len(wins))
	for i, w := range wins {
		// Ghost DCA 在"eval 区间"上评估（warmup 不注资、不计 NAV）
		evalBars := w.EvalBars()
		dca := quant.SimulateGhostDCA(evalBars, quant.GhostDCAConfig{
			InitialCapitalUSDT: initialUSDT,
			MonthlyInjectUSDT:  monthlyInject,
		})
		baselines[i] = DCABaseline{
			FinalEquity:   dca.FinalEquity,
			TotalInjected: dca.TotalInjected,
			MaxDrawdown:   dca.MaxDrawdown,
			ROI:           dca.ROI,
		}
	}
	return &EvaluablePlan{
		Pair:          pair,
		TemplateName:  templateName,
		Spawn:         spawn,
		LotStep:       lotStep,
		LotMin:        lotMin,
		Windows:       wins,
		DCABaselines:  baselines,
		InitialUSDT:   initialUSDT,
		MonthlyInject: monthlyInject,
	}
}
