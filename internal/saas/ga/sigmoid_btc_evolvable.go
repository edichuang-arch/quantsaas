package ga

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"

	"github.com/edi/quantsaas/internal/adapters/backtest"
	"github.com/edi/quantsaas/internal/quant"
	sigmoidbtc "github.com/edi/quantsaas/internal/strategies/sigmoid-btc"
)

// SigmoidBTCEvolvable 是 sigmoid-btc 策略的 EvolvableStrategy 实现。
// 引擎通过 8 个动词操作不透明 Gene；Gene 在此包内部是 quant.Chromosome。
type SigmoidBTCEvolvable struct{}

// NewSigmoidBTCEvolvable 构造一个实例（无状态；可安全并发使用）。
func NewSigmoidBTCEvolvable() *SigmoidBTCEvolvable { return &SigmoidBTCEvolvable{} }

func (s *SigmoidBTCEvolvable) StrategyID() string { return sigmoidbtc.StrategyID }

// --- 1. Sample：从“合理区”（InitBounds）均匀采样 ---
//
// 注意：采样用 InitMin/InitMax（窄区间，避开策略地狱区），
// 而不是 Min/Max（HardBounds，宽到全员 fatal）。
// Mutate 仍用 HardBounds，让 GA 后期能突破探索极端值。
func (s *SigmoidBTCEvolvable) Sample(rng *rand.Rand) Gene {
	b := quant.HardBounds
	c := quant.Chromosome{
		Beta:                uniform(rng, b.Beta.InitMin, b.Beta.InitMax),
		Gamma:               uniform(rng, b.Gamma.InitMin, b.Gamma.InitMax),
		SigmaFloorPct:       uniform(rng, b.SigmaFloorPct.InitMin, b.SigmaFloorPct.InitMax),
		BaseDays:            uniformInt(rng, int(b.BaseDays.InitMin), int(b.BaseDays.InitMax)),
		Multiplier:          uniform(rng, b.Multiplier.InitMin, b.Multiplier.InitMax),
		BetaThreshold:       uniform(rng, b.BetaThreshold.InitMin, b.BetaThreshold.InitMax),
		PriceDiscountBoost:  uniform(rng, b.PriceDiscountBoost.InitMin, b.PriceDiscountBoost.InitMax),
		DeadlineForcePct:    uniform(rng, b.DeadlineForcePct.InitMin, b.DeadlineForcePct.InitMax),
		MinAgeMonths:        uniformInt(rng, int(b.MinAgeMonths.InitMin), int(b.MinAgeMonths.InitMax)),
		SoftReleaseMaxRatio: uniform(rng, b.SoftReleaseMaxRatio.InitMin, b.SoftReleaseMaxRatio.InitMax),
		BullTimeDilation:    uniform(rng, b.BullTimeDilation.InitMin, b.BullTimeDilation.InitMax),
		BearTimeDilation:    uniform(rng, b.BearTimeDilation.InitMin, b.BearTimeDilation.InitMax),
		BullBetaMultiplier:  uniform(rng, b.BullBetaMultiplier.InitMin, b.BullBetaMultiplier.InitMax),
		BearBetaMultiplier:  uniform(rng, b.BearBetaMultiplier.InitMin, b.BearBetaMultiplier.InitMax),
		MicroReservePct:     uniform(rng, b.MicroReservePct.InitMin, b.MicroReservePct.InitMax),
	}
	return quant.ClampChromosome(c)
}

// --- 2. Mutate：每维度独立 Bernoulli(prob) 决定是否变异；变异量为 N(0,1)×step×scale ---

