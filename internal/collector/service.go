package collector

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/store"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Service 把 K 线写入 store.KLine 表。
// 支持两种模式：
//   - Backfill：指定 [from, to] 一次性补齐历史（分批拉取，自动限流）
//   - RunLive：启动后循环拉最新 bar（用于 SaaS 常驻）
type Service struct {
	DB     *store.DB
	Client *PublicClient
	Log    *zap.Logger

	// RequestGapMs 两次请求之间的最小间隔（毫秒），用于避免触发 rate limit。
	// Binance 公开接口每分钟权重 1200，klines 单请求权重 1-2；默认 250ms ≈ 240 req/min。
	RequestGapMs int
}

// NewService 构造 Service。client 为 nil 时使用默认 Binance endpoint。
func NewService(db *store.DB, client *PublicClient, log *zap.Logger) *Service {
	if client == nil {
		client = NewPublicClient("")
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{DB: db, Client: client, Log: log, RequestGapMs: 250}
}

// Backfill 从 fromMs 到 toMs（毫秒）拉取 symbol/interval 的全部 K 线并 upsert。
// 若 toMs <= 0 则使用当前时间。
// 每批最多 1000 根。返回实际写入/跳过的统计。
func (s *Service) Backfill(
	ctx context.Context,
	symbol, interval string,
	fromMs, toMs int64,
) (inserted, skipped int, err error) {
	intervalMs := IntervalDurationMs(interval)
	if intervalMs == 0 {
		return 0, 0, fmt.Errorf("unsupported interval %q", interval)
	}
	if toMs <= 0 {
		toMs = time.Now().UnixMilli()
	}
	if fromMs <= 0 {
		return 0, 0, errors.New("fromMs must be > 0")
	}

	cursor := fromMs
	totalBatches := 0
	for cursor < toMs {
		if ctx.Err() != nil {
			return inserted, skipped, ctx.Err()
		}
		bars, err := s.Client.FetchKLines(ctx, symbol, interval, cursor, toMs, maxKlinesPerReq)
		if err != nil {
			return inserted, skipped, fmt.Errorf("fetch batch at %d: %w", cursor, err)
		}
		if len(bars) == 0 {
			break // 到底了
		}
		ins, skp, err := s.upsertBars(ctx, symbol, interval, bars)
		if err != nil {
			return inserted, skipped, err
		}
		inserted += ins
		skipped += skp
		totalBatches++
		if totalBatches%10 == 0 {
			s.Log.Info("backfill progress",
				zap.String("symbol", symbol),
				zap.String("interval", interval),
				zap.Int("batches", totalBatches),
				zap.Int("inserted", inserted),
				zap.Int("skipped", skipped),
				zap.Time("cursor", time.UnixMilli(cursor)))
		}

		// 推进游标到最新一根的下一个 bar
		lastOpen := bars[len(bars)-1].OpenTime
		next := lastOpen + intervalMs
		if next <= cursor {
			// 异常：无法推进，避免死循环
			break
		}
		cursor = next

		// 速率限制
		if s.RequestGapMs > 0 {
			select {
			case <-ctx.Done():
				return inserted, skipped, ctx.Err()
			case <-time.After(time.Duration(s.RequestGapMs) * time.Millisecond):
			}
		}
	}
	return inserted, skipped, nil
}

// RunLive 后台循环：每 interval 周期拉最近 limit 根 bar 做增量同步。
// 适用于 SaaS 常驻；Cron Tick 每次 tick 前就有最新数据。
// ctx 取消时 goroutine 退出。
func (s *Service) RunLive(ctx context.Context, symbol, interval string, limit int) {
	intervalMs := IntervalDurationMs(interval)
	if intervalMs == 0 {
		s.Log.Error("invalid interval", zap.String("interval", interval))
		return
	}
	if limit <= 0 {
		limit = 5 // 每次拉最近 5 根，容错漏抓
	}
	// 首次立即拉一次
	s.pullLatest(ctx, symbol, interval, limit)

	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pullLatest(ctx, symbol, interval, limit)
		}
	}
}

func (s *Service) pullLatest(ctx context.Context, symbol, interval string, limit int) {
	bars, err := s.Client.FetchKLines(ctx, symbol, interval, 0, 0, limit)
	if err != nil {
		s.Log.Warn("live pull failed",
			zap.String("symbol", symbol), zap.Error(err))
		return
	}
	ins, skp, err := s.upsertBars(ctx, symbol, interval, bars)
	if err != nil {
		s.Log.Warn("live upsert failed", zap.Error(err))
		return
	}
	if ins > 0 {
		s.Log.Info("kline live sync",
			zap.String("symbol", symbol),
			zap.String("interval", interval),
			zap.Int("inserted", ins),
			zap.Int("skipped", skp))
	}
}

// upsertBars 将 bars 批量插入 store.KLine；已有记录（同 symbol+interval+open_time）跳过。
func (s *Service) upsertBars(
	ctx context.Context,
	symbol, interval string,
	bars []quant.Bar,
) (inserted, skipped int, err error) {
	if len(bars) == 0 {
		return 0, 0, nil
	}
	rows := make([]store.KLine, len(bars))
	for i, b := range bars {
		rows[i] = store.KLine{
			Symbol: symbol, Interval: interval,
			OpenTime: b.OpenTime, CloseTime: b.CloseTime,
			Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
		}
	}
	// 使用 ON CONFLICT DO NOTHING 让唯一索引 (symbol, interval, open_time) 去重。
	tx := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, 500)
	if tx.Error != nil {
		return 0, 0, fmt.Errorf("upsert klines: %w", tx.Error)
	}
	inserted = int(tx.RowsAffected)
	skipped = len(rows) - inserted
	return
}

// LatestOpenTime 返回某 symbol+interval 当前已存最新 bar 的 OpenTime；无则 0。
func (s *Service) LatestOpenTime(ctx context.Context, symbol, interval string) (int64, error) {
	var row store.KLine
	err := s.DB.WithContext(ctx).
		Where("symbol = ? AND interval = ?", symbol, interval).
		Order("open_time DESC").
		Limit(1).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return row.OpenTime, err
}
