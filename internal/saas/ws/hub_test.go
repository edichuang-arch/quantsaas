package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-jwt-secret-at-least-32-bytes-long!!"

// fakeDeltaHandler 记录收到的 Delta 报告。
type fakeDeltaHandler struct {
	mu     sync.Mutex
	deltas []wsproto.DeltaReport
	err    error
}

func (f *fakeDeltaHandler) HandleDelta(ctx context.Context, userID uint, r wsproto.DeltaReport) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deltas = append(f.deltas, r)
	return f.err
}

func startHubServer(t *testing.T, hub *Hub) (*httptest.Server, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ws/agent", hub.HandleConnection)
	srv := httptest.NewServer(r)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/agent"
	t.Cleanup(srv.Close)
	return srv, wsURL
}

// dialAgent 模拟 Agent 端发起 ws 连接。
func dialAgent(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// sendEnvelope 模拟 Agent 发送消息。
func sendEnvelope(t *testing.T, c *websocket.Conn, ty wsproto.Type, payload any) {
	t.Helper()
	env := wsproto.Envelope{Type: ty, Payload: payload}
	blob, _ := json.Marshal(env)
	require.NoError(t, c.WriteMessage(websocket.TextMessage, blob))
}

func readEnvelope(t *testing.T, c *websocket.Conn) wsproto.Envelope {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := c.ReadMessage()
	require.NoError(t, err)
	var env wsproto.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}

func TestHub_RejectsUnauthenticatedConnection(t *testing.T) {
	svc := auth.NewService(testJWTSecret, 24, 168)
	hub := NewHub(svc, nil, nil)
	_, wsURL := startHubServer(t, hub)

	conn := dialAgent(t, wsURL)
	// 不发 auth → Server 在 AuthTimeout(10s) 后发 auth_result OK=false 并关闭连接。
	// Client 用比 AuthTimeout 更长的 deadline 等待第一条消息。
	_ = conn.SetReadDeadline(time.Now().Add(AuthTimeout + 3*time.Second))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err, "should receive auth_result before connection closes")

	var env wsproto.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, wsproto.TypeAuthResult, env.Type)
	blob, _ := json.Marshal(env.Payload)
	var pl wsproto.AuthResultPayload
	_ = json.Unmarshal(blob, &pl)
	assert.False(t, pl.OK, "auth_result should signal failure")

	// 读第二次应该是连接关闭
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = conn.ReadMessage()
	assert.Error(t, err, "connection should be closed after auth failure")
}

func TestHub_RejectsUserTokenInsteadOfAgent(t *testing.T) {
	svc := auth.NewService(testJWTSecret, 24, 168)
	hub := NewHub(svc, nil, nil)
	_, wsURL := startHubServer(t, hub)

	// 签发 user token（非 agent）
	userToken, err := svc.SignUserToken(42, "a@b.com", "pro")
	require.NoError(t, err)

	conn := dialAgent(t, wsURL)
	sendEnvelope(t, conn, wsproto.TypeAuth, wsproto.AuthPayload{JWT: userToken})

	env := readEnvelope(t, conn)
	require.Equal(t, wsproto.TypeAuthResult, env.Type)
	blob, _ := json.Marshal(env.Payload)
	var pl wsproto.AuthResultPayload
	_ = json.Unmarshal(blob, &pl)
	assert.False(t, pl.OK)
}

func TestHub_FullAuthFlow(t *testing.T) {
	svc := auth.NewService(testJWTSecret, 24, 168)
	handler := &fakeDeltaHandler{}
	hub := NewHub(svc, handler, nil)
	_, wsURL := startHubServer(t, hub)

	agentToken, err := svc.SignAgentToken(42, "agent@example.com")
	require.NoError(t, err)

	conn := dialAgent(t, wsURL)
	sendEnvelope(t, conn, wsproto.TypeAuth, wsproto.AuthPayload{JWT: agentToken})

	// 应收到 auth_result OK
	env := readEnvelope(t, conn)
	assert.Equal(t, wsproto.TypeAuthResult, env.Type)

	// 心跳测试
	sendEnvelope(t, conn, wsproto.TypeHeartbeat, wsproto.HeartbeatPayload{})
	env = readEnvelope(t, conn)
	assert.Equal(t, wsproto.TypeHeartbeatAck, env.Type)

	// 发 delta_report
	sendEnvelope(t, conn, wsproto.TypeDeltaReport, wsproto.DeltaReport{
		ClientOrderID: "oid-1",
		Balances: []wsproto.Balance{
			{Asset: "USDT", Free: "1000", Locked: "0"},
		},
	})
	env = readEnvelope(t, conn)
	assert.Equal(t, wsproto.TypeReportAck, env.Type)

	// fakeDeltaHandler 应记录到
	handler.mu.Lock()
	assert.Len(t, handler.deltas, 1)
	assert.Equal(t, "oid-1", handler.deltas[0].ClientOrderID)
	handler.mu.Unlock()
}

// SendToUser：未连接返回 ErrAgentNotConnected；连接后能收到 command 消息。
func TestHub_SendToUser_OnlineAndOffline(t *testing.T) {
	svc := auth.NewService(testJWTSecret, 24, 168)
	hub := NewHub(svc, nil, nil)
	_, wsURL := startHubServer(t, hub)

	// 未连接
	err := hub.SendToUser(context.Background(), 99, wsproto.TradeCommand{ClientOrderID: "x"})
	assert.ErrorIs(t, err, instance.ErrAgentNotConnected)

	// 建立连接
	agentToken, _ := svc.SignAgentToken(99, "x@y.com")
	conn := dialAgent(t, wsURL)
	sendEnvelope(t, conn, wsproto.TypeAuth, wsproto.AuthPayload{JWT: agentToken})
	_ = readEnvelope(t, conn) // auth_result

	// 等待 hub 注册（上面 readEnvelope 已经同步了 auth_result，但 register 在 auth_result 之后）
	assertEventually(t, 2*time.Second, func() bool { return hub.IsOnline(99) })

	// 发送 command 成功
	require.NoError(t, hub.SendToUser(context.Background(), 99, wsproto.TradeCommand{
		ClientOrderID: "oid-send-1",
		Action:        "BUY",
		Symbol:        "BTCUSDT",
		AmountUSDT:    "10",
	}))

	// Agent 端应收到
	env := readEnvelope(t, conn)
	assert.Equal(t, wsproto.TypeCommand, env.Type)
}

func TestHub_OnlineCount(t *testing.T) {
	svc := auth.NewService(testJWTSecret, 24, 168)
	hub := NewHub(svc, nil, nil)
	_, wsURL := startHubServer(t, hub)

	assert.Equal(t, 0, hub.OnlineCount())

	tok, _ := svc.SignAgentToken(1, "a@b.com")
	conn := dialAgent(t, wsURL)
	sendEnvelope(t, conn, wsproto.TypeAuth, wsproto.AuthPayload{JWT: tok})
	_ = readEnvelope(t, conn)

	assertEventually(t, 2*time.Second, func() bool { return hub.OnlineCount() == 1 })
}

// assertEventually 在超时内轮询 cond；兼容 CheckOrigin 后 Hub 异步 register。
func assertEventually(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// 确保 http.StatusOK 引用以防 import 意外删除
var _ = http.StatusOK
