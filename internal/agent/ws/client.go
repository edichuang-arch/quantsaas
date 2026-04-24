// Package ws LocalAgent 的 WebSocket 客户端主循环。
//
// 职责（docs/系统总体拓扑结构.md 5.1 / 6.3）：
//   1. 通过 REST /api/v1/auth/login 获取 JWT
//   2. 建立到 /ws/agent 的 WebSocket 长连接
//   3. 连接建立后立即发送 auth 消息，等 auth_result
//   4. 鉴权通过后立即发送初始 DeltaReport（余额快照，ClientOrderID 空）
//   5. 进入消息循环：command → command_ack → 异步执行 → delta_report
//   6. 每 30 秒发送 heartbeat
//   7. 连接断开后指数退避重连（1s → 5min）
package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	agentcfg "github.com/edi/quantsaas/internal/agent/config"
	"github.com/edi/quantsaas/internal/agent/exchange"
	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// HeartbeatInterval Agent 心跳频率（docs 5.1 约定 30 秒）。
const HeartbeatInterval = 30 * time.Second

// Client LocalAgent 主客户端，持有 config + Binance client + 重连状态。
type Client struct {
	Cfg      *agentcfg.AgentConfig
	Exchange *exchange.Client
	Log      *zap.Logger

	jwt     string
	conn    *websocket.Conn
	writeMu sync.Mutex
}

// NewClient 构造 Agent 客户端。
func NewClient(cfg *agentcfg.AgentConfig, ex *exchange.Client, log *zap.Logger) *Client {
	if log == nil {
		log = zap.NewNop()
	}
	return &Client{Cfg: cfg, Exchange: ex, Log: log}
}

// Run 启动主循环：连接→消息处理→断开→指数退避→重连。
// ctx 取消时退出所有循环，关闭连接。
func (c *Client) Run(ctx context.Context) error {
	backoff := c.Cfg.InitialBackoff()
	maxBackoff := c.Cfg.MaxBackoff()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := c.connectOnce(ctx)
		if err != nil {
			c.Log.Warn("agent session ended", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectOnce 一次完整的会话：登录 → WebSocket → 循环 → 断开。
func (c *Client) connectOnce(ctx context.Context) error {
	// 1. REST 登录获取 JWT
	token, err := c.loginForJWT(ctx)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.jwt = token

	// 2. 建立 WebSocket 连接
	wsURL, err := toWSURL(c.Cfg.SaaSURL, "/ws/agent")
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial ws: %w", err)
	}
	c.conn = conn
	defer func() {
		_ = conn.Close()
		c.conn = nil
	}()

	// 3. auth
	if err := c.sendEnvelope(wsproto.TypeAuth, wsproto.AuthPayload{
		JWT:        c.jwt,
		AgentVer:   "0.1.0",
		Capability: "binance-spot",
	}); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// 4. 等待 auth_result
	if err := c.awaitAuthResult(); err != nil {
		return err
	}
	c.Log.Info("agent authenticated")

	// 5. 初始 DeltaReport（余额快照）
	if err := c.sendInitialSnapshot(); err != nil {
		c.Log.Warn("send initial snapshot failed", zap.Error(err))
	}

	// 6. 启动 heartbeat goroutine
	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()
	go c.heartbeatLoop(hbCtx)

	// 7. 消息循环
	return c.messageLoop(ctx)
}

// loginForJWT 调用 POST /api/v1/auth/login，返回 JWT。
func (c *Client) loginForJWT(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"email":    c.Cfg.Email,
		"password": c.Cfg.Password,
	})
	restURL := strings.TrimRight(c.Cfg.SaaSURL, "/") + "/api/v1/auth/login"
	// SaaS URL 可能是 ws://；切换到 http 版本
	restURL = strings.Replace(restURL, "ws://", "http://", 1)
	restURL = strings.Replace(restURL, "wss://", "https://", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, restURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("empty token in login response")
	}
	return out.Token, nil
}

