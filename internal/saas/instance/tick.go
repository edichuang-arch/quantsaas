package instance

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/store"
	sigmoidbtc "github.com/edi/quantsaas/internal/strategies/sigmoid-btc"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BarSource 抽象"最新 K 线"的数据源。
// Phase 6 阶段使用 DB KLine 表做数据源（DBBarSource）；
// Phase 7 之后可以替换为真实 Binance 公共 API。
type BarSource interface {
	// LoadBars 返回指定 symbol+interval 的 K 线（按时间升序，取最新 limit 条）。
	LoadBars(ctx context.Context, symbol, interval string, limit int) ([]quant.Bar, error)
}

// DBBarSource 从 store.KLine 表查最新 N 根 bar。
type DBBarSource struct{ DB *store.DB }

// LoadBars 实现 BarSource。
func (s *DBBarSource) LoadBars(ctx context.Context, symbol, interval string, limit int) ([]quant.Bar, error) {
	if limit <= 0 {
		limit = 300
	}
	var rows []store.KLine
	err := s.DB.WithContext(ctx).
		Where("symbol = ? AND interval = ?", symbol, interval).
		Order("open_time DESC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	bars := make([]quant.Bar, len(rows))
	// 反转为时间升序
	for i, r := range rows {
		b := quant.Bar{
			OpenTime:  r.OpenTime,
			CloseTime: r.CloseTime,
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    r.Volume,
		}
		bars[len(rows)-1-i] = b
	}
	return bars, nil
}

// Ticker 实例单根 bar 推进器。Stateless；可并发。
type Ticker struct {
	DB      *store.DB
	Genomes *ga.GenomeStore
	Bars    BarSource
	Sender  CommandSender
	Log     *zap.Logger

	// Interval 默认扫描聚合周期（Plan 预设 5m）。
	Interval string
	// MinBars 策略所需最少 K 线数（默认 quant.MicroVolRatioLongBars = 112）。
	MinBars int
}

// NewTicker 构造一个 Ticker。
func NewTicker(db *store.DB, gs *ga.GenomeStore, bars BarSource, sender CommandSender, log *zap.Logger) *Ticker {
	if log == nil {
		log = zap.NewNop()
	}
	return &Ticker{
		DB: db, Genomes: gs, Bars: bars, Sender: sender, Log: log,
		Interval: "5m",
		MinBars:  quant.MicroVolRatioLongBars + 10,
	}
}

// Tick 对单个实例执行一次 Step() 推进。
//
// 10 步流程（docs/系统总体拓扑结构.md 6.2）：
//   1. 拉 K 线；若最新 bar 时间 <= LastProcessedBarTime → 跳过（幂等桶）
//   2. 读 PortfolioState / RuntimeState
//   3. 读 champion 参数包（Redis → DB）
//   4. ACL 降级 Bar → closes []float64 + ts []int64
//   5. 构建 StrategyInput
//   6. 调用 sigmoidbtc.Step()
//   7. 持久化 RuntimeState
//   8. 处理 ReleaseIntent：只改 SaaS 账本 + 写 AuditLog，不下发
//   9. TradeIntent → pending SpotExecution → WebSocket 下发
//   10. 更新 LastProcessedBarTime
func (t *Ticker) Tick(ctx context.Context, inst store.StrategyInstance) error {
	logger := t.Log.With(zap.Uint("instance_id", inst.ID), zap.String("symbol", inst.Symbol))

	// 1. 拉 K 线 + 幂等桶
	bars, err := t.Bars.LoadBars(ctx, inst.Symbol, t.Interval, t.MinBars*2)
	if err != nil {
		return fmt.Errorf("load bars: %w", err)
	}
	if len(bars) == 0 {
		return errWarn(logger, "no bars available")
	}
	if len(bars) < t.MinBars {
		return errWarn(logger, "insufficient bars", zap.Int("have", len(bars)), zap.Int("need", t.MinBars))
	}
	latestBar := bars[len(bars)-1]
	if latestBar.OpenTime <= inst.LastProcessedBarTime {
		// 同一聚合桶已处理过，跳过
		return nil
	}

	// 2. 读 PortfolioState + RuntimeState
	var pf store.PortfolioState
	if err := t.DB.WithContext(ctx).
		Where("instance_id = ?", inst.ID).
		First(&pf).Error; err != nil {
		return fmt.Errorf("load portfolio: %w", err)
	}
	var rtRec store.RuntimeState
	if err := t.DB.WithContext(ctx).
		Where("instance_id = ?", inst.ID).
		First(&rtRec).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("load runtime state: %w", err)
	}
	prevRuntime := decodeRuntime(rtRec.Content)

	// 3. 读 champion 参数包
	champ, err := t.Genomes.LoadChampion(ctx, sigmoidbtc.StrategyID, inst.Symbol)
	if err != nil {
		return fmt.Errorf("load champion: %w", err)
	}
	var paramPack []byte
	if champ != nil {
		paramPack = champ.ParamPack
	}
	params := sigmoidbtc.LoadParams(paramPack)

	// 4. ACL 降级
	closes := quant.ExtractCloses(bars)
	timestamps := quant.ExtractTimestamps(bars)

	// 5. StrategyInput
	in := quant.StrategyInput{
		NowMs:             latestBar.OpenTime,
		Closes:            closes,
		Timestamps:        timestamps,
		Portfolio: quant.PortfolioSnapshot{
			USDTBalance:     pf.USDTBalance,
			DeadStackAsset:  pf.DeadStackAsset,
			FloatStackAsset: pf.FloatStackAsset,
			ColdSealedAsset: pf.ColdSealedAsset,
			CurrentPrice:    latestBar.Close,
		},
		Symbol:            inst.Symbol,
		MonthlyInjectUSDT: inst.MonthlyInjectUSDT,
		PrevRuntime:       prevRuntime,
	}

	// 6. Step()
	out := sigmoidbtc.Step(in, params)

	if out.SkipReason != "" {
		logger.Info("step skipped", zap.String("reason", out.SkipReason))
		return nil
	}

	// 7. 持久化 RuntimeState（即使无意图也要保存推进游标）
	newRt := out.NewRuntime
	rtBlob, _ := marshalJSON(newRt)
	if err := t.DB.Save(&store.RuntimeState{
		ID:         rtRec.ID,
		InstanceID: inst.ID,
		Content:    datatypes.JSON(rtBlob),
	}).Error; err != nil {
		return fmt.Errorf("save runtime state: %w", err)
	}

	// 8. 底仓释放：只改 SaaS 账本 + AuditLog
	if err := t.handleReleases(ctx, inst.ID, out.Releases); err != nil {
		logger.Error("release failed", zap.Error(err))
		// 释放失败不阻断交易下发（释放只是账本语义，失败可重试）
	}

	// 9. TradeIntent → SpotExecution → WebSocket
	if err := t.handleIntents(ctx, inst, latestBar, out.Intents, out.DecisionReason); err != nil {
		logger.Error("intents failed", zap.Error(err))
		// 继续推进游标，避免卡死；下一 tick 会依据最新 Portfolio 重算
	}

	// 10. 更新 LastProcessedBarTime
	if err := t.DB.Model(&store.StrategyInstance{}).
		Where("id = ?", inst.ID).
		Update("last_processed_bar_time", latestBar.OpenTime).Error; err != nil {
		return fmt.Errorf("update processed bar time: %w", err)
	}
	return nil
}

