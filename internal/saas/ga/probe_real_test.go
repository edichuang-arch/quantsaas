//go:build gaprobe
// +build gaprobe

// 真实 BTC K 线 GA gen-0 探测。
//
// 跟 probe_test.go 不同：这里读运行中的 Postgres，用真实 365 天 BTC 5m K 线
// 跑 50 个 Sample()，对比 InitBounds vs HardBounds 的 fatal 率与 fitness 分布。
//
// 跑法：
//
//	docker compose up -d postgres
//	export DB_PASSWORD=quantsaas-dev-pw
//	go test -tags=gaprobe -timeout 600s ./internal/saas/ga -run TestProbeReal_InitVsHard -v
//
// 这个 test 是只读的（绝不写 DB）。
package ga

import (
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/adapters/backtest"
	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/store"
	sigmoidbtc "github.com/edi/quantsaas/internal/strategies/sigmoid-btc"
	"github.com/stretchr/testify/require"
)

func TestProbeReal_InitVsHard(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host: getenv("DB_HOST", "localhost"),
		Port: 5432,
		User: getenv("DB_USER", "quantsaas"),
		Password: getenv("DB_PASSWORD", "quantsaas-dev-pw"),
		DBName: getenv("DB_NAME", "quantsaas"),
		SSLMode: "disable",
	}
	db, err := store.NewDB(cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer func() { _ = db.Close() }()

	// 拉真实 5m K 线（365 天 ~ 105k 根，回测足够）
	var rows []store.KLine
	require.NoError(t, db.Where("symbol = ? AND interval = ?", "BTCUSDT", "5m").
		Order("open_time ASC").
		Find(&rows).Error)
	require.Greater(t, len(rows), 240*288, "need at least ~240d of 5m bars (warmup 60 + 6m eval)")

	bars := make([]quant.Bar, len(rows))
	for i, r := range rows {
		bars[i] = quant.Bar{
			OpenTime: r.OpenTime, CloseTime: r.CloseTime,
			Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		}
	}
	t.Logf("loaded %d real BTC 5m bars (%.1f days)", len(bars), float64(len(bars))/288.0)

	// warmup 60 天，让 6m 窗口能跑（365 天数据不够 1200 天默认 warmup）
	plan := BuildEvaluablePlan(
		bars, "BTCUSDT", "sigmoid-btc",
		0.00001, 0.00001, 10000, 300, nil, 60,
	)
	require.NotNil(t, plan)
	require.NotEmpty(t, plan.Windows)
	t.Logf("plan windows: %d", len(plan.Windows))
	for _, w := range plan.Windows {
		t.Logf("  window %s: %d bars, weight %.2f", w.Label, len(w.Bars), w.Weight)
	}

	// N=10 而非 50：100k bar × 50 个体的回测在普通机器要 10+ 分钟。
	// 想要更稳定的统计可改大，但记得 timeout 也要跟着开。
	const N = 10

	t.Run("InitBounds", func(t *testing.T) { reportProbe(t, plan, N, true) })
	t.Run("HardBounds", func(t *testing.T) { reportProbe(t, plan, N, false) })
}

