package sigmoidbtc

import (
	"fmt"

	"github.com/edi/quantsaas/internal/quant"
)

// 硬风控阀值。这些是策略级硬规则，不进入基因组（防止 GA 演化出绕过风控的解）。
//
// 设计动机（详见 docs/2026-04-28-sigmafloor-bug.md）：
//   - 4/28 真实 BTC 365 天回测发现策略 MaxDD = 98.29%，远超 BTC 自身 52% 跌幅
//   - 5/2 OnBar 诊断发现 6 个月成交 45,561 笔，手续费 $11,066 烧光本金（千刀割死）
//   - 死亡螺旋：跌中 Micro 持续加仓 → USDT 燒光 → 滿倉等反彈，跌幅再放大
//   - 解法：流动性/仓位守门 + 最小信号门槛 + 冷却时间，三层防御
//
// 注意：守门只阻止 BUY，从不强制 SELL。SELL 仍由 Sigmoid 引擎自由决策。
const (
	// HardCashFloorRatio: USDT 现金 < 总权益 × 此比例时禁止 BUY。
	// 0.10 = 至少保留 10% 现金应对回撤。
	HardCashFloorRatio = 0.10

	// HardPositionCeiling: 资产持仓 / 总权益 > 此比例时禁止 BUY。
	// 0.90 = 倉位最高 90%，永远保留 ≥10% 现金做反向操作。
	HardPositionCeiling = 0.90

	// MicroCooldownMs: 两次 micro 决策的最小间隔。
	// 1 小时 = 12 根 5m K 线，避免在 5m 时间尺度上反复进出导致手续费燒乾。
	// 这是「过度交易防御」的时间维度补充（金额维度由 MicroMinDeltaWeight 守门）。
	MicroCooldownMs = 60 * 60 * 1000
)

// shouldBlockBuy 检查 portfolio 是否应该拒绝 BUY intent。
//
// SELL 不受此守门影响：处于满仓状态时反而需要 SELL 来恢复流动性。
// totalEquity ≤ 0 时返回 false（数据异常，保守不拦）。
func shouldBlockBuy(p quant.PortfolioSnapshot, totalEquity float64) (bool, string) {
	if totalEquity <= 0 {
		return false, ""
	}
	cashRatio := p.USDTBalance / totalEquity
	if cashRatio < HardCashFloorRatio {
		return true, fmt.Sprintf("hard_cash_floor: usdt=%.1f%% < %.0f%%",
			cashRatio*100, HardCashFloorRatio*100)
	}
	posValue := (p.DeadStackAsset + p.FloatStackAsset + p.ColdSealedAsset) * p.CurrentPrice
	posRatio := posValue / totalEquity
	if posRatio > HardPositionCeiling {
		return true, fmt.Sprintf("hard_position_ceiling: pos=%.1f%% > %.0f%%",
			posRatio*100, HardPositionCeiling*100)
	}
	return false, ""
}

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
	// 硬风控守门触发时记录理由，最后并入 DecisionReason
	var blockedMacroReason, blockedMicroReason string

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
		// 硬风控守门：现金水位下限 + 仓位上限（参见 docs/2026-04-28-sigmafloor-bug.md）
		if block, reason := shouldBlockBuy(in.Portfolio, totalEquity); block {
			out.NewRuntime.Extras[ExtraKeyLastBlockedMacroMs] = float64(in.NowMs)
			blockedMacroReason = reason
			// 直接丢弃 macro intent，不计入 out.Intents
		} else {
			// 扣除已分配的 macro 预算以免 macro + micro 合计超过 spendable
			spendable -= macroOut.Intent.AmountUSDT
			out.Intents = append(out.Intents, *macroOut.Intent)
		}
	}

	// 5. 微观引擎（含冷却时间守门）。
	//
	// 冷却时间动机：5m K 线尺度下 micro 信号大量是噪音，反复进出会被手续费燒乾。
	// 距上次 micro 决策时间 < MicroCooldownMs 时直接跳过，不浪费 CPU 也不下单。
	var microIntent *quant.TradeIntent
	var microOut quant.MicroOutput
	lastMicroMs := int64(in.PrevRuntime.Extras[ExtraKeyLastMicroDecisionMs])
	microCoolingDown := lastMicroMs > 0 && in.NowMs-lastMicroMs < MicroCooldownMs

	if microCoolingDown {
		// 跳过整个 micro 计算 + 下单，但保留 reason 让 dashboard 看到
		microOut = quant.MicroOutput{Skipped: true, SkipReason: "cooldown"}
	} else {
		microOut = runMicro(in, params, state, totalEquity)
		microIntent = translateMicroToIntent(microOut, in.Portfolio.CurrentPrice)
	}
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
		// 硬风控守门：BUY 才检查（SELL 永远放行，反而是恢复流动性的手段）
		if microIntent != nil && microIntent.Action == quant.ActionBuy {
			if block, reason := shouldBlockBuy(in.Portfolio, totalEquity); block {
				out.NewRuntime.Extras[ExtraKeyLastBlockedMicroMs] = float64(in.NowMs)
				blockedMicroReason = reason
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

	// 8. 决策摘要（含硬风控触发理由，便于运维一眼看到为什么没下单）
	reason := fmt.Sprintf(
		"state=%s macro=%s micro=%+v",
		state.Kind, macroOut.Reason, summarizeMicro(microOut),
	)
	if blockedMacroReason != "" {
		reason += " | block_macro=" + blockedMacroReason
	}
	if blockedMicroReason != "" {
		reason += " | block_micro=" + blockedMicroReason
	}
	out.DecisionReason = reason
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
