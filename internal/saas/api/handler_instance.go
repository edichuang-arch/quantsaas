package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/edi/quantsaas/internal/saas/instance"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/gin-gonic/gin"
)

// InstanceHandler 处理 /api/v1/instances/* 路由。
type InstanceHandler struct {
	Manager *instance.Manager
	DB      *store.DB
}

// NewInstanceHandler 构造 handler。
func NewInstanceHandler(mgr *instance.Manager, db *store.DB) *InstanceHandler {
	return &InstanceHandler{Manager: mgr, DB: db}
}

// List GET /api/v1/instances
func (h *InstanceHandler) List(c *gin.Context) {
	claims := getClaims(c)
	insts, err := h.Manager.ListByUser(c.Request.Context(), claims.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, insts)
}

// CreateInstanceRequest 创建实例请求体。
type CreateInstanceRequest struct {
	TemplateID         uint    `json:"template_id"`
	Name               string  `json:"name"`
	Symbol             string  `json:"symbol"`
	InitialCapitalUSDT float64 `json:"initial_capital_usdt"`
	MonthlyInjectUSDT  float64 `json:"monthly_inject_usdt"`
}

// Create POST /api/v1/instances
func (h *InstanceHandler) Create(c *gin.Context) {
	claims := getClaims(c)
	var req CreateInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inst, err := h.Manager.Create(c.Request.Context(), instance.CreateRequest{
		UserID:             claims.UserID,
		TemplateID:         req.TemplateID,
		Name:               req.Name,
		Symbol:             req.Symbol,
		InitialCapitalUSDT: req.InitialCapitalUSDT,
		MonthlyInjectUSDT:  req.MonthlyInjectUSDT,
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, instance.ErrQuotaExceeded) {
			code = http.StatusForbidden
		}
		c.JSON(code, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, inst)
}

// Start POST /api/v1/instances/:id/start
func (h *InstanceHandler) Start(c *gin.Context) {
	claims := getClaims(c)
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.Manager.Start(c.Request.Context(), id, claims.UserID); err != nil {
		c.JSON(resolveCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "running"})
}

// Stop POST /api/v1/instances/:id/stop
func (h *InstanceHandler) Stop(c *gin.Context) {
	claims := getClaims(c)
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.Manager.Stop(c.Request.Context(), id, claims.UserID); err != nil {
		c.JSON(resolveCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

// Delete DELETE /api/v1/instances/:id
func (h *InstanceHandler) Delete(c *gin.Context) {
	claims := getClaims(c)
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.Manager.Delete(c.Request.Context(), id, claims.UserID); err != nil {
		c.JSON(resolveCode(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// GetPortfolio GET /api/v1/instances/:id/portfolio — 返回实时账户快照。
func (h *InstanceHandler) GetPortfolio(c *gin.Context) {
	claims := getClaims(c)
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	// 验证实例归属当前用户
	var inst store.StrategyInstance
	if err := h.DB.WithContext(c.Request.Context()).
		Where("id = ? AND user_id = ?", id, claims.UserID).
		First(&inst).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}

	var pf store.PortfolioState
	if err := h.DB.WithContext(c.Request.Context()).
		Where("instance_id = ?", id).First(&pf).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "portfolio not found"})
		return
	}
	c.JSON(http.StatusOK, pf)
}

// TradesResponse 分页+筛选后的成交清单回应。
type TradesResponse struct {
	Data     []store.TradeRecord `json:"data"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int64               `json:"total"`
}

// ListTrades GET /api/v1/instances/:id/trades
//
// Query params（全部 optional）：
//   page=1            分页页码（从 1 开始）
//   page_size=50      每页笔数（上限 200）
//   action=BUY|SELL   动作筛选
//   engine=MACRO|MICRO Engine 筛选
//   lot_type=DEAD_STACK|FLOATING|COLD_SEALED  Lot 类型筛选
//
// 回应固定为 TradesResponse。
func (h *InstanceHandler) ListTrades(c *gin.Context) {
	claims := getClaims(c)
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var inst store.StrategyInstance
	if err := h.DB.WithContext(c.Request.Context()).
		Where("id = ? AND user_id = ?", id, claims.UserID).First(&inst).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}

	page := parseIntDefault(c.Query("page"), 1, 1, 100000)
	pageSize := parseIntDefault(c.Query("page_size"), 50, 1, 200)

	q := h.DB.WithContext(c.Request.Context()).Model(&store.TradeRecord{}).
		Where("instance_id = ?", id)
	if action := c.Query("action"); action != "" {
		q = q.Where("action = ?", action)
	}
	if engine := c.Query("engine"); engine != "" {
		q = q.Where("engine = ?", engine)
	}
	if lotType := c.Query("lot_type"); lotType != "" {
		q = q.Where("lot_type = ?", lotType)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var trades []store.TradeRecord
	if err := q.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&trades).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, TradesResponse{
		Data: trades, Page: page, PageSize: pageSize, Total: total,
	})
}

// parseIntDefault 解析 query string int；解析失败或越界 fallback 为 def。
func parseIntDefault(raw string, def, lo, hi int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- 私有工具 ---

func parseUintParam(c *gin.Context, name string) (uint, error) {
	raw := c.Param(name)
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(v), nil
}

// resolveCode 把 manager 错误映射到合适的 HTTP code。
func resolveCode(err error) int {
	switch {
	case errors.Is(err, instance.ErrInstanceNotFound):
		return http.StatusNotFound
	case errors.Is(err, instance.ErrQuotaExceeded):
		return http.StatusForbidden
	case errors.Is(err, instance.ErrInvalidTransition),
		errors.Is(err, instance.ErrDeleteRunning):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
