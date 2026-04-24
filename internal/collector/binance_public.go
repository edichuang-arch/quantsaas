// Package collector 历史 K 线收集器。
//
// 独立于 LocalAgent：本包只读取 Binance **公开** K 线端点
// （GET /api/v3/klines 不需要 API Key），将资料写入 store.KLine 表。
//
// 用途：
//   - CLI `cmd/collector` 做一次性补历史
//   - SaaS 启动后每 5 分钟拉最新 1-2 根 bar（增量同步）
//
// 铁律 #5 不适用：本包不持有任何私钥，所有请求都是公开资料。
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/edi/quantsaas/internal/quant"
)

const (
	defaultBaseURL = "https://api.binance.com"
	// Binance /api/v3/klines 的 limit 上限
	maxKlinesPerReq = 1000
)

// PublicClient 只读 K 线客户端。
// 无状态；可并发复用。
type PublicClient struct {
	baseURL string
	httpc   *http.Client
}

// NewPublicClient 构造客户端。baseURL 空使用 https://api.binance.com（测试时可指向 httptest）。
func NewPublicClient(baseURL string) *PublicClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &PublicClient{
		baseURL: baseURL,
		httpc:   &http.Client{Timeout: 15 * time.Second},
	}
}

// FetchKLines 拉取 [startMs, endMs] 区间的 K 线。
// endMs = 0 表示不设上限（由 limit 控制）。
// limit 最多 1000；大于则自动截断。
func (c *PublicClient) FetchKLines(
	ctx context.Context,
	symbol, interval string,
	startMs, endMs int64,
	limit int,
) ([]quant.Bar, error) {
	if limit <= 0 || limit > maxKlinesPerReq {
		limit = maxKlinesPerReq
	}
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))
	if startMs > 0 {
		params.Set("startTime", strconv.FormatInt(startMs, 10))
	}
	if endMs > 0 {
		params.Set("endTime", strconv.FormatInt(endMs, 10))
	}

	full := c.baseURL + "/api/v3/klines?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch klines: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance HTTP %d: %s", resp.StatusCode, string(raw))
	}
	// Binance 返回二维数组，每根 bar 为 [openTime, open, high, low, close, volume, closeTime, ...]
	var matrix [][]any
	if err := json.Unmarshal(raw, &matrix); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}
	bars := make([]quant.Bar, 0, len(matrix))
	for _, row := range matrix {
		if len(row) < 7 {
			continue
		}
		bars = append(bars, quant.Bar{
			OpenTime:  toInt64(row[0]),
			Open:      toFloat(row[1]),
			High:      toFloat(row[2]),
			Low:       toFloat(row[3]),
			Close:     toFloat(row[4]),
			Volume:    toFloat(row[5]),
			CloseTime: toInt64(row[6]),
		})
	}
	return bars, nil
}

// ServerTime 返回 Binance 服务器当前毫秒时间（用于对齐）。
func (c *PublicClient) ServerTime(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v3/time", nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var out struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.ServerTime, nil
}

// --- 内部工具：Binance K 线返回的是 JSON 里 string/number 混合，需要容错转换 ---

func toInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	case int64:
		return x
	}
	return 0
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// IntervalDurationMs 返回 Binance interval 字串对应的毫秒数。
// 支援常见值：1m / 5m / 15m / 1h / 4h / 1d；其他返回 0（调用方需处理）。
func IntervalDurationMs(interval string) int64 {
	switch interval {
	case "1m":
		return 60_000
	case "5m":
		return 5 * 60_000
	case "15m":
		return 15 * 60_000
	case "1h":
		return 60 * 60_000
	case "4h":
		return 4 * 60 * 60_000
	case "1d":
		return 24 * 60 * 60_000
	}
	return 0
}
