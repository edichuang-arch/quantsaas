package quant

import "math"

// 本文件是策略无关的基础统计工具集。
// 铁律：所有函数纯函数、无 I/O、不跨标的比较绝对价格。

// EMA 对输入序列计算指数平滑均线，返回最新一个值。
// 窗长 period < 1 或数据不足时返回 NaN，调用方必须检查。
func EMA(series []float64, period int) float64 {
	if period < 1 || len(series) < period {
		return math.NaN()
	}
	alpha := 2.0 / float64(period+1)
	// 以前 period 个点的算术均值作为 EMA 起点，再往后递推。
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += series[i]
	}
	ema := sum / float64(period)
	for i := period; i < len(series); i++ {
		ema = alpha*series[i] + (1-alpha)*ema
	}
	return ema
}

// EMASeries 返回与输入同长度的 EMA 序列，前 period-1 个位置填 NaN。
// 用于需要逐点对比价格与均线的场景。
func EMASeries(series []float64, period int) []float64 {
	out := make([]float64, len(series))
	if period < 1 || len(series) < period {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	alpha := 2.0 / float64(period+1)
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += series[i]
		out[i] = math.NaN()
	}
	ema := sum / float64(period)
	out[period-1] = ema
	for i := period; i < len(series); i++ {
		ema = alpha*series[i] + (1-alpha)*ema
		out[i] = ema
	}
	return out
}

// StdDev 计算最新窗口的样本标准差（n-1 分母）。
// 数据不足返回 NaN。
func StdDev(series []float64, period int) float64 {
	if period < 2 || len(series) < period {
		return math.NaN()
	}
	start := len(series) - period
	sum := 0.0
	for i := start; i < len(series); i++ {
		sum += series[i]
	}
	mean := sum / float64(period)
	sq := 0.0
	for i := start; i < len(series); i++ {
		diff := series[i] - mean
		sq += diff * diff
	}
	return math.Sqrt(sq / float64(period-1))
}

// MAVAbsChange 最近 L 根收盘的平均绝对涨跌。
// 公式：Σ|close[i] − close[i-1]| / (L − 1)，不依赖 High/Low，仅用收盘价。
// 数据不足（L < 2 或 len(closes) < L）返回 NaN。
func MAVAbsChange(closes []float64, L int) float64 {
	if L < 2 || len(closes) < L {
		return math.NaN()
	}
	start := len(closes) - L
	sum := 0.0
	for i := start + 1; i < len(closes); i++ {
		sum += math.Abs(closes[i] - closes[i-1])
	}
	return sum / float64(L-1)
}

// ClipFloat64 将 x 夹紧到 [lo, hi]。若 lo > hi 会返回 hi（容错）。
func ClipFloat64(x, lo, hi float64) float64 {
	if lo > hi {
		lo, hi = hi, lo
	}
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// RoundToUSDT 将金额四舍五入到 2 位小数。
func RoundToUSDT(x float64) float64 {
	return math.Round(x*100) / 100
}

// RoundToAssetQty 将资产数量向下截断到 5 位小数（0.00001）。
//
// 选择 floor 而非 round 的原因:
//   - SELL 时若 round up 可能超过实际持仓 → 单子被拒
//   - Binance 主流现货对（BTC/ETH/BNB/SOL/USDT）LOT_SIZE.stepSize = 0.00001
//   - 未来若策略要支持其他 stepSize, tick.go 应改用 instance.LotStep 动态截断
func RoundToAssetQty(x float64) float64 {
	return math.Floor(x*1e5) / 1e5
}

// LogReturn 对数收益率 ln(p1/p0)。p0 <= 0 返回 NaN。
// 无量纲，且可跨标的做加性聚合。
func LogReturn(p0, p1 float64) float64 {
	if p0 <= 0 || p1 <= 0 {
		return math.NaN()
	}
	return math.Log(p1 / p0)
}

// SafeDiv 除法保护，除数为 0 返回 fallback。
func SafeDiv(num, den, fallback float64) float64 {
	if den == 0 {
		return fallback
	}
	return num / den
}
