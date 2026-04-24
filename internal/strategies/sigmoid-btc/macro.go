package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// runMacro 是 quant.ComputeMacroDecision 的薄包装：
// 负责把策略层的 Chromosome + MarketState 注入到引擎输入结构里。
//
// 本函数是纯函数，不做任何 I/O。
func runMacro(
	in quant.StrategyInput,
	params Params,
	state quant.MarketState,
	totalEquity, spendableUSDT float64,
) quant.MacroOutput {
	c := params.Chromosome

	// 把基因里的 Bull/Bear 乘数应用到 state，让季节性的时间扩张与 β 放大都由基因决定。
	adjustedState := applyChromosomeToState(state, c)

	mi := quant.MacroInput{
		NowMs:                   in.NowMs,
		Closes:                  in.Closes,
		CurrentPrice:            in.Portfolio.CurrentPrice,
		SpendableUSDT:           spendableUSDT,
		MonthlyInjectUSDT:       in.MonthlyInjectUSDT,
		Prev:                    in.PrevRuntime,
		State:                   adjustedState,
		BaseDays:                c.BaseDays,
		Multiplier:              c.Multiplier,
		BetaThreshold:           c.BetaThreshold,
		PriceDiscountBoost:      c.PriceDiscountBoost,
		DeadlineForcePct:        c.DeadlineForcePct,
		AggressivenessMultiplier: 1.0, // 当前不启用季节层（实验室才用，实盘取默认 1.0）
	}
	_ = totalEquity // 当前宏观引擎不直接读取 TotalEquity，预留给未来扩展
	return quant.ComputeMacroDecision(mi)
}

// applyChromosomeToState 用基因参数覆盖 market state 的默认乘数。
// 四个字段只在 BULL / BEAR 状态下被替换；QUIET / NORMAL 不变。
func applyChromosomeToState(s quant.MarketState, c quant.Chromosome) quant.MarketState {
	out := s
	switch s.Kind {
	case quant.MarketBull:
		out.TimeDilationMultiplier = c.BullTimeDilation
		out.BetaMultiplier = c.BullBetaMultiplier
	case quant.MarketBear:
		out.TimeDilationMultiplier = c.BearTimeDilation
		out.BetaMultiplier = c.BearBetaMultiplier
	}
	return out
}
