// Package api 提供 Gin HTTP handler。
// 本文件只做进化相关 4 个 endpoint 的骨架；完整路由注册在 Phase 9 的 routes.go。
package api

import (
	"errors"
	"net/http"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/epoch"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/gin-gonic/gin"
)

// EvolutionHandler 持有任务服务 + 基因库 + 配置。
// 由 cmd/saas/main.go 在启动时构造并注册到路由。
type EvolutionHandler struct {
	Config  *config.Config
	Service *epoch.Service
	Genomes *ga.GenomeStore
}

// NewEvolutionHandler 构造 handler。
func NewEvolutionHandler(cfg *config.Config, svc *epoch.Service, genomes *ga.GenomeStore) *EvolutionHandler {
	return &EvolutionHandler{Config: cfg, Service: svc, Genomes: genomes}
}

// CreateTaskRequest POST /api/v1/evolution/tasks 的请求体。
type CreateTaskRequest struct {
	StrategyID     string             `json:"strategy_id"`
	Symbol         string             `json:"symbol"`
	PopSize        int                `json:"pop_size"`
	MaxGenerations int                `json:"max_generations"`
	SpawnMode      epoch.SpawnMode    `json:"spawn_mode"`
	SpawnPoint     *spawnPointJSON    `json:"spawn_point,omitempty"`
	InitialUSDT    float64            `json:"initial_usdt"`
	MonthlyInject  float64            `json:"monthly_inject"`
	LotStep        float64            `json:"lot_step"`
	LotMin         float64            `json:"lot_min"`
	WarmupDays     int                `json:"warmup_days"`
	TestMode       bool               `json:"test_mode"` // Pop=10, Gen=3 快速测试
}

type spawnPointJSON struct {
	Policy struct {
		DeadReserveRatio float64 `json:"dead_reserve_ratio"`
		GlobalStopLoss   float64 `json:"global_stop_loss"`
	} `json:"policy"`
	Risk struct {
		MaxLeverage float64 `json:"max_leverage"`
		TakerFeeBps int     `json:"taker_fee_bps"`
	} `json:"risk"`
}

// CreateTask POST /api/v1/evolution/tasks — 创建并启动进化任务。
// 权限：lab / dev 才允许。
func (h *EvolutionHandler) CreateTask(c *gin.Context) {
	if !h.Config.IsEvolutionAllowed() {
		c.JSON(http.StatusForbidden, gin.H{"error": "evolution is disabled in app_role=saas"})
		return
	}
	var req CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TestMode {
		req.PopSize = 10
		req.MaxGenerations = 3
	}
	startReq := epoch.StartRequest{
		StrategyID:     req.StrategyID,
		Symbol:         req.Symbol,
		PopSize:        req.PopSize,
		MaxGenerations: req.MaxGenerations,
		SpawnMode:      req.SpawnMode,
		InitialUSDT:    req.InitialUSDT,
		MonthlyInject:  req.MonthlyInject,
		LotStep:        req.LotStep,
		LotMin:         req.LotMin,
		WarmupDays:     req.WarmupDays,
	}
	if req.SpawnPoint != nil {
		startReq.SpawnPoint = spawnFromJSON(req.SpawnPoint)
	}

	task, err := h.Service.Start(c.Request.Context(), startReq)
	if err != nil {
		if errors.Is(err, epoch.ErrTaskAlreadyRunning) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, task)
}

// GetTaskStatus GET /api/v1/evolution/tasks — 返回当前任务 + 最近 challenger 列表。
func (h *EvolutionHandler) GetTaskStatus(c *gin.Context) {
	if !h.Config.IsEvolutionAllowed() {
		c.JSON(http.StatusForbidden, gin.H{"error": "evolution is disabled"})
		return
	}
	strategyID := c.DefaultQuery("strategy_id", "sigmoid-btc")
	symbol := c.DefaultQuery("symbol", "BTCUSDT")

	resp := gin.H{}
	if cur := h.Service.Current(); cur != nil {
		resp["current"] = cur
	}
	challengers, err := h.Genomes.ListChallengers(c.Request.Context(), strategyID, symbol, 20)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp["challengers"] = challengers
	c.JSON(http.StatusOK, resp)
}

// PromoteChallenger POST /api/v1/evolution/tasks/:id/promote
func (h *EvolutionHandler) PromoteChallenger(c *gin.Context) {
	if !h.Config.IsEvolutionAllowed() {
		c.JSON(http.StatusForbidden, gin.H{"error": "evolution is disabled"})
		return
	}
	var body struct {
		ChallengerID uint `json:"challenger_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ChallengerID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "challenger_id required"})
		return
	}
	if err := h.Genomes.Promote(c.Request.Context(), body.ChallengerID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "promoted"})
}

// GetChampion GET /api/v1/genome/champion
func (h *EvolutionHandler) GetChampion(c *gin.Context) {
	strategyID := c.DefaultQuery("strategy_id", "sigmoid-btc")
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	rec, err := h.Genomes.LoadChampion(c.Request.Context(), strategyID, symbol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rec == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no champion yet"})
		return
	}
	c.JSON(http.StatusOK, rec)
}

// RegisterEvolutionRoutes 把 4 个 endpoint 注册到 Gin 路由。
// Phase 9 的 routes.go 会调用此函数。
func (h *EvolutionHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/evolution/tasks", h.CreateTask)
	rg.GET("/evolution/tasks", h.GetTaskStatus)
	rg.POST("/evolution/tasks/:id/promote", h.PromoteChallenger)
	rg.GET("/genome/champion", h.GetChampion)
}

func spawnFromJSON(sp *spawnPointJSON) *quant.SpawnPoint {
	if sp == nil {
		return nil
	}
	out := &quant.SpawnPoint{
		Policy: quant.CapitalPolicy{
			DeadReserveRatio: sp.Policy.DeadReserveRatio,
			GlobalStopLoss:   sp.Policy.GlobalStopLoss,
		},
		Risk: quant.RiskBounds{
			MaxLeverage: sp.Risk.MaxLeverage,
			TakerFeeBps: sp.Risk.TakerFeeBps,
		},
	}
	if out.Risk.MaxLeverage == 0 {
		out.Risk.MaxLeverage = 1
	}
	return out
}

// 保证 store 包被引用（避免 future import 删除误伤）
var _ = store.RoleChampion
