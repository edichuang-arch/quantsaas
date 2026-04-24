// Package ws SaaS 侧 WebSocket Hub。
//
// 核心职责（docs/系统总体拓扑结构.md 5.1）：
//   - 每个用户最多一个 Agent WebSocket 连接
//   - 处理完整的 auth → heartbeat → command / delta_report 消息循环
//   - 实现 instance.CommandSender 接口，把 TradeCommand 推到正确的 Agent
//   - 把 DeltaReport 路由给 PortfolioReconciler（Phase 8b）
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// AuthTimeout 未认证连接的强制断开超时。
const AuthTimeout = 10 * time.Second

// DeltaHandler 处理 DeltaReport 的接口（Phase 8b 的 Reconciler 实现）。
type DeltaHandler interface {
	HandleDelta(ctx context.Context, userID uint, report wsproto.DeltaReport) error
}

// Hub WebSocket Hub；同时实现 instance.CommandSender。
type Hub struct {
	Auth     *auth.Service
	Delta    DeltaHandler
	Log      *zap.Logger

	upgrader websocket.Upgrader
	mu       sync.RWMutex
	conns    map[uint]*agentConn // userID → conn
}

// agentConn 单个 Agent 连接的封装（写入带锁以允许并发写）。
type agentConn struct {
	userID  uint
	conn    *websocket.Conn
	writeMu sync.Mutex
	closed  chan struct{}
	once    sync.Once
}

// NewHub 构造 Hub。DeltaHandler 可为 nil（测试用 Fake）。
func NewHub(authSvc *auth.Service, delta DeltaHandler, log *zap.Logger) *Hub {
	if log == nil {
		log = zap.NewNop()
	}
	return &Hub{
		Auth:  authSvc,
		Delta: delta,
		Log:   log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true }, // Agent 是受信任的本地进程
		},
		conns: map[uint]*agentConn{},
	}
}

// SendToUser 实现 instance.CommandSender。
// Agent 未连接时返回 instance.ErrAgentNotConnected，Tick 层记 warn 并跳过。
func (h *Hub) SendToUser(ctx context.Context, userID uint, cmd wsproto.TradeCommand) error {
	h.mu.RLock()
	ac := h.conns[userID]
	h.mu.RUnlock()
	if ac == nil {
		return instance.ErrAgentNotConnected
	}
	return ac.writeEnvelope(wsproto.TypeCommand, wsproto.CommandPayload{TradeCommand: cmd})
}

// IsOnline 返回用户是否有活跃 Agent 连接。
func (h *Hub) IsOnline(userID uint) bool {
	h.mu.RLock()
	_, ok := h.conns[userID]
	h.mu.RUnlock()
	return ok
}

// OnlineCount 返回当前活跃连接数（监控用）。
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// HandleConnection Gin 路由 GET /ws/agent。
// 流程：Upgrade → 等 10 秒内收到 auth 消息 → 鉴权 → 注册连接 → 消息循环。
func (h *Hub) HandleConnection(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.Log.Warn("ws upgrade failed", zap.Error(err))
		return
	}
	ac := &agentConn{conn: conn, closed: make(chan struct{})}

	// 1. 鉴权（10 秒超时）
	claims, err := h.authenticate(ac)
	if err != nil {
		h.Log.Warn("ws auth failed", zap.Error(err))
		_ = ac.writeEnvelope(wsproto.TypeAuthResult, wsproto.AuthResultPayload{
			OK:    false,
			Error: err.Error(),
		})
		_ = conn.Close()
		return
	}
	ac.userID = claims.UserID

	// 2. 回 auth_result + 注册
	if err := ac.writeEnvelope(wsproto.TypeAuthResult, wsproto.AuthResultPayload{
		OK: true, UserID: claims.UserID,
	}); err != nil {
		_ = conn.Close()
		return
	}
	h.register(ac)
	defer h.deregister(ac)

	h.Log.Info("agent connected", zap.Uint("user_id", claims.UserID))

	// 3. 消息循环
	ctx := c.Request.Context()
	h.messageLoop(ctx, ac)
}

