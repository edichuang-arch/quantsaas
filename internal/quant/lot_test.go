package quant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func buildLots(now time.Time) []Lot {
	return []Lot{
		{LotType: LotDeadStack, Amount: 1.0, CostPrice: 30000, CreatedAt: now.AddDate(0, -12, 0)}, // 12个月前
		{LotType: LotDeadStack, Amount: 0.5, CostPrice: 40000, CreatedAt: now.AddDate(0, -2, 0)},  // 2个月前
		{LotType: LotDeadStack, Amount: 2.0, CostPrice: 50000, IsColdSealed: true, CreatedAt: now}, // 冷封存
		{LotType: LotFloating, Amount: 0.3, CostPrice: 35000, CreatedAt: now},
	}
}

func TestDeadStackAmount_ExcludesColdSealed(t *testing.T) {
	now := time.Now()
	lots := buildLots(now)
	// 活跃 DeadStack = 1.0 + 0.5 = 1.5；冷封存 2.0 不计
	assert.InDelta(t, 1.5, DeadStackAmount(lots), 1e-9)
}

func TestFloatStackAmount(t *testing.T) {
	now := time.Now()
	lots := buildLots(now)
	assert.InDelta(t, 0.3, FloatStackAmount(lots), 1e-9)
}

func TestColdSealedAmount(t *testing.T) {
	now := time.Now()
	lots := buildLots(now)
	assert.InDelta(t, 2.0, ColdSealedAmount(lots), 1e-9)
}

func TestSoftReleaseAmount_RespectsMinAge(t *testing.T) {
	now := time.Now()
	lots := buildLots(now)
	cfg := SoftReleaseConfig{
		MinAgeMonths: 6,
		MaxRatio:     0.5,
		NowMs:        now.UnixMilli(),
	}
	// 只有 12 个月前的 1.0 BTC 符合老化条件；上限 = 1.5 × 0.5 = 0.75
	// 0.75 < 1.0 → 释放 0.75
	assert.InDelta(t, 0.75, SoftReleaseAmount(lots, cfg), 1e-9)
}

func TestSoftReleaseAmount_ZeroMaxRatio(t *testing.T) {
	now := time.Now()
	cfg := SoftReleaseConfig{MinAgeMonths: 6, MaxRatio: 0, NowMs: now.UnixMilli()}
	assert.Equal(t, 0.0, SoftReleaseAmount(buildLots(now), cfg))
}

func TestSoftReleaseAmount_ColdSealedNeverReleased(t *testing.T) {
	now := time.Now()
	// 只有冷封存 lot
	lots := []Lot{
		{LotType: LotDeadStack, Amount: 5.0, IsColdSealed: true, CreatedAt: now.AddDate(-10, 0, 0)},
	}
	cfg := SoftReleaseConfig{MinAgeMonths: 1, MaxRatio: 1.0, NowMs: now.UnixMilli()}
	assert.Equal(t, 0.0, SoftReleaseAmount(lots, cfg))
}

func TestHardReleaseAmount_NeverExceedsAvailable(t *testing.T) {
	now := time.Now()
	lots := buildLots(now) // DeadStack 活跃 = 1.5
	// 要求 2.0，但只有 1.5 可用 → 返回 1.5
	assert.InDelta(t, 1.5, HardReleaseAmount(lots, 2.0), 1e-9)
}

func TestHardReleaseAmount_ReturnsGapWhenSufficient(t *testing.T) {
	now := time.Now()
	lots := buildLots(now)
	assert.InDelta(t, 0.8, HardReleaseAmount(lots, 0.8), 1e-9)
}

func TestHardReleaseAmount_ZeroGap(t *testing.T) {
	now := time.Now()
	assert.Equal(t, 0.0, HardReleaseAmount(buildLots(now), 0))
	assert.Equal(t, 0.0, HardReleaseAmount(buildLots(now), -5))
}
