package instance

import (
	"context"
	"testing"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB 构建一个 SQLite in-memory DB 并 AutoMigrate 全部模型。
// 测试结束由 t.Cleanup 关闭。
// 注意：SQLite 对 numeric(20,8) 等 Postgres 方言的类型会降级为 REAL/NUMERIC，
// 业务精度略有差异但不影响状态机/流程测试。
func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	raw, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, raw.AutoMigrate(store.AllModels()...))
	db := &store.DB{DB: raw}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// staticBarSource 测试用 BarSource：返回预设的 bars。
type staticBarSource struct{ bars []quant.Bar }

func (s *staticBarSource) LoadBars(_ context.Context, _, _ string, _ int) ([]quant.Bar, error) {
	return s.bars, nil
}

// buildUpTrendBars 构造一条线性上升的合成 K 线（足够长通过 MinBars 检查）。
func buildUpTrendBars(n int, base float64) []quant.Bar {
	bars := make([]quant.Bar, n)
	for i := 0; i < n; i++ {
		p := base + float64(i)*0.3
		bars[i] = quant.Bar{
			OpenTime:  int64(i) * 60_000,
			CloseTime: int64(i)*60_000 + 59_999,
			Open:      p, High: p, Low: p, Close: p, Volume: 1,
		}
	}
	return bars
}

// seedUser 创建一个用户记录（带 MaxInstances 配额）。
func seedUser(t *testing.T, db *store.DB, email string, maxInsts int) store.User {
	t.Helper()
	u := store.User{
		Email:        email,
		PasswordHash: "test",
		Plan:         store.PlanFree,
		MaxInstances: maxInsts,
	}
	require.NoError(t, db.Create(&u).Error)
	return u
}

// seedTemplate 创建一个策略模板。
func seedTemplate(t *testing.T, db *store.DB) store.StrategyTemplate {
	t.Helper()
	tpl := store.StrategyTemplate{
		StrategyID: "sigmoid-btc",
		Name:       "Sigmoid BTC",
		Version:    "0.1.0",
		IsSpot:     true,
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
	}
	require.NoError(t, db.Create(&tpl).Error)
	return tpl
}

// newGenomeStoreForTest 构造一个无 Redis 的 GenomeStore（DB-only）。
func newGenomeStoreForTest(db *store.DB) *ga.GenomeStore {
	return &ga.GenomeStore{DB: db, Redis: nil}
}
