// cmd/saas 是云端 SaaS 进程的入口。
//
// 启动顺序（docs/系统总体拓扑结构.md 6.1）：
//   1. 读配置 + 初始化 logger
//   2. 连接 Postgres + AutoMigrate 全部模型
//   3. 连接 Redis
//   4. 初始化 Auth + GenomeStore
//   5. 初始化 WebSocket Hub + Reconciler
//   6. 初始化实例管理器 + Ticker
//   7. 初始化 GA 引擎 + EpochService
//   8. 注册 HTTP 路由
//   9. 启动 Cron 调度器
//   10. 启动 HTTP Server
//   11. 监听 SIGTERM/SIGINT，执行优雅停机
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edi/quantsaas/internal/collector"
	"github.com/edi/quantsaas/internal/saas/api"
	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/config"
	cronpkg "github.com/edi/quantsaas/internal/saas/cron"
	"github.com/edi/quantsaas/internal/saas/epoch"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/edi/quantsaas/internal/saas/ws"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to saas config")
	flag.Parse()

	log, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintln(os.Stderr, "zap init:", err)
		os.Exit(1)
	}
	defer log.Sync()

	// 1. 读配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("load config", zap.Error(err))
	}
	log.Info("quantsaas starting", zap.String("app_role", string(cfg.AppRole)))

	// 2. 连 Postgres + AutoMigrate
	db, err := store.NewDB(cfg.Database)
	if err != nil {
		log.Fatal("open db", zap.Error(err))
	}

	// 3. 连 Redis
	rds, err := store.NewRedis(cfg.Redis)
	if err != nil {
		log.Fatal("open redis", zap.Error(err))
	}

	// 4. Auth + GenomeStore
	authSvc := auth.NewService(cfg.JWT.Secret, cfg.JWT.ExpireHours, cfg.JWT.AgentTokenTTLH)
	genomes := ga.NewGenomeStore(db, rds)

	// 5. WebSocket Hub + Reconciler
	reconciler := ws.NewReconciler(db, log)
	hub := ws.NewHub(authSvc, reconciler, log)

	// 6. 实例管理器 + Ticker
	mgr := instance.NewManager(db, log)
	barSource := &instance.DBBarSource{DB: db}
	ticker := instance.NewTicker(db, genomes, barSource, hub, log)

	// 7. GA 引擎 + EpochService
	evolvable := ga.NewSigmoidBTCEvolvable()
	engine := ga.NewEngine(evolvable, genomes, ga.DefaultConfig)
	epochSvc := epoch.NewService(db, engine, genomes, log)

	// 8. HTTP 路由
	if cfg.AppRole != config.RoleDev {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(log))

	// 前端 dist 目录：优先读 WEB_DIST_DIR 环境变量（Docker 用 /app/web-dist）。
	webDist := os.Getenv("WEB_DIST_DIR")
	if webDist == "" {
		webDist = "web-frontend/dist"
	}
	if _, err := os.Stat(webDist); err != nil {
		webDist = "" // 前端未 build 时不挂载
	}

	deps := &api.Dependencies{
		Config:     cfg,
		Auth:       authSvc,
		AuthH:      api.NewAuthHandler(db, authSvc),
		InstanceH:  api.NewInstanceHandler(mgr, db),
		DashboardH: api.NewDashboardHandler(db, hub, cfg),
		EvolutionH: api.NewEvolutionHandler(cfg, epochSvc, genomes),
		Hub:        hub,
		WebDistDir: webDist,
	}
	api.RegisterRoutes(r, deps)

	// 9. Cron 调度器
	cronCtx, cronCancel := context.WithCancel(context.Background())
	defer cronCancel()
	sched := cronpkg.NewScheduler(mgr, ticker, log)
	if err := sched.Start(cronCtx); err != nil {
		log.Fatal("cron start", zap.Error(err))
	}

	// 9.5 K 线增量同步：后台 goroutine，每 5m 拉最新几根 bar 喂给 Ticker
	// 通过环境变量 DISABLE_COLLECTOR=1 可以关闭（测试场景）
	if os.Getenv("DISABLE_COLLECTOR") != "1" {
		collectorSvc := collector.NewService(db, nil, log)
		// Live 同步：symbol 和 interval 目前写死为 BTCUSDT 5m；未来可做成从运行中实例列表聚合
		go collectorSvc.RunLive(cronCtx, "BTCUSDT", "5m", 5)
		log.Info("kline collector live loop started", zap.String("symbol", "BTCUSDT"), zap.String("interval", "5m"))
	}

	// 10. HTTP Server
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Info("http server listening", zap.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("http server", zap.Error(err))
		}
	}()

	// 11. 优雅停机
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigs
	log.Info("shutting down", zap.String("signal", sig.String()))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停接受新请求
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown", zap.Error(err))
	}
	// 停 cron + 等当前 tick
	sched.Stop(shutdownCtx)
	// 关 WebSocket
	hub.Shutdown()
	// 关 DB / Redis
	_ = db.Close()
	_ = rds.Close()

	log.Info("quantsaas exited cleanly")
}

// requestLogger 简易 Gin middleware。
func requestLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("took", time.Since(start)))
	}
}
