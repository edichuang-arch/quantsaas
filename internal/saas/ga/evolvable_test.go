package ga

import (
	"math/rand"
	"testing"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRNG() *rand.Rand { return rand.New(rand.NewSource(42)) }

// Sample 结果必须落在 InitBounds（窄区间）内 —— 比 HardBounds 更严的约束。
// 这是 gen-0 不再全员 fatal 的关键：随机采样只在“合理区”内。
func TestSample_WithinInitBounds(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	b := quant.HardBounds
	for i := 0; i < 200; i++ {
		g := ev.Sample(rng)
		c, ok := g.(quant.Chromosome)
		require.True(t, ok)
		// 每一维都必须落在 InitBounds 内
		assert.GreaterOrEqual(t, c.Beta, b.Beta.InitMin)
		assert.LessOrEqual(t, c.Beta, b.Beta.InitMax)
		assert.GreaterOrEqual(t, c.Gamma, b.Gamma.InitMin)
		assert.LessOrEqual(t, c.Gamma, b.Gamma.InitMax)
		assert.GreaterOrEqual(t, c.SigmaFloor, b.SigmaFloor.InitMin)
		assert.LessOrEqual(t, c.SigmaFloor, b.SigmaFloor.InitMax)
		assert.GreaterOrEqual(t, c.BaseDays, int(b.BaseDays.InitMin))
		assert.LessOrEqual(t, c.BaseDays, int(b.BaseDays.InitMax))
		assert.GreaterOrEqual(t, c.Multiplier, b.Multiplier.InitMin)
		assert.LessOrEqual(t, c.Multiplier, b.Multiplier.InitMax)
		assert.GreaterOrEqual(t, c.BetaThreshold, b.BetaThreshold.InitMin)
		assert.LessOrEqual(t, c.BetaThreshold, b.BetaThreshold.InitMax)
		assert.GreaterOrEqual(t, c.PriceDiscountBoost, b.PriceDiscountBoost.InitMin)
		assert.LessOrEqual(t, c.PriceDiscountBoost, b.PriceDiscountBoost.InitMax)
		assert.GreaterOrEqual(t, c.DeadlineForcePct, b.DeadlineForcePct.InitMin)
		assert.LessOrEqual(t, c.DeadlineForcePct, b.DeadlineForcePct.InitMax)
		assert.GreaterOrEqual(t, c.MinAgeMonths, int(b.MinAgeMonths.InitMin))
		assert.LessOrEqual(t, c.MinAgeMonths, int(b.MinAgeMonths.InitMax))
		assert.GreaterOrEqual(t, c.SoftReleaseMaxRatio, b.SoftReleaseMaxRatio.InitMin)
		assert.LessOrEqual(t, c.SoftReleaseMaxRatio, b.SoftReleaseMaxRatio.InitMax)
		assert.GreaterOrEqual(t, c.BullTimeDilation, b.BullTimeDilation.InitMin)
		assert.LessOrEqual(t, c.BullTimeDilation, b.BullTimeDilation.InitMax)
		assert.GreaterOrEqual(t, c.BearTimeDilation, b.BearTimeDilation.InitMin)
		assert.LessOrEqual(t, c.BearTimeDilation, b.BearTimeDilation.InitMax)
		assert.GreaterOrEqual(t, c.BullBetaMultiplier, b.BullBetaMultiplier.InitMin)
		assert.LessOrEqual(t, c.BullBetaMultiplier, b.BullBetaMultiplier.InitMax)
		assert.GreaterOrEqual(t, c.BearBetaMultiplier, b.BearBetaMultiplier.InitMin)
		assert.LessOrEqual(t, c.BearBetaMultiplier, b.BearBetaMultiplier.InitMax)
		assert.GreaterOrEqual(t, c.MicroReservePct, b.MicroReservePct.InitMin)
		assert.LessOrEqual(t, c.MicroReservePct, b.MicroReservePct.InitMax)
	}
}

// TestInitBounds_SubsetOfHardBounds 防呆：所有 InitBounds 必须是 HardBounds 子集。
// 任何人改 HardBounds 时都会被这条测试拦下来。
func TestInitBounds_SubsetOfHardBounds(t *testing.T) {
	b := quant.HardBounds
	cases := []struct {
		name  string
		bound quant.Bound
	}{
		{"Beta", b.Beta},
		{"Gamma", b.Gamma},
		{"SigmaFloor", b.SigmaFloor},
		{"BaseDays", b.BaseDays},
		{"Multiplier", b.Multiplier},
		{"BetaThreshold", b.BetaThreshold},
		{"PriceDiscountBoost", b.PriceDiscountBoost},
		{"DeadlineForcePct", b.DeadlineForcePct},
		{"MinAgeMonths", b.MinAgeMonths},
		{"SoftReleaseMaxRatio", b.SoftReleaseMaxRatio},
		{"BullTimeDilation", b.BullTimeDilation},
		{"BearTimeDilation", b.BearTimeDilation},
		{"BullBetaMultiplier", b.BullBetaMultiplier},
		{"BearBetaMultiplier", b.BearBetaMultiplier},
		{"MicroReservePct", b.MicroReservePct},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.GreaterOrEqual(t, c.bound.InitMin, c.bound.Min,
				"%s.InitMin (%v) must >= Min (%v)", c.name, c.bound.InitMin, c.bound.Min)
			assert.LessOrEqual(t, c.bound.InitMax, c.bound.Max,
				"%s.InitMax (%v) must <= Max (%v)", c.name, c.bound.InitMax, c.bound.Max)
			assert.LessOrEqual(t, c.bound.InitMin, c.bound.InitMax,
				"%s.InitMin (%v) must <= InitMax (%v)", c.name, c.bound.InitMin, c.bound.InitMax)
		})
	}
}

