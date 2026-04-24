package quant

import "math"

// Sigmoid 动态天平 — 微观引擎设计哲学：
//
//   Signal 是外力（市场信号）；InventoryBias 是弹簧恢复力；
//   Beta 是弹簧刚度；Gamma 决定是否启用弹簧；
//   VolatilityRatio 楔形过滤控制安静期的粉尘订单。
//
// 计算步骤（来自系统设计真源，严禁改变结构）：
//   1. 信号层计算 EMA 与 σ（窗长为不可进化常量）。
//   2. 计算无量纲 signal = (close − EMA) / σ。
//   3. Sigmoid 目标权重：
//        EffectiveBeta = max(0.01, β × BetaMultiplier)
//        InventoryBias = clamp(CurrentWeight, 0, 1) − 0.5
//        Exponent = EffectiveBeta × Signal + γ × InventoryBias
//        TargetWeight = 1 / (1 + exp(Exponent))，夹紧到 [0, 1]
//   4. DeltaWeight = TargetWeight − CurrentWeight
//      TheoreticalUSD = DeltaWeight × TotalEquity
//   5. VolatilityRatio = clip(MAV短 / MAV长, 0.1, 3.0)，默认 1.0。
//   6. 楔形区过滤：
//        - |TheoreticalUSD| ≥ MinOrderUSDT：原值下单
//        - |TheoreticalUSD| ∈ (0, MinOrderUSDT)：
//              若非安静态且（|DeltaWeight| ≥ WedgeDeltaThreshold 或
//              VolatilityRatio ≥ WedgeVolThreshold），强制下 ±MinOrderUSDT；
//              否则 OrderUSD = 0。

// Sigmoid 引擎的不可进化常量（实现细节，不进入基因组）。
const (
	MicroSignalEMABars      = 21  // 短期均线窗长
	MicroSignalStdDevBars   = 21  // 信号标准差窗长
	MicroVolRatioShortBars  = 16  // MAV 短窗
	MicroVolRatioLongBars   = 112 // MAV 长窗
	MicroMinOrderUSDT       = 10.1
	MicroWedgeDeltaThreshold = 0.02 // |DeltaWeight| ≥ 2%
	MicroWedgeVolThreshold   = 1.5  // VolatilityRatio ≥ 1.5
)

// MicroInput 封装 Sigmoid 动态天平一次决策需要的全部上下文。
// 所有参数都是拉平的基础类型，没有任何对外部可变状态的引用。
type MicroInput struct {
	Closes        []float64 // 收盘价序列，索引 0 为最早
	CurrentPrice  float64
	TotalEquity   float64
	CurrentWeight float64 // FloatStack × Price / TotalEquity

	// 可进化参数
	Beta      float64 // Sigmoid 激进系数（β）
	Gamma     float64 // 仓位偏置系数（γ）
	SigmaFloor float64 // σ 的最小值保护（防止极平坦市场 σ 趋 0 导致 signal 爆炸）

	// 来自市场状态感知层
	BetaMultiplier float64 // >1.0 时极端行情放大 β；1.0 为正常
	IsQuiet        bool    // true 时粉尘订单归零
}

// MicroOutput 微观引擎的决策结果。OrderUSD > 0 表示买入，< 0 表示卖出。
// Signal/TargetWeight/VolatilityRatio 保留做调试与前端展示。
type MicroOutput struct {
	Signal          float64
	EMA             float64
	Sigma           float64
	TargetWeight    float64
	DeltaWeight     float64
	TheoreticalUSD  float64
	VolatilityRatio float64
	OrderUSD        float64 // 最终下单金额（含符号）；0 表示不下单
	Skipped         bool    // 数据不足等原因跳过
	SkipReason      string
}

// ComputeMicroDecision 纯函数，相同输入永远产出相同输出。
func ComputeMicroDecision(in MicroInput) MicroOutput {
	out := MicroOutput{VolatilityRatio: 1.0}

	// 数据窗口长度检查。取 EMA / StdDev / MAV 长窗中的最大值。
	minLen := MicroSignalEMABars
	if MicroSignalStdDevBars > minLen {
		minLen = MicroSignalStdDevBars
	}
	if MicroVolRatioLongBars > minLen {
		minLen = MicroVolRatioLongBars
	}
	if len(in.Closes) < minLen {
		out.Skipped = true
		out.SkipReason = "insufficient closes"
		return out
	}

	// Step 1：EMA 与 σ。
	ema := EMA(in.Closes, MicroSignalEMABars)
	sigma := StdDev(in.Closes, MicroSignalStdDevBars)
	if in.SigmaFloor > 0 && sigma < in.SigmaFloor {
		sigma = in.SigmaFloor
	}
	if sigma <= 0 || math.IsNaN(sigma) {
		out.Skipped = true
		out.SkipReason = "sigma zero or NaN"
		return out
	}
	out.EMA = ema
	out.Sigma = sigma

	// Step 2：无量纲信号 = (close − EMA) / σ
	// 正值 = 价格高于均线（偏多→倾向减仓）；负值 = 价格低于均线（偏空→倾向加仓）
	signal := (in.CurrentPrice - ema) / sigma
	out.Signal = signal

	// Step 3：Sigmoid 目标权重。
	effBeta := in.Beta * in.BetaMultiplier
	if effBeta < 0.01 {
		effBeta = 0.01
	}
	inventoryBias := ClipFloat64(in.CurrentWeight, 0, 1) - 0.5
	exponent := effBeta*signal + in.Gamma*inventoryBias
	// 防止 exp 溢出。Exponent 范围约束为 ±40（等效 e^40 ≈ 2e17）。
	if exponent > 40 {
		exponent = 40
	} else if exponent < -40 {
		exponent = -40
	}
	target := 1.0 / (1.0 + math.Exp(exponent))
	target = ClipFloat64(target, 0, 1)
	out.TargetWeight = target

	// Step 4：DeltaWeight 与理论订单金额。
	delta := target - in.CurrentWeight
	out.DeltaWeight = delta
	theoretical := delta * in.TotalEquity
	out.TheoreticalUSD = theoretical

	// Step 5：VolatilityRatio 楔形过滤分母。
	mavShort := MAVAbsChange(in.Closes, MicroVolRatioShortBars)
	mavLong := MAVAbsChange(in.Closes, MicroVolRatioLongBars)
	if !math.IsNaN(mavShort) && !math.IsNaN(mavLong) && mavLong > 0 {
		ratio := mavShort / mavLong
		out.VolatilityRatio = ClipFloat64(ratio, 0.1, 3.0)
	}

	// Step 6：楔形区过滤。
	absTheo := math.Abs(theoretical)
	switch {
	case absTheo >= MicroMinOrderUSDT:
		out.OrderUSD = theoretical
	case absTheo > 0 && !in.IsQuiet &&
		(math.Abs(delta) >= MicroWedgeDeltaThreshold || out.VolatilityRatio >= MicroWedgeVolThreshold):
		// 强制最小订单（保留符号）。
		if theoretical >= 0 {
			out.OrderUSD = MicroMinOrderUSDT
		} else {
			out.OrderUSD = -MicroMinOrderUSDT
		}
	default:
		out.OrderUSD = 0
	}
	return out
}
