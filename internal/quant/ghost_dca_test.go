package quant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeDailyBars 生成 n 天的日线 bars，价格序列按 pricesFn(i) 给定。
func makeDailyBars(start time.Time, n int, pricesFn func(i int) float64) []Bar {
	bars := make([]Bar, n)
	for i := 0; i < n; i++ {
		t := start.AddDate(0, 0, i)
		bars[i] = Bar{
			OpenTime:  t.UnixMilli(),
			CloseTime: t.Add(24*time.Hour - time.Millisecond).UnixMilli(),
			Open:      pricesFn(i),
			High:      pricesFn(i),
			Low:       pricesFn(i),
			Close:     pricesFn(i),
			Volume:    1,
		}
	}
	return bars
}

// 空 bars 返回零值。
func TestGhostDCA_EmptyBars(t *testing.T) {
	r := SimulateGhostDCA(nil, GhostDCAConfig{InitialCapitalUSDT: 1000})
	assert.Equal(t, 0.0, r.FinalEquity)
}

// 价格恒定：只做第一天 allin 买入 + 每月注资，终值应等于 (InitialCapital + Σ注资) × 1.0。
func TestGhostDCA_ConstantPriceNoReturn(t *testing.T) {
	// 2024-01-01 起 90 天，价格恒 100
	bars := makeDailyBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 90,
		func(i int) float64 { return 100 })

	r := SimulateGhostDCA(bars, GhostDCAConfig{
		InitialCapitalUSDT: 1000,
		MonthlyInjectUSDT:  300,
	})
	// 90 天覆盖 2/1 与 3/1 两次月初注资，共 +600 USDT
	assert.InDelta(t, 1000+600, r.FinalEquity, 1e-6)
	assert.InDelta(t, 600, r.TotalInjected, 1e-6)
	// 恒价不应产生回撤
	assert.InDelta(t, 0, r.MaxDrawdown, 1e-6)
	// Modified Dietz：期末 = 期初 + 注资 → 超额部分 0，ROI ≈ 0
	assert.InDelta(t, 0, r.ROI, 1e-6)
}

// 价格上涨 → 终权益明显大于注入本金，ROI > 0。
func TestGhostDCA_UptrendProducesPositiveROI(t *testing.T) {
	// 价格线性上升 100 → 200，跨 60 天
	bars := makeDailyBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 60,
		func(i int) float64 { return 100 + float64(i) })

	r := SimulateGhostDCA(bars, GhostDCAConfig{
		InitialCapitalUSDT: 1000,
		MonthlyInjectUSDT:  300,
	})
	assert.Greater(t, r.FinalEquity, 1300.0)
	assert.Greater(t, r.ROI, 0.0)
}

// 价格下跌 → 终权益小于注入本金，ROI < 0。
func TestGhostDCA_DowntrendProducesNegativeROI(t *testing.T) {
	bars := makeDailyBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 60,
		func(i int) float64 { return 200 - float64(i) })

	r := SimulateGhostDCA(bars, GhostDCAConfig{
		InitialCapitalUSDT: 1000,
		MonthlyInjectUSDT:  300,
	})
	assert.Less(t, r.FinalEquity, 1300.0)
	assert.Less(t, r.ROI, 0.0)
	assert.Greater(t, r.MaxDrawdown, 0.0)
}

// NAV 序列长度必须等于 bars 长度。
func TestGhostDCA_NAVSeriesLengthMatches(t *testing.T) {
	bars := makeDailyBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 30,
		func(i int) float64 { return 100 })
	r := SimulateGhostDCA(bars, GhostDCAConfig{InitialCapitalUSDT: 100, MonthlyInjectUSDT: 0})
	require.Len(t, r.NAVSeries, len(bars))
}

// MaxDrawdown 单元测试
func TestMaxDrawdown_NoDrawdownForMonotonic(t *testing.T) {
	nav := []float64{100, 110, 120, 130, 140}
	assert.InDelta(t, 0, MaxDrawdown(nav), 1e-9)
}

func TestMaxDrawdown_KnownPeak(t *testing.T) {
	// 峰值 200，谷底 100 → DD = 50%
	nav := []float64{100, 150, 200, 100, 120}
	assert.InDelta(t, 0.5, MaxDrawdown(nav), 1e-9)
}

func TestMaxDrawdown_Empty(t *testing.T) {
	assert.Equal(t, 0.0, MaxDrawdown(nil))
}

// Modified Dietz 通用函数测试
func TestModifiedDietzROI_NoFlows(t *testing.T) {
	// 100 → 110，无现金流 → 10%
	assert.InDelta(t, 0.1, ModifiedDietzROI(100, 110, nil, nil, 30), 1e-9)
}

func TestModifiedDietzROI_WithFlow(t *testing.T) {
	// 期初 100，期末 220，总天数 30，第 0 天注资 100
	// numerator = 220 - 100 - 100 = 20
	// weight = (30 - 0) / 30 = 1.0
	// denominator = 100 + 100 × 1 = 200
	// ROI = 20 / 200 = 0.1
	r := ModifiedDietzROI(100, 220, []float64{100}, []int{0}, 30)
	assert.InDelta(t, 0.1, r, 1e-9)
}

func TestModifiedDietzROI_ZeroTotalDays(t *testing.T) {
	assert.Equal(t, 0.0, ModifiedDietzROI(100, 110, nil, nil, 0))
}
