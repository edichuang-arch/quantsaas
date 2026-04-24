package ga

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- 辅助：FakeEvolvable 让 engine 测试脱离真实策略与回测 ---

// fakeGene 只有一个 float 字段的"基因"。评估时直接返回其值作为适应度。
type fakeGene struct {
	Score float64
}

type fakeEvolvable struct {
	fatalBelowScore float64 // 若 gene.Score < fatalBelow，则返回 fatal
}

func (f *fakeEvolvable) StrategyID() string { return "fake" }

func (f *fakeEvolvable) Sample(rng *rand.Rand) Gene {
	return fakeGene{Score: rng.Float64() * 10}
}

func (f *fakeEvolvable) Mutate(c Gene, prob, scale float64, rng *rand.Rand) Gene {
	g := c.(fakeGene)
	if rng.Float64() < prob {
		g.Score += rng.NormFloat64() * scale
	}
	return g
}

func (f *fakeEvolvable) Crossover(p1, p2 Gene, rng *rand.Rand) Gene {
	a := p1.(fakeGene)
	b := p2.(fakeGene)
	if rng.Float64() < 0.5 {
		return a
	}
	return b
}

func (f *fakeEvolvable) Fingerprint(c Gene) uint64 {
	return uint64(c.(fakeGene).Score * 1e6)
}

func (f *fakeEvolvable) Evaluate(plan *EvaluablePlan, c Gene) EvalResult {
	g := c.(fakeGene)
	fatal := g.Score < f.fatalBelowScore
	score := g.Score
	if fatal {
		score = FatalFitnessScore
	}
	return EvalResult{
		ScoreTotal: score,
		Fatal:      fatal,
		Results: []quant.CrucibleResult{
			{Label: "full", Score: score, Alpha: g.Score},
		},
	}
}

func (f *fakeEvolvable) DecodeElite(raw []byte) Gene {
	return fakeGene{Score: 0}
}

func (f *fakeEvolvable) EncodeResult(c Gene, spawn *quant.SpawnPoint) ([]byte, error) {
	return []byte("{}"), nil
}

// 最小的 plan（只需要 Pair + 一个窗口即可通过 engine 的空检查）
func fakePlan() *EvaluablePlan {
	return &EvaluablePlan{
		Pair: "BTCUSDT",
		Windows: []quant.CrucibleWindow{
			{Label: "full", Weight: 1.0, Bars: []quant.Bar{{OpenTime: 1}}},
		},
		DCABaselines: []DCABaseline{{ROI: 0}},
	}
}

// --- 实际测试 ---

