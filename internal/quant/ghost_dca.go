package quant

import "math"

// Ghost DCA 基准模拟器。
//
// 作为 GA 适应度评估的对照组：以相同起始资本与月度注资节奏
// 被动买入标的资产，记录 NAV 曲线。策略个体必须跑赢此基准（Alpha > 0）才算有效。
//
// 公式：Modified Dietz ROI
//   ROI = (期末权益 − 期初权益 − Σ现金流) / (期初权益 + Σ(现金流_i × 加权因子_i))
//   加权因子 = (总天数 − 现金流发生日) / 总天数

// GhostDCAConfig 基准参数。
type GhostDCAConfig struct {
	InitialCapitalUSDT float64 // 初始资本，第一根 bar 全部买入
	MonthlyInjectUSDT  float64 // 每自然月月初注资金额
}

// GhostDCAResult 基准回测结果。
type GhostDCAResult struct {
	FinalEquity   float64
	TotalInjected float64 // 不含 InitialCapital
	MaxDrawdown   float64
	ROI           float64 // Modified Dietz
	NAVSeries     []float64 // 与 bars 同长度
}

// SimulateGhostDCA 在 bars 上跑被动 DCA 策略。bars 按时间升序。
func SimulateGhostDCA(bars []Bar, cfg GhostDCAConfig) GhostDCAResult {
	result := GhostDCAResult{}
	if len(bars) == 0 || cfg.InitialCapitalUSDT <= 0 {
		return result
	}

	// 第一根 bar 把 InitialCapital 全部换成资产。
	assetHolding := cfg.InitialCapitalUSDT / bars[0].Close
	cashHolding := 0.0
	nav := make([]float64, len(bars))
	cashFlows := make([]struct {
		Amount float64
		Day    int
	}, 0, 16)

	totalDays := float64((bars[len(bars)-1].OpenTime-bars[0].OpenTime)/msPerDay) + 1
	if totalDays <= 0 {
		totalDays = 1
	}

	var prevYear, prevMonth int
	// 第一根月份作为起始月，不触发注资。
	prevYear, prevMonth, _ = civilFromMs(bars[0].OpenTime)

	var peak float64
	var maxDD float64

	for i, b := range bars {
		y, m, _ := civilFromMs(b.OpenTime)
		if (y != prevYear || m != prevMonth) && cfg.MonthlyInjectUSDT > 0 {
			// 新月份月初：注资并立即买入。
			assetHolding += cfg.MonthlyInjectUSDT / b.Close
			result.TotalInjected += cfg.MonthlyInjectUSDT
			dayOffset := int((b.OpenTime - bars[0].OpenTime) / msPerDay)
			cashFlows = append(cashFlows, struct {
				Amount float64
				Day    int
			}{cfg.MonthlyInjectUSDT, dayOffset})
			prevYear, prevMonth = y, m
		}

		equity := cashHolding + assetHolding*b.Close
		nav[i] = equity

		if equity > peak {
			peak = equity
		}
		if peak > 0 {
			dd := (peak - equity) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}

	result.NAVSeries = nav
	result.FinalEquity = nav[len(nav)-1]
	result.MaxDrawdown = maxDD

	// Modified Dietz ROI
	numerator := result.FinalEquity - cfg.InitialCapitalUSDT - result.TotalInjected
	denominator := cfg.InitialCapitalUSDT
	for _, cf := range cashFlows {
		w := (totalDays - float64(cf.Day)) / totalDays
		denominator += cf.Amount * w
	}
	if denominator > 0 {
		result.ROI = numerator / denominator
	}
	return result
}

// MaxDrawdown 基于 NAV 曲线计算峰值到谷底的最大相对回撤。
// 返回值在 [0, 1] 区间；空曲线或全零返回 0。
func MaxDrawdown(nav []float64) float64 {
	if len(nav) == 0 {
		return 0
	}
	var peak, maxDD float64
	for _, v := range nav {
		if v > peak {
			peak = v
		}
		if peak > 0 {
			dd := (peak - v) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	if math.IsNaN(maxDD) || math.IsInf(maxDD, 0) {
		return 0
	}
	return maxDD
}

// ModifiedDietzROI 通用 Modified Dietz 收益率计算。
// start, end：期初期末权益
// cashFlows：注资/提现（注资为正、提现为负）
// cashFlowDays：对应 cashFlows 的发生日（从 0 开始）
// totalDays：总天数
func ModifiedDietzROI(start, end float64, cashFlows []float64, cashFlowDays []int, totalDays int) float64 {
	if totalDays <= 0 {
		return 0
	}
	var sumCF, weightedCF float64
	for i, cf := range cashFlows {
		sumCF += cf
		if i < len(cashFlowDays) {
			w := float64(totalDays-cashFlowDays[i]) / float64(totalDays)
			weightedCF += cf * w
		}
	}
	den := start + weightedCF
	if den <= 0 {
		return 0
	}
	return (end - start - sumCF) / den
}
