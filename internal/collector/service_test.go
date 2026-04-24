package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/edi/quantsaas/internal/saas/store"
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

// generateBars 生成 n 根连续 5m bar，从 startMs 开始。
// 返回值是 Binance 二维矩阵 JSON 格式（字符串/数字混合）。
func generateBars(startMs, n int) [][]any {
	out := make([][]any, n)
	step := int64(5 * 60 * 1000)
	for i := 0; i < n; i++ {
		openTime := int64(startMs) + int64(i)*step
		price := 100.0 + float64(i)*0.5
		out[i] = []any{
			openTime,
			strconv.FormatFloat(price, 'f', 2, 64),
			strconv.FormatFloat(price+0.1, 'f', 2, 64),
			strconv.FormatFloat(price-0.1, 'f', 2, 64),
			strconv.FormatFloat(price, 'f', 2, 64),
			"1.0",
			openTime + step - 1,
			"100",
			10,
			"0.5",
			"50",
			"0",
		}
	}
	return out
}

// mockBinanceWithBatch 模擬能返回指定 bars 的 Binance server；每次請求會根據 startTime 切片。
func mockBinanceWithBatch(bars [][]any) *httptest.Server {
	step := int64(5 * 60 * 1000)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		startMs, _ := strconv.ParseInt(q.Get("startTime"), 10, 64)
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit == 0 {
			limit = 1000
		}
		// 找到 openTime >= startMs 的第一根
		idx := 0
		for i, row := range bars {
			if toInt64(row[0]) >= startMs {
				idx = i
				break
			}
			if i == len(bars)-1 {
				idx = len(bars)
			}
		}
		end := idx + limit
		if end > len(bars) {
			end = len(bars)
		}
		_ = json.NewEncoder(w).Encode(bars[idx:end])
		_ = step
	}))
}

func TestBackfill_InsertsAllBars(t *testing.T) {
	db := newTestDB(t)
	bars := generateBars(1_700_000_000_000, 50)
	srv := mockBinanceWithBatch(bars)
	defer srv.Close()

	svc := NewService(db, NewPublicClient(srv.URL), nil)
	svc.RequestGapMs = 0 // 测试不延迟

	fromMs := int64(1_700_000_000_000)
	toMs := fromMs + int64(50*5*60*1000) + 1
	ins, skp, err := svc.Backfill(context.Background(), "BTCUSDT", "5m", fromMs, toMs)
	require.NoError(t, err)
	assert.Equal(t, 50, ins)
	assert.Equal(t, 0, skp)

	// DB 应有 50 笔
	var count int64
	require.NoError(t, db.Model(&store.KLine{}).Count(&count).Error)
	assert.Equal(t, int64(50), count)
}

func TestBackfill_IdempotentReRun(t *testing.T) {
	db := newTestDB(t)
	bars := generateBars(1_700_000_000_000, 20)
	srv := mockBinanceWithBatch(bars)
	defer srv.Close()

	svc := NewService(db, NewPublicClient(srv.URL), nil)
	svc.RequestGapMs = 0

	fromMs := int64(1_700_000_000_000)
	toMs := fromMs + int64(20*5*60*1000) + 1

	// 第一次：全插入
	ins1, _, err := svc.Backfill(context.Background(), "BTCUSDT", "5m", fromMs, toMs)
	require.NoError(t, err)
	assert.Equal(t, 20, ins1)

	// 第二次：全部被去重（ON CONFLICT DO NOTHING）
	ins2, skp2, err := svc.Backfill(context.Background(), "BTCUSDT", "5m", fromMs, toMs)
	require.NoError(t, err)
	assert.Equal(t, 0, ins2, "re-run should not insert duplicates")
	assert.Equal(t, 20, skp2, "all 20 should be reported skipped")
}

func TestBackfill_RejectsInvalidInterval(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil)
	_, _, err := svc.Backfill(context.Background(), "BTCUSDT", "weird", 1, 2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported interval")
}

func TestBackfill_RejectsZeroFrom(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil)
	_, _, err := svc.Backfill(context.Background(), "BTCUSDT", "5m", 0, 100)
	assert.Error(t, err)
}

func TestLatestOpenTime_EmptyReturnsZero(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil)
	ts, err := svc.LatestOpenTime(context.Background(), "BTCUSDT", "5m")
	require.NoError(t, err)
	assert.Equal(t, int64(0), ts)
}

func TestLatestOpenTime_ReturnsMax(t *testing.T) {
	db := newTestDB(t)
	// 种 3 根 bar
	bars := []store.KLine{
		{Symbol: "BTCUSDT", Interval: "5m", OpenTime: 100, CloseTime: 200, Close: 1},
		{Symbol: "BTCUSDT", Interval: "5m", OpenTime: 300, CloseTime: 400, Close: 2},
		{Symbol: "BTCUSDT", Interval: "5m", OpenTime: 500, CloseTime: 600, Close: 3},
	}
	require.NoError(t, db.CreateInBatches(bars, 10).Error)

	svc := NewService(db, nil, nil)
	ts, err := svc.LatestOpenTime(context.Background(), "BTCUSDT", "5m")
	require.NoError(t, err)
	assert.Equal(t, int64(500), ts)
}
