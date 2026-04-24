package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// 本策略的 RuntimeState 复用 quant.RuntimeState 通用容器：
//   LastProcessedBarTime / LastMacroDecisionMs / MonthlyInjectedUSDT / MonthAnchorMs / Extras
//
// 如需追加本策略独有的状态字段，统一放入 Extras map（key 用下述常量）。
// 这样可以避免频繁改 struct 触发 DB migration，也让 RuntimeState 对 quant 包保持通用。

const (
	ExtraKeyLastMicroDecisionMs = "last_micro_decision_ms"
	ExtraKeyLastReleaseMs       = "last_release_ms"
)

// ensureExtras 确保 Extras map 非 nil，便于后续写入。
func ensureExtras(rt quant.RuntimeState) quant.RuntimeState {
	if rt.Extras == nil {
		rt.Extras = map[string]float64{}
	}
	return rt
}