func (c *Client) awaitAuthResult() error {
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer c.conn.SetReadDeadline(time.Time{})

	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth_result: %w", err)
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if env.Type != wsproto.TypeAuthResult {
		return fmt.Errorf("expected auth_result, got %s", env.Type)
	}
	// Re-decode payload
	payloadBlob, _ := json.Marshal(env.Payload)
	var pl wsproto.AuthResultPayload
	_ = json.Unmarshal(payloadBlob, &pl)
	if !pl.OK {
		return fmt.Errorf("auth rejected: %s", pl.Error)
	}
	return nil
}

func (c *Client) sendInitialSnapshot() error {
	bals, err := c.Exchange.GetBalances()
	if err != nil {
		return err
	}
	return c.sendEnvelope(wsproto.TypeDeltaReport, wsproto.DeltaReport{
		Balances: bals,
		SentAtMs: time.Now().UnixMilli(),
	})
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendEnvelope(wsproto.TypeHeartbeat, wsproto.HeartbeatPayload{
				SentAtMs: time.Now().UnixMilli(),
			}); err != nil {
				c.Log.Warn("heartbeat send failed", zap.Error(err))
				return
			}
		}
	}
}

// messageLoop 循环读取 server 消息。阻塞到连接断开。
func (c *Client) messageLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			c.Log.Warn("decode envelope failed", zap.Error(err))
			continue
		}
		switch env.Type {
		case wsproto.TypeCommand:
			c.handleCommand(env.Payload)
		case wsproto.TypeHeartbeatAck:
			// no-op
		case wsproto.TypeReportAck:
			// no-op（只做记录）
		default:
			c.Log.Debug("unknown message type", zap.String("type", string(env.Type)))
		}
	}
}

// handleCommand 处理 TradeCommand：立即 command_ack，异步下单 + delta_report。
func (c *Client) handleCommand(payload any) {
	blob, _ := json.Marshal(payload)
	var cp wsproto.CommandPayload
	if err := json.Unmarshal(blob, &cp); err != nil {
		c.Log.Warn("decode command payload failed", zap.Error(err))
		return
	}
	cmd := cp.TradeCommand

	// 立即 ack（不等下单完成）
	_ = c.sendEnvelope(wsproto.TypeCommandAck, wsproto.CommandAckPayload{
		ClientOrderID: cmd.ClientOrderID,
	})

	go c.executeCommand(cmd)
}

func (c *Client) executeCommand(cmd wsproto.TradeCommand) {
	logger := c.Log.With(zap.String("client_order_id", cmd.ClientOrderID))
	exec, err := c.Exchange.PlaceOrder(cmd)

	// 无论成功失败都上报 DeltaReport
	report := wsproto.DeltaReport{
		ClientOrderID: cmd.ClientOrderID,
		Symbol:        cmd.Symbol,
		SentAtMs:      time.Now().UnixMilli(),
	}
	if err != nil {
		logger.Error("place order failed", zap.Error(err))
		report.Execution = &wsproto.ExecutionDetail{
			ClientOrderID: cmd.ClientOrderID,
			Status:        "FAILED",
			ErrorMessage:  err.Error(),
		}
	} else {
		report.Execution = exec
	}

	// 刷新全量余额（不论成败）
	if bals, berr := c.Exchange.GetBalances(); berr == nil {
		report.Balances = bals
	}

	if serr := c.sendEnvelope(wsproto.TypeDeltaReport, report); serr != nil {
		logger.Warn("send delta report failed", zap.Error(serr))
	}
}

// sendEnvelope 序列化 + 写 ws（带 write lock，并发 heartbeat / exec goroutine 安全）。
func (c *Client) sendEnvelope(t wsproto.Type, payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.conn == nil {
		return errors.New("ws connection nil")
	}
	env := wsproto.Envelope{Type: t, Payload: payload}
	blob, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, blob)
}

// toWSURL 把 http(s):// 或 ws(s):// 形式的 SaaSURL + path 拼成 ws(s) URL。
func toWSURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// 保持
	default:
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String(), nil
}
