package quant

// 本文件实现"现货策略 ACL 外圈"的 OHLCV 降级。
// 铁律：现货策略内核（internal/strategies/*）禁止直接依赖 Bar；
// 必须在调用 Step() 前通过下列函数将 []Bar 降级为 []float64 + []int64。

// ExtractCloses 从 bars 中提取收盘价序列。bars 为 nil 或空时返回空切片。
func ExtractCloses(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Close
	}
	return out
}

// ExtractTimestamps 提取与 ExtractCloses 一一对应的 OpenTime（毫秒）序列。
func ExtractTimestamps(bars []Bar) []int64 {
	out := make([]int64, len(bars))
	for i, b := range bars {
		out[i] = b.OpenTime
	}
	return out
}

// ExtractVolumes 提取成交量序列（如策略需要）。
func ExtractVolumes(bars []Bar) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.Volume
	}
	return out
}

// LastClose 返回最后一根 bar 的收盘价，空切片返回 0。
func LastClose(bars []Bar) float64 {
	if len(bars) == 0 {
		return 0
	}
	return bars[len(bars)-1].Close
}

// LastOpenTime 返回最后一根 bar 的 OpenTime。
func LastOpenTime(bars []Bar) int64 {
	if len(bars) == 0 {
		return 0
	}
	return bars[len(bars)-1].OpenTime
}
