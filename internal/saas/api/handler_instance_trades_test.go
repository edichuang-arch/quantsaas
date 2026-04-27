package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 用 SQLite in-memory 起一个 DB + 写 N 笔 TradeRecord，覆盖 ListTrades 分页 + 筛选。
//
// 重点：
//   - 分页：page=2, page_size=10 应该返回第 11–20 笔
//   - 篩選：action=BUY 只回 BUY；engine=MICRO 只回 MICRO
//   - 边界：page_size 越界（>200）应被夹紧；page=0 应退回 1
//   - 异用户：不能跨 user_id 看别人的成交
func TestListTrades_PaginationAndFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTradesTestDB(t)

	// 种 user + instance + 30 笔成交（10 BUY/MACRO/DEAD_STACK + 10 SELL/MICRO/FLOATING + 10 BUY/MICRO/FLOATING）
	user := store.User{Email: "t@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&user).Error)
	other := store.User{Email: "o@b.com", PasswordHash: "x", Plan: store.PlanFree, MaxInstances: 1}
	require.NoError(t, db.Create(&other).Error)

	inst := store.StrategyInstance{UserID: user.ID, Name: "i", Symbol: "BTCUSDT", Status: store.InstRunning, InitialCapitalUSDT: 10000}
	require.NoError(t, db.Create(&inst).Error)

	type spec struct {
		action, engine string
		lot            store.LotType
		count          int
	}
	specs := []spec{
		{"BUY", "MACRO", store.LotDeadStack, 10},
		{"SELL", "MICRO", store.LotFloating, 10},
		{"BUY", "MICRO", store.LotFloating, 10},
	}
	id := 0
	for _, s := range specs {
		for i := 0; i < s.count; i++ {
			id++
			require.NoError(t, db.Create(&store.TradeRecord{
				InstanceID: inst.ID, ClientOrderID: fmt.Sprintf("oid-%d", id),
				Action: s.action, Engine: s.engine, Symbol: "BTCUSDT", LotType: s.lot,
				FilledQty: 0.001, FilledPrice: 50000, FilledUSDT: 50, Fee: 0.05,
			}).Error)
		}
	}

	h := &InstanceHandler{DB: db}

	// 1. 默认分页：page=1, page_size=50 → 共 30 笔，全部返回
	resp := callListTrades(t, h, user.ID, inst.ID, "")
	assert.Equal(t, http.StatusOK, resp.Code)
	body := decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, int64(30), body.Total)
	assert.Equal(t, 1, body.Page)
	assert.Equal(t, 50, body.PageSize)
	assert.Len(t, body.Data, 30)

	// 2. 分页：page=2, page_size=10 → 应回 10 笔
	resp = callListTrades(t, h, user.ID, inst.ID, "?page=2&page_size=10")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, 2, body.Page)
	assert.Equal(t, 10, body.PageSize)
	assert.Equal(t, int64(30), body.Total)
	assert.Len(t, body.Data, 10)

	// 3. action 筛选：BUY → 20 笔（MACRO/DEAD + MICRO/FLOATING 两组）
	resp = callListTrades(t, h, user.ID, inst.ID, "?action=BUY&page_size=200")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, int64(20), body.Total)
	for _, tr := range body.Data {
		assert.Equal(t, "BUY", tr.Action)
	}

	// 4. engine 筛选：MICRO → 20 笔
	resp = callListTrades(t, h, user.ID, inst.ID, "?engine=MICRO&page_size=200")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, int64(20), body.Total)

	// 5. 复合筛选：action=BUY + engine=MICRO → 10 笔
	resp = callListTrades(t, h, user.ID, inst.ID, "?action=BUY&engine=MICRO&page_size=200")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, int64(10), body.Total)

	// 6. lot_type 筛选
	resp = callListTrades(t, h, user.ID, inst.ID, "?lot_type=DEAD_STACK&page_size=200")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, int64(10), body.Total)

	// 7. 边界：page_size > 200 应被夹紧到 200
	resp = callListTrades(t, h, user.ID, inst.ID, "?page_size=9999")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, 200, body.PageSize)

	// 8. 边界：page=0 / 负数应退回 1
	resp = callListTrades(t, h, user.ID, inst.ID, "?page=0")
	body = decodeTrades(t, resp.Body.Bytes())
	assert.Equal(t, 1, body.Page)

	// 9. 异用户：other 用 own user_id 但访问 user1 的 instance → 404
	resp = callListTrades(t, h, other.ID, inst.ID, "")
	assert.Equal(t, http.StatusNotFound, resp.Code)

	// 10. 不存在的 instance → 404
	resp = callListTrades(t, h, user.ID, 99999, "")
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

// callListTrades 模拟一个已通过 auth 中间件的请求，直接调用 handler。
func callListTrades(t *testing.T, h *InstanceHandler, userID, instanceID uint, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := gin.New()
	r.GET("/api/v1/instances/:id/trades", func(c *gin.Context) {
		c.Set(ctxKeyClaims, &auth.Claims{UserID: userID})
		h.ListTrades(c)
	})
	url := fmt.Sprintf("/api/v1/instances/%d/trades%s", instanceID, query)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	r.ServeHTTP(w, req)
	return w
}

func decodeTrades(t *testing.T, raw []byte) TradesResponse {
	t.Helper()
	var resp TradesResponse
	require.NoError(t, json.Unmarshal(raw, &resp))
	return resp
}

func newTradesTestDB(t *testing.T) *store.DB {
	t.Helper()
	raw, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&mode=memory"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, raw.AutoMigrate(store.AllModels()...))
	db := &store.DB{DB: raw}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// parseIntDefault 边界单元测试。
func TestParseIntDefault(t *testing.T) {
	assert.Equal(t, 50, parseIntDefault("", 50, 1, 200))     // 空值 → default
	assert.Equal(t, 50, parseIntDefault("abc", 50, 1, 200))  // 无法解析 → default
	assert.Equal(t, 1, parseIntDefault("0", 50, 1, 200))     // 越界小 → lo
	assert.Equal(t, 200, parseIntDefault("9999", 50, 1, 200)) // 越界大 → hi
	assert.Equal(t, 75, parseIntDefault("75", 50, 1, 200))   // 正常
}
