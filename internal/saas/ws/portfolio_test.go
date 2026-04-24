package ws

import (
	"context"
	"testing"

	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	raw, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&mode=memory"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, raw.AutoMigrate(store.AllModels()...))
	db := &store.DB{DB: raw}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 端到端：pending SpotExecution + FILLED DeltaReport → SpotExecution updated + TradeRecord written
func TestReconciler_AppliesFilledExecution(t *testing.T) {
	db := newTestDB(t)

	// 种子数据：user + instance + portfolio + pending exec
	user := store.User{Email: "r@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&user).Error)

	inst := store.StrategyInstance{
		UserID: user.ID, TemplateID: 1, Name: "x", Symbol: "BTCUSDT",
		Status: store.InstRunning, InitialCapitalUSDT: 10000,
	}
	require.NoError(t, db.Create(&inst).Error)
	pf := store.PortfolioState{InstanceID: inst.ID, USDTBalance: 10000}
	require.NoError(t, db.Create(&pf).Error)

	exec := store.SpotExecution{
		InstanceID:    inst.ID,
		ClientOrderID: "oid-1",
		Action:        "BUY",
		Engine:        "MACRO",
		Symbol:        "BTCUSDT",
		LotType:       store.LotDeadStack,
		AmountUSDT:    50,
		Status:        store.ExecPending,
	}
	require.NoError(t, db.Create(&exec).Error)

	// 构造 DeltaReport
	report := wsproto.DeltaReport{
		ClientOrderID: "oid-1",
		Execution: &wsproto.ExecutionDetail{
			ClientOrderID: "oid-1",
			Status:        "FILLED",
			FilledQty:     "0.001",
			FilledPrice:   "50000",
			FilledQuote:   "50.00",
			Fee:           "0.05",
			FeeAsset:      "USDT",
		},
		Balances: []wsproto.Balance{
			{Asset: "USDT", Free: "9949.95", Locked: "0"},
			{Asset: "BTC", Free: "0.001", Locked: "0"},
		},
	}

	rec := NewReconciler(db, nil)
	require.NoError(t, rec.HandleDelta(context.Background(), user.ID, report))

	// SpotExecution 应为 FILLED
	var updated store.SpotExecution
	require.NoError(t, db.First(&updated, exec.ID).Error)
	assert.Equal(t, store.ExecFilled, updated.Status)
	assert.InDelta(t, 0.001, updated.FilledQty, 1e-9)

	// TradeRecord 应被创建
	var trades []store.TradeRecord
	require.NoError(t, db.Where("instance_id = ?", inst.ID).Find(&trades).Error)
	require.Len(t, trades, 1)
	assert.Equal(t, "oid-1", trades[0].ClientOrderID)
	assert.InDelta(t, 50.0, trades[0].FilledUSDT, 1e-6)

	// Portfolio USDT 已被 Balances 更新
	var pf2 store.PortfolioState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&pf2).Error)
	assert.InDelta(t, 9949.95, pf2.USDTBalance, 1e-6)
}

func TestReconciler_FailedExecution_NoTradeRecord(t *testing.T) {
	db := newTestDB(t)
	user := store.User{Email: "f@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&user).Error)
	inst := store.StrategyInstance{UserID: user.ID, Name: "f", Symbol: "BTCUSDT", Status: store.InstRunning, InitialCapitalUSDT: 1000}
	require.NoError(t, db.Create(&inst).Error)
	require.NoError(t, db.Create(&store.PortfolioState{InstanceID: inst.ID, USDTBalance: 1000}).Error)
	require.NoError(t, db.Create(&store.SpotExecution{
		InstanceID: inst.ID, ClientOrderID: "oid-f", Action: "BUY", Engine: "MICRO",
		Symbol: "BTCUSDT", LotType: store.LotFloating, AmountUSDT: 50, Status: store.ExecPending,
	}).Error)

	report := wsproto.DeltaReport{
		ClientOrderID: "oid-f",
		Execution: &wsproto.ExecutionDetail{
			ClientOrderID: "oid-f",
			Status:        "FAILED",
			ErrorMessage:  "insufficient balance",
		},
		Balances: []wsproto.Balance{{Asset: "USDT", Free: "1000", Locked: "0"}},
	}

	rec := NewReconciler(db, nil)
	require.NoError(t, rec.HandleDelta(context.Background(), user.ID, report))

	// SpotExecution 应为 FAILED
	var updated store.SpotExecution
	require.NoError(t, db.Where("client_order_id = ?", "oid-f").First(&updated).Error)
	assert.Equal(t, store.ExecFailed, updated.Status)
	assert.Equal(t, "insufficient balance", updated.ErrorMessage)

	// 不应有 TradeRecord
	var count int64
	db.Model(&store.TradeRecord{}).Where("instance_id = ?", inst.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestReconciler_InitialSnapshot_RefreshesBalances(t *testing.T) {
	db := newTestDB(t)
	user := store.User{Email: "snap@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 2}
	require.NoError(t, db.Create(&user).Error)

	for i := 0; i < 2; i++ {
		inst := store.StrategyInstance{
			UserID: user.ID, Name: "i", Symbol: "BTCUSDT",
			Status: store.InstRunning, InitialCapitalUSDT: 1000,
		}
		require.NoError(t, db.Create(&inst).Error)
		require.NoError(t, db.Create(&store.PortfolioState{InstanceID: inst.ID, USDTBalance: 1000}).Error)
	}

	// 初始快照（ClientOrderID 空）
	report := wsproto.DeltaReport{
		Balances: []wsproto.Balance{{Asset: "USDT", Free: "5555", Locked: "0"}},
	}
	rec := NewReconciler(db, nil)
	require.NoError(t, rec.HandleDelta(context.Background(), user.ID, report))

	var portfolios []store.PortfolioState
	require.NoError(t, db.Find(&portfolios).Error)
	require.Len(t, portfolios, 2)
	for _, pf := range portfolios {
		assert.InDelta(t, 5555.0, pf.USDTBalance, 1e-6)
	}
}

func TestReconciler_RejectsMismatchedUser(t *testing.T) {
	db := newTestDB(t)
	// user1 的 exec，但以 user2 的身份上报
	u1 := store.User{Email: "u1@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&u1).Error)
	inst := store.StrategyInstance{UserID: u1.ID, Name: "x", Symbol: "BTCUSDT", Status: store.InstRunning, InitialCapitalUSDT: 1000}
	require.NoError(t, db.Create(&inst).Error)
	require.NoError(t, db.Create(&store.PortfolioState{InstanceID: inst.ID, USDTBalance: 1000}).Error)
	require.NoError(t, db.Create(&store.SpotExecution{
		InstanceID: inst.ID, ClientOrderID: "oid-m", Action: "BUY", Engine: "MACRO",
		Symbol: "BTCUSDT", LotType: store.LotDeadStack, AmountUSDT: 50, Status: store.ExecPending,
	}).Error)

	rec := NewReconciler(db, nil)
	err := rec.HandleDelta(context.Background(), 999, wsproto.DeltaReport{
		ClientOrderID: "oid-m",
		Execution:     &wsproto.ExecutionDetail{Status: "FILLED"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "different user")
}

func TestReconciler_UnknownClientOrderID(t *testing.T) {
	db := newTestDB(t)
	rec := NewReconciler(db, nil)
	err := rec.HandleDelta(context.Background(), 1, wsproto.DeltaReport{
		ClientOrderID: "missing",
		Execution:     &wsproto.ExecutionDetail{Status: "FILLED"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pending execution")
}
