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

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/store"
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
