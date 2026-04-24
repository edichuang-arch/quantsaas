package sigmoidbtc

import (
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeCloses 生成一条长度足以通过 Step 数据窗口检查的收盘价序列。
func makeCloses(baseline, latest float64, n int) []float64 {
	out := make([]float64, n)
	for i := 0; i < n-1; i++ {
		out[i] = baseline
	}
	out[n-1] = latest
	return out
}

func defaultInput(nowMs int64) quant.StrategyInput {
	closes := makeCloses(100, 110, 150)
	return quant.StrategyInput{
		NowMs:              nowMs,
		Closes:             closes,
		Timestamps:         make([]int64, 150),
		Portfolio: quant.PortfolioSnapshot{
			USDTBalance:     1000,
			FloatStackAsset: 0.05,
			DeadStackAsset:  0.10,
			CurrentPrice:    110,
		},
		Symbol:            "BTCUSDT",
		MonthlyInjectUSDT: 300,
		LotStep:           0.00001,
		LotMin:            0.00001,
		PrevRuntime:       quant.RuntimeState{Extras: map[string]float64{}},
	}
}

// 数据不足时必须跳过，输出 SkipReason，不产生任何 intent。
func TestStep_InsufficientDataSkips(t *testing.T) {
	in := quant.StrategyInput{
		NowMs:  time.Now().UTC().UnixMilli(),
		Closes: []float64{100, 101, 102},
		Portfolio: quant.PortfolioSnapshot{
			USDTBalance:  1000,
			CurrentPrice: 102,
		},
	}
	out := Step(in, DefaultParams())
	assert.NotEmpty(t, out.SkipReason)
	assert.Empty(t, out.Intents)
}

// CurrentPrice <= 0 时跳过（防御性检查）。
func TestStep_InvalidPriceSkips(t *testing.T) {
	in := defaultInput(time.Now().UTC().UnixMilli())
	in.Portfolio.CurrentPrice = 0
	out := Step(in, DefaultParams())
	assert.Contains(t, out.SkipReason, "price")
}

// 纯函数确定性：两次相同输入必须产出完全相同输出。
func TestStep_Deterministic(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)
	params := DefaultParams()
	a := Step(in, params)
	b := Step(in, params)
	assert.Equal(t, a, b)
}

// 铁律：Macro 产出的所有 intent 必须是 BUY + DEAD_STACK。
func TestStep_MacroIntentIsAlwaysBuyDeadStack(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)
	// 触发宏观决策：LastMacroDecisionMs 距今很久
	in.PrevRuntime.LastMacroDecisionMs = nowMs - int64(30)*24*60*60*1000
	in.PrevRuntime.MonthAnchorMs = nowMs - int64(5)*24*60*60*1000

	out := Step(in, DefaultParams())
	for _, intent := range out.Intents {
		if intent.Engine == quant.EngineMacro {
			assert.Equal(t, quant.ActionBuy, intent.Action)
			assert.Equal(t, quant.LotDeadStack, intent.LotType)
			assert.Greater(t, intent.AmountUSDT, 0.0)
		}
	}
}

// RuntimeState 正确推进：LastProcessedBarTime 应更新到 NowMs。
func TestStep_LastProcessedBarTimeUpdated(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)
	out := Step(in, DefaultParams())
	assert.Equal(t, nowMs, out.NewRuntime.LastProcessedBarTime)
}

// MicroReservePct 保留效果验证：Spendable 被正确扣除。
func TestStep_MicroReserveReducesSpendable(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)

	// 检查 computeSpendableUSDT 是否按 MicroReservePct 扣减
	params := DefaultParams()
	spendable := computeSpendableUSDT(in.Portfolio, params.Chromosome)
	expected := in.Portfolio.USDTBalance * (1 - params.Chromosome.MicroReservePct)
	assert.InDelta(t, quant.RoundToUSDT(expected), spendable, 0.01)
}

// 总下单金额不得超过 Spendable。
func TestStep_TotalOrderWithinSpendable(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)
	in.Portfolio.USDTBalance = 50 // 故意压很小
	in.PrevRuntime.LastMacroDecisionMs = nowMs - int64(30)*24*60*60*1000

	out := Step(in, DefaultParams())
	var totalBuy float64
	for _, intent := range out.Intents {
		if intent.Action == quant.ActionBuy {
			totalBuy += intent.AmountUSDT
		}
	}
	spendable := computeSpendableUSDT(in.Portfolio, DefaultParams().Chromosome)
	assert.LessOrEqual(t, totalBuy, spendable+0.01)
}

// Manifest 构建不报错且有合理 ParamPack。
func TestBuildManifest(t *testing.T) {
	m, err := BuildManifest()
	require.NoError(t, err)
	assert.Equal(t, StrategyID, m.ID)
	assert.Equal(t, "binance", m.Exchange)
	assert.Equal(t, "BTCUSDT", m.Symbol)
	assert.True(t, m.IsSpot)
	assert.Greater(t, len(m.ParamPackDefault), 0)

	// ParamPackDefault 应能被 DecodeParamPack 还原
	c, _ := quant.DecodeParamPack(m.ParamPackDefault)
	assert.Equal(t, quant.DefaultSeedChromosome, c)
}

// LoadParams 对 nil blob 的回退行为。
func TestLoadParams_NilBlobFallsBack(t *testing.T) {
	p := LoadParams(nil)
	assert.Equal(t, quant.DefaultSeedChromosome, p.Chromosome)
	assert.Equal(t, quant.DefaultSpawnPoint, p.Spawn)
}

// ensureExtras 幂等性
func TestEnsureExtras(t *testing.T) {
	rt := quant.RuntimeState{}
	got := ensureExtras(rt)
	assert.NotNil(t, got.Extras)

	got.Extras["foo"] = 1.23
	got2 := ensureExtras(got)
	assert.Equal(t, 1.23, got2.Extras["foo"])
}
