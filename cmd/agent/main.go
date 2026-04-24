// cmd/agent 是 LocalAgent 本地执行端的入口。
//
// 启动流程：
//   1. 读 config.agent.yaml（路径由 --config flag 指定）
//   2. 初始化 Binance 客户端
//   3. 启动 WebSocket 主循环（含自动重连）
//   4. 监听 SIGTERM/SIGINT 优雅退出
//
// 铁律 #5：本二进制直接读取本地 config.agent.yaml 中的 API Key；
// 永远不会把凭证上传到 SaaS 或 DB。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	agentcfg "github.com/edi/quantsaas/internal/agent/config"
	"github.com/edi/quantsaas/internal/agent/exchange"
	"github.com/edi/quantsaas/internal/agent/ws"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config.agent.yaml", "path to agent config file")
	flag.Parse()

	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintln(os.Stderr, "zap logger init failed:", err)
		os.Exit(1)
	}
	defer log.Sync()

	cfg, err := agentcfg.Load(*configPath)
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}
	log.Info("agent starting",
		zap.String("saas_url", cfg.SaaSURL),
		zap.String("email", cfg.Email),
		zap.String("exchange", cfg.Exchange.Name))

	ex := exchange.NewClient(
		cfg.Exchange.APIKey,
		cfg.Exchange.SecretKey,
		cfg.Exchange.Sandbox,
		cfg.Exchange.BaseURL,
	)

	client := ws.NewClient(cfg, ex, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 信号处理
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
		sig := <-sigs
		log.Info("received signal, shutting down", zap.String("signal", sig.String()))
		cancel()
	}()

	if err := client.Run(ctx); err != nil && err != context.Canceled {
		log.Error("agent run ended", zap.Error(err))
		os.Exit(1)
	}
	log.Info("agent exited cleanly")
}
