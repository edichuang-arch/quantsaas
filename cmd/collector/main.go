// cmd/collector 历史 K 线补齐工具（一次性 CLI）。
//
// 用法：
//   # 补最近 365 天的 5m BTCUSDT 历史
//   ./bin/collector backfill -symbol BTCUSDT -interval 5m -days 365
//
//   # 指定起点（ISO 8601）
//   ./bin/collector backfill -symbol BTCUSDT -interval 5m -from 2024-01-01T00:00:00Z
//
//   # 查看当前 DB 中最新 bar 时间
//   ./bin/collector status -symbol BTCUSDT -interval 5m
//
// 环境变量：
//   DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME / DB_SSLMODE
//   （与 SaaS 相同；配置来源优先级：flag > env > config.yaml）
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edi/quantsaas/internal/collector"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/store"
	"go.uber.org/zap"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	os.Args = append(os.Args[:1], os.Args[2:]...)

	configPath := flag.String("config", "config.yaml", "path to saas config (for DB settings)")
	symbol := flag.String("symbol", "BTCUSDT", "trading pair")
	interval := flag.String("interval", "5m", "kline interval: 1m/5m/15m/1h/4h/1d")
	days := flag.Int("days", 0, "backfill this many days back from now")
	fromStr := flag.String("from", "", "backfill start time (RFC3339); overrides -days")
	flag.Parse()

	log, _ := zap.NewDevelopment()
	defer log.Sync()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}
	db, err := store.NewDB(cfg.Database)
	if err != nil {
		log.Fatal("open db", zap.Error(err))
	}
	defer db.Close()

	svc := collector.NewService(db, nil, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		<-sigs
		log.Info("interrupt; cancelling")
		cancel()
	}()

	switch cmd {
	case "backfill":
		runBackfill(ctx, svc, *symbol, *interval, *days, *fromStr, log)
	case "status":
		runStatus(ctx, svc, *symbol, *interval, log)
	default:
		usage()
		os.Exit(2)
	}
}

func runBackfill(ctx context.Context, svc *collector.Service, symbol, interval string, days int, fromStr string, log *zap.Logger) {
	var fromMs int64
	if fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			log.Fatal("invalid -from", zap.Error(err))
		}
		fromMs = t.UnixMilli()
	} else if days > 0 {
		fromMs = time.Now().AddDate(0, 0, -days).UnixMilli()
	} else {
		log.Fatal("must specify -days or -from")
	}

	start := time.Now()
	log.Info("backfill start",
		zap.String("symbol", symbol),
		zap.String("interval", interval),
		zap.Time("from", time.UnixMilli(fromMs)))

	ins, skp, err := svc.Backfill(ctx, symbol, interval, fromMs, 0)
	if err != nil {
		log.Fatal("backfill failed", zap.Error(err))
	}
	log.Info("backfill done",
		zap.Int("inserted", ins),
		zap.Int("skipped", skp),
		zap.Duration("elapsed", time.Since(start)))
}

func runStatus(ctx context.Context, svc *collector.Service, symbol, interval string, log *zap.Logger) {
	ts, err := svc.LatestOpenTime(ctx, symbol, interval)
	if err != nil {
		log.Fatal("query failed", zap.Error(err))
	}
	if ts == 0 {
		fmt.Printf("No data for %s %s\n", symbol, interval)
		return
	}
	fmt.Printf("Latest %s %s bar: %s (ts=%d)\n",
		symbol, interval, time.UnixMilli(ts).Format(time.RFC3339), ts)
}

func usage() {
	fmt.Fprintf(os.Stderr, `collector - QuantSaaS K-line backfill utility

Usage:
  collector backfill [flags]
  collector status   [flags]

Flags:
  -config string    SaaS config yaml (default "config.yaml")
  -symbol string    trading pair (default "BTCUSDT")
  -interval string  1m/5m/15m/1h/4h/1d (default "5m")
  -days int         backfill this many days back from now
  -from string      RFC3339 start time (overrides -days)

Examples:
  collector backfill -symbol BTCUSDT -interval 5m -days 30
  collector status   -symbol BTCUSDT -interval 5m
`)
}
