package exchange

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sign 的单元测试：相同输入 → 相同 HMAC；不同 secret → 不同结果。
func TestSign_Deterministic(t *testing.T) {
	a := sign("secret", "symbol=BTCUSDT&timestamp=123")
	b := sign("secret", "symbol=BTCUSDT&timestamp=123")
	assert.Equal(t, a, b)

	c := sign("other", "symbol=BTCUSDT&timestamp=123")
	assert.NotEqual(t, a, c)

	// HMAC 输出为 64 位 hex（32 字节 × 2）
	assert.Len(t, a, 64)
}

func TestNewClient_UsesSandboxURL(t *testing.T) {
	c := NewClient("k", "s", true, "")
	assert.Equal(t, "https://testnet.binance.vision", c.baseURL)
}

func TestNewClient_RespectsCustomBaseURL(t *testing.T) {
	c := NewClient("k", "s", false, "http://mock.local/")
	assert.Equal(t, "http://mock.local", c.baseURL)
}

// GetBalances 与 PlaceOrder 端到端：用 httptest.Server 模拟 Binance。
func TestGetBalances_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/account", r.URL.Path)
		assert.NotEmpty(t, r.URL.Query().Get("signature"))
		assert.Equal(t, "test-key", r.Header.Get("X-MBX-APIKEY"))
		_, _ = w.Write([]byte(`{"balances":[
			{"asset":"BTC","free":"0.5","locked":"0"},
			{"asset":"USDT","free":"1000","locked":"10"},
			{"asset":"DOGE","free":"0","locked":"0"}
		]}`))
	}))
	defer srv.Close()

	c := NewClient("test-key", "test-secret", false, srv.URL)
	bals, err := c.GetBalances()
	require.NoError(t, err)
	// DOGE 余额为 0 → 被过滤掉
	assert.Len(t, bals, 2)

	// 确认每个字段
	hasBTC, hasUSDT := false, false
	for _, b := range bals {
		if b.Asset == "BTC" {
			hasBTC = true
			assert.Equal(t, "0.5", b.Free)
		}
		if b.Asset == "USDT" {
			hasUSDT = true
			assert.Equal(t, "1000", b.Free)
			assert.Equal(t, "10", b.Locked)
		}
	}
	assert.True(t, hasBTC)
	assert.True(t, hasUSDT)
}

func TestPlaceOrder_BUYSendsQuoteOrderQty(t *testing.T) {
	var receivedBody []byte
	var receivedQS url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/order", r.URL.Path)
		receivedBody, _ = io.ReadAll(r.Body)
		receivedQS = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol":              "BTCUSDT",
			"clientOrderId":       "oid-1",
			"status":              "FILLED",
			"executedQty":         "0.001",
			"cummulativeQuoteQty": "50.00",
			"fills": []map[string]any{
				{"price": "50000", "qty": "0.001", "commission": "0.05", "commissionAsset": "USDT"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient("k", "s", false, srv.URL)
	exec, err := c.PlaceOrder(wsproto.TradeCommand{
		ClientOrderID: "oid-1",
		Action:        "BUY",
		Symbol:        "BTCUSDT",
		AmountUSDT:    "50.00",
		LotType:       "DEAD_STACK",
	})
	require.NoError(t, err)
	_ = receivedBody // 当前实现走 query string

	// quoteOrderQty 应该在 query 里
	assert.Equal(t, "50.00", receivedQS.Get("quoteOrderQty"))
	assert.Equal(t, "BUY", receivedQS.Get("side"))
	assert.Equal(t, "MARKET", receivedQS.Get("type"))

	// 返回的 ExecutionDetail
	assert.Equal(t, "FILLED", exec.Status)
	assert.Equal(t, "0.001", exec.FilledQty)
	assert.Equal(t, "50000", exec.FilledPrice) // 均价
	assert.Equal(t, "USDT", exec.FeeAsset)
}

func TestPlaceOrder_SELLSendsQuantity(t *testing.T) {
	var receivedQS url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQS = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol":              "BTCUSDT",
			"clientOrderId":       "oid-2",
			"status":              "FILLED",
			"executedQty":         "0.001",
			"cummulativeQuoteQty": "50.00",
			"fills":               []map[string]any{},
		})
	}))
	defer srv.Close()

	c := NewClient("k", "s", false, srv.URL)
	_, err := c.PlaceOrder(wsproto.TradeCommand{
		ClientOrderID: "oid-2",
		Action:        "SELL",
		Symbol:        "BTCUSDT",
		QtyAsset:      "0.001",
	})
	require.NoError(t, err)
	assert.Equal(t, "0.001", receivedQS.Get("quantity"))
	assert.Equal(t, "SELL", receivedQS.Get("side"))
}

func TestPlaceOrder_RejectsInvalidAction(t *testing.T) {
	c := NewClient("k", "s", false, "http://example.com")
	_, err := c.PlaceOrder(wsproto.TradeCommand{Action: "INVALID"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid action")
}

func TestPlaceOrder_PropagatesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"msg":"INVALID_PARAM"}`))
	}))
	defer srv.Close()

	c := NewClient("k", "s", false, srv.URL)
	_, err := c.PlaceOrder(wsproto.TradeCommand{
		ClientOrderID: "oid",
		Action:        "BUY",
		Symbol:        "BTCUSDT",
		AmountUSDT:    "10",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
}

// 保证 strings 包被引用（防 import 工具删除）
var _ = strings.Contains
