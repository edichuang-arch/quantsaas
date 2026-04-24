package quant

// 市场状态感知层。
//
// 你选择 Plan 方案 Y（不另写"策略数学引擎.md"），所以本实现采用以下合理预设：
//   牛（BULL）：价格站上长期 EMA 且短期 EMA > 长期 EMA（金叉）
//   熊（BEAR）：价格跌破长期 EMA 且短期 EMA < 长期 EMA（死叉）
//   安静（QUIET）：近期波动率（MAV 短 / MAV 长）接近 1，震荡盘
//   正常（NORMAL）：其余情况
//
// 这只是一个启动预设，未来可以通过修改本文件的参数或引入马尔可夫模型替换。

// MarketStateKind 市场状态枚举。对外输出到 StrategyInput 时保持字符串便于调试。
type MarketStateKind string

const (
	MarketBull   MarketStateKind = "BULL"
	MarketBear   MarketStateKind = "BEAR"
	MarketQuiet  MarketStateKind = "QUIET"
	MarketNormal MarketStateKind = "NORMAL"
)

// MarketState 是给宏观引擎与微观 Sigmoid 的接口契约。
// 任何新分类方法都必须产出同样的四个字段，保证下游代码无感。
type MarketState struct {
	Kind                   MarketStateKind
	TimeDilationMultiplier float64 // 给宏观引擎：>1 扩展时间窗口
	BetaMultiplier         float64 // 给微观 Sigmoid：>1 加速调仓
	IsQuiet                bool    // true 时微观粉尘订单归零
	Reason                 string  // 调试用说明，不影响决策
}

// 默认参数。
const (
	msShortEMA  = 20  // ~20 根：短期趋势
	msLongEMA   = 99  // ~99 根：长期趋势
	msQuietLow  = 0.9 // VolRatio 小于此视为安静（但高于 1 也可能安静）
	msQuietHigh = 1.1
)

// ComputeMarketState 纯函数。数据不足时返回 NORMAL（保守默认）。
func ComputeMarketState(closes []float64) MarketState {
	defaultState := MarketState{
		Kind:                   MarketNormal,
		TimeDilationMultiplier: 1.0,
		BetaMultiplier:         1.0,
		IsQuiet:                false,
		Reason:                 "insufficient data",
	}
	if len(closes) < msLongEMA {
		return defaultState
	}

	short := EMA(closes, msShortEMA)
	long := EMA(closes, msLongEMA)
	price := closes[len(closes)-1]

	// 波动率比值判定安静态。
	mavShort := MAVAbsChange(closes, MicroVolRatioShortBars)
	mavLong := MAVAbsChange(closes, MicroVolRatioLongBars)
	var volRatio float64 = 1.0
	if mavLong > 0 {
		volRatio = ClipFloat64(mavShort/mavLong, 0.1, 3.0)
	}
	isQuiet := volRatio >= msQuietLow && volRatio <= msQuietHigh

	switch {
	case price > long && short > long:
		// 牛市：时间放缓（宏观降速以免追高）、β 加倍以更快调仓
		return MarketState{
			Kind:                   MarketBull,
			TimeDilationMultiplier: 1.5,
			BetaMultiplier:         1.3,
			IsQuiet:                false,
			Reason:                 "price > long_ema && short_ema > long_ema",
		}
	case price < long && short < long:
		// 熊市：宏观加速（更勤快地 DCA 吸入）、β 加倍以更快减仓
		return MarketState{
			Kind:                   MarketBear,
			TimeDilationMultiplier: 0.75,
			BetaMultiplier:         1.3,
			IsQuiet:                false,
			Reason:                 "price < long_ema && short_ema < long_ema",
		}
	case isQuiet:
		// 安静态：保持标准节奏，但粉尘订单归零（楔形过滤关闭）
		return MarketState{
			Kind:                   MarketQuiet,
			TimeDilationMultiplier: 1.0,
			BetaMultiplier:         1.0,
			IsQuiet:                true,
			Reason:                 "vol ratio in quiet band",
		}
	default:
		return MarketState{
			Kind:                   MarketNormal,
			TimeDilationMultiplier: 1.0,
			BetaMultiplier:         1.0,
			IsQuiet:                false,
			Reason:                 "default normal",
		}
	}
}
