package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// decideDeadRelease 综合"软释放"与"硬释放"两种情况，返回一个释放意图。
//
//   软释放（Soft）：DeadStack lot 老化超过 MinAgeMonths 个月后，按 SoftReleaseMaxRatio
//                   的比例转为 FLOATING，供微观引擎卖出。
//   硬释放（Hard）：若本次微观 SELL 意图的数量 > FloatStack 当前余量，
//                   从 DeadStack（任何年龄，非 ColdSealed）补足缺口。
//
// 铁律 #9：释放只更新 SaaS 侧账本，不下发 Agent，不产生 TradeCommand。
// 调用方收到 ReleaseIntent 后需写 AuditLog 并在 DB 事务里更新 SpotLot 分类。
//
// 当前 Step() 层只持有 PortfolioSnapshot（聚合字段），不持有 Lot 列表。
// 真实 lot 扣减交给 SaaS 侧的 release service 完成。这里只产出"需要释放多少数量"的意图。
func decideDeadRelease(
	in quant.StrategyInput,
	params Params,
	microSellQty float64,
) *quant.ReleaseIntent {
	c := params.Chromosome
	dead := in.Portfolio.DeadStackAsset
	if dead <= 0 {
		return nil
	}

	// Hard release：当微观 SELL 数量超过浮动仓余量，需从 DeadStack 补差额。
	floating := in.Portfolio.FloatStackAsset
	if microSellQty > floating {
		gap := microSellQty - floating
		if gap > dead {
			gap = dead
		}
		if gap > 0 {
			return &quant.ReleaseIntent{
				Amount: quant.RoundToAssetQty(gap),
				Reason: "hard_demand",
			}
		}
	}

	// Soft release：按基因配置的 ratio 计算本次可转出数量。
	if c.SoftReleaseMaxRatio <= 0 {
		return nil
	}
	soft := dead * c.SoftReleaseMaxRatio
	soft = quant.RoundToAssetQty(soft)
	if soft <= 0 {
		return nil
	}
	return &quant.ReleaseIntent{
		Amount: soft,
		Reason: "soft_age",
	}
}
