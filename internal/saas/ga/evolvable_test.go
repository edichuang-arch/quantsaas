package ga

import (
	"math/rand"
	"testing"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRNG() *rand.Rand { return rand.New(rand.NewSource(42)) }

// Sample 结果必须落在 HardBounds 范围内。
func TestSample_WithinBounds(t *testing.T) {
	ev := NewSigmoidBTCEvolvable()
	rng := newRNG()
	for i := 0; i < 100; i++ {
		g := ev.Sample(rng)
		c, ok := g.(quant.Chromosome)
		require.True(t, ok)
		assert.GreaterOrEqual(t, c.Beta, quant.HardBounds.Beta.Min)
		assert.LessOrEqual(t, c.Beta, quant.HardBounds.Beta.Max)
		assert.GreaterOrEqual(t, c.BaseDays, int(quant.HardBounds.BaseDays.Min))
		assert.LessOrEqual(t, c.BaseDays, int(quant.HardBounds.BaseDays.Max))
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
