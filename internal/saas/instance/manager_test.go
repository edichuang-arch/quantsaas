package instance

import (
	"context"
	"testing"

	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_Create_Success(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)

	user := seedUser(t, db, "a@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, err := mgr.Create(context.Background(), CreateRequest{
		UserID:             user.ID,
		TemplateID:         tpl.ID,
		Name:               "My BTC Bot",
		Symbol:             "BTCUSDT",
		InitialCapitalUSDT: 10000,
		MonthlyInjectUSDT:  300,
	})
	require.NoError(t, err)
	assert.Equal(t, store.InstStopped, inst.Status)
	assert.Equal(t, 10000.0, inst.InitialCapitalUSDT)

	// 同时创建了 PortfolioState 和 RuntimeState
	var pf store.PortfolioState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&pf).Error)
	assert.Equal(t, 10000.0, pf.USDTBalance)

	var rt store.RuntimeState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&rt).Error)
	assert.NotNil(t, rt.Content)
}

func TestManager_Create_EnforcesQuota(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "q@b.com", 1) // 配额=1
	tpl := seedTemplate(t, db)

	_, err := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "first",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	require.NoError(t, err)

	// 第二个实例应被配额拒绝
	_, err = mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "second",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	assert.ErrorIs(t, err, ErrQuotaExceeded)
}

func TestManager_Create_RejectsZeroCapital(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "z@b.com", 5)
	tpl := seedTemplate(t, db)

	_, err := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "x",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 0,
	})
	assert.Error(t, err)
}

func TestManager_StartStop_StateMachine(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "s@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "s",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})

	// 起始 STOPPED
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	var reloaded store.StrategyInstance
	require.NoError(t, db.First(&reloaded, inst.ID).Error)
	assert.Equal(t, store.InstRunning, reloaded.Status)
	assert.NotNil(t, reloaded.StartedAt)

	// Start 幂等性：RUNNING → Start 会失败（from 不含 RUNNING）
	err := mgr.Start(context.Background(), inst.ID, user.ID)
	assert.ErrorIs(t, err, ErrInvalidTransition)

	// Stop
	require.NoError(t, mgr.Stop(context.Background(), inst.ID, user.ID))
	require.NoError(t, db.First(&reloaded, inst.ID).Error)
	assert.Equal(t, store.InstStopped, reloaded.Status)
	assert.NotNil(t, reloaded.StoppedAt)
}

func TestManager_Stop_FromErrorWorks(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "e@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "e",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	// 手动改为 ERROR
	require.NoError(t, db.Model(&inst).Update("status", store.InstError).Error)

	// ERROR → Stop 应能成功（Stop 的 from 包含 Running / Error）
	require.NoError(t, mgr.Stop(context.Background(), inst.ID, user.ID))
}

func TestManager_Delete_RefusesRunning(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "d@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "d",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	err := mgr.Delete(context.Background(), inst.ID, user.ID)
	assert.ErrorIs(t, err, ErrDeleteRunning)
}

func TestManager_Delete_StoppedInstance(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "ok@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "ok",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	require.NoError(t, mgr.Delete(context.Background(), inst.ID, user.ID))
	// 软删除：再查应 not found（GORM default）
	var reloaded store.StrategyInstance
	err := db.First(&reloaded, inst.ID).Error
	assert.Error(t, err)
}

func TestManager_ListRunning_OnlyReturnsRunning(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "l@b.com", 5)
	tpl := seedTemplate(t, db)

	for i := 0; i < 3; i++ {
		inst, _ := mgr.Create(context.Background(), CreateRequest{
			UserID: user.ID, TemplateID: tpl.ID,
			Name:   "inst",
			Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
		})
		if i < 2 {
			require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
		}
	}
	running, err := mgr.ListRunning(context.Background())
	require.NoError(t, err)
	assert.Len(t, running, 2)
}

func TestManager_MarkError_WritesAuditLog(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	user := seedUser(t, db, "er@b.com", 5)
	tpl := seedTemplate(t, db)

	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "er",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 1000,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	require.NoError(t, mgr.MarkError(context.Background(), inst.ID, "tick timeout"))

	var reloaded store.StrategyInstance
	require.NoError(t, db.First(&reloaded, inst.ID).Error)
	assert.Equal(t, store.InstError, reloaded.Status)

	var logs []store.AuditLog
	require.NoError(t, db.Where("instance_id = ? AND event_type = ?", inst.ID, "instance_error").Find(&logs).Error)
	assert.Len(t, logs, 1)
}