func (s *SigmoidBTCEvolvable) Mutate(c Gene, prob, scale float64, rng *rand.Rand) Gene {
	cur := toChromo(c)
	b := quant.HardBounds
	mutate := func(x *float64, bound quant.Bound) {
		if rng.Float64() < prob {
			*x += rng.NormFloat64() * bound.Step * scale
		}
	}
	mutateInt := func(x *int, bound quant.Bound) {
		if rng.Float64() < prob {
			*x += int(math.Round(rng.NormFloat64() * bound.Step * scale))
		}
	}
	mutate(&cur.Beta, b.Beta)
	mutate(&cur.Gamma, b.Gamma)
	mutate(&cur.SigmaFloorPct, b.SigmaFloorPct)
	mutateInt(&cur.BaseDays, b.BaseDays)
	mutate(&cur.Multiplier, b.Multiplier)
	mutate(&cur.BetaThreshold, b.BetaThreshold)
	mutate(&cur.PriceDiscountBoost, b.PriceDiscountBoost)
	mutate(&cur.DeadlineForcePct, b.DeadlineForcePct)
	mutateInt(&cur.MinAgeMonths, b.MinAgeMonths)
	mutate(&cur.SoftReleaseMaxRatio, b.SoftReleaseMaxRatio)
	mutate(&cur.BullTimeDilation, b.BullTimeDilation)
	mutate(&cur.BearTimeDilation, b.BearTimeDilation)
	mutate(&cur.BullBetaMultiplier, b.BullBetaMultiplier)
	mutate(&cur.BearBetaMultiplier, b.BearBetaMultiplier)
	mutate(&cur.MicroReservePct, b.MicroReservePct)
	return quant.ClampChromosome(cur)
}

// --- 3. Crossover：每维度 50% 概率从两个父代任选一个 ---

func (s *SigmoidBTCEvolvable) Crossover(p1, p2 Gene, rng *rand.Rand) Gene {
	a := toChromo(p1)
	b := toChromo(p2)
	pick := func(x, y float64) float64 {
		if rng.Float64() < 0.5 {
			return x
		}
		return y
	}
	pickInt := func(x, y int) int {
		if rng.Float64() < 0.5 {
			return x
		}
		return y
	}
	child := quant.Chromosome{
		Beta:                pick(a.Beta, b.Beta),
		Gamma:               pick(a.Gamma, b.Gamma),
		SigmaFloorPct:       pick(a.SigmaFloorPct, b.SigmaFloorPct),
		BaseDays:            pickInt(a.BaseDays, b.BaseDays),
		Multiplier:          pick(a.Multiplier, b.Multiplier),
		BetaThreshold:       pick(a.BetaThreshold, b.BetaThreshold),
		PriceDiscountBoost:  pick(a.PriceDiscountBoost, b.PriceDiscountBoost),
		DeadlineForcePct:    pick(a.DeadlineForcePct, b.DeadlineForcePct),
		MinAgeMonths:        pickInt(a.MinAgeMonths, b.MinAgeMonths),
		SoftReleaseMaxRatio: pick(a.SoftReleaseMaxRatio, b.SoftReleaseMaxRatio),
		BullTimeDilation:    pick(a.BullTimeDilation, b.BullTimeDilation),
		BearTimeDilation:    pick(a.BearTimeDilation, b.BearTimeDilation),
		BullBetaMultiplier:  pick(a.BullBetaMultiplier, b.BullBetaMultiplier),
		BearBetaMultiplier:  pick(a.BearBetaMultiplier, b.BearBetaMultiplier),
		MicroReservePct:     pick(a.MicroReservePct, b.MicroReservePct),
	}
	return quant.ClampChromosome(child)
}

// --- 4. Fingerprint：FNV-1a-64，精度 1e-6 量化 ---

func (s *SigmoidBTCEvolvable) Fingerprint(c Gene) uint64 {
	cur := toChromo(c)
	h := fnv.New64a()
	var buf [8]byte
	quantize := func(f float64) {
		q := int64(math.Round(f * 1e6)) // 精度 1e-6
		binary.LittleEndian.PutUint64(buf[:], uint64(q))
		_, _ = h.Write(buf[:])
	}
	quantize(cur.Beta)
	quantize(cur.Gamma)
	quantize(cur.SigmaFloorPct)
	quantize(float64(cur.BaseDays))
	quantize(cur.Multiplier)
	quantize(cur.BetaThreshold)
	quantize(cur.PriceDiscountBoost)
	quantize(cur.DeadlineForcePct)
	quantize(float64(cur.MinAgeMonths))
	quantize(cur.SoftReleaseMaxRatio)
	quantize(cur.BullTimeDilation)
	quantize(cur.BearTimeDilation)
	quantize(cur.BullBetaMultiplier)
	quantize(cur.BearBetaMultiplier)
	quantize(cur.MicroReservePct)
	return h.Sum64()
}

// --- 5. Evaluate：按窗口从短到长级联短路（进化文档 3.4） ---

