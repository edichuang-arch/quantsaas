package sigmoidbtc

import (
	"fmt"

	"github.com/edi/quantsaas/internal/quant"
)

// Step 是本策略对外的唯一入口（铁律 #2 策略同构）：
//
//   - 回测适配器与实盘 cron tick 都通过此函数推进
//   - 纯函数：相同输入 → 完全相同输出
//   - 无任何 I/O（网络、DB、文件、time.Now 全禁）
//
// 决策顺序：
//
//   1. 数据窗口充足性检查（不足则返回 SkipReason）
//   2. 从 Portfolio 计算 TotalEquity / SpendableUSDT / CurrentMicroWeight
//   3. 计算市场状态（牛/熊/安静/正常）
//   4. 宏观引擎 → BUY（DEAD_STACK）意图
//   5. 微观引擎 Sigmoid → BUY / SELL（FLOATING）意图
//   6. 底仓释放决策 → ReleaseIntent
//   7. 更新 RuntimeState（LastProcessedBarTime / Last*DecisionMs 等）
//   8. 组装 StrategyOutput 返回
func Step(in quant.StrategyInput, params Params) quant.StrategyOutput {
	out := quant.StrategyOutput{NewRuntime: ensureExtras(in.PrevRuntime)}

	// 1. 数据窗口充足性检查。取所有引擎中最长的窗口。
	minBars := quant.MicroVolRatioLongBars // 微观 MAV 长窗：112
	if len(in.Closes) < minBars {
		out.SkipReason = fmt.Sprintf("need >= %d bars, got %d", minBars, len(in.Closes))
		return out
	}
	if in.Portfolio.CurrentPrice <= 0 {
		out.SkipReason = "portfolio current price <= 0"
		return out
	}

	// 2. 派生字段：TotalEquity / SpendableUSDT / CurrentMicroWeight
	totalEquity := in.Portfolio.TotalEquity()
	spendable := computeSpendableUSDT(in.Portfolio, params.Chromosome)

	// 3. 市场状态感知
	state := quant.ComputeMarketState(in.Closes)

	// 4. 宏观引擎（只产出 BUY→DEAD_STACK）
	macroOut := runMacro(in, params, state, totalEquity, spendable)
	out.NewRuntime = ensureExtras(macroOut.NewRuntime)
	if macroOut.Intent != nil {
		// 防御性校验：宏观引擎永远不允许产生 SELL 或 FLOATING lot
		if macroOut.Intent.Action != quant.ActionBuy || macroOut.Intent.LotType != quant.LotDeadStack {
			// 发现违反铁律立即停下并报告（空输出）
			out.SkipReason = "macro produced non-BUY/non-DEAD_STACK intent (iron law violation)"
			return out
		}
		// 扣除已分配的 macro 预算以免 macro + micro 合计超过 spendable
		spendable -= macroOut.Intent.AmountUSDT
		out.Intents = append(out.Intents, *macroOut.Intent)
	}

	// 5. 微观引擎
	microOut := runMicro(in, params, state, totalEquity)
	microIntent := translateMicroToIntent(microOut, in.Portfolio.CurrentPrice)
	if microIntent != nil {
		// BUY 的 USDT 金额也要受 spendable 限制
		if microIntent.Action == quant.ActionBuy {
			if microIntent.AmountUSDT > spendable {
				microIntent.AmountUSDT = quant.RoundToUSDT(spendable)
			}
			if microIntent.AmountUSDT < quant.MicroMinOrderUSDT {
				microIntent = nil
			}
		}
	}
	if microIntent != nil {
		out.Intents = append(out.Intents, *microIntent)
		// 记录最后一次微观决策时间
		out.NewRuntime.Extras[ExtraKeyLastMicroDecisionMs] = float64(in.NowMs)
	}

	// 6. 底仓释放决策（根据微观 SELL 意图量推导）
	var microSellQty float64
	if microIntent != nil && microIntent.Action == quant.ActionSell {
		microSellQty = microIntent.QtyAsset
	}
	if rel := decideDeadRelease(in, params, microSellQty); rel != nil {
		out.Releases = append(out.Releases, *rel)
		out.NewRuntime.Extras[ExtraKeyLastReleaseMs] = float64(in.NowMs)
	}

	// 7. 更新 RuntimeState：推进处理光标
	out.NewRuntime.LastProcessedBarTime = in.NowMs

	// 8. 决策摘要
	out.DecisionReason = fmt.Sprintf(
		"state=%s macro=%s micro=%+v",
		state.Kind, macroOut.Reason, summarizeMicro(microOut),
	)
	return out
}

// computeSpendableUSDT 根据 MicroReservePct 扣留一部分 USDT。
// MicroReservePct = 0.25 时，只用 75% 的 USDT 余额给宏观+微观买入，保留 25% 应对回撤。
func computeSpendableUSDT(p quant.PortfolioSnapshot, c quant.Chromosome) float64 {
	available := p.USDTBalance
	reserve := available * c.MicroReservePct
	return quant.RoundToUSDT(available - reserve)
}

// summarizeMicro 用于日志，不影响决策。
func summarizeMicro(m quant.MicroOutput) string {
	if m.Skipped {
		return "skip:" + m.SkipReason
	}
	return fmt.Sprintf(
		"sig=%.3f tw=%.3f order=%.2f",
		m.Signal, m.TargetWeight, m.OrderUSD,
	)
}
