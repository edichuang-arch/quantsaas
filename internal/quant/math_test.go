package quant

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEMA_InsufficientData(t *testing.T) {
	assert.True(t, math.IsNaN(EMA([]float64{1, 2, 3}, 5)))
	assert.True(t, math.IsNaN(EMA(nil, 3)))
	assert.True(t, math.IsNaN(EMA([]float64{1, 2, 3}, 0)))
}

func TestEMA_ConstantSeriesConvergesToValue(t *testing.T) {
	// 常数序列的 EMA 永远等于该常数
	series := make([]float64, 100)
	for i := range series {
		series[i] = 42.0
	}
	got := EMA(series, 10)
	assert.InDelta(t, 42.0, got, 1e-9)
}

func TestEMA_Determinism(t *testing.T) {
	series := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	first := EMA(series, 5)
	second := EMA(series, 5)
	assert.Equal(t, first, second)
}

func TestEMASeries_LengthMatchesInput(t *testing.T) {
	series := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	out := EMASeries(series, 3)
	require.Len(t, out, len(series))
	// 前 period-1 个位置 NaN
	for i := 0; i < 2; i++ {
		assert.True(t, math.IsNaN(out[i]), "idx %d should be NaN", i)
	}
	for i := 2; i < len(series); i++ {
		assert.False(t, math.IsNaN(out[i]))
	}
}

func TestStdDev_ZeroForConstant(t *testing.T) {
	series := []float64{5, 5, 5, 5, 5}
	assert.InDelta(t, 0, StdDev(series, 5), 1e-9)
}

func TestStdDev_KnownValue(t *testing.T) {
	// [1,2,3,4,5] 的样本标准差 = sqrt(2.5) ≈ 1.58114
	series := []float64{1, 2, 3, 4, 5}
	assert.InDelta(t, math.Sqrt(2.5), StdDev(series, 5), 1e-9)
}

func TestMAVAbsChange_KnownValue(t *testing.T) {
	// closes = [10, 11, 9, 12]，绝对差 = [1, 2, 3]，平均 = 2.0
	closes := []float64{10, 11, 9, 12}
	assert.InDelta(t, 2.0, MAVAbsChange(closes, 4), 1e-9)
}

func TestClipFloat64(t *testing.T) {
	assert.Equal(t, 5.0, ClipFloat64(5, 0, 10))
	assert.Equal(t, 0.0, ClipFloat64(-3, 0, 10))
	assert.Equal(t, 10.0, ClipFloat64(99, 0, 10))
	// 乱序边界容错
	assert.Equal(t, 10.0, ClipFloat64(99, 10, 0))
}

func TestRoundToUSDT(t *testing.T) {
	assert.Equal(t, 1.23, RoundToUSDT(1.234))
	assert.Equal(t, 1.24, RoundToUSDT(1.235)) // banker's rounding 不适用，标准四舍五入
}

func TestRoundToAssetQty(t *testing.T) {
	// floor 到 5 位小数（与 Binance 主流现货对 LOT_SIZE.stepSize=0.00001 对齐）
	assert.InDelta(t, 0.12345, RoundToAssetQty(0.1234567), 1e-9)
	assert.InDelta(t, 0.12345, RoundToAssetQty(0.123459), 1e-9, "must floor not round up")
	assert.InDelta(t, 0.0, RoundToAssetQty(0.000009), 1e-9, "below stepSize → 0")
}

func TestLogReturn(t *testing.T) {
	assert.InDelta(t, 0, LogReturn(100, 100), 1e-9)
	assert.Greater(t, LogReturn(100, 150), 0.0)
	assert.Less(t, LogReturn(100, 50), 0.0)
	// 价格 <= 0 返回 NaN
	assert.True(t, math.IsNaN(LogReturn(0, 100)))
	assert.True(t, math.IsNaN(LogReturn(100, 0)))
}

func TestSafeDiv(t *testing.T) {
	assert.Equal(t, 5.0, SafeDiv(10, 2, -1))
	assert.Equal(t, -1.0, SafeDiv(10, 0, -1))
}
