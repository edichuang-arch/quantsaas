package quant

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildCloses 构造一条长度 >= MicroVolRatioLongBars 的收盘价序列。
// baseline 是前 90% 的常数价；tail 是最后 10% 的价格，用于模拟近期趋势。
// 当 tail == baseline 时，EMA 恰好等于 baseline，Signal 为 0。
func buildCloses(baseline, tail float64, length int) []float64 {
	if length < MicroVolRatioLongBars {
		length = MicroVolRatioLongBars + 10
	}
	out := make([]float64, length)
	cut := int(float64(length) * 0.9)
	for i := 0; i < cut; i++ {
		out[i] = baseline
	}
	for i := cut; i < length; i++ {
		out[i] = tail
	}
	return out
}

// 性质 1：Signal > 0（价格高于均线）→ TargetWeight < 0.5（减仓倾向）
func TestSigmoid_PositiveSignalReducesTarget(t *testing.T) {
	closes := buildCloses(100, 120, 150) // 近期拉升
	in := MicroInput{
		Closes:         closes,
		CurrentPrice:   120,
		TotalEquity:    10000,
		CurrentWeight:  0.5,
		Beta:           1.5,
		Gamma:          1.0,
		SigmaFloor:     0.01,
		BetaMultiplier: 1.0,
	}
	out := ComputeMicroDecision(in)
	require.False(t, out.Skipped, "should not skip: %s", out.SkipReason)
	assert.Greater(t, out.Signal, 0.0, "signal should be positive")
	assert.Less(t, out.TargetWeight, 0.5, "target weight should be < 0.5")
}

// 性质 2：Signal < 0（价格低于均线）→ TargetWeight > 0.5（加仓倾向）
func TestSigmoid_NegativeSignalRaisesTarget(t *testing.T) {
	closes := buildCloses(100, 80, 150) // 近期下跌
	in := MicroInput{
		Closes:         closes,
		CurrentPrice:   80,
		TotalEquity:    10000,
		CurrentWeight:  0.5,
		Beta:           1.5,
		Gamma:          1.0,
		SigmaFloor:     0.01,
		BetaMultiplier: 1.0,
	}
	out := ComputeMicroDecision(in)
	require.False(t, out.Skipped)
	assert.Less(t, out.Signal, 0.0)
	assert.Greater(t, out.TargetWeight, 0.5)
}

// 性质 3：CurrentWeight = 0.5 且 Signal = 0 且 Gamma > 0 → TargetWeight = 0.5
// 即：仓位恰好在中性点时偏置为零。
func TestSigmoid_NeutralWeightGivesNeutralTarget(t *testing.T) {
	closes := buildCloses(100, 100, 150) // 常数
	in := MicroInput{
		Closes:         closes,
		CurrentPrice:   100,
		TotalEquity:    10000,
		CurrentWeight:  0.5,
		Beta:           1.5,
		Gamma:          2.0, // gamma > 0 有启用
		SigmaFloor:     0.1, // 防止 sigma = 0 → skip
		BetaMultiplier: 1.0,
	}
	out := ComputeMicroDecision(in)
	require.False(t, out.Skipped, "should not skip: %s", out.SkipReason)
	assert.InDelta(t, 0.0, out.Signal, 1e-9)
	assert.InDelta(t, 0.5, out.TargetWeight, 1e-9)
}

// 性质 4：纯函数确定性 — 相同输入两次调用必须产出完全相同输出。
func TestSigmoid_Deterministic(t *testing.T) {
	closes := buildCloses(100, 105, 150)
	mkIn := func() MicroInput {
		return MicroInput{
			Closes:         closes,
			CurrentPrice:   105,
			TotalEquity:    10000,
			CurrentWeight:  0.3,
			Beta:           1.5,
			Gamma:          1.0,
			SigmaFloor:     0.01,
			BetaMultiplier: 1.2,
		}
	}
	a := ComputeMicroDecision(mkIn())
	b := ComputeMicroDecision(mkIn())
	assert.Equal(t, a, b, "identical inputs must produce identical outputs")
}

