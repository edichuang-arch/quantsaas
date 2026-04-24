// Package cron 基于 robfig/cron 的基础调度器。
//
// 职责：每分钟扫描所有 RUNNING 实例，对每个实例并发启动 Ticker.Tick goroutine。
// Tick 内部的幂等桶去重保证同一聚合周期不会重复推进。
package cron

import (
	"context"
	"sync"
	"time"

	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// Scheduler 持有 cron runner + 依赖服务。
type Scheduler struct {
	Manager *instance.Manager
	Ticker  *instance.Ticker
	Log     *zap.Logger

	// Spec cron 表达式。默认 "@every 1m"（每分钟）。
	Spec string
	// TickTimeout 单次 Tick 最大执行时长（防止卡死）。默认 30 秒。
	TickTimeout time.Duration

	c       *cron.Cron
	mu      sync.Mutex
	running bool
}

// NewScheduler 构造调度器。
func NewScheduler(mgr *instance.Manager, ticker *instance.Ticker, log *zap.Logger) *Scheduler {
	if log == nil {
		log = zap.NewNop()
	}
	return &Scheduler{
		Manager:     mgr,
		Ticker:      ticker,
		Log:         log,
		Spec:        "@every 1m",
		TickTimeout: 30 * time.Second,
	}
}

// Start 启动 cron runner。必须先调用 Stop 再重新 Start。
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.c = cron.New(cron.WithLocation(time.UTC))
	_, err := s.c.AddFunc(s.Spec, func() {
		s.runOnce(ctx)
	})
	if err != nil {
		return err
	}
	s.c.Start()
	s.running = true
	s.Log.Info("cron scheduler started", zap.String("spec", s.Spec))
	return nil
}

// Stop 停止 cron runner 并等待正在执行的 job 完成（最多 30 秒）。
func (s *Scheduler) Stop(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	stopCtx := s.c.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(30 * time.Second):
		s.Log.Warn("cron stop timeout after 30s")
	case <-ctx.Done():
	}
	s.running = false
	s.Log.Info("cron scheduler stopped")
}

// RunOnce 手动触发一次扫描（测试用）。
func (s *Scheduler) RunOnce(ctx context.Context) {
	s.runOnce(ctx)
}

// runOnce 扫描所有 RUNNING 实例并并发启动 Tick。
// 每个 Tick 独立 goroutine + 超时保护；异常不会影响其他实例。
func (s *Scheduler) runOnce(ctx context.Context) {
	insts, err := s.Manager.ListRunning(ctx)
	if err != nil {
		s.Log.Error("list running instances failed", zap.Error(err))
		return
	}
	if len(insts) == 0 {
		return
	}
	s.Log.Debug("cron scan", zap.Int("running_count", len(insts)))

	var wg sync.WaitGroup
	for _, inst := range insts {
		inst := inst
		wg.Add(1)
		go func() {
			defer wg.Done()
			tickCtx, cancel := context.WithTimeout(ctx, s.TickTimeout)
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					s.Log.Error("tick panic",
						zap.Uint("instance_id", inst.ID),
						zap.Any("panic", r))
					_ = s.Manager.MarkError(ctx, inst.ID, "tick panicked")
				}
			}()
			if err := s.Ticker.Tick(tickCtx, inst); err != nil {
				s.Log.Error("tick failed",
					zap.Uint("instance_id", inst.ID),
					zap.Error(err))
				// 连续失败次数统计由上层监控处理；此处不直接 MarkError
				// 避免临时网络抖动把实例打进 ERROR 状态
			}
		}()
	}
	wg.Wait()
}

// 确保 store 被引用（future: 可能直接读 DB 状态）
var _ = store.InstRunning