func (s *SigmoidBTCEvolvable) Evaluate(plan *EvaluablePlan, c Gene) EvalResult {
	cur := toChromo(c)
	paramsBlob, err := quant.EncodeParamPack(cur, planSpawn(plan))
	if err != nil {
		return EvalResult{ScoreTotal: FatalFitnessScore, Fatal: true}
	}

	res := EvalResult{Results: make([]quant.CrucibleResult, 0, len(plan.Windows))}
	for i, w := range plan.Windows {
		base := plan.DCABaselines[i]

		cfg := backtest.Config{
			InitialUSDT:       plan.InitialUSDT,
			MonthlyInjectUSDT: plan.MonthlyInject,
			LotStep:           plan.LotStep,
			LotMin:            plan.LotMin,
			TakerFeeBps:       planTakerFeeBps(plan),
			ParamsBlob:        paramsBlob,
			LoadParams: func(raw []byte) any {
				return sigmoidbtc.LoadParams(raw)
			},
			Step: func(in quant.StrategyInput, p any) quant.StrategyOutput {
				return sigmoidbtc.Step(in, p.(sigmoidbtc.Params))
			},
			Symbol: plan.Pair,
		}
		r := backtest.Run(w.Bars, cfg, w.EvalStartMs)

		roi := computeROI(r, plan.InitialUSDT)
		alpha := roi - base.ROI

		cr := quant.CrucibleResult{
			Label:       w.Label,
			ROI:         roi,
			MaxDrawdown: r.MaxDrawdown,
			Alpha:       alpha,
		}

		// Fatal 检查（进化文档 3.1）
		if r.MaxDrawdown >= FatalDrawdownThreshold {
			cr.Fatal = true
			cr.Score = FatalFitnessScore
			res.Results = append(res.Results, cr)
			res.Fatal = true
			res.ScoreTotal = FatalFitnessScore
			if r.MaxDrawdown > res.MaxDrawdown {
				res.MaxDrawdown = r.MaxDrawdown
			}
			return res // 级联短路：短窗 fatal 后不再评估更长窗口
		}

		// SliceScore = Alpha − 1.5 × max(0, MaxDD_strategy − MaxDD_baseline)
		penalty := r.MaxDrawdown - base.MaxDrawdown
		if penalty < 0 {
			penalty = 0
		}
		cr.Score = alpha - 1.5*penalty
		res.ScoreTotal += w.Weight * cr.Score

		if r.MaxDrawdown > res.MaxDrawdown {
			res.MaxDrawdown = r.MaxDrawdown
		}
		res.Results = append(res.Results, cr)
	}
	return res
}

// --- 6/7. DecodeElite / EncodeResult ---

func (s *SigmoidBTCEvolvable) DecodeElite(raw []byte) Gene {
	c, _ := quant.DecodeParamPack(raw)
	return c
}

func (s *SigmoidBTCEvolvable) EncodeResult(c Gene, spawn *quant.SpawnPoint) ([]byte, error) {
	cur := toChromo(c)
	sp := quant.DefaultSpawnPoint
	if spawn != nil {
		sp = *spawn
	}
	return quant.EncodeParamPack(cur, sp)
}

// --- 私有工具 ---

func toChromo(g Gene) quant.Chromosome {
	if c, ok := g.(quant.Chromosome); ok {
		return c
	}
	return quant.DefaultSeedChromosome
}

func uniform(rng *rand.Rand, lo, hi float64) float64 {
	return lo + rng.Float64()*(hi-lo)
}

func uniformInt(rng *rand.Rand, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + rng.Intn(hi-lo+1)
}

func planSpawn(plan *EvaluablePlan) quant.SpawnPoint {
	if plan != nil && plan.Spawn != nil {
		return *plan.Spawn
	}
	return quant.DefaultSpawnPoint
}

func planTakerFeeBps(plan *EvaluablePlan) int {
	if plan != nil && plan.Spawn != nil && plan.Spawn.Risk.TakerFeeBps > 0 {
		return plan.Spawn.Risk.TakerFeeBps
	}
	return quant.DefaultSpawnPoint.Risk.TakerFeeBps
}

// computeROI 用 Modified Dietz（剔除注资跳变的影响）计算回测 ROI。
func computeROI(r backtest.Result, initial float64) float64 {
	return quant.ModifiedDietzROI(initial, r.FinalEquity, r.CashFlows, r.CashFlowDays, r.TotalDays)
}
