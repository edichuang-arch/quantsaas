// Package instance 管理策略实例的生命周期（创建/启动/停止/删除）
// 以及 cron Tick 驱动的 Step() 推进。
//
// 铁律：
//   - 实例状态机只在本包内修改；其他包必须通过 Manager 的方法操作
//   - Tick 必须幂等（同一 bar 不重复推进）
//   - 底仓释放（DEAD_STACK → FLOATING）只改 SaaS 侧账本，不下发 Agent
package instance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/edi/quantsaas/internal/saas/store"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Manager 实例 CRUD + 状态机。
type Manager struct {
	DB  *store.DB
	Log *zap.Logger
}

// NewManager 构造 Manager。
func NewManager(db *store.DB, log *zap.Logger) *Manager {
	if log == nil {
		log = zap.NewNop()
	}
	return &Manager{DB: db, Log: log}
}

// CreateRequest 创建实例的请求体。
type CreateRequest struct {
	UserID             uint
	TemplateID         uint
	Name               string
	Symbol             string
	InitialCapitalUSDT float64
	MonthlyInjectUSDT  float64
}

// Create 创建一个新实例（状态默认 STOPPED，同时初始化空 PortfolioState + RuntimeState）。
// 订阅配额守门：用户当前实例数 >= plan.MaxInstances 时返回 ErrQuotaExceeded。
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*store.StrategyInstance, error) {
	if req.Name == "" || req.Symbol == "" || req.TemplateID == 0 {
		return nil, errors.New("name/symbol/template_id required")
	}
	if req.InitialCapitalUSDT <= 0 {
		return nil, errors.New("initial_capital_usdt must be positive")
	}

	// 配额检查
	var user store.User
	if err := m.DB.WithContext(ctx).First(&user, req.UserID).Error; err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	var count int64
	if err := m.DB.WithContext(ctx).Model(&store.StrategyInstance{}).
		Where("user_id = ?", req.UserID).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("count user instances: %w", err)
	}
	if int(count) >= user.MaxInstances {
		return nil, ErrQuotaExceeded
	}

	// 事务：Instance + PortfolioState + RuntimeState 一次建好
	var inst store.StrategyInstance
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		inst = store.StrategyInstance{
			UserID:             req.UserID,
			TemplateID:         req.TemplateID,
			Name:               req.Name,
			Symbol:             req.Symbol,
			Status:             store.InstStopped,
			InitialCapitalUSDT: req.InitialCapitalUSDT,
			MonthlyInjectUSDT:  req.MonthlyInjectUSDT,
		}
		if err := tx.Create(&inst).Error; err != nil {
			return err
		}
		pf := store.PortfolioState{
			InstanceID:  inst.ID,
			USDTBalance: req.InitialCapitalUSDT,
			// 初始时 TotalEquity = 现金；后续 Tick 根据当前价重新计算
			TotalEquity: req.InitialCapitalUSDT,
		}
		if err := tx.Create(&pf).Error; err != nil {
			return err
		}
		rt := store.RuntimeState{
			InstanceID: inst.ID,
			Content:    datatypes.JSON([]byte("{}")),
		}
		return tx.Create(&rt).Error
	})
	if err != nil {
		return nil, fmt.Errorf("create instance tx: %w", err)
	}
	return &inst, nil
}

// Start 把实例从 STOPPED/ERROR 切到 RUNNING，写 StartedAt。
func (m *Manager) Start(ctx context.Context, instanceID, userID uint) error {
	return m.transition(ctx, instanceID, userID, []store.InstanceStatus{
		store.InstStopped, store.InstError,
	}, store.InstRunning, func(upd map[string]any) {
		now := time.Now().UTC()
		upd["started_at"] = &now
	})
}

// Stop 把 RUNNING → STOPPED，写 StoppedAt。
func (m *Manager) Stop(ctx context.Context, instanceID, userID uint) error {
	return m.transition(ctx, instanceID, userID, []store.InstanceStatus{
		store.InstRunning, store.InstError,
	}, store.InstStopped, func(upd map[string]any) {
		now := time.Now().UTC()
		upd["stopped_at"] = &now
	})
}

// Delete 软删除实例（状态先设回 STOPPED，再 gorm soft delete）。
func (m *Manager) Delete(ctx context.Context, instanceID, userID uint) error {
	return m.DB.Transaction(func(tx *gorm.DB) error {
		var inst store.StrategyInstance
		if err := tx.Where("id = ? AND user_id = ?", instanceID, userID).
			First(&inst).Error; err != nil {
			return fmt.Errorf("load instance: %w", err)
		}
		if inst.Status == store.InstRunning {
			return ErrDeleteRunning
		}
		return tx.Delete(&inst).Error
	})
}

// ListByUser 返回指定用户的全部实例。
func (m *Manager) ListByUser(ctx context.Context, userID uint) ([]store.StrategyInstance, error) {
	var insts []store.StrategyInstance
	err := m.DB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&insts).Error
	return insts, err
}

// ListRunning 返回所有 RUNNING 状态的实例（cron 扫描用）。
func (m *Manager) ListRunning(ctx context.Context) ([]store.StrategyInstance, error) {
	var insts []store.StrategyInstance
	err := m.DB.WithContext(ctx).
		Where("status = ?", store.InstRunning).
		Find(&insts).Error
	return insts, err
}

// MarkError 将实例标记为 ERROR 状态（用于 Tick 失败时的熔断）。
// errMsg 写入 AuditLog 便于排查。
func (m *Manager) MarkError(ctx context.Context, instanceID uint, reason string) error {
	err := m.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&store.StrategyInstance{}).
			Where("id = ?", instanceID).
			Updates(map[string]any{
				"status":     store.InstError,
				"stopped_at": &now,
			}).Error; err != nil {
			return err
		}
		payload, _ := datatypesJSON(map[string]any{
			"reason": reason,
		})
		return tx.Create(&store.AuditLog{
			InstanceID: &instanceID,
			EventType:  "instance_error",
			Payload:    payload,
		}).Error
	})
	if err != nil {
		m.Log.Error("mark error failed", zap.Uint("instance_id", instanceID), zap.Error(err))
	}
	return err
}

// --- 私有工具 ---

func (m *Manager) transition(
	ctx context.Context,
	instanceID, userID uint,
	from []store.InstanceStatus,
	to store.InstanceStatus,
	setExtra func(map[string]any),
) error {
	var inst store.StrategyInstance
	err := m.DB.WithContext(ctx).
		Where("id = ? AND user_id = ?", instanceID, userID).
		First(&inst).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInstanceNotFound
		}
		return fmt.Errorf("load instance: %w", err)
	}
	// 检查当前状态是否允许此转换
	allowed := false
	for _, s := range from {
		if inst.Status == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: current=%s, target=%s", ErrInvalidTransition, inst.Status, to)
	}
	upd := map[string]any{"status": to}
	if setExtra != nil {
		setExtra(upd)
	}
	return m.DB.WithContext(ctx).Model(&inst).Updates(upd).Error
}

// datatypesJSON marshal helper（避免上层处理 err）
func datatypesJSON(v any) (datatypes.JSON, error) {
	blob, err := marshalJSON(v)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(blob), nil
}

// --- 错误定义 ---

var (
	ErrInstanceNotFound  = errors.New("instance not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrQuotaExceeded     = errors.New("subscription quota exceeded")
	ErrDeleteRunning     = errors.New("cannot delete a running instance, stop it first")
)
