package quant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDailyBarsCrucible(start time.Time, n int, pricesFn func(i int) float64) []Bar {
	bars := make([]Bar, n)
	for i := 0; i < n; i++ {
		t := start.AddDate(0, 0, i)
		bars[i] = Bar{
			OpenTime:  t.UnixMilli(),
			CloseTime: t.Add(24*time.Hour - time.Millisecond).UnixMilli(),
			Close:     pricesFn(i),
		}
	}
	return bars
}

func TestBuildCrucibleWindows_EmptyBars(t *testing.T) {
	assert.Nil(t, BuildCrucibleWindows(nil, 1200))
	assert.Nil(t, BuildCrucibleWindows([]Bar{}, 1200))
}

// 当数据很短（只够 6m + warmup）时，只应返回 6m 和 full。
func TestBuildCrucibleWindows_ShortData(t *testing.T) {
	// 总共 1200 + 183 + 10 天 = 1393 天
	total := 1200 + 183 + 10
	bars := makeDailyBarsCrucible(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), total,
		func(i int) float64 { return 100 })

	wins := BuildCrucibleWindows(bars, 1200)
	// 只有 6m 和 full 能构成
	labels := map[string]bool{}
	for _, w := range wins {
		labels[w.Label] = true
	}
	assert.True(t, labels["6m"])
	assert.True(t, labels["full"])
	assert.False(t, labels["5y"], "5y should be skipped due to insufficient data")
}

// 充足数据下四窗全出，且按 bar 数量升序（6m → 2y → 5y → full）。
func TestBuildCrucibleWindows_FullData(t *testing.T) {
	// 1200 warmup + 1825 (5y eval)，再多给 100 天保持缓冲 = 3125 天
	total := 1200 + 1825 + 100
	bars := makeDailyBarsCrucible(time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC), total,
		func(i int) float64 { return 100 })

	wins := BuildCrucibleWindows(bars, 1200)
	require.Len(t, wins, 4)
	expectedOrder := []string{"6m", "2y", "5y", "full"}
	for i, w := range wins {
		assert.Equal(t, expectedOrder[i], w.Label)
	}
	// 权重必须与 WindowWeights 匹配
	for _, w := range wins {
		assert.Equal(t, WindowWeights[w.Label], w.Weight)
	}
	// 各窗口总和为 1.0
	var sum float64
	for _, w := range wins {
		sum += w.Weight
	}
	assert.InDelta(t, 1.0, sum, 1e-9)
}

// EvalBars 必须只含 OpenTime >= EvalStartMs 的 bar（warmup 被切掉）。
func TestCrucibleWindow_EvalBarsExcludesWarmup(t *testing.T) {
	total := 1200 + 183 + 20
	bars := makeDailyBarsCrucible(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), total,
		func(i int) float64 { return 100 })

	wins := BuildCrucibleWindows(bars, 1200)
	var sixm *CrucibleWindow
	for i := range wins {
		if wins[i].Label == "6m" {
			sixm = &wins[i]
		}
	}
	require.NotNil(t, sixm)

	evalBars := sixm.EvalBars()
	for _, b := range evalBars {
		assert.GreaterOrEqual(t, b.OpenTime, sixm.EvalStartMs)
	}
	// EvalBars 的第一个 bar 应 >= EvalStartMs
	assert.GreaterOrEqual(t, evalBars[0].OpenTime, sixm.EvalStartMs)
}