// handleReleases DeadStack → Floating 语义转换（铁律 #9：不下发 Agent）。
// 同时对每次释放写 AuditLog 便于追溯。
func (t *Ticker) handleReleases(ctx context.Context, instanceID uint, releases []quant.ReleaseIntent) error {
	if len(releases) == 0 {
		return nil
	}
	return t.DB.Transaction(func(tx *gorm.DB) error {
		for _, rel := range releases {
			if rel.Amount <= 0 {
				continue
			}
			// 更新 PortfolioState：DeadStack -= amount; FloatStack += amount
			var pf store.PortfolioState
			if err := tx.Where("instance_id = ?", instanceID).First(&pf).Error; err != nil {
				return err
			}
			actual := rel.Amount
			if actual > pf.DeadStackAsset {
				actual = pf.DeadStackAsset
			}
			if actual <= 0 {
				continue
			}
			pf.DeadStackAsset -= actual
			pf.FloatStackAsset += actual
			if err := tx.Save(&pf).Error; err != nil {
				return err
			}
			payload, _ := datatypesJSON(map[string]any{
				"amount": actual,
				"reason": rel.Reason,
			})
			if err := tx.Create(&store.AuditLog{
				InstanceID: &instanceID,
				EventType:  "dead_stack_release",
				Payload:    payload,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// handleIntents 写 pending SpotExecution 并通过 CommandSender 下发。
// Agent 离线时，pending 记录仍然落库，但跳过网络下发（下次 tick 后 Step 重新计算）。
func (t *Ticker) handleIntents(
	ctx context.Context,
	inst store.StrategyInstance,
	bar quant.Bar,
	intents []quant.TradeIntent,
	decisionReason string,
) error {
	if len(intents) == 0 {
		return nil
	}
	for i, intent := range intents {
		coid := makeClientOrderID(inst.ID, string(intent.Engine), bar.OpenTime, i)
		exec := store.SpotExecution{
			InstanceID:    inst.ID,
			ClientOrderID: coid,
			Action:        string(intent.Action),
			Engine:        string(intent.Engine),
			Symbol:        inst.Symbol,
			LotType:       store.LotType(intent.LotType),
			AmountUSDT:    intent.AmountUSDT,
			QtyAsset:      intent.QtyAsset,
			Status:        store.ExecPending,
			SentAt:        time.Now().UTC(),
		}
		if err := t.DB.WithContext(ctx).Create(&exec).Error; err != nil {
			return fmt.Errorf("write pending exec: %w", err)
		}

		cmd := TradeCommand{
			ClientOrderID: coid,
			Action:        string(intent.Action),
			Engine:        string(intent.Engine),
			Symbol:        inst.Symbol,
			LotType:       string(intent.LotType),
		}
		if intent.Action == quant.ActionBuy {
			cmd.AmountUSDT = formatFloat(intent.AmountUSDT)
		} else {
			cmd.QtyAsset = formatFloat(intent.QtyAsset)
		}

		err := t.Sender.SendToUser(ctx, inst.UserID, cmd)
		if errors.Is(err, ErrAgentNotConnected) {
			t.Log.Warn("agent offline; pending exec recorded, skipped send",
				zap.Uint("instance_id", inst.ID),
				zap.String("client_order_id", coid))
			continue
		}
		if err != nil {
			// 更新 exec 为 failed，便于前端可见性（并不回滚 pending 记录）
			_ = t.DB.Model(&exec).Updates(map[string]any{
				"status":        store.ExecFailed,
				"error_message": err.Error(),
			}).Error
			return fmt.Errorf("send command: %w", err)
		}
	}
	// 写一条 decision audit log（顶层摘要）
	if decisionReason != "" {
		payload, _ := datatypesJSON(map[string]any{
			"reason":       decisionReason,
			"bar_time":     bar.OpenTime,
			"intent_count": len(intents),
		})
		instID := inst.ID
		_ = t.DB.Create(&store.AuditLog{
			InstanceID: &instID,
			EventType:  "step_decision",
			Payload:    payload,
		}).Error
	}
	return nil
}

// --- 私有工具 ---

// makeClientOrderID 生成全局唯一 ID（docs 5.3 规定格式：inst{id}-{engine}-{ts}）。
// 附加 slot 防止同一 bar 内多个 intent 冲突。
func makeClientOrderID(instanceID uint, engine string, barTime int64, slot int) string {
	return fmt.Sprintf("inst%d-%s-%d-%d", instanceID, engine, barTime, slot)
}

func formatFloat(x float64) string {
	// 保留 8 位小数（Binance 现货最高精度）。去除尾随 0。
	return strconv.FormatFloat(x, 'f', -1, 64)
}

// decodeRuntime 从 RuntimeState.Content JSON blob 还原 quant.RuntimeState。
// nil / 解码失败返回空状态（Step 会自行初始化 Extras map）。
func decodeRuntime(raw datatypes.JSON) quant.RuntimeState {
	rt := quant.RuntimeState{Extras: map[string]float64{}}
	if len(raw) == 0 {
		return rt
	}
	_ = unmarshalJSON(raw, &rt)
	if rt.Extras == nil {
		rt.Extras = map[string]float64{}
	}
	return rt
}

// errWarn log warning 级别但返回 nil（非致命错误，cron 会重试）。
func errWarn(log *zap.Logger, msg string, fields ...zap.Field) error {
	log.Warn(msg, fields...)
	return nil
}
