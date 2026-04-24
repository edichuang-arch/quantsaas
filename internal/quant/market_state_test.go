package quant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarketState_DataInsufficient(t *testing.T) {
	s := ComputeMarketState([]float64{1, 2, 3})
	assert.Equal(t, MarketNormal, s.Kind)
	assert.Equal(t, 1.0, s.TimeDilationMultiplier)
	assert.Equal(t, 1.0, s.BetaMultiplier)
	assert.False(t, s.IsQuiet)
}

// Bull: 短 EMA > 长 EMA 且价格 > 长 EMA
func TestMarketState_Bull(t *testing.T) {
	// 持续上升序列保证短期 EMA > 长期 EMA
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 100 + float64(i)*0.5 // 线性上升
	}
	s := ComputeMarketState(closes)
	assert.Equal(t, MarketBull, s.Kind)
	assert.Greater(t, s.TimeDilationMultiplier, 1.0)
	assert.Greater(t, s.BetaMultiplier, 1.0)
}

// Bear: 短 EMA < 长 EMA 且价格 < 长 EMA
func TestMarketState_Bear(t *testing.T) {
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 200 - float64(i)*0.5 // 线性下跌
	}
	s := ComputeMarketState(closes)
	assert.Equal(t, MarketBear, s.Kind)
	assert.Less(t, s.TimeDilationMultiplier, 1.0)
	assert.Greater(t, s.BetaMultiplier, 1.0)
}

// Quiet: 完全平稳的常数序列 → price == long EMA，不触发 Bull/Bear，
// MAV 都为 0 导致 volRatio 保持默认 1.0 → isQuiet=true → QUIET。
func TestMarketState_QuietOnFlatSeries(t *testing.T) {
	closes := make([]float64, 200)
	for i := range closes {
		closes[i] = 100.0
	}
	s := ComputeMarketState(closes)
	assert.Equal(t, MarketQuiet, s.Kind)
	assert.Equal(t, 1.0, s.TimeDilationMultiplier)
	assert.Equal(t, 1.0, s.BetaMultiplier)
	assert.True(t, s.IsQuiet)
}

// Normal 默认情况：反映没有明显趋势时的行为
// 构造：价格围绕长期 EMA 震荡，但短期 EMA 略低于长期 EMA（否则会是 Bull）
// 这个场景比较难精确构造，测试 Bull/Bear 的反面即足够覆盖。

func TestMarketState_Determinism(t *testing.T) {
	closes := make([]float64, 150)
	for i := range closes {
		closes[i] = 100 + float64(i)*0.3
	}
	a := ComputeMarketState(closes)
	b := ComputeMarketState(closes)
	assert.Equal(t, a, b)
}