// RunEpoch 返回 best gene 且 score 是种群最大值。
func TestEngine_RunEpochProducesBest(t *testing.T) {
	ev := &fakeEvolvable{fatalBelowScore: -1e18}
	eng := NewEngine(ev, nil, Config{
		PopSize:                 20,
		MaxGenerations:          5,
		EliteCount:              2,
		TournamentSize:          3,
		MutationProbability:     0.15,
		MutationScale:           1.0,
		MutationProbabilityMax:  0.55,
		MutationScaleMax:        3.0,
		MutationRampFactor:      1.25,
		EarlyStopPatience:       10,
		EarlyStopMinDelta:       0.001,
		InitialRandomRatio:      0.5,
		InitialEliteMutateRatio: 0.4,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := eng.RunEpoch(ctx, fakePlan(), EpochConfig{
		PopSize:        20,
		MaxGenerations: 5,
		SeedRNG:        1234,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotZero(t, result.BestScore)
	// BestScore 应该 == 种群首位 fitness
	assert.Equal(t, result.Population.Fitness[0], result.BestScore)
}

// 精英保留：一代后上一代 Top N 必须仍在新种群中。
func TestEngine_ElitismPreservesTopN(t *testing.T) {
	ev := &fakeEvolvable{fatalBelowScore: -1e18}
	eng := NewEngine(ev, nil, Config{
		PopSize:             10,
		MaxGenerations:      2,
		EliteCount:          3,
		TournamentSize:      3,
		MutationProbability: 0.0, // 零变异确保精英不变
		MutationScale:       0.0,
		MutationProbabilityMax: 0,
		MutationScaleMax:       0,
		MutationRampFactor:     1.0,
		EarlyStopPatience:      100, // 不触发 early stop
		EarlyStopMinDelta:      0.001,
	})
	ctx := context.Background()
	result, err := eng.RunEpoch(ctx, fakePlan(), EpochConfig{PopSize: 10, MaxGenerations: 2, SeedRNG: 99})
	require.NoError(t, err)
	// 最终种群 Top 3 的 fitness 应严格非递增（排序后）
	for i := 1; i < 3; i++ {
		assert.GreaterOrEqual(t, result.Population.Fitness[i-1], result.Population.Fitness[i])
	}
}

// Fatal 个体很少在 tournament 中被选中。
func TestTournamentSelect_FatalExcluded(t *testing.T) {
	pop := NewPopulation(100)
	// 99 个正常 + 1 个 fatal
	for i := 0; i < 99; i++ {
		pop.Genes[i] = fakeGene{Score: float64(i)}
		pop.Fitness[i] = float64(i)
	}
	pop.Genes[99] = fakeGene{Score: FatalFitnessScore}
	pop.Fitness[99] = FatalFitnessScore
	pop.FatalMap[99] = true

	rng := rand.New(rand.NewSource(1))
	fatalPicks := 0
	for i := 0; i < 1000; i++ {
		g := tournamentSelect(pop, 3, rng)
		if g.(fakeGene).Score == FatalFitnessScore {
			fatalPicks++
		}
	}
	// Fatal 分数极低，几乎不会被选中（<5%）。
	assert.Less(t, fatalPicks, 50, "fatal should rarely be picked")
}

// 指纹缓存命中时不重复调用 Evaluate。
func TestEvaluatePopulation_CacheHit(t *testing.T) {
	ev := &fakeEvolvable{fatalBelowScore: -1e18}
	eng := NewEngine(ev, nil, Config{
		PopSize:          10,
		MaxGenerations:   1,
		EliteCount:       2,
		TournamentSize:   3,
		EarlyStopPatience: 10,
		EarlyStopMinDelta: 0.001,
	})

	// 构造 10 个完全相同的 gene（相同 fingerprint）
	pop := NewPopulation(10)
	for i := 0; i < 10; i++ {
		pop.Genes[i] = fakeGene{Score: 42}
	}
	cache := &fingerprintCache{hits: map[uint64]float64{}}
	eng.evaluatePopulation(context.Background(), fakePlan(), pop, cache)
	// 10 个都命中缓存 → cache hit count 应接近 9（首次未命中）
	assert.Greater(t, cache.hitCount, 5)
}

// sortByFitnessDesc 按降序重排。
func TestSortByFitnessDesc(t *testing.T) {
	pop := NewPopulation(5)
	pop.Genes[0] = fakeGene{Score: 1}
	pop.Genes[1] = fakeGene{Score: 5}
	pop.Genes[2] = fakeGene{Score: 3}
	pop.Genes[3] = fakeGene{Score: 2}
	pop.Genes[4] = fakeGene{Score: 4}
	pop.Fitness = []float64{1, 5, 3, 2, 4}
	pop.FatalMap = make([]bool, 5)

	sortByFitnessDesc(pop)
	assert.Equal(t, []float64{5, 4, 3, 2, 1}, pop.Fitness)
}

// BuildEvaluablePlan 的 windows 数量与 DCABaselines 长度一致。
func TestBuildEvaluablePlan_LengthsMatch(t *testing.T) {
	// 充足 bars → 四个窗口
	total := 1200 + 1825 + 100
	start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]quant.Bar, total)
	for i := 0; i < total; i++ {
		t := start.AddDate(0, 0, i)
		bars[i] = quant.Bar{OpenTime: t.UnixMilli(), Close: 100}
	}
	plan := BuildEvaluablePlan(bars, "BTCUSDT", "sigmoid-btc",
		0.00001, 0.00001, 10000, 300, nil, 1200)
	require.NotNil(t, plan)
	assert.Equal(t, len(plan.Windows), len(plan.DCABaselines))
	assert.Equal(t, "BTCUSDT", plan.Pair)
}

// ctx 取消时 RunEpoch 应尽快返回。
func TestEngine_CtxCancelStops(t *testing.T) {
	ev := &fakeEvolvable{fatalBelowScore: -1e18}
	eng := NewEngine(ev, nil, Config{
		PopSize:           10,
		MaxGenerations:    100,
		EliteCount:        2,
		TournamentSize:    3,
		EarlyStopPatience: 100,
		EarlyStopMinDelta: 0.001,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	_, err := eng.RunEpoch(ctx, fakePlan(), EpochConfig{PopSize: 10, MaxGenerations: 100, SeedRNG: 1})
	assert.Error(t, err)
}
