package quant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultSeedChromosome_IsValid(t *testing.T) {
	require.NoError(t, ValidateChromosome(DefaultSeedChromosome))
	// 默认种子 Clamp 后不应改变任何字段（必须已经在边界内）
	clamped := ClampChromosome(DefaultSeedChromosome)
	assert.Equal(t, DefaultSeedChromosome, clamped)
}

func TestClampChromosome_BelowMin(t *testing.T) {
	c := Chromosome{
		Beta:                -10, // min 0.1
		Gamma:               -1,  // min 0
		SigmaFloor:          -1,
		BaseDays:            0,   // min 1
		Multiplier:          -1,  // min 0.2
		BetaThreshold:       -1,
		PriceDiscountBoost:  -1,
		DeadlineForcePct:    -1,
		MinAgeMonths:        0,
		SoftReleaseMaxRatio: -1,
		BullTimeDilation:    -1,
		BearTimeDilation:    -1,
		BullBetaMultiplier:  -1,
		BearBetaMultiplier:  -1,
		MicroReservePct:     -1,
	}
	got := ClampChromosome(c)
	assert.Equal(t, HardBounds.Beta.Min, got.Beta)
	assert.Equal(t, HardBounds.Gamma.Min, got.Gamma)
	assert.Equal(t, int(HardBounds.BaseDays.Min), got.BaseDays)
	assert.Equal(t, HardBounds.Multiplier.Min, got.Multiplier)
	assert.Equal(t, HardBounds.MicroReservePct.Min, got.MicroReservePct)
}

func TestClampChromosome_AboveMax(t *testing.T) {
	c := Chromosome{
		Beta:                100,
		Gamma:               100,
		SigmaFloor:          9999,
		BaseDays:             999,
		Multiplier:          999,
		BetaThreshold:       999,
		PriceDiscountBoost:  999,
		DeadlineForcePct:    999,
		MinAgeMonths:        999,
		SoftReleaseMaxRatio: 999,
		BullTimeDilation:    999,
		BearTimeDilation:    999,
		BullBetaMultiplier:  999,
		BearBetaMultiplier:  999,
		MicroReservePct:     999,
	}
	got := ClampChromosome(c)
	assert.Equal(t, HardBounds.Beta.Max, got.Beta)
	assert.Equal(t, HardBounds.Gamma.Max, got.Gamma)
	assert.Equal(t, int(HardBounds.BaseDays.Max), got.BaseDays)
	assert.Equal(t, HardBounds.MicroReservePct.Max, got.MicroReservePct)
}

// ParamPack JSON 序列化 → 反序列化往返必须完全一致。
func TestParamPack_Roundtrip(t *testing.T) {
	original := DefaultSeedChromosome
	original.Beta = 2.75
	original.BaseDays = 14
	original.MicroReservePct = 0.33

	blob, err := EncodeParamPack(original, DefaultSpawnPoint)
	require.NoError(t, err)

	gotC, gotSP := DecodeParamPack(blob)
	assert.Equal(t, original, gotC)
	assert.Equal(t, DefaultSpawnPoint, gotSP)
}

// nil / 空 blob 必须回退到 DefaultSeed。
func TestDecodeParamPack_NilFallsBackToSeed(t *testing.T) {
	c, sp := DecodeParamPack(nil)
	assert.Equal(t, DefaultSeedChromosome, c)
	assert.Equal(t, DefaultSpawnPoint, sp)

	c2, sp2 := DecodeParamPack([]byte{})
	assert.Equal(t, DefaultSeedChromosome, c2)
	assert.Equal(t, DefaultSpawnPoint, sp2)
}

// 损坏的 JSON 必须回退到 DefaultSeed（实盘不能因为 DB 脏数据挂掉）。
func TestDecodeParamPack_CorruptFallsBackToSeed(t *testing.T) {
	c, _ := DecodeParamPack([]byte("{not valid json"))
	assert.Equal(t, DefaultSeedChromosome, c)
}

// 部分字段缺失时 Clamp 会把零值纠正到合法下限（证明 Decode 使用 ClampChromosome）。
func TestDecodeParamPack_PartialFieldsClamped(t *testing.T) {
	// 只带一个字段，其他全零
	partial := []byte(`{"sigmoid_btc_config":{"beta":2.5},"spawn_point":{"risk":{"max_leverage":1}}}`)
	c, _ := DecodeParamPack(partial)
	assert.Equal(t, 2.5, c.Beta)
	// BaseDays 原本是 0（非法），Clamp 后应为下限 1
	assert.GreaterOrEqual(t, c.BaseDays, int(HardBounds.BaseDays.Min))
}
