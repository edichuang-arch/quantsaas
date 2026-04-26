package ws

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/edi/quantsaas/internal/wsproto"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Reconciler 处理 Agent 上报的 DeltaReport（docs/系统总体拓扑结构.md 5.5）。
//
// 三类情况：
//   A. 初始快照（ClientOrderID 空）：仅用 Balances 刷新 PortfolioState
//   B. 成交上报（ClientOrderID 非空 + Execution.Status = FILLED）：
//        - 更新 SpotExecution 为 filled
//        - 按 LotType 更新 PortfolioState（DeadStack / FloatStack）
//        - 写 TradeRecord
//   C. 失败上报（ClientOrderID 非空 + Status = FAILED）：
//        - 更新 SpotExecution 为 failed（含 error_message）
//        - 仅刷新 Balances；不写 TradeRecord
//
// 所有更新在单个事务内完成，保证 PortfolioState 与 SpotExecution 一致性。
type Reconciler struct {
	DB  *store.DB
	Log *zap.Logger
}

// NewReconciler 构造 Reconciler。
func NewReconciler(db *store.DB, log *zap.Logger) *Reconciler {
	if log == nil {
		log = zap.NewNop()
	}
	return &Reconciler{DB: db, Log: log}
}

// HandleDelta 实现 DeltaHandler 接口。
func (r *Reconciler) HandleDelta(ctx context.Context, userID uint, report wsproto.DeltaReport) error {
	// Initial snapshot：只刷新 Balances
	if report.ClientOrderID == "" {
		return r.refreshBalancesForUser(ctx, userID, report.Balances)
	}

	// 找到对应的 SpotExecution
	var exec store.SpotExecution
	err := r.DB.WithContext(ctx).
		Where("client_order_id = ?", report.ClientOrderID).
		First(&exec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("no pending execution for %s", report.ClientOrderID)
		}
		return fmt.Errorf("load execution: %w", err)
	}

	// 验证实例归属
	var inst store.StrategyInstance
	if err := r.DB.WithContext(ctx).First(&inst, exec.InstanceID).Error; err != nil {
		return fmt.Errorf("load instance: %w", err)
	}
	if inst.UserID != userID {
		return fmt.Errorf("execution %s belongs to different user", report.ClientOrderID)
	}

	if report.Execution == nil {
		return errors.New("delta report missing execution detail")
	}

	switch strings.ToUpper(report.Execution.Status) {
	case "FILLED":
		return r.applyFilled(ctx, &inst, &exec, report)
	case "FAILED":
		return r.applyFailed(ctx, &inst, &exec, report)
	default:
		// PARTIALLY_FILLED 或其他：保持 pending，不改 Portfolio
		r.Log.Warn("non-terminal execution status",
			zap.String("client_order_id", report.ClientOrderID),
			zap.String("status", report.Execution.Status))
		return nil
	}
}

// applyFilled 成交落账 + 刷新余额（事务）。
func (r *Reconciler) applyFilled(
	ctx context.Context,
	inst *store.StrategyInstance,
	exec *store.SpotExecution,
	report wsproto.DeltaReport,
) error {
	filledQty := parseFloat(report.Execution.FilledQty)
	filledPrice := parseFloat(report.Execution.FilledPrice)
	filledQuote := parseFloat(report.Execution.FilledQuote)
	fee := parseFloat(report.Execution.Fee)

	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		// 1. 更新 SpotExecution
		if err := tx.Model(exec).Updates(map[string]any{
			"status":       store.ExecFilled,
			"filled_qty":   filledQty,
			"filled_price": filledPrice,
			"fee":          fee,
			"filled_at":    &now,
		}).Error; err != nil {
			return fmt.Errorf("update exec: %w", err)
		}

		// 2. 写 TradeRecord
		tr := store.TradeRecord{
			InstanceID:    inst.ID,
			ClientOrderID: exec.ClientOrderID,
			Action:        exec.Action,
			Engine:        exec.Engine,
			Symbol:        exec.Symbol,
			LotType:       exec.LotType,
			FilledQty:     filledQty,
			FilledPrice:   filledPrice,
			FilledUSDT:    filledQuote,
			Fee:           fee,
			FeeAsset:      report.Execution.FeeAsset,
		}
		if err := tx.Create(&tr).Error; err != nil {
			return fmt.Errorf("insert trade record: %w", err)
		}

		// 3. 更新 PortfolioState 的 lot 分类字段
		var pf store.PortfolioState
		if err := tx.Where("instance_id = ?", inst.ID).First(&pf).Error; err != nil {
			return fmt.Errorf("load portfolio: %w", err)
		}
		applyTradeToPortfolio(&pf, exec, filledQty, filledQuote, fee)
		// 4. 用 Balances 覆盖现金余额（信任 Agent 的真实数据）
		applyBalancesToPortfolio(&pf, report.Balances, inst.Symbol)
		// 5. 用本次成交价重算 LastPriceUSDT 与 TotalEquity（前端 Dashboard 直接读这两个字段）
		if filledPrice > 0 {
			pf.LastPriceUSDT = filledPrice
		}
		pf.TotalEquity = recomputeTotalEquity(&pf)
		if err := tx.Save(&pf).Error; err != nil {
			return fmt.Errorf("save portfolio: %w", err)
		}

		// 5. Audit
		payload, _ := marshalJSON(map[string]any{
			"client_order_id": exec.ClientOrderID,
			"filled_qty":      filledQty,
			"filled_price":    filledPrice,
			"lot_type":        exec.LotType,
		})
		instID := inst.ID
		return tx.Create(&store.AuditLog{
			InstanceID: &instID,
			EventType:  "trade_filled",
			Payload:    payload,
		}).Error
	})
}

