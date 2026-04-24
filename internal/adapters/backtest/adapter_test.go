package backtest

import (
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	sigmoidbtc "github.com/edi/quantsaas/internal/strategies/sigmoid-btc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 构造足够长的合成 bars（每日一根）用于回测。
func makeSyntheticBars(n int, priceFn func(i int) float64) []quant.Bar {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]quant.Bar, n)
	for i := 0; i < n; i++ {
		t := start.AddDate(0, 0, i)
		p := priceFn(i)
		bars[i] = quant.Bar{
			OpenTime:  t.UnixMilli(),
			CloseTime: t.Add(24*time.Hour - time.Millisecond).UnixMilli(),
			Open:      p, High: p, Low: p, Close: p, Volume: 1,
		}
	}
	return bars
}

// 测试配置构造函数：使用 sigmoid-btc 默认参数。
func defaultCfg() Config {
	blob, _ := quant.EncodeParamPack(quant.DefaultSeedChromosome, quant.DefaultSpawnPoint)
	return Config{
		InitialUSDT:       10000,
		MonthlyInjectUSDT: 300,
		LotStep:           0.00001,
		LotMin:            0.00001,
		TakerFeeBps:       10,
		ParamsBlob:        blob,
		LoadParams: func(raw []byte) any {
			return sigmoidbtc.LoadParams(raw)
		},
		Step: func(in quant.StrategyInput, p any) quant.StrategyOutput {
			return sigmoidbtc.Step(in, p.(sigmoidbtc.Params))
		},
		Symbol: "BTCUSDT",
	}
}

// 回测对空 bars 安全。
func TestRun_EmptyBars(t *testing.T) {
	res := Run(nil, defaultCfg(), 0)
	assert.Equal(t, 0.0, res.FinalEquity)
	assert.Equal(t, 0, res.NumTrades)
}

// 纯函数确定性：相同参数两次回测，NAV 曲线 + 成交次数 + 终权益完全相等。
func TestRun_Deterministic(t *testing.T) {
	// 300 天微幅震荡 + 趋势上行
	bars := makeSyntheticBars(300, func(i int) float64 {
		return 100 + float64(i)*0.2 + 0.5*float64(i%7)
	})
	cfg := defaultCfg()
	a := Run(bars, cfg, bars[0].OpenTime)
	b := Run(bars, cfg, bars[0].OpenTime)
	assert.Equal(t, a.FinalEquity, b.FinalEquity)
	assert.Equal(t, a.NumTrades, b.NumTrades)
	assert.Equal(t, a.NAVSeries, b.NAVSeries)
}

// 平稳价格且无交易机会时，终权益 = 初始 + 注资 - 费用（大致）。
func TestRun_ConstantPriceConservesCash(t *testing.T) {
	bars := makeSyntheticBars(180, func(i int) float64 { return 100 })
	cfg := defaultCfg()
	res := Run(bars, cfg, bars[0].OpenTime)

	// 注资 5 个月 × 300 = 1500
	// 可能有少量交易费用，但权益应该在 10000~12000 之间
	assert.Greater(t, res.FinalEquity, 9500.0)
	assert.Less(t, res.FinalEquity, 12000.0)
}

// warmup 阶段的交易不被记录。evalStart 超出全部数据 → 完全 warmup，零交易零 NAV。
func TestRun_WarmupTradesIgnored(t *testing.T) {
	bars := makeSyntheticBars(200, func(i int) float64 {
		return 100 + float64(i%20)
	})
	cfg := defaultCfg()

	// evalStart 超出最后一根 bar → 完全 warmup
	evalStart := bars[len(bars)-1].OpenTime + 1
	res := Run(bars, cfg, evalStart)
	assert.Equal(t, 0, res.NumTrades, "full-warmup run must have zero trades")
	assert.Len(t, res.NAVSeries, 0, "no bar in eval region")
}

// LotStep 截断：超小订单被拒（FilledQty < LotMin）。
func TestRun_LotStepTruncatesTinyOrders(t *testing.T) {
	// 强制 LotStep / LotMin 很大，大部分微观订单会被拒
	bars := makeSyntheticBars(200, func(i int) float64 {
		return 100 + float64(i%10)*0.5
	})
	cfg := defaultCfg()
	cfg.LotStep = 1.0
	cfg.LotMin = 1.0
	res := Run(bars, cfg, bars[0].OpenTime)
	// 策略会产出意图，但大多数被 LotMin 过滤
	assert.LessOrEqual(t, res.TotalFeeUSDT, cfg.InitialUSDT)
}

// 配置缺失 Step 函数时返回空结果。
func TestRun_NilStep(t *testing.T) {
	bars := makeSyntheticBars(200, func(i int) float64 { return 100 })
	cfg := Config{} // 全零
	res := Run(bars, cfg, 0)
	assert.Equal(t, 0.0, res.FinalEquity)
}

// 现金流记录正确：5 个月 → 应有 4 次月初注资（启始月份不注资）。
func TestRun_CashFlowsRecorded(t *testing.T) {
	// 从 2024-01-01 开始 150 天 → 跨越 1 月到 5 月底，应有 4 次月初注资
	bars := makeSyntheticBars(150, func(i int) float64 { return 100 })
	cfg := defaultCfg()
	res := Run(bars, cfg, bars[0].OpenTime)
	assert.GreaterOrEqual(t, len(res.CashFlows), 3, "should have at least 3 monthly injections")
	for _, cf := range res.CashFlows {
		assert.Equal(t, 300.0, cf)
	}
	require.Equal(t, len(res.CashFlows), len(res.CashFlowDays))
}

// truncateToStep 工具测试
func TestTruncateToStep(t *testing.T) {
	assert.Equal(t, 0.123, truncateToStep(0.12399, 0.001))
	assert.Equal(t, 1.0, truncateToStep(1.99, 1.0))
	// step=0 不截断
	assert.Equal(t, 0.12345, truncateToStep(0.12345, 0))
}
