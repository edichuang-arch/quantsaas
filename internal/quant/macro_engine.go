package quant

import "math"

// 宏观引擎。
//
// 方案 Y 启动预设（DCA + 偏离加码 + 死线兜底）：
//
//   1. 节奏：按"基础天数"推进；上次宏观决策距今 < baseDays × TimeDilationMultiplier 时跳过
//   2. 基础档位：单次 BUY 金额 = monthlyInjectUSDT / 30 × baseDays × Multiplier × AggressivenessMultiplier
//   3. 偏离加码：当前价格低于长期 EMA 的幅度（百分比）超过 betaThreshold 时，额外乘以
//      (1 + priceDiscount × betaMultiplier)
//   4. 死线兜底：若本月已注资 < 计划 × deadlineForcePct 且距月末 ≤ 3 天，一次性补齐剩余配额
//
// 铁律：只产出 BUY，绝对不产出 SELL；订单金额 ≤ SpendableUSDT。
// 小于 MicroMinOrderUSDT (10.1) 的订单直接舍弃。

// MacroInput 宏观引擎的一次决策输入。
type MacroInput struct {
	NowMs             int64
	Closes            []float64
	CurrentPrice      float64
	SpendableUSDT     float64 // 实例可用 USDT 余额
	MonthlyInjectUSDT float64 // 实例月度注资计划
	Prev              RuntimeState
	State             MarketState

	// 可进化参数
	BaseDays                int     // 基础定投间隔（天）
	Multiplier              float64 // 基础档位乘数（资金激进度）
	BetaThreshold           float64 // 偏离触发阈值（%），例如 0.05 = 5%
	PriceDiscountBoost      float64 // 偏离加码系数
	DeadlineForcePct        float64 // 死线兜底阈值（已注资 < 计划 × 此值触发）
	AggressivenessMultiplier float64 // 季节层注入（冬/春/夏/秋），由 SpawnPoint 带入
}

// MacroOutput 宏观引擎输出。
type MacroOutput struct {
	Intent         *TradeIntent // nil 表示本次不下单
	NewRuntime     RuntimeState
	Reason         string
}

const (
	macroBaseDaysDefault        = 7
	deadlineDaysBeforeMonthEnd  = 3
	msPerDay             int64  = 24 * 60 * 60 * 1000
)

// ComputeMacroDecision 纯函数。
// 返回 MacroOutput，NewRuntime 会更新 LastMacroDecisionMs / MonthlyInjectedUSDT / MonthAnchorMs 等字段。
func ComputeMacroDecision(in MacroInput) MacroOutput {
	rt := in.Prev

	// 跨月检测：每当进入新自然月，重置已注资与月锚点。
	if isNewMonth(rt.MonthAnchorMs, in.NowMs) {
		rt.MonthAnchorMs = in.NowMs
		rt.MonthlyInjectedUSDT = 0
	}

	out := MacroOutput{NewRuntime: rt}

	// 节奏门：距上次决策时间 ≥ baseDays × TimeDilation 才考虑下单。
	baseDays := in.BaseDays
	if baseDays <= 0 {
		baseDays = macroBaseDaysDefault
	}
	minInterval := int64(float64(baseDays) * in.State.TimeDilationMultiplier * float64(msPerDay))
	timeGate := rt.LastMacroDecisionMs == 0 || in.NowMs-rt.LastMacroDecisionMs >= minInterval

	// 计算基础档位金额。
	perDayBase := in.MonthlyInjectUSDT / 30.0
	multiplier := in.Multiplier
	if multiplier <= 0 {
		multiplier = 1.0
	}
	aggr := in.AggressivenessMultiplier
	if aggr <= 0 {
		aggr = 1.0
	}
	baseOrder := perDayBase * float64(baseDays) * multiplier * aggr

	// 偏离加码：当价格低于长期 EMA 超过 BetaThreshold% 时放大订单。
	if len(in.Closes) >= msLongEMA && in.BetaThreshold > 0 {
		longEMA := EMA(in.Closes, msLongEMA)
		if longEMA > 0 {
			discount := (longEMA - in.CurrentPrice) / longEMA // 正值 = 价格偏低
			if discount > in.BetaThreshold {
				boost := 1.0 + discount*in.State.BetaMultiplier*math.Max(in.PriceDiscountBoost, 0)
				baseOrder *= boost
			}
		}
	}

	// 死线兜底：接近月末但本月注资不足，一次性补齐剩余配额。
	var orderUSD float64
	var reason string
	if timeGate && baseOrder >= MicroMinOrderUSDT {
		orderUSD = baseOrder
		reason = "scheduled DCA"
	}

	if in.DeadlineForcePct > 0 && in.MonthlyInjectUSDT > 0 {
		threshold := in.MonthlyInjectUSDT * in.DeadlineForcePct
		daysLeft := daysToMonthEnd(in.NowMs)
		if rt.MonthlyInjectedUSDT < threshold && daysLeft <= deadlineDaysBeforeMonthEnd {
			deficit := threshold - rt.MonthlyInjectedUSDT
			if deficit > orderUSD {
				orderUSD = deficit
				reason = "deadline fallback"
			}
		}
	}

	// 用可用资金 clamp。
	if orderUSD > in.SpendableUSDT {
		orderUSD = in.SpendableUSDT
		reason += " (clamped by spendable)"
	}

	// 下限过滤。
	if orderUSD < MicroMinOrderUSDT {
		out.NewRuntime = rt
		out.Reason = "below min order"
		return out
	}

	orderUSD = RoundToUSDT(orderUSD)
	rt.LastMacroDecisionMs = in.NowMs
	rt.MonthlyInjectedUSDT += orderUSD

	out.Intent = &TradeIntent{
		Action:     ActionBuy,
		Engine:     EngineMacro,
		LotType:    LotDeadStack,
		AmountUSDT: orderUSD,
		Note:       reason,
	}
	out.NewRuntime = rt
	out.Reason = reason
	return out
}

// isNewMonth 返回 true 当 currentMs 与 anchorMs 不在同一自然月。
// anchorMs 为 0（尚未记录）视为需要初始化 → true。
func isNewMonth(anchorMs, currentMs int64) bool {
	if anchorMs == 0 {
		return true
	}
	ay, am, _ := civilFromMs(anchorMs)
	cy, cm, _ := civilFromMs(currentMs)
	return ay != cy || am != cm
}

// daysToMonthEnd 计算距当月最后一天的剩余天数（含当天则为 0）。
func daysToMonthEnd(ms int64) int {
	y, m, d := civilFromMs(ms)
	return daysInUTCMonth(y, m) - d
}

func daysInUTCMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if (year%4 == 0 && year%100 != 0) || year%400 == 0 {
			return 29
		}
		return 28
	}
	return 30
}

func civilFromMs(ms int64) (year, month, day int) {
	days := ms / msPerDay
	days += 719468
	era := days / 146097
	if days < 0 && days%146097 != 0 {
		era--
	}
	doe := days - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
		y++
	}
	return int(y), int(m), int(d)
}