// Mutate 后的结果仍必须夹紧到 HardBounds（不是 InitBounds —— mutate 可以突破窄区）。
func TestMutate_RespectsHardBounds(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	b := quant.HardBounds
	original := quant.DefaultSeedChromosome
	// 用极端 prob/scale 让所有维度都被大幅扰动
	for i := 0; i < 200; i++ {
		g := ev.Mutate(original, 1.0, 5.0, rng)
		c := g.(quant.Chromosome)
		assert.GreaterOrEqual(t, c.Beta, b.Beta.Min)
		assert.LessOrEqual(t, c.Beta, b.Beta.Max)
		assert.GreaterOrEqual(t, c.SigmaFloor, b.SigmaFloor.Min)
		assert.LessOrEqual(t, c.SigmaFloor, b.SigmaFloor.Max)
		assert.GreaterOrEqual(t, c.MicroReservePct, b.MicroReservePct.Min)
		assert.LessOrEqual(t, c.MicroReservePct, b.MicroReservePct.Max)
	}
}

// Mutate 在 prob=0 时必须保持基因不变。
func TestMutate_ZeroProbNoChange(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	original := quant.DefaultSeedChromosome
	mutated := ev.Mutate(original, 0, 1.0, rng)
	assert.Equal(t, original, mutated)
}

// Mutate 在 prob=1 时所有字段都会被扰动（跟原始不同）。
func TestMutate_HighProbAltersFields(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	original := quant.DefaultSeedChromosome
	mutated := ev.Mutate(original, 1.0, 1.0, rng)
	// 至少有一个字段不同
	assert.NotEqual(t, original, mutated)
}

// Crossover 的产出必须由两个父代的基因组合而成（每维度取自其中一个）。
func TestCrossover_ComponentsFromParents(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	p1 := quant.Chromosome{Beta: 1.0, Gamma: 2.0, Multiplier: 0.5, BaseDays: 5}
	p1 = quant.ClampChromosome(p1)
	p2 := quant.Chromosome{Beta: 4.0, Gamma: 0.5, Multiplier: 2.5, BaseDays: 25}
	p2 = quant.ClampChromosome(p2)
	for i := 0; i < 20; i++ {
		child := ev.Crossover(p1, p2, rng)
		c := child.(quant.Chromosome)
		assert.Contains(t, []float64{p1.Beta, p2.Beta}, c.Beta)
		assert.Contains(t, []float64{p1.Gamma, p2.Gamma}, c.Gamma)
	}
}

// Fingerprint 相同染色体 → 相同指纹（稳定性）。
func TestFingerprint_Stable(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	c := quant.DefaultSeedChromosome
	fp1 := ev.Fingerprint(c)
	fp2 := ev.Fingerprint(c)
	assert.Equal(t, fp1, fp2)
}

// Fingerprint 不同染色体 → 不同指纹。
func TestFingerprint_DiffersForDifferentGenes(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	a := quant.DefaultSeedChromosome
	b := a
	b.Beta = a.Beta + 1.5
	assert.NotEqual(t, ev.Fingerprint(a), ev.Fingerprint(b))
}

// Fingerprint 在 1e-6 精度内相同（量化后相等）。
func TestFingerprint_QuantizedAt1e6(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	a := quant.DefaultSeedChromosome
	b := a
	b.Beta = a.Beta + 1e-9 // 小于 1e-6 → 量化后应被视为相同
	assert.Equal(t, ev.Fingerprint(a), ev.Fingerprint(b))
}

// DecodeElite 对 nil 返回默认种子。
func TestDecodeElite_NilFallsBackToSeed(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	g := ev.DecodeElite(nil)
	assert.Equal(t, quant.DefaultSeedChromosome, g)
}

// EncodeResult + DecodeElite 往返必须一致。
func TestEncodeResult_Roundtrip(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	original := quant.DefaultSeedChromosome
	original.Beta = 2.5
	sp := quant.DefaultSpawnPoint
	blob, err := ev.EncodeResult(original, &sp)
	require.NoError(t, err)
	restored := ev.DecodeElite(blob)
	assert.Equal(t, original, restored)
}

// StrategyID 必须匹配策略包常量。
func TestStrategyID(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	assert.Equal(t, "sigmoid-btc", ev.StrategyID())
}
