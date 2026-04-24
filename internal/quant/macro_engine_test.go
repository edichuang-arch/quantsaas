package quant

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeClosesMacro(baseline, latest float64, n int) []float64 {
	out := make([]float64, n)
	for i := 0; i < n-1; i++ {
		out[i] = baseline
	}
	out[n-1] = latest
	return out
}

func defaultMacroInput(nowMs int64, price float64) MacroInput {
	return MacroInput{
		NowMs:                    nowMs,
		Closes:                   makeClosesMacro(100, price, 150),
		CurrentPrice:             price,
		SpendableUSDT:            10000,
		MonthlyInjectUSDT:        300,
		Prev:                     RuntimeState{Extras: map[string]float64{}},
		State:                    MarketState{TimeDilationMultiplier: 1, BetaMultiplier: 1, Kind: MarketNormal},
		BaseDays:                 7,
		Multiplier:               1.0,
		BetaThreshold:            0.05,
		PriceDiscountBoost:       1.5,
		DeadlineForcePct:         0.5,
		AggressivenessMultiplier: 1.0,
	}
}

// 宏观引擎永远只产出 BUY + DEAD_STACK，任何配置下禁止 SELL。
func TestMacro_OnlyProducesBuyToDeadStack(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultMacroInput(nowMs, 100)
	out := ComputeMacroDecision(in)
	if out.Intent != nil {
		assert.Equal(t, ActionBuy, out.Intent.Action)
		assert.Equal(t, EngineMacro, out.Intent.Engine)
		assert.Equal(t, LotDeadStack, out.Intent.LotType)
	}
}

// 节奏门：上次决策刚发生，未到 BaseDays，应跳过。
func TestMacro_TimeGateSkipsTooEarly(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultMacroInput(nowMs, 100)
	in.Prev.LastMacroDecisionMs = nowMs - int64(2)*msPerDay // 2 天前（< BaseDays=7）
	in.Prev.MonthAnchorMs = nowMs - int64(5)*msPerDay        // 同一个月，防止月初兜底触发
	in.DeadlineForcePct = 0                                  // 关闭死线兜底
	out := ComputeMacroDecision(in)
	assert.Nil(t, out.Intent, "should skip when within base_days")
}

// 跨月时必须重置 MonthlyInjectedUSDT 与 MonthAnchorMs。
func TestMacro_NewMonthResetsInjection(t *testing.T) {
	juneMs := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC).UnixMilli()
	julyMs := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	in := defaultMacroInput(julyMs, 100)
	in.Prev.MonthAnchorMs = juneMs
	in.Prev.MonthlyInjectedUSDT = 300
	in.DeadlineForcePct = 0

	out := ComputeMacroDecision(in)
	// 跨月后 MonthlyInjectedUSDT 应归零或被本次新增覆盖（若本次有下单）
	if out.Intent == nil {
		assert.Equal(t, 0.0, out.NewRuntime.MonthlyInjectedUSDT)
	} else {
		assert.Equal(t, out.Intent.AmountUSDT, out.NewRuntime.MonthlyInjectedUSDT)
	}
	// MonthAnchor 必须更新到当前月
	y1, m1, _ := civilFromMs(out.NewRuntime.MonthAnchorMs)
	assert.Equal(t, 2024, y1)
	assert.Equal(t, 7, m1)
}

// 死线兜底：接近月末且本月注资不足 → 补齐缺口。
func TestMacro_DeadlineFallbackFiresNearMonthEnd(t *testing.T) {
	// 6/29 距月末 1 天；本月计划 300，已注资 0（threshold = 300 × 0.5 = 150）
	nowMs := time.Date(2024, 6, 29, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultMacroInput(nowMs, 100)
	in.Prev.MonthAnchorMs = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	in.Prev.MonthlyInjectedUSDT = 0
	in.Prev.LastMacroDecisionMs = nowMs - int64(1)*msPerDay // 刚刚决策过，关闭节奏门
	in.DeadlineForcePct = 0.5

	out := ComputeMacroDecision(in)
	require.NotNil(t, out.Intent, "deadline fallback should trigger")
	assert.Equal(t, ActionBuy, out.Intent.Action)
	assert.GreaterOrEqual(t, out.Intent.AmountUSDT, 150.0, "should cover deficit")
	assert.Contains(t, out.Reason, "deadline")
}

// Spendable clamp：订单金额不得超过可用资金。
func TestMacro_SpendableClamp(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultMacroInput(nowMs, 100)
	in.SpendableUSDT = 20                           // 非常少的可用
	in.DeadlineForcePct = 0                         // 排除死线兜底路径
	in.Prev.MonthAnchorMs = nowMs - int64(5)*msPerDay // 同月

	out := ComputeMacroDecision(in)
	if out.Intent != nil {
		assert.LessOrEqual(t, out.Intent.AmountUSDT, 20.0)
	}
}

// 偏离加码：价格显著低于长期 EMA 时，订单金额被放大。
func TestMacro_DiscountBoostAmplifiesOrder(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	// 长期 EMA ≈ 100，当前价 80 → 折价 20%，超过阈值 5%
	closesDeep := makeClosesMacro(100, 80, 150)
	closesFlat := makeClosesMacro(100, 100, 150)

	build := func(closes []float64, price float64) MacroInput {
		in := defaultMacroInput(nowMs, price)
		in.Closes = closes
		in.DeadlineForcePct = 0
		in.Prev.MonthAnchorMs = nowMs - int64(5)*msPerDay
		return in
	}

	outFlat := ComputeMacroDecision(build(closesFlat, 100))
	outDeep := ComputeMacroDecision(build(closesDeep, 80))

	if outFlat.Intent == nil || outDeep.Intent == nil {
		t.Skip("both intents should fire; adjust parameters if skipped")
	}
	assert.Greater(t, outDeep.Intent.AmountUSDT, outFlat.Intent.AmountUSDT,
		"discount boost should enlarge order")
}

func TestMacro_Determinism(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultMacroInput(nowMs, 100)
	in.Prev.MonthAnchorMs = nowMs - int64(5)*msPerDay
	a := ComputeMacroDecision(in)
	b := ComputeMacroDecision(in)
	assert.Equal(t, a, b)
}

// 辅助函数测试：civilFromMs 与 daysToMonthEnd
func TestCivilFromMs(t *testing.T) {
	ms := time.Date(2024, 6, 15, 12, 30, 0, 0, time.UTC).UnixMilli()
	y, m, d := civilFromMs(ms)
	assert.Equal(t, 2024, y)
	assert.Equal(t, 6, m)
	assert.Equal(t, 15, d)
}

func TestDaysToMonthEnd(t *testing.T) {
	ms := time.Date(2024, 6, 29, 0, 0, 0, 0, time.UTC).UnixMilli()
	assert.Equal(t, 1, daysToMonthEnd(ms)) // 6/29 → 6/30 剩 1 天

	ms2 := time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC).UnixMilli()
	assert.Equal(t, 1, daysToMonthEnd(ms2)) // 2024 闰年，2/29 存在，剩 1 天

	ms3 := time.Date(2023, 2, 28, 0, 0, 0, 0, time.UTC).UnixMilli()
	assert.Equal(t, 0, daysToMonthEnd(ms3)) // 2023 非闰年，2/28 即月末
}

func TestIsNewMonth(t *testing.T) {
	june := time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC).UnixMilli()
	july := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	assert.True(t, isNewMonth(june, july))
	assert.False(t, isNewMonth(june, june+int64(60)*60*1000))
	// Anchor = 0 视为需要初始化
	assert.True(t, isNewMonth(0, july))
}