func reportProbe(t *testing.T, plan *EvaluablePlan, N int, useInit bool) {
	t.Helper()
	ev := NewSigmoidBTCEvolvable()
	rng := rand.New(rand.NewSource(20240101))

	fatalCnt := 0
	scores := make([]float64, 0, N)
	maxDDs := make([]float64, 0, N)
	for i := 0; i < N; i++ {
		var g Gene
		if useInit {
			g = ev.Sample(rng)
		} else {
			g = sampleFromHardBounds(rng)
		}
		res := ev.Evaluate(plan, g)
		maxDDs = append(maxDDs, res.MaxDrawdown)
		if res.Fatal {
			fatalCnt++
			continue
		}
		scores = append(scores, res.ScoreTotal)
	}
	fatalPct := 100.0 * float64(fatalCnt) / float64(N)
	sort.Float64s(scores)
	sort.Float64s(maxDDs)

	label := "HardBounds"
	if useInit {
		label = "InitBounds"
	}
	t.Logf("=== %s on real BTC (N=%d) ===", label, N)
	t.Logf("  fatal:           %d/%d = %.1f%%", fatalCnt, N, fatalPct)
	t.Logf("  non-fatal score: %s", summarizeStats(scores))
	if len(scores) > 0 {
		t.Logf("  median score:    %.4f", scores[len(scores)/2])
	}
	t.Logf("  MaxDD all:       %s", summarizeStats(maxDDs))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// TestProbeReal_DefaultSeedOnly 单独跑 DefaultSeedChromosome 看真实 BTC 回测结果。
// 用于验证：是策略本身 fatal，还是 GA 流程的 bug 让 best 个体含 SigmaFloorPct=0。
//
// 跑法：go test -tags=gaprobe -timeout 600s ./internal/saas/ga -run TestProbeReal_DefaultSeedOnly -v
func TestProbeReal_DefaultSeedOnly(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     getenv("DB_HOST", "localhost"),
		Port:     5432,
		User:     getenv("DB_USER", "quantsaas"),
		Password: getenv("DB_PASSWORD", "quantsaas-dev-pw"),
		DBName:   getenv("DB_NAME", "quantsaas"),
		SSLMode:  "disable",
	}
	db, err := store.NewDB(cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer func() { _ = db.Close() }()

	var rows []store.KLine
	require.NoError(t, db.Where("symbol = ? AND interval = ?", "BTCUSDT", "5m").
		Order("open_time ASC").Find(&rows).Error)

	bars := make([]quant.Bar, len(rows))
	for i, r := range rows {
		bars[i] = quant.Bar{
			OpenTime: r.OpenTime, CloseTime: r.CloseTime,
			Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		}
	}
	t.Logf("loaded %d real BTC 5m bars", len(bars))

	plan := BuildEvaluablePlan(
		bars, "BTCUSDT", "sigmoid-btc",
		0.00001, 0.00001, 10000, 300, nil, 60,
	)
	require.NotNil(t, plan)
	t.Logf("windows: %d", len(plan.Windows))
	for _, w := range plan.Windows {
		t.Logf("  %s: %d bars (weight %.2f)", w.Label, len(w.Bars), w.Weight)
	}

	ev := NewSigmoidBTCEvolvable()
	seed := quant.DefaultSeedChromosome
	t.Logf("running DefaultSeed: SigmaFloorPct=%.6f, Beta=%.2f, Gamma=%.2f, MicroReservePct=%.2f",
		seed.SigmaFloorPct, seed.Beta, seed.Gamma, seed.MicroReservePct)

	res := ev.Evaluate(plan, seed)

	t.Logf("=== DefaultSeed evaluation result ===")
	t.Logf("  Fatal:        %v", res.Fatal)
	t.Logf("  ScoreTotal:   %.4f", res.ScoreTotal)
	t.Logf("  MaxDrawdown:  %.4f (%.2f%%)", res.MaxDrawdown, res.MaxDrawdown*100)
	for _, r := range res.Results {
		t.Logf("  window %-5s ROI=%.4f MaxDD=%.4f Alpha=%.4f Score=%.4f Fatal=%v",
			r.Label, r.ROI, r.MaxDrawdown, r.Alpha, r.Score, r.Fatal)
	}

	if res.Fatal {
		t.Logf(">>> CONCLUSION: strategy itself is fatal on this 365d BTC data")
		t.Logf("    SigmaFloorPct fix + hard guards do NOT save backtest")
		t.Logf("    But guards still protect live testnet from death spiral")
	} else {
		t.Logf(">>> CONCLUSION: DefaultSeed is NOT fatal — task 4 had a GA-flow bug")
	}
}

// TestProbeReal_DefaultSeedWithDiagLog 用 OnBar callback 跟踪 6m 窗口每根 bar 的
// USDT/BTC/Equity，找 equity 第一次跌破 50%/80%/90% peak 的具体时点。
//
// 跑法：go test -tags=gaprobe -timeout 600s ./internal/saas/ga -run TestProbeReal_DefaultSeedWithDiagLog -v
func TestProbeReal_DefaultSeedWithDiagLog(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     getenv("DB_HOST", "localhost"),
		Port:     5432,
		User:     getenv("DB_USER", "quantsaas"),
		Password: getenv("DB_PASSWORD", "quantsaas-dev-pw"),
		DBName:   getenv("DB_NAME", "quantsaas"),
		SSLMode:  "disable",
	}
	db, err := store.NewDB(cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer func() { _ = db.Close() }()

	var rows []store.KLine
	require.NoError(t, db.Where("symbol = ? AND interval = ?", "BTCUSDT", "5m").
		Order("open_time ASC").Find(&rows).Error)

	bars := make([]quant.Bar, len(rows))
	for i, r := range rows {
		bars[i] = quant.Bar{
			OpenTime: r.OpenTime, CloseTime: r.CloseTime,
			Open: r.Open, High: r.High, Low: r.Low, Close: r.Close, Volume: r.Volume,
		}
	}
	t.Logf("loaded %d bars", len(bars))

	// 取 6m 窗口（fatal 在这里触发）
	plan := BuildEvaluablePlan(
		bars, "BTCUSDT", "sigmoid-btc",
		0.00001, 0.00001, 10000, 300, nil, 60,
	)
	require.NotEmpty(t, plan.Windows)
	var win6m quant.CrucibleWindow
	for _, w := range plan.Windows {
		if w.Label == "6m" {
			win6m = w
			break
		}
	}
	require.NotEqual(t, "", win6m.Label, "6m window not found")
	t.Logf("6m window: %d bars, evalStartMs=%d", len(win6m.Bars), win6m.EvalStartMs)

	// 编码 DefaultSeed 成 ParamPack
	seed := quant.DefaultSeedChromosome
	paramsBlob, err := quant.EncodeParamPack(seed, quant.DefaultSpawnPoint)
	require.NoError(t, err)

	// 收集诊断快照
	var (
		peakEquity         float64
		drop50Idx, drop80Idx, drop90Idx int = -1, -1, -1
		drop50Snap, drop80Snap, drop90Snap backtest.BarDiag
		firstEvalSnap      backtest.BarDiag
		firstEvalRecorded  bool
		lastSnap           backtest.BarDiag
		samples            []backtest.BarDiag // 每 5000 根存一笔
	)

	bcfg := backtest.Config{
		InitialUSDT:       10000,
		MonthlyInjectUSDT: 300,
		LotStep:           0.00001,
		LotMin:            0.00001,
		TakerFeeBps:       10,
		ParamsBlob:        paramsBlob,
		LoadParams:        func(raw []byte) any { return sigmoidbtc.LoadParams(raw) },
		Step: func(in quant.StrategyInput, p any) quant.StrategyOutput {
			return sigmoidbtc.Step(in, p.(sigmoidbtc.Params))
		},
		Symbol: "BTCUSDT",
		OnBar: func(d backtest.BarDiag) {
			if !d.IsEvalRange {
				return // warmup 期间 NAV 不计
			}
			if !firstEvalRecorded {
				firstEvalSnap = d
				firstEvalRecorded = true
			}
			if d.Equity > peakEquity {
				peakEquity = d.Equity
			}
			if peakEquity > 0 {
				dd := (peakEquity - d.Equity) / peakEquity
				if dd >= 0.50 && drop50Idx < 0 {
					drop50Idx = d.I
					drop50Snap = d
				}
				if dd >= 0.80 && drop80Idx < 0 {
					drop80Idx = d.I
					drop80Snap = d
				}
				if dd >= 0.90 && drop90Idx < 0 {
					drop90Idx = d.I
					drop90Snap = d
				}
			}
			if d.I%5000 == 0 {
				samples = append(samples, d)
			}
			lastSnap = d
		},
	}

	res := backtest.Run(win6m.Bars, bcfg, win6m.EvalStartMs)

	t.Logf("=== Backtest result ===")
	t.Logf("  FinalEquity:  %.2f", res.FinalEquity)
	t.Logf("  MaxDrawdown:  %.4f (%.2f%%)", res.MaxDrawdown, res.MaxDrawdown*100)
	t.Logf("  NumTrades:    %d", res.NumTrades)
	t.Logf("  TotalFee:     %.2f", res.TotalFeeUSDT)
	t.Logf("  TotalDays:    %d", res.TotalDays)
	t.Logf("  CashFlows:    %v", res.CashFlows)
	t.Logf("")
	t.Logf("=== First eval bar ===")
	t.Logf("  i=%d t=%s price=%.2f USDT=%.2f Float=%.5f Equity=%.2f",
		firstEvalSnap.I, msToTime(firstEvalSnap.OpenTime),
		firstEvalSnap.Price, firstEvalSnap.USDTBalance,
		firstEvalSnap.FloatStack, firstEvalSnap.Equity)
	t.Logf("=== Peak equity ===")
	t.Logf("  %.2f", peakEquity)
	t.Logf("")
	t.Logf("=== Drawdown waypoints ===")
	if drop50Idx >= 0 {
		t.Logf("  -50%%  at i=%d t=%s price=%.2f USDT=%.2f Float=%.5f Dead=%.5f Equity=%.2f",
			drop50Snap.I, msToTime(drop50Snap.OpenTime),
			drop50Snap.Price, drop50Snap.USDTBalance,
			drop50Snap.FloatStack, drop50Snap.DeadStack, drop50Snap.Equity)
	}
	if drop80Idx >= 0 {
		t.Logf("  -80%%  at i=%d t=%s price=%.2f USDT=%.2f Float=%.5f Dead=%.5f Equity=%.2f",
			drop80Snap.I, msToTime(drop80Snap.OpenTime),
			drop80Snap.Price, drop80Snap.USDTBalance,
			drop80Snap.FloatStack, drop80Snap.DeadStack, drop80Snap.Equity)
	}
	if drop90Idx >= 0 {
		t.Logf("  -90%%  at i=%d t=%s price=%.2f USDT=%.2f Float=%.5f Dead=%.5f Equity=%.2f",
			drop90Snap.I, msToTime(drop90Snap.OpenTime),
			drop90Snap.Price, drop90Snap.USDTBalance,
			drop90Snap.FloatStack, drop90Snap.DeadStack, drop90Snap.Equity)
	}
	t.Logf("")
	t.Logf("=== Last bar ===")
	t.Logf("  i=%d t=%s price=%.2f USDT=%.2f Float=%.5f Dead=%.5f Equity=%.2f",
		lastSnap.I, msToTime(lastSnap.OpenTime),
		lastSnap.Price, lastSnap.USDTBalance,
		lastSnap.FloatStack, lastSnap.DeadStack, lastSnap.Equity)
	t.Logf("")
	t.Logf("=== Samples every 5000 bars ===")
	for _, s := range samples {
		t.Logf("  i=%-7d t=%s price=%.2f USDT=%.2f Float=%.5f Dead=%.5f Equity=%.2f",
			s.I, msToTime(s.OpenTime),
			s.Price, s.USDTBalance, s.FloatStack, s.DeadStack, s.Equity)
	}
}

func msToTime(ms int64) string {
	return time.Unix(ms/1000, 0).UTC().Format("2006-01-02 15:04")
}