// applyFailed 只更新 SpotExecution + 刷新 balances，不写 TradeRecord。
//
// equity 重算理由：订单虽然失败，但 Balances 可能因其他事件变动
// （手续费退款、其他订单成交、外部充提），TotalEquity 必须用最新 USDT/资产
// 重算。LastPriceUSDT 沿用旧值（applyFailed 没有 filledPrice）。
func (r *Reconciler) applyFailed(
	ctx context.Context,
	inst *store.StrategyInstance,
	exec *store.SpotExecution,
	report wsproto.DeltaReport,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(exec).Updates(map[string]any{
			"status":        store.ExecFailed,
			"error_message": report.Execution.ErrorMessage,
		}).Error; err != nil {
			return err
		}
		var pf store.PortfolioState
		if err := tx.Where("instance_id = ?", inst.ID).First(&pf).Error; err != nil {
			return err
		}
		applyBalancesToPortfolio(&pf, report.Balances, inst.Symbol)
		// 与 applyFilled 一致：只要动了 Balances，就用最新现金 + 资产 × 旧 LastPrice 重算
		pf.TotalEquity = recomputeTotalEquity(&pf)
		return tx.Save(&pf).Error
	})
}

// refreshBalancesForUser 遍历该用户的所有实例，对每个实例用同一份 Balances 刷新
// （Agent 是账户级，而非实例级，但 Binance 现货一个账户通常对应一个策略/实例）。
//
// 初始快照场景：Agent 启动时 push 一次 Balances（无 ClientOrderID）；
// 这时 PortfolioState.LastPriceUSDT 可能仍是 0（从未成交过），
// recomputeTotalEquity 会 fallback 为 USDTBalance —— 保守但合理。
func (r *Reconciler) refreshBalancesForUser(ctx context.Context, userID uint, balances []wsproto.Balance) error {
	var insts []store.StrategyInstance
	if err := r.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&insts).Error; err != nil {
		return err
	}
	for _, inst := range insts {
		var pf store.PortfolioState
		if err := r.DB.WithContext(ctx).
			Where("instance_id = ?", inst.ID).First(&pf).Error; err != nil {
			continue
		}
		applyBalancesToPortfolio(&pf, balances, inst.Symbol)
		pf.TotalEquity = recomputeTotalEquity(&pf)
		if err := r.DB.WithContext(ctx).Save(&pf).Error; err != nil {
			r.Log.Warn("refresh balance failed",
				zap.Uint("instance_id", inst.ID), zap.Error(err))
		}
	}
	return nil
}

// applyTradeToPortfolio 按 LotType + Action 调整 Dead/Float 仓位 + USDT。
func applyTradeToPortfolio(pf *store.PortfolioState, exec *store.SpotExecution, qty, quote, fee float64) {
	switch exec.Action {
	case "BUY":
		// BUY 花费 quote USDT + fee（费用可能以基础资产结算，交由 applyBalances 兜底）
		pf.USDTBalance -= (quote + fee)
		switch exec.LotType {
		case store.LotDeadStack:
			pf.DeadStackAsset += qty
		case store.LotFloating:
			pf.FloatStackAsset += qty
		}
	case "SELL":
		// SELL 扣减资产 + 进账 quote - fee
		switch exec.LotType {
		case store.LotFloating:
			pf.FloatStackAsset -= qty
		case store.LotDeadStack:
			pf.DeadStackAsset -= qty
		}
		pf.USDTBalance += (quote - fee)
	}
}

// applyBalancesToPortfolio 用 Agent 上报的真实余额覆盖 PortfolioState 的现金与总持仓
// 粗度量；lot 内部分布（DeadStack / FloatStack）仍由交易记录推算。
func applyBalancesToPortfolio(pf *store.PortfolioState, balances []wsproto.Balance, symbol string) {
	baseAsset := strings.TrimSuffix(symbol, "USDT")
	for _, b := range balances {
		free := parseFloat(b.Free)
		locked := parseFloat(b.Locked)
		switch b.Asset {
		case "USDT":
			pf.USDTBalance = free
			pf.USDTFrozen = locked
		case baseAsset:
			total := free + locked
			// Dead/Float/ColdSealed 之和应等于 total；若有偏差，按比例缩放
			currentSum := pf.DeadStackAsset + pf.FloatStackAsset + pf.ColdSealedAsset
			if currentSum > 0 && total > 0 {
				scale := total / currentSum
				pf.DeadStackAsset *= scale
				pf.FloatStackAsset *= scale
				// ColdSealedAsset 不缩放（它是静态的；余额差异归到可变仓位）
				if pf.ColdSealedAsset > total {
					pf.ColdSealedAsset = total
				}
			} else if currentSum == 0 && total > 0 {
				// 首次持仓（例如初始快照）：先全部记为 FloatStack，等后续成交推导 lot 归属
				pf.FloatStackAsset = total
			}
		}
	}
}

// recomputeTotalEquity 用 LastPriceUSDT 计算 TotalEquity。
// 公式与 quant.PortfolioSnapshot.TotalEquity() 等价：USDT + (Dead+Float+Cold)*price。
// LastPriceUSDT = 0（尚未有成交价）→ 退化为 USDTBalance（保守估计）。
func recomputeTotalEquity(pf *store.PortfolioState) float64 {
	asset := pf.DeadStackAsset + pf.FloatStackAsset + pf.ColdSealedAsset
	if pf.LastPriceUSDT <= 0 {
		return pf.USDTBalance
	}
	return pf.USDTBalance + asset*pf.LastPriceUSDT
}

// --- 内部工具 ---

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func marshalJSON(v any) (datatypes.JSON, error) {
	blob, err := jsonMarshal(v)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(blob), nil
}
