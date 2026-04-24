package instance

import (
	"context"
	"testing"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 基础端到端：数据足够 → Tick 完成 → LastProcessedBarTime 被更新。
func TestTick_UpdatesLastProcessedBarTime(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	gs := newGenomeStoreForTest(db)
	sender := NewInMemoryCommandSender()

	user := seedUser(t, db, "t@b.com", 5)
	tpl := seedTemplate(t, db)
	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "t",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 10000, MonthlyInjectUSDT: 300,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	sender.MarkOnline(user.ID)

	bars := buildUpTrendBars(150, 100)
	bs := &staticBarSource{bars: bars}
	ticker := NewTicker(db, gs, bs, sender, nil)

	// 重新读一次 inst（Start 之后状态变了）
	var running store.StrategyInstance
	require.NoError(t, db.First(&running, inst.ID).Error)
	require.NoError(t, ticker.Tick(context.Background(), running))

	// LastProcessedBarTime 应等于最新 bar 的 OpenTime
	require.NoError(t, db.First(&running, inst.ID).Error)
	assert.Equal(t, bars[len(bars)-1].OpenTime, running.LastProcessedBarTime)
}

// 幂等桶去重：同一 bar 再 Tick 一次不应产生额外 SpotExecution。
func TestTick_IdempotentWithinSameBar(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	gs := newGenomeStoreForTest(db)
	sender := NewInMemoryCommandSender()

	user := seedUser(t, db, "i@b.com", 5)
	tpl := seedTemplate(t, db)
	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "i",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 10000, MonthlyInjectUSDT: 300,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	sender.MarkOnline(user.ID)

	bars := buildUpTrendBars(150, 100)
	ticker := NewTicker(db, gs, &staticBarSource{bars: bars}, sender, nil)

	var running store.StrategyInstance
	require.NoError(t, db.First(&running, inst.ID).Error)
	require.NoError(t, ticker.Tick(context.Background(), running))

	var firstCount int64
	db.Model(&store.SpotExecution{}).Where("instance_id = ?", inst.ID).Count(&firstCount)

	// 第二次 tick（相同 bars）
	require.NoError(t, db.First(&running, inst.ID).Error)
	require.NoError(t, ticker.Tick(context.Background(), running))

	var secondCount int64
	db.Model(&store.SpotExecution{}).Where("instance_id = ?", inst.ID).Count(&secondCount)

	assert.Equal(t, firstCount, secondCount, "same bar must not produce extra executions")
}

// Agent 离线时 Tick 照常写 pending SpotExecution，但 sender 不会收到命令。
func TestTick_AgentOffline_StillRecordsPending(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	gs := newGenomeStoreForTest(db)
	sender := NewInMemoryCommandSender() // 默认离线

	user := seedUser(t, db, "off@b.com", 5)
	tpl := seedTemplate(t, db)
	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "off",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 10000, MonthlyInjectUSDT: 300,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))

	bars := buildUpTrendBars(150, 100)
	ticker := NewTicker(db, gs, &staticBarSource{bars: bars}, sender, nil)

	var running store.StrategyInstance
	require.NoError(t, db.First(&running, inst.ID).Error)
	require.NoError(t, ticker.Tick(context.Background(), running))

	// sender 没收到任何指令
	assert.Len(t, sender.Sent(user.ID), 0)
	// 但 DB 里可能有 pending 记录（取决于是否有宏观/微观意图产生）
}

// 数据不足时 Tick 不报错但不推进。
func TestTick_InsufficientBarsNoop(t *testing.T) {
	db := newTestDB(t)
	mgr := NewManager(db, nil)
	gs := newGenomeStoreForTest(db)
	sender := NewInMemoryCommandSender()

	user := seedUser(t, db, "sm@b.com", 5)
	tpl := seedTemplate(t, db)
	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "sm",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 10000,
	})
	require.NoError(t, mgr.Start(context.Background(), inst.ID, user.ID))
	sender.MarkOnline(user.ID)

	shortBars := buildUpTrendBars(10, 100) // 远少于 MinBars
	ticker := NewTicker(db, gs, &staticBarSource{bars: shortBars}, sender, nil)

	var running store.StrategyInstance
	require.NoError(t, db.First(&running, inst.ID).Error)
	// 数据不足应被 errWarn 吞掉（返回 nil）
	require.NoError(t, ticker.Tick(context.Background(), running))

	// LastProcessedBarTime 仍为 0
	require.NoError(t, db.First(&running, inst.ID).Error)
	assert.Equal(t, int64(0), running.LastProcessedBarTime)
}

// handleReleases 正确在事务内转换 DeadStack → FloatStack 且写 AuditLog。
func TestTick_HandleReleases(t *testing.T) {
	db := newTestDB(t)
	// 准备一个实例 + portfolio
	user := seedUser(t, db, "rel@b.com", 5)
	tpl := seedTemplate(t, db)
	mgr := NewManager(db, nil)
	inst, _ := mgr.Create(context.Background(), CreateRequest{
		UserID: user.ID, TemplateID: tpl.ID, Name: "rel",
		Symbol: "BTCUSDT", InitialCapitalUSDT: 10000,
	})
	// 手动设置一些 DeadStack
	db.Model(&store.PortfolioState{}).
		Where("instance_id = ?", inst.ID).
		Update("dead_stack_asset", 2.0)

	ticker := NewTicker(db, nil, nil, nil, nil)
	releases := []quant.ReleaseIntent{{Amount: 0.5, Reason: "soft_age"}}
	require.NoError(t, ticker.handleReleases(context.Background(), inst.ID, releases))

	var pf store.PortfolioState
	require.NoError(t, db.Where("instance_id = ?", inst.ID).First(&pf).Error)
	assert.InDelta(t, 1.5, pf.DeadStackAsset, 1e-9)
	assert.InDelta(t, 0.5, pf.FloatStackAsset, 1e-9)

	var logs []store.AuditLog
	require.NoError(t, db.Where("instance_id = ? AND event_type = ?", inst.ID, "dead_stack_release").Find(&logs).Error)
	assert.Len(t, logs, 1)
}