// 性质 5：IsQuiet=true 且 |TheoreticalUSD| < 10.1 → OrderUSD = 0
func TestSigmoid_QuietDustOrderSilenced(t *testing.T) {
	// 构造 TheoreticalUSD 在 (0, 10.1) 内的场景：CurrentWeight=0.45, 常数价, Gamma>0
	closes := buildCloses(100, 100, 150)
	in := MicroInput{
		Closes:         closes,
		CurrentPrice:   100,
		TotalEquity:    50, // 小总权益，确保 DeltaWeight × Equity 很小
		CurrentWeight:  0.45,
		Beta:           1.5,
		Gamma:          1.0,
		SigmaFloor:     0.1,
		BetaMultiplier: 1.0,
		IsQuiet:        true,
	}
	out := ComputeMicroDecision(in)
	require.False(t, out.Skipped)
	require.Less(t, math.Abs(out.TheoreticalUSD), MicroMinOrderUSDT, "precondition: theoretical < min")
	require.Greater(t, math.Abs(out.TheoreticalUSD), 0.0, "precondition: theoretical > 0")
	assert.Equal(t, 0.0, out.OrderUSD, "quiet dust must be zero")
}

// 性质 6：IsQuiet=false 且满足楔形突破（|DeltaWeight| ≥ 0.02）→ OrderUSD = ±MicroMinOrderUSDT
func TestSigmoid_WedgeForcesMinOrder(t *testing.T) {
	// CurrentWeight=0.45，常数价 → Signal=0，InventoryBias=-0.05
	// Exponent = -0.05 × 1 = -0.05；TargetWeight ≈ 0.5125；DeltaWeight ≈ 0.0625 (>= 0.02)
	// TotalEquity=50 → TheoreticalUSD ≈ 3.125 < 10.1
	closes := buildCloses(100, 100, 150)
	in := MicroInput{
		Closes:         closes,
		CurrentPrice:   100,
		TotalEquity:    50,
		CurrentWeight:  0.45,
		Beta:           1.5,
		Gamma:          1.0,
		SigmaFloor:     0.1,
		BetaMultiplier: 1.0,
		IsQuiet:        false,
	}
	out := ComputeMicroDecision(in)
	require.False(t, out.Skipped)
	require.Greater(t, math.Abs(out.DeltaWeight), MicroWedgeDeltaThreshold, "precondition: wedge delta")
	require.Less(t, math.Abs(out.TheoreticalUSD), MicroMinOrderUSDT)
	assert.InDelta(t, MicroMinOrderUSDT, math.Abs(out.OrderUSD), 1e-9, "must force min order")
	assert.Greater(t, out.OrderUSD, 0.0, "direction should be positive (BUY)")
}

// 额外：数据不足时必须跳过且不输出订单
func TestSigmoid_InsufficientDataSkipped(t *testing.T) {
	in := MicroInput{
		Closes:        []float64{100, 101, 102},
		CurrentPrice:  102,
		TotalEquity:   1000,
		CurrentWeight: 0.5,
		Beta:          1.5,
	}
	out := ComputeMicroDecision(in)
	assert.True(t, out.Skipped)
	assert.Equal(t, 0.0, out.OrderUSD)
}

// 额外：sigma = 0（常数序列且 SigmaFloor=0）时必须跳过
func TestSigmoid_ZeroSigmaSkipped(t *testing.T) {
	closes := buildCloses(100, 100, 150)
	in := MicroInput{
		Closes:        closes,
		CurrentPrice:  100,
		TotalEquity:   1000,
		CurrentWeight: 0.5,
		Beta:          1.0,
		SigmaFloor:    0, // 不保护 → 常数序列 sigma=0
	}
	out := ComputeMicroDecision(in)
	assert.True(t, out.Skipped)
	assert.Contains(t, out.SkipReason, "sigma")
}

// 额外：BetaMultiplier 确实放大 exponent 的影响
func TestSigmoid_BetaMultiplierAmplifies(t *testing.T) {
	closes := buildCloses(100, 120, 150)
	base := MicroInput{
		Closes: closes, CurrentPrice: 120, TotalEquity: 10000, CurrentWeight: 0.5,
		Beta: 1.0, Gamma: 0, SigmaFloor: 0.01,
	}
	base.BetaMultiplier = 1.0
	baseline := ComputeMicroDecision(base)

	base.BetaMultiplier = 3.0
	amplified := ComputeMicroDecision(base)

	// Signal > 0 → 减仓；放大后应更激进（更低 target）
	assert.Less(t, amplified.TargetWeight, baseline.TargetWeight)
}