// authenticate 等待第一条 auth 消息，校验 JWT。
func (h *Hub) authenticate(ac *agentConn) (*auth.Claims, error) {
	_ = ac.conn.SetReadDeadline(time.Now().Add(AuthTimeout))
	defer ac.conn.SetReadDeadline(time.Time{})

	_, raw, err := ac.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Type != wsproto.TypeAuth {
		return nil, errors.New("expected auth message")
	}
	blob, _ := json.Marshal(env.Payload)
	var pl wsproto.AuthPayload
	if err := json.Unmarshal(blob, &pl); err != nil {
		return nil, err
	}
	if pl.JWT == "" {
		return nil, errors.New("empty jwt")
	}
	claims, err := h.Auth.ParseToken(pl.JWT)
	if err != nil {
		return nil, err
	}
	if claims.Actor != auth.ActorAgent {
		return nil, errors.New("token is not an agent token")
	}
	return claims, nil
}

// messageLoop 读取 Agent 消息直到连接关闭或 ctx 取消。
func (h *Hub) messageLoop(ctx context.Context, ac *agentConn) {
	for {
		_, raw, err := ac.conn.ReadMessage()
		if err != nil {
			h.Log.Info("agent disconnected",
				zap.Uint("user_id", ac.userID), zap.Error(err))
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			h.Log.Warn("bad envelope", zap.Error(err))
			continue
		}
		switch env.Type {
		case wsproto.TypeHeartbeat:
			_ = ac.writeEnvelope(wsproto.TypeHeartbeatAck, wsproto.HeartbeatAckPayload{
				ReceivedAtMs: time.Now().UnixMilli(),
			})
		case wsproto.TypeCommandAck:
			// 确认收到（不等执行完成）；当前只做日志
			blob, _ := json.Marshal(env.Payload)
			var p wsproto.CommandAckPayload
			_ = json.Unmarshal(blob, &p)
			h.Log.Debug("command ack",
				zap.Uint("user_id", ac.userID),
				zap.String("client_order_id", p.ClientOrderID))
		case wsproto.TypeDeltaReport:
			h.handleDeltaReport(ctx, ac, env.Payload)
		default:
			h.Log.Debug("unhandled message", zap.String("type", string(env.Type)))
		}
	}
}

func (h *Hub) handleDeltaReport(ctx context.Context, ac *agentConn, payload any) {
	blob, _ := json.Marshal(payload)
	var report wsproto.DeltaReport
	if err := json.Unmarshal(blob, &report); err != nil {
		h.Log.Warn("decode delta_report failed", zap.Error(err))
		_ = ac.writeEnvelope(wsproto.TypeReportAck, wsproto.ReportAckPayload{
			OK: false, Error: err.Error(),
		})
		return
	}
	if h.Delta != nil {
		if err := h.Delta.HandleDelta(ctx, ac.userID, report); err != nil {
			h.Log.Error("handle delta failed",
				zap.Uint("user_id", ac.userID), zap.Error(err))
			_ = ac.writeEnvelope(wsproto.TypeReportAck, wsproto.ReportAckPayload{
				ClientOrderID: report.ClientOrderID,
				OK:            false, Error: err.Error(),
			})
			return
		}
	}
	_ = ac.writeEnvelope(wsproto.TypeReportAck, wsproto.ReportAckPayload{
		ClientOrderID: report.ClientOrderID,
		OK:            true,
	})
}

// register 注册连接；若已有旧连接则关闭之，只允许单会话。
func (h *Hub) register(ac *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.conns[ac.userID]; old != nil {
		old.close()
	}
	h.conns[ac.userID] = ac
}

func (h *Hub) deregister(ac *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur := h.conns[ac.userID]; cur == ac {
		delete(h.conns, ac.userID)
	}
	ac.close()
}

// agentConn.writeEnvelope 序列化 + 写 ws（带 lock 支持并发）。
func (ac *agentConn) writeEnvelope(t wsproto.Type, payload any) error {
	ac.writeMu.Lock()
	defer ac.writeMu.Unlock()
	select {
	case <-ac.closed:
		return errors.New("connection closed")
	default:
	}
	blob, err := json.Marshal(wsproto.Envelope{Type: t, Payload: payload})
	if err != nil {
		return err
	}
	return ac.conn.WriteMessage(websocket.TextMessage, blob)
}

func (ac *agentConn) close() {
	ac.once.Do(func() {
		close(ac.closed)
		_ = ac.conn.Close()
	})
}

// Shutdown 关闭所有连接（用于优雅停机）。
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ac := range h.conns {
		ac.close()
	}
	h.conns = map[uint]*agentConn{}
}

// 为了避免 unused import 警告，确保 instance 包被引用
var _ instance.CommandSender = (*Hub)(nil)
