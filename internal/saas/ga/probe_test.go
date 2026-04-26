package ga

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/quant"
)

// TestProbe_GenZeroFatalRate 量化“收窄 InitBounds”在 gen-0 的效果。
//
//   - synthetic：用 jump-diffusion（GBM + 偶发 -15% 暴跌）生成 1500 根日线，
//     近似真实 BTC 的 fat tail 行为
//   - 50 个 Sample() 个体 → Evaluate()，分别用 InitBounds 与 HardBounds 采样
//   - 报告 fatal 率 + 非 fatal 个体的 fitness 分布
//
// 信息性测试（弱断言，InitBounds 至少不显著更糟）。
//
// 注意：合成 jump-diffusion 的 fatal 率上限受“随机跳跃平均会抵消”限制，
// 重现不了真实 BTC 在 2018-2019/2022 那种连续熊市导致的 98% fatal。
// 真正的“fatal 率从 X% 变成 Y%”要靠真实 K 线 + 30 代演化验证。
// 这个 probe 主要用途是：
//   1. 防止未来误改让 InitBounds 比 HardBounds 还差
//   2. 监控两组的 score / MaxDD 分布是否合理（不 NaN、不极端）
//
// 跑法：go test ./internal/saas/ga -run TestProbe_GenZeroFatalRate -v
func TestProbe_GenZeroFatalRate(t *testing.T) {
	if testing.Short() {
		t.Skip("probe is slow; skip in -short")
	}

	bars := jumpDiffusionBars(1500, 0.0003, 0.05, -0.15, 0.005, 42)
	plan := BuildEvaluablePlan(
		bars, "BTCUSDT", "sigmoid-btc",
		0.00001, 0.00001, 10000, 300, nil, 1200,
	)
	if plan == nil || len(plan.Windows) == 0 {
		t.Fatal("plan empty —— synthetic bar 数量不足")
	}
	t.Logf("plan windows: %d (synthetic jump-diffusion 1500d)", len(plan.Windows))

	const N = 50

	initStats := runProbe(t, plan, N, "InitBounds (new Sample)", true, 20240101)
	hardStats := runProbe(t, plan, N, "HardBounds (legacy wide)", false, 20240101)

	t.Logf("--- Δ summary ---")
	t.Logf("fatal rate:  InitBounds %.1f%% vs HardBounds %.1f%% (Δ=%+.1fpp)",
		initStats.fatalPct, hardStats.fatalPct, initStats.fatalPct-hardStats.fatalPct)
	t.Logf("avg score:   InitBounds %.4f vs HardBounds %.4f",
		mean(initStats.scores), mean(hardStats.scores))

	// 弱断言：InitBounds 至少不能比 HardBounds 更差
	if initStats.fatalPct > hardStats.fatalPct+5.0 {
		t.Errorf("InitBounds fatal %.1f%% significantly worse than HardBounds %.1f%%",
			initStats.fatalPct, hardStats.fatalPct)
	}
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

type probeStats struct {
	fatalPct  float64
	scores    []float64
	maxDDs    []float64
	fatalCnt  int
	totalCnt  int
}

// runProbe 用指定采样方式跑 N 个个体。
// useInitBounds=true → 走 evolvable.Sample()（默认现在用 InitBounds）；
// false → 直接用 HardBounds 的 Min/Max 采样（模拟旧行为）。
func runProbe(t *testing.T, plan *EvaluablePlan, N int, label string, useInitBounds bool, seed int64) probeStats {
	t.Helper()
	ev := NewSigmoidBTCEvolvable()
	rng := rand.New(rand.NewSource(seed))

	stats := probeStats{
		scores: make([]float64, 0, N),
		maxDDs: make([]float64, 0, N),
		totalCnt: N,
	}

	for i := 0; i < N; i++ {
		var gene Gene
		if useInitBounds {
			gene = ev.Sample(rng)
		} else {
			gene = sampleFromHardBounds(rng)
		}
		res := ev.Evaluate(plan, gene)
		stats.maxDDs = append(stats.maxDDs, res.MaxDrawdown)
		if res.Fatal {
			stats.fatalCnt++
			continue
		}
		stats.scores = append(stats.scores, res.ScoreTotal)
	}
	stats.fatalPct = 100.0 * float64(stats.fatalCnt) / float64(N)

	t.Logf("=== %s (N=%d) ===", label, N)
	t.Logf("  fatal:           %d/%d = %.1f%%", stats.fatalCnt, N, stats.fatalPct)
	t.Logf("  non-fatal score: %s", summarizeStats(stats.scores))
	t.Logf("  MaxDD all:       %s", summarizeStats(stats.maxDDs))
	return stats
}

// sampleFromHardBounds 模拟旧版采样行为（直接用 HardBounds 的 Min/Max 全宽随机）。
// 用于探测对照组。
func sampleFromHardBounds(rng *rand.Rand) Gene {
	b := quant.HardBounds
	c := quant.Chromosome{
		Beta:                uniform(rng, b.Beta.Min, b.Beta.Max),
		Gamma:               uniform(rng, b.Gamma.Min, b.Gamma.Max),
		SigmaFloor:          uniform(rng, b.SigmaFloor.Min, b.SigmaFloor.Max),
		BaseDays:            uniformInt(rng, int(b.BaseDays.Min), int(b.BaseDays.Max)),
		Multiplier:          uniform(rng, b.Multiplier.Min, b.Multiplier.Max),
		BetaThreshold:       uniform(rng, b.BetaThreshold.Min, b.BetaThreshold.Max),
		PriceDiscountBoost:  uniform(rng, b.PriceDiscountBoost.Min, b.PriceDiscountBoost.Max),
		DeadlineForcePct:    uniform(rng, b.DeadlineForcePct.Min, b.DeadlineForcePct.Max),
		MinAgeMonths:        uniformInt(rng, int(b.MinAgeMonths.Min), int(b.MinAgeMonths.Max)),
		SoftReleaseMaxRatio: uniform(rng, b.SoftReleaseMaxRatio.Min, b.SoftReleaseMaxRatio.Max),
		BullTimeDilation:    uniform(rng, b.BullTimeDilation.Min, b.BullTimeDilation.Max),
		BearTimeDilation:    uniform(rng, b.BearTimeDilation.Min, b.BearTimeDilation.Max),
		BullBetaMultiplier:  uniform(rng, b.BullBetaMultiplier.Min, b.BullBetaMultiplier.Max),
		BearBetaMultiplier:  uniform(rng, b.BearBetaMultiplier.Min, b.BearBetaMultiplier.Max),
		MicroReservePct:     uniform(rng, b.MicroReservePct.Min, b.MicroReservePct.Max),
	}
	return quant.ClampChromosome(c)
}

// jumpDiffusionBars 生成 GBM + 泊松跳跃合成日线，近似真实 BTC 的 fat tail。
//   S_{t+1} = S_t × exp(μ + σ ε_t + jump_t)
//   jump_t 以 jumpProb 概率发生，幅度为 jumpSize（如 -0.15 表示 -15% 暴跌）
func jumpDiffusionBars(n int, mu, sigma, jumpSize, jumpProb float64, seed int64) []quant.Bar {
	r := rand.New(rand.NewSource(seed))
	start := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	day := int64(24 * 60 * 60 * 1000)
	bars := make([]quant.Bar, n)
	price := 10000.0
	for i := 0; i < n; i++ {
		shock := r.NormFloat64() * sigma
		jump := 0.0
		if r.Float64() < jumpProb {
			jump = jumpSize
		}
		price *= math.Exp(mu + shock + jump)
		bars[i] = quant.Bar{
			OpenTime:  start + int64(i)*day,
			CloseTime: start + int64(i+1)*day - 1,
			Open:      price,
			High:      price,
			Low:       price,
			Close:     price,
			Volume:    1000,
		}
	}
	return bars
}

func summarizeStats(xs []float64) string {
	if len(xs) == 0 {
		return "(empty)"
	}
	mn, mx := xs[0], xs[0]
	sum := 0.0
	for _, x := range xs {
		if x < mn {
			mn = x
		}
		if x > mx {
			mx = x
		}
		sum += x
	}
	avg := sum / float64(len(xs))
	return fmt.Sprintf("n=%d min=%.4f avg=%.4f max=%.4f", len(xs), mn, avg, mx)
}
