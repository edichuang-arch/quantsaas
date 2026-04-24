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

// ListTrades GET /api/v1/instances/:id/trades
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
	var trades []store.TradeRecord
	if err := h.DB.WithContext(c.Request.Context()).
		Where("instance_id = ?", id).
		Order("created_at DESC").
		Limit(100).Find(&trades).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, trades)
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
