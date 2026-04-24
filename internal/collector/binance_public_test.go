package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock Binance /api/v3/klines 端点。
func newMockBinance(t *testing.T) (*httptest.Server, *[]url.Values) {
	t.Helper()
	captured := &[]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = append(*captured, r.URL.Query())
		switch r.URL.Path {
		case "/api/v3/klines":
			// 返回 2 根假 bar；字段：[openTime, open, high, low, close, volume, closeTime, ...]
			_, _ = w.Write([]byte(`[
				[1700000000000, "50000.00", "50100.00", "49900.00", "50050.00", "1.23", 1700000299999, "61530.15", 10, "0.6", "30015.00", "0"],
				[1700000300000, "50050.00", "50200.00", "50000.00", "50150.00", "2.45", 1700000599999, "122867.50", 15, "1.2", "60090.00", "0"]
			]`))
		case "/api/v3/time":
			_, _ = w.Write([]byte(`{"serverTime": 1700000999999}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestFetchKLines_ParsesMatrix(t *testing.T) {
	srv, captured := newMockBinance(t)
	c := NewPublicClient(srv.URL)

	bars, err := c.FetchKLines(context.Background(), "BTCUSDT", "5m", 0, 0, 500)
	require.NoError(t, err)
	require.Len(t, bars, 2)

	assert.Equal(t, int64(1700000000000), bars[0].OpenTime)
	assert.InDelta(t, 50000.0, bars[0].Open, 1e-9)
	assert.InDelta(t, 50050.0, bars[0].Close, 1e-9)
	assert.InDelta(t, 1.23, bars[0].Volume, 1e-9)
	assert.Equal(t, int64(1700000299999), bars[0].CloseTime)

	// Query 参数正确
	q := (*captured)[0]
	assert.Equal(t, "BTCUSDT", q.Get("symbol"))
	assert.Equal(t, "5m", q.Get("interval"))
	assert.Equal(t, "500", q.Get("limit"))
}

func TestFetchKLines_ClampsLimitTo1000(t *testing.T) {
	srv, captured := newMockBinance(t)
	c := NewPublicClient(srv.URL)

	_, err := c.FetchKLines(context.Background(), "BTCUSDT", "5m", 0, 0, 9999)
	require.NoError(t, err)
	q := (*captured)[0]
	assert.Equal(t, "1000", q.Get("limit"))
}

func TestFetchKLines_StartTimeEndTimeForwarded(t *testing.T) {
	srv, captured := newMockBinance(t)
	c := NewPublicClient(srv.URL)

	_, err := c.FetchKLines(context.Background(), "BTCUSDT", "5m", 1000, 2000, 100)
	require.NoError(t, err)
	q := (*captured)[0]
	assert.Equal(t, "1000", q.Get("startTime"))
	assert.Equal(t, "2000", q.Get("endTime"))
}

func TestFetchKLines_HTTPErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":-1003,"msg":"too many requests"}`))
	}))
	defer srv.Close()

	c := NewPublicClient(srv.URL)
	_, err := c.FetchKLines(context.Background(), "BTCUSDT", "5m", 0, 0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 429")
}

func TestServerTime(t *testing.T) {
	srv, _ := newMockBinance(t)
	c := NewPublicClient(srv.URL)
	ts, err := c.ServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1700000999999), ts)
}

func TestIntervalDurationMs(t *testing.T) {
	cases := map[string]int64{
		"1m":  60_000,
		"5m":  5 * 60_000,
		"15m": 15 * 60_000,
		"1h":  60 * 60_000,
		"4h":  4 * 60 * 60_000,
		"1d":  24 * 60 * 60_000,
	}
	for iv, want := range cases {
		assert.Equal(t, want, IntervalDurationMs(iv), iv)
	}
	assert.Equal(t, int64(0), IntervalDurationMs("unknown"))
}

func TestToFloatToInt_Coercion(t *testing.T) {
	assert.Equal(t, 1.23, toFloat("1.23"))
	assert.Equal(t, 1.23, toFloat(float64(1.23)))
	assert.Equal(t, 0.0, toFloat(nil))

	assert.Equal(t, int64(42), toInt64("42"))
	assert.Equal(t, int64(42), toInt64(float64(42)))
	assert.Equal(t, int64(42), toInt64(int64(42)))
}
