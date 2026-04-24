package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// runMicro 是 quant.ComputeMicroDecision（Sigmoid 动态天平）的薄包装。
// 负责把 Chromosome / MarketState / Portfolio 翻译成 MicroInput。
func runMicro(
	in quant.StrategyInput,
	params Params,
	state quant.MarketState,
	totalEquity float64,
) quant.MicroOutput {
	c := params.Chromosome
	adjusted := applyChromosomeToState(state, c)

	mi := quant.MicroInput{
		Closes:        in.Closes,
		CurrentPrice:  in.Portfolio.CurrentPrice,
		TotalEquity:   totalEquity,
		CurrentWeight: in.Portfolio.CurrentMicroWeight(),
		Beta:          c.Beta,
		Gamma:         c.Gamma,
		SigmaFloor:    c.SigmaFloor,
		BetaMultiplier: adjusted.BetaMultiplier,
		IsQuiet:       adjusted.IsQuiet,
	}
	return quant.ComputeMicroDecision(mi)
}

// translateMicroToIntent 将 MicroOutput.OrderUSD 转成 TradeIntent。
// OrderUSD > 0 → BUY（买入 FLOATING）；< 0 → SELL（卖出 FLOATING，按当前价格估算数量）。
// OrderUSD == 0 时返回 nil。
func translateMicroToIntent(out quant.MicroOutput, price float64) *quant.TradeIntent {
	if out.OrderUSD == 0 {
		return nil
	}
	if out.OrderUSD > 0 {
		return &quant.TradeIntent{
			Action:     quant.ActionBuy,
			Engine:     quant.EngineMicro,
			LotType:    quant.LotFloating,
			AmountUSDT: quant.RoundToUSDT(out.OrderUSD),
			Note:       "sigmoid BUY",
		}
	}
	// SELL 使用 QtyAsset 字段（策略用浮动仓的基础资产卖出）。
	if price <= 0 {
		return nil
	}
	qty := quant.RoundToAssetQty(-out.OrderUSD / price)
	if qty <= 0 {
		return nil
	}
	return &quant.TradeIntent{
		Action:   quant.ActionSell,
		Engine:   quant.EngineMicro,
		LotType:  quant.LotFloating,
		QtyAsset: qty,
		Note:     "sigmoid SELL",
	}
}
