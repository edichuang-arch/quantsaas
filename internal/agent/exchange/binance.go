// Package exchange LocalAgent 对 Binance 现货 REST API v3 的最小封装。
//
// 只暴露两个动作（docs/系统总体拓扑结构.md 2.2）：
//   1. PlaceOrder：执行单条 TradeCommand
//   2. GetBalances：取全量余额快照（重连初始上报用）
//
// 铁律：
//   - 本包不依赖 saas/*，不 import 任何策略代码
//   - APIKey/SecretKey 通过构造函数传入；调用者从 config.agent.yaml 获取
//   - 所有请求带 5 秒超时，失败直接返回 error 让上层决定重试
package exchange

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/edi/quantsaas/internal/wsproto"
)

const (
	defaultBaseURL  = "https://api.binance.com"
	sandboxBaseURL  = "https://testnet.binance.vision"
	defaultTimeout  = 5 * time.Second
	recvWindowMs    = 5000
)

// Client 最小的 Binance 现货客户端。
type Client struct {
	APIKey    string
	secretKey string
	baseURL   string
	httpc     *http.Client
}

// NewClient 构造客户端。
// sandbox=true 使用 testnet；baseURL 非空时覆盖默认。
func NewClient(apiKey, secretKey string, sandbox bool, baseURL string) *Client {
	bu := defaultBaseURL
	if sandbox {
		bu = sandboxBaseURL
	}
	if baseURL != "" {
		bu = strings.TrimRight(baseURL, "/")
	}
	return &Client{
		APIKey:    apiKey,
		secretKey: secretKey,
		baseURL:   bu,
		httpc:     &http.Client{Timeout: defaultTimeout},
	}
}

// PlaceOrder 执行一条 TradeCommand。
//
// BUY：Binance 现货用 quoteOrderQty 指定花费 USDT 金额
// SELL：用 quantity 指定卖出资产数量
//
// 成交数据在 response 里的 fills 数组；市价单通常立即成交。
func (c *Client) PlaceOrder(cmd wsproto.TradeCommand) (*wsproto.ExecutionDetail, error) {
	params := url.Values{}
	params.Set("symbol", cmd.Symbol)
	params.Set("side", cmd.Action) // BUY / SELL
	params.Set("type", "MARKET")
	params.Set("newClientOrderId", cmd.ClientOrderID)

	switch cmd.Action {
	case "BUY":
		if cmd.AmountUSDT == "" {
			return nil, errors.New("BUY requires amount_usdt")
		}
		params.Set("quoteOrderQty", cmd.AmountUSDT)
	case "SELL":
		if cmd.QtyAsset == "" {
			return nil, errors.New("SELL requires qty_asset")
		}
		params.Set("quantity", cmd.QtyAsset)
	default:
		return nil, fmt.Errorf("invalid action %q", cmd.Action)
	}

	resp, err := c.signedPost("/api/v3/order", params)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Symbol              string `json:"symbol"`
		ClientOrderID       string `json:"clientOrderId"`
		TransactTime        int64  `json:"transactTime"`
		Status              string `json:"status"`
		ExecutedQty         string `json:"executedQty"`
		CumulativeQuoteQty  string `json:"cummulativeQuoteQty"` // Binance 拼写如此
		Fills               []struct {
			Price       string `json:"price"`
			Qty         string `json:"qty"`
			Commission  string `json:"commission"`
			CommissionAsset string `json:"commissionAsset"`
		} `json:"fills"`
	}
	if err := json.Unmarshal(resp, &raw); err != nil {
		return nil, fmt.Errorf("decode order response: %w (raw: %s)", err, string(resp))
	}

	out := &wsproto.ExecutionDetail{
		ClientOrderID: raw.ClientOrderID,
		Status:        raw.Status,
		FilledQty:     raw.ExecutedQty,
		FilledQuote:   raw.CumulativeQuoteQty,
	}
	// 计算均价 + 汇总手续费
	if len(raw.Fills) > 0 {
		totalQ := 0.0
		totalQuote := 0.0
		totalFee := 0.0
		var feeAsset string
		for _, f := range raw.Fills {
			q, _ := strconv.ParseFloat(f.Qty, 64)
			p, _ := strconv.ParseFloat(f.Price, 64)
			fee, _ := strconv.ParseFloat(f.Commission, 64)
			totalQ += q
			totalQuote += q * p
			totalFee += fee
			if feeAsset == "" {
				feeAsset = f.CommissionAsset
			}
		}
		if totalQ > 0 {
			out.FilledPrice = strconv.FormatFloat(totalQuote/totalQ, 'f', -1, 64)
		}
		out.Fee = strconv.FormatFloat(totalFee, 'f', -1, 64)
		out.FeeAsset = feeAsset
	}
	return out, nil
}

// GetBalances 读取账户所有资产的可用/冻结余额。
func (c *Client) GetBalances() ([]wsproto.Balance, error) {
	resp, err := c.signedGet("/api/v3/account", url.Values{})
	if err != nil {
		return nil, err
	}
	var raw struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(resp, &raw); err != nil {
		return nil, fmt.Errorf("decode account response: %w", err)
	}
	out := make([]wsproto.Balance, 0, len(raw.Balances))
	for _, b := range raw.Balances {
		// 过滤零余额，减少上报 payload
		free, _ := strconv.ParseFloat(b.Free, 64)
		locked, _ := strconv.ParseFloat(b.Locked, 64)
		if free == 0 && locked == 0 {
			continue
		}
		out = append(out, wsproto.Balance{Asset: b.Asset, Free: b.Free, Locked: b.Locked})
	}
	return out, nil
}

// --- 私有：签名请求工具 ---

func (c *Client) signedGet(path string, params url.Values) ([]byte, error) {
	return c.signed(http.MethodGet, path, params)
}

func (c *Client) signedPost(path string, params url.Values) ([]byte, error) {
	return c.signed(http.MethodPost, path, params)
}

// signed 构造带 HMAC-SHA256 签名的请求。
// Binance 要求将 querystring + body 合并后签名；本实现统一把 params 放在 query。
func (c *Client) signed(method, path string, params url.Values) ([]byte, error) {
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", strconv.Itoa(recvWindowMs))
	qs := params.Encode()
	sig := sign(c.secretKey, qs)
	full := c.baseURL + path + "?" + qs + "&signature=" + sig

	req, err := http.NewRequest(method, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.APIKey)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s %s -> HTTP %d: %s", method, path, resp.StatusCode, string(body))
	}
	return body, nil
}

// sign HMAC-SHA256(secret, payload) → hex
func sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
