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

	// LastPriceUSDT 用本次成交价；TotalEquity = USDT + 资产 × 成交价
	assert.InDelta(t, 50000.0, pf2.LastPriceUSDT, 1e-6)
	expected := pf2.USDTBalance + (pf2.DeadStackAsset+pf2.FloatStackAsset+pf2.ColdSealedAsset)*pf2.LastPriceUSDT
	assert.InDelta(t, expected, pf2.TotalEquity, 1e-6, "TotalEquity should be reconciled after fill")
	assert.Greater(t, pf2.TotalEquity, 9000.0, "must include both cash and asset value")
}

// recomputeTotalEquity 直接验证（即使没成交，给定价也要算对）。
func TestRecomputeTotalEquity_PriceWeighted(t *testing.T) {
	pf := &store.PortfolioState{
		USDTBalance:    100,
		DeadStackAsset: 0.5,
		FloatStackAsset: 0.3,
		ColdSealedAsset: 0.2,
		LastPriceUSDT:  1000,
	}
	got := recomputeTotalEquity(pf)
	// 100 + (0.5+0.3+0.2)*1000 = 100 + 1000 = 1100
	assert.InDelta(t, 1100.0, got, 1e-9)
}

func TestRecomputeTotalEquity_NoPriceFallsBackToUSDT(t *testing.T) {
	pf := &store.PortfolioState{
		USDTBalance:    500,
		FloatStackAsset: 1.0,
		LastPriceUSDT:  0, // 尚未有成交价
	}
	assert.Equal(t, 500.0, recomputeTotalEquity(pf), "without price, equity == cash")
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

// FAILED 路径也必须重算 TotalEquity（即使没成交价，至少要把现金部分反映出来）。
func TestReconciler_FailedExecution_RecomputesTotalEquity(t *testing.T) {
	db := newTestDB(t)
	user := store.User{Email: "fe@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&user).Error)
	inst := store.StrategyInstance{UserID: user.ID, Name: "fe", Symbol: "BTCUSDT", Status: store.InstRunning, InitialCapitalUSDT: 1000}
	require.NoError(t, db.Create(&inst).Error)
	// 初始 portfolio：有资产 0.5 BTC + 上次成交价 100，TotalEquity 应该是 1000+0.5*100=1050
	require.NoError(t, db.Create(&store.PortfolioState{
		InstanceID: inst.ID, USDTBalance: 1000,
		FloatStackAsset: 0.5, LastPriceUSDT: 100,
		TotalEquity: 9999, // 故意写错值，确认 reconciler 会重算
	}).Error)
	require.NoError(t, db.Create(&store.SpotExecution{
		InstanceID: inst.ID, ClientOrderID: "oid-fe", Action: "BUY", Engine: "MICRO",
		Symbol: "BTCUSDT", LotType: store.LotFloating, AmountUSDT: 50, Status: store.ExecPending,
	}).Error)

	report := wsproto.DeltaReport{
		ClientOrderID: "oid-fe",
		Execution:     &wsproto.ExecutionDetail{ClientOrderID: "oid-fe", Status: "FAILED", ErrorMessage: "x"},
		// Balance 故意改一点：USDT 从 1000 → 950（外部某事件）
		Balances: []wsproto.Balance{
			{Asset: "USDT", Free: "950", Locked: "0"},
			{Asset: "BTC", Free: "0.5", Locked: "0"},
		},
	}

	rec := NewReconciler(db, nil)
	require.NoError(t, rec.HandleDelta(context.Background(), user.ID, report))

	var pf store.PortfolioState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&pf).Error)
	assert.InDelta(t, 950.0, pf.USDTBalance, 1e-6)
	// TotalEquity = USDT(950) + asset(0.5) × LastPriceUSDT(100) = 1000
	assert.InDelta(t, 1000.0, pf.TotalEquity, 1e-6, "must be recomputed, not stale 9999")
	// LastPriceUSDT 沿用旧值（FAILED 路径没有 filledPrice）
	assert.InDelta(t, 100.0, pf.LastPriceUSDT, 1e-6)
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
		// 没有 LastPriceUSDT（首次启动）→ TotalEquity 退化为 USDTBalance
		assert.InDelta(t, 5555.0, pf.TotalEquity, 1e-6, "fallback to cash when no LastPrice")
	}
}

// 初始快照 + 已有 LastPriceUSDT（之前成交过）：TotalEquity 应该按价格重算。
func TestReconciler_InitialSnapshot_RecomputesEquityWithLastPrice(t *testing.T) {
	db := newTestDB(t)
	user := store.User{Email: "snap2@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&user).Error)
	inst := store.StrategyInstance{UserID: user.ID, Name: "i", Symbol: "BTCUSDT", Status: store.InstRunning, InitialCapitalUSDT: 10000}
	require.NoError(t, db.Create(&inst).Error)
	// 已有 LastPriceUSDT=200 + 0.3 BTC（之前成交过）
	require.NoError(t, db.Create(&store.PortfolioState{
		InstanceID: inst.ID, USDTBalance: 100,
		FloatStackAsset: 0.3, LastPriceUSDT: 200,
		TotalEquity: 1, // 故意写错
	}).Error)

	// Agent 重启发来快照：USDT 跳到 500（外部充值），BTC 仍 0.3
	report := wsproto.DeltaReport{
		Balances: []wsproto.Balance{
			{Asset: "USDT", Free: "500", Locked: "0"},
			{Asset: "BTC", Free: "0.3", Locked: "0"},
		},
	}
	rec := NewReconciler(db, nil)
	require.NoError(t, rec.HandleDelta(context.Background(), user.ID, report))

	var pf store.PortfolioState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&pf).Error)
	assert.InDelta(t, 500.0, pf.USDTBalance, 1e-6)
	// TotalEquity = 500 + 0.3 × 200 = 560
	assert.InDelta(t, 560.0, pf.TotalEquity, 1e-6, "must use cached LastPriceUSDT")
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
