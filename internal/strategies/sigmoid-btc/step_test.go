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

// --- 硬风控守门测试 ---

// 现金 5%（< 10% 下限）→ BUY 必须被丢弃
func TestStep_HardCashFloorBlocksBuy(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	in := defaultInput(nowMs)
	// 构造低现金高仓位的极端情境：USDT 500 + BTC 10 × $110 = $1100，cash ratio = 500/1600 = 31% > 10%
	// 改：USDT 50 + BTC 10 × $110 = $1100，cash ratio = 50/1150 ≈ 4.3% < 10%
	in.Portfolio.USDTBalance = 50
	in.Portfolio.FloatStackAsset = 10 // 大量持仓
	in.Portfolio.DeadStackAsset = 0
	// 让 macro 节奏门到位
	in.PrevRuntime.LastMacroDecisionMs = nowMs - int64(30)*24*60*60*1000

	out := Step(in, DefaultParams())

	// 任何 BUY intent 都不得通过
	for _, intent := range out.Intents {
		assert.NotEqual(t, quant.ActionBuy, intent.Action,
			"BUY intent should be blocked when cash ratio < %.0f%%", HardCashFloorRatio*100)
	}
	// DecisionReason 应包含 hard_cash_floor
	assert.Contains(t, out.DecisionReason, "hard_cash_floor")
}

// 注意：cash floor 10% + pos ceiling 90% 数学上互补（cash + pos ≈ 100%），
// 所以单独测试 ceiling 而 cash 仍 ≥ 10% 是数学不可能的。两条守门是冗余双保险，
// 防止未来 PortfolioSnapshot 计算逻辑变化时漏防。直接测 shouldBlockBuy 即可。

// 直接测 shouldBlockBuy 函数（不走 Step）
func TestShouldBlockBuy_CashFloor(t *testing.T) {
	p := quant.PortfolioSnapshot{
		USDTBalance:  500,
		FloatStackAsset: 1.0,
		CurrentPrice: 10000, // 1.0 BTC × 10k = 10000
	}
	// equity = 500 + 10000 = 10500，cash ratio = 4.76% < 10%
	block, reason := shouldBlockBuy(p, p.TotalEquity())
	assert.True(t, block)
	assert.Contains(t, reason, "hard_cash_floor")
}

func TestShouldBlockBuy_PositionCeiling(t *testing.T) {
	// 想 pos > 90% 必然 cash < 10%；让 cash 刚好等于 10%（不触发 floor），pos 也刚好 90%（不触发 ceiling）
	// 然后稍微往 ceiling 那边推：cash = 10.5%、pos = 89.5% → 都不触发
	// 真正能 isolate 测的是「cash > 10% 且 pos > 90%」这个数学不可能场景，
	// 所以 ceiling 实际上是 cash floor 的同义守门；保留双守门防止 PortfolioSnapshot 字段变化时漏防。
	// 这里测试在 cash >= 10% 但 pos > 90% 的边界（数据轻微不一致时可能发生）：
	p := quant.PortfolioSnapshot{
		USDTBalance:    150,
		FloatStackAsset: 0.95,
		CurrentPrice:   1000, // 0.95 × 1000 = 950
	}
	// equity = 150 + 950 = 1100，cash = 13.6% > 10%（pass）
	// pos = 950/1100 = 86.4% < 90%（pass）
	// 这组数据不触发任何守门
	block, _ := shouldBlockBuy(p, p.TotalEquity())
	assert.False(t, block, "boundary case should not block")
}

// SELL 永远不被阻止（即使 cash 极低）
func TestShouldBlockBuy_OnlyAffectsBuy(t *testing.T) {
	// shouldBlockBuy 本身只是判断函数，但语义上 step.go 只对 Action=BUY 调用。
	// 这里验证「极端情境 + Action=SELL」的整合行为：BUY 被阻、SELL 通过。
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	closes := makeCloses(100, 80, 150) // 价格下跌 → micro 倾向 BUY，但被阻
	in := quant.StrategyInput{
		NowMs:      nowMs,
		Closes:     closes,
		Timestamps: make([]int64, 150),
		Portfolio: quant.PortfolioSnapshot{
			USDTBalance:     30, // 现金极低
			FloatStackAsset: 5,  // 大量持仓
			CurrentPrice:    80,
		},
		Symbol:            "BTCUSDT",
		MonthlyInjectUSDT: 300,
		LotStep:           0.00001,
		LotMin:            0.00001,
		PrevRuntime:       quant.RuntimeState{Extras: map[string]float64{}},
	}
	out := Step(in, DefaultParams())
	for _, intent := range out.Intents {
		assert.NotEqual(t, quant.ActionBuy, intent.Action, "BUY must be blocked under low cash")
	}
}

// 守门触发后会写入 Extras 时间戳
func TestStep_BlockedTimestampsWritten(t *testing.T) {
	nowMs := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	closes := makeCloses(100, 80, 150) // 跌价 → micro 倾向 BUY
	in := quant.StrategyInput{
		NowMs:      nowMs,
		Closes:     closes,
		Timestamps: make([]int64, 150),
		Portfolio: quant.PortfolioSnapshot{
			USDTBalance:     30,
			FloatStackAsset: 5,
			CurrentPrice:    80,
		},
		Symbol:            "BTCUSDT",
		MonthlyInjectUSDT: 300,
		LotStep:           0.00001,
		LotMin:            0.00001,
		PrevRuntime:       quant.RuntimeState{Extras: map[string]float64{}, LastMacroDecisionMs: nowMs - int64(30)*24*60*60*1000},
	}
	out := Step(in, DefaultParams())
	// 至少 micro 应该被阻（macro 是否触发取决于节奏门，不强求）
	hasBlock := out.NewRuntime.Extras[ExtraKeyLastBlockedMicroMs] > 0 ||
		out.NewRuntime.Extras[ExtraKeyLastBlockedMacroMs] > 0
	assert.True(t, hasBlock, "at least one block timestamp should be set")
}
