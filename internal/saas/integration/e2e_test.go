//go:build integration
// +build integration

// Package integration 端到端测试，需要真实 Postgres。
//
// 跑法：
//   docker compose up -d postgres redis
//   export DB_PASSWORD=quantsaas-dev-pw
//   export JWT_SECRET="test-jwt-secret-at-least-32-bytes-long!!"
//   go test -tags=integration ./internal/saas/integration/...
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/edi/quantsaas/internal/saas/api"
	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/epoch"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/edi/quantsaas/internal/saas/ws"
	"github.com/edi/quantsaas/internal/wsproto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testConfig 根据环境变量组装最小可运行配置。
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	host := getEnv("DB_HOST", "localhost")
	password := getEnv("DB_PASSWORD", "quantsaas-dev-pw")
	jwtSecret := getEnv("JWT_SECRET", "test-jwt-secret-at-least-32-bytes-long!!")
	return &config.Config{
		AppRole: config.RoleDev,
		Server:  config.ServerConfig{Host: "127.0.0.1", Port: 0},
		Database: config.DatabaseConfig{
			Host: host, Port: 5432, User: "quantsaas",
			Password: password, DBName: "quantsaas", SSLMode: "disable",
		},
		Redis: config.RedisConfig{Addr: getEnv("REDIS_ADDR", "localhost:6379")},
		JWT:   config.JWTConfig{Secret: jwtSecret, ExpireHours: 24, AgentTokenTTLH: 168},
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// suite 一个完整的内存中 SaaS 实例 + httptest 服务器。
type suite struct {
	cfg       *config.Config
	db        *store.DB
	redis     *store.Redis
	auth      *auth.Service
	hub       *ws.Hub
	srv       *httptest.Server
	t         *testing.T
}

func newSuite(t *testing.T) *suite {
	t.Helper()
	cfg := testConfig(t)

	db, err := store.NewDB(cfg.Database)
	if err != nil {
		t.Skipf("postgres unavailable: %v (start via `docker compose up -d postgres`)", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 清空 schema（测试隔离）
	for _, m := range store.AllModels() {
		require.NoError(t, db.Unscoped().Where("1=1").Delete(m).Error)
	}

	rds, err := store.NewRedis(cfg.Redis)
	if err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { _ = rds.Close() })
	_ = rds.Raw().FlushDB(context.Background()).Err()

	authSvc := auth.NewService(cfg.JWT.Secret, cfg.JWT.ExpireHours, cfg.JWT.AgentTokenTTLH)
	genomes := ga.NewGenomeStore(db, rds)
	reconciler := ws.NewReconciler(db, zap.NewNop())
	hub := ws.NewHub(authSvc, reconciler, zap.NewNop())
	mgr := instance.NewManager(db, zap.NewNop())
	evolvable := ga.NewSigmoidBTCEvolvable()
	engine := ga.NewEngine(evolvable, genomes, ga.DefaultConfig)
	epochSvc := epoch.NewService(db, engine, genomes, zap.NewNop())

	// 种一个 StrategyTemplate 让创建实例不报 FK 错
	require.NoError(t, db.Create(&store.StrategyTemplate{
		StrategyID: "sigmoid-btc", Name: "Sigmoid BTC", Version: "0.1.0",
		IsSpot: true, Exchange: "binance", Symbol: "BTCUSDT",
	}).Error)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	api.RegisterRoutes(r, &api.Dependencies{
		Config:     cfg,
		Auth:       authSvc,
		AuthH:      api.NewAuthHandler(db, authSvc),
		InstanceH:  api.NewInstanceHandler(mgr, db),
		DashboardH: api.NewDashboardHandler(db, hub, cfg),
		EvolutionH: api.NewEvolutionHandler(cfg, epochSvc, genomes),
		Hub:        hub,
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &suite{cfg: cfg, db: db, redis: rds, auth: authSvc, hub: hub, srv: srv, t: t}
}

// httpPost / httpGet 测试 helper。
func (s *suite) httpRequest(method, path, token string, body any) (int, []byte) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, reader)
	require.NoError(s.t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(s.t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// --- 测试 1：注册 → 登录 → 创建实例 → 启动实例 ---

func TestE2E_RegisterLoginCreateStartInstance(t *testing.T) {
	s := newSuite(t)

	// 1. 注册
	code, raw := s.httpRequest(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"email": "e2e-1@example.com", "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, code, string(raw))

	var regResp api.TokenResponse
	require.NoError(t, json.Unmarshal(raw, &regResp))
	assert.NotEmpty(t, regResp.Token)
	assert.Equal(t, "e2e-1@example.com", regResp.Email)

	token := regResp.Token

	// 2. 查询 me
	code, raw = s.httpRequest(http.MethodGet, "/api/v1/auth/me", token, nil)
	require.Equal(t, http.StatusOK, code, string(raw))

	// 3. 建立实例（使用 template）
	var tpl store.StrategyTemplate
	require.NoError(t, s.db.First(&tpl).Error)
	code, raw = s.httpRequest(http.MethodPost, "/api/v1/instances", token, map[string]any{
		"template_id":          tpl.ID,
		"name":                 "e2e inst",
		"symbol":               "BTCUSDT",
		"initial_capital_usdt": 10000,
		"monthly_inject_usdt":  300,
	})
	require.Equal(t, http.StatusCreated, code, string(raw))
	var inst store.StrategyInstance
	require.NoError(t, json.Unmarshal(raw, &inst))
	require.Equal(t, store.InstStopped, inst.Status)

	// 4. 启动实例
	code, raw = s.httpRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/instances/%d/start", inst.ID), token, nil)
	require.Equal(t, http.StatusOK, code, string(raw))

	// 5. 查询列表
	code, raw = s.httpRequest(http.MethodGet, "/api/v1/instances", token, nil)
	require.Equal(t, http.StatusOK, code)
	var list []store.StrategyInstance
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list, 1)
	assert.Equal(t, store.InstRunning, list[0].Status)

	// 6. Portfolio 端点
	code, raw = s.httpRequest(http.MethodGet,
		fmt.Sprintf("/api/v1/instances/%d/portfolio", inst.ID), token, nil)
	require.Equal(t, http.StatusOK, code, string(raw))

	// 7. Dashboard 端点
	code, _ = s.httpRequest(http.MethodGet, "/api/v1/dashboard", token, nil)
	require.Equal(t, http.StatusOK, code)
}

// --- 测试 2：Agent WS 连接 + 初始快照 + 接收 command ---

func TestE2E_AgentWebSocketFlow(t *testing.T) {
	s := newSuite(t)

	// 注册用户
	code, raw := s.httpRequest(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"email": "e2e-ws@example.com", "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, code)
	var reg api.TokenResponse
	_ = json.Unmarshal(raw, &reg)

	// 取 agent token
	code, raw = s.httpRequest(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": "e2e-ws@example.com", "password": "supersecret", "actor": "agent",
	})
	require.Equal(t, http.StatusOK, code)
	var agentTokResp api.TokenResponse
	_ = json.Unmarshal(raw, &agentTokResp)

	// 连接 ws
	wsURL := strings.Replace(s.srv.URL, "http", "ws", 1) + "/ws/agent"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// 发 auth
	sendEnv(t, conn, wsproto.TypeAuth, wsproto.AuthPayload{JWT: agentTokResp.Token})

	// 收 auth_result
	env := readEnv(t, conn, 3*time.Second)
	assert.Equal(t, wsproto.TypeAuthResult, env.Type)

	// 发 delta_report（初始快照）
	sendEnv(t, conn, wsproto.TypeDeltaReport, wsproto.DeltaReport{
		Balances: []wsproto.Balance{
			{Asset: "USDT", Free: "5000", Locked: "0"},
		},
	})
	env = readEnv(t, conn, 3*time.Second)
	assert.Equal(t, wsproto.TypeReportAck, env.Type)

	// 心跳
	sendEnv(t, conn, wsproto.TypeHeartbeat, wsproto.HeartbeatPayload{})
	env = readEnv(t, conn, 3*time.Second)
	assert.Equal(t, wsproto.TypeHeartbeatAck, env.Type)

	// Hub 应标记用户在线
	assert.True(t, s.hub.IsOnline(reg.UserID))

	// SaaS 主动发 command
	require.NoError(t, s.hub.SendToUser(context.Background(), reg.UserID, wsproto.TradeCommand{
		ClientOrderID: "e2e-cmd-1",
		Action:        "BUY",
		Engine:        "MACRO",
		Symbol:        "BTCUSDT",
		AmountUSDT:    "50",
		LotType:       "DEAD_STACK",
	}))

	// Agent 端应收到 command
	env = readEnv(t, conn, 3*time.Second)
	assert.Equal(t, wsproto.TypeCommand, env.Type)
}

// --- 测试 3：进化任务触发 → 跑完 → 写 challenger 记录 ---

func TestE2E_EvolutionTaskFlow(t *testing.T) {
	s := newSuite(t)

	// 种 K 线数据：5y + warmup（使用 daily bars 简化）
	seedKLines(t, s.db, "BTCUSDT", "5m", 3500)

	// 注册
	code, raw := s.httpRequest(http.MethodPost, "/api/v1/auth/register", "", map[string]string{
		"email": "e2e-evo@example.com", "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, code)
	var reg api.TokenResponse
	_ = json.Unmarshal(raw, &reg)

	// 触发 test_mode 进化（快速完成）
	code, raw = s.httpRequest(http.MethodPost, "/api/v1/evolution/tasks", reg.Token, map[string]any{
		"strategy_id":     "sigmoid-btc",
		"symbol":          "BTCUSDT",
		"initial_usdt":    10000,
		"monthly_inject":  300,
		"lot_step":        0.00001,
		"lot_min":         0.00001,
		"warmup_days":     30,
		"test_mode":       true, // Pop=10, Gen=3
	})
	require.Equal(t, http.StatusAccepted, code, string(raw))

	// 等待任务完成（最多 60 秒）
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		s.db.Model(&store.GeneRecord{}).
			Where("strategy_id = ? AND symbol = ? AND role = ?",
				"sigmoid-btc", "BTCUSDT", store.RoleChallenger).
			Count(&count)
		if count > 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	var challenger store.GeneRecord
	require.NoError(t, s.db.
		Where("strategy_id = ? AND role = ?", "sigmoid-btc", store.RoleChallenger).
		First(&challenger).Error)
	assert.NotEmpty(t, challenger.ParamPack)
}

// --- ws helper ---

func sendEnv(t *testing.T, c *websocket.Conn, ty wsproto.Type, payload any) {
	t.Helper()
	env := wsproto.Envelope{Type: ty, Payload: payload}
	blob, _ := json.Marshal(env)
	require.NoError(t, c.WriteMessage(websocket.TextMessage, blob))
}

func readEnv(t *testing.T, c *websocket.Conn, d time.Duration) wsproto.Envelope {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	_, raw, err := c.ReadMessage()
	require.NoError(t, err)
	var env wsproto.Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}

// seedKLines 批量写入合成 K 线（100 + i 的上升趋势）。
func seedKLines(t *testing.T, db *store.DB, symbol, interval string, n int) {
	t.Helper()
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	step := int64(5 * 60 * 1000) // 5 分钟
	if interval == "1d" {
		step = int64(24 * 60 * 60 * 1000)
	}
	lines := make([]store.KLine, n)
	for i := 0; i < n; i++ {
		p := 100.0 + float64(i)*0.1
		lines[i] = store.KLine{
			Symbol:   symbol,
			Interval: interval,
			OpenTime: base + int64(i)*step,
			CloseTime: base + int64(i+1)*step - 1,
			Open: p, High: p, Low: p, Close: p, Volume: 1,
		}
	}
	require.NoError(t, db.CreateInBatches(lines, 500).Error)
}
