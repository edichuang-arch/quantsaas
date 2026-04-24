package api

import (
	"net/http"

	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/edi/quantsaas/internal/saas/ws"
	"github.com/gin-gonic/gin"
)

// DashboardHandler 处理 /api/v1/dashboard 与 /api/v1/system/* 路由。
type DashboardHandler struct {
	DB  *store.DB
	Hub *ws.Hub
	Cfg *config.Config
}

// NewDashboardHandler 构造 handler。
func NewDashboardHandler(db *store.DB, hub *ws.Hub, cfg *config.Config) *DashboardHandler {
	return &DashboardHandler{DB: db, Hub: hub, Cfg: cfg}
}

// Overview GET /api/v1/dashboard — 返回实例汇总、总资产、最近成交等。
func (h *DashboardHandler) Overview(c *gin.Context) {
	claims := getClaims(c)
	var insts []store.StrategyInstance
	if err := h.DB.WithContext(c.Request.Context()).
		Where("user_id = ?", claims.UserID).Find(&insts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 汇总每个实例的 PortfolioState 与最近成交次数
	type summary struct {
		Instance         store.StrategyInstance `json:"instance"`
		Portfolio        *store.PortfolioState  `json:"portfolio,omitempty"`
		RecentTradeCount int64                  `json:"recent_trade_count"`
	}
	out := make([]summary, 0, len(insts))
	for _, inst := range insts {
		var pf store.PortfolioState
		_ = h.DB.Where("instance_id = ?", inst.ID).First(&pf).Error
		var count int64
		_ = h.DB.Model(&store.TradeRecord{}).Where("instance_id = ?", inst.ID).Count(&count).Error
		// PF 若读不到，传 nil 避免误导
		var pfPtr *store.PortfolioState
		if pf.ID > 0 {
			pfCopy := pf
			pfPtr = &pfCopy
		}
		out = append(out, summary{Instance: inst, Portfolio: pfPtr, RecentTradeCount: count})
	}
	c.JSON(http.StatusOK, out)
}

// SystemStatus GET /api/v1/system/status — 给前端 Topbar 轮询用。
func (h *DashboardHandler) SystemStatus(c *gin.Context) {
	claims := getClaims(c)
	status := gin.H{
		"app_role":      h.Cfg.AppRole,
		"api_connected": false, // 当前用户是否有 Agent 在线
		"online_total":  0,
		"server_time":   nowUnixMS(),
	}
	if h.Hub != nil {
		status["api_connected"] = h.Hub.IsOnline(claims.UserID)
		status["online_total"] = h.Hub.OnlineCount()
	}
	c.JSON(http.StatusOK, status)
}

// AgentStatus GET /api/v1/agents/status — 前端 /agents 页面使用。
func (h *DashboardHandler) AgentStatus(c *gin.Context) {
	claims := getClaims(c)
	online := false
	if h.Hub != nil {
		online = h.Hub.IsOnline(claims.UserID)
	}
	c.JSON(http.StatusOK, gin.H{
		"online":         online,
		"user_id":        claims.UserID,
		"api_configured": false, // Agent 本地持有，SaaS 无法知晓；前端仅显示"已连接"等代理
	})
}
