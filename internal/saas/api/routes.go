package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/edi/quantsaas/internal/saas/ws"
	"github.com/gin-gonic/gin"
)

// Dependencies 路由注册时所需的全部依赖。
// cmd/saas/main.go 构造完毕后一次性传入 RegisterRoutes。
type Dependencies struct {
	Config     *config.Config
	Auth       *auth.Service
	AuthH      *AuthHandler
	InstanceH  *InstanceHandler
	DashboardH *DashboardHandler
	EvolutionH *EvolutionHandler
	Hub        *ws.Hub
	// WebDistDir 前端静态资源目录（由 vite build 产出 web-frontend/dist）。
	// 空字符串表示不挂载前端（例如 dev 或 lab 模式）。
	WebDistDir string
}

// RegisterRoutes 在 Gin engine 上注册所有路由。
func RegisterRoutes(r *gin.Engine, dep *Dependencies) {
	// 健康检查（无鉴权）
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// WebSocket Hub 路由（鉴权逻辑在 Hub.HandleConnection 内，需要首条 auth 消息）
	if dep.Hub != nil {
		r.GET("/ws/agent", dep.Hub.HandleConnection)
	}

	v1 := r.Group("/api/v1")

	// 公开路由
	if dep.AuthH != nil {
		v1.POST("/auth/register", dep.AuthH.Register)
		v1.POST("/auth/login", dep.AuthH.Login)
	}

	// 鉴权路由组
	authed := v1.Group("")
	authed.Use(AuthMiddleware(dep.Auth))

	if dep.AuthH != nil {
		authed.GET("/auth/me", dep.AuthH.Me)
	}

	// 实例 CRUD（write 接口需要 saas/dev 角色）
	if dep.InstanceH != nil {
		authed.GET("/instances", dep.InstanceH.List)
		authed.GET("/instances/:id/portfolio", dep.InstanceH.GetPortfolio)
		authed.GET("/instances/:id/trades", dep.InstanceH.ListTrades)

		writeGroup := authed.Group("")
		writeGroup.Use(RequireWrite(dep.Config))
		writeGroup.POST("/instances", dep.InstanceH.Create)
		writeGroup.POST("/instances/:id/start", dep.InstanceH.Start)
		writeGroup.POST("/instances/:id/stop", dep.InstanceH.Stop)
		writeGroup.DELETE("/instances/:id", dep.InstanceH.Delete)
	}

	// Dashboard 与系统状态
	if dep.DashboardH != nil {
		authed.GET("/dashboard", dep.DashboardH.Overview)
		authed.GET("/system/status", dep.DashboardH.SystemStatus)
		authed.GET("/agents/status", dep.DashboardH.AgentStatus)
	}

	// 进化路由（仅 lab/dev 可用）
	if dep.EvolutionH != nil {
		evo := authed.Group("")
		evo.Use(RequireEvolution(dep.Config))
		evo.POST("/evolution/tasks", dep.EvolutionH.CreateTask)
		evo.GET("/evolution/tasks", dep.EvolutionH.GetTaskStatus)
		evo.POST("/evolution/tasks/:id/promote", dep.EvolutionH.PromoteChallenger)
		evo.GET("/genome/champion", dep.EvolutionH.GetChampion)
	}

	// 前端静态资源（SPA fallback：非 API 路径全部返回 index.html 给 React Router 处理）
	if dep.WebDistDir != "" {
		mountSPA(r, dep.WebDistDir)
	}
}

// mountSPA 挂载 Vite 构建产物，并为非 API/WS/healthz 路径做 SPA fallback。
func mountSPA(r *gin.Engine, dist string) {
	// 静态资源
	r.Static("/assets", filepath.Join(dist, "assets"))
	// favicon 之类
	for _, f := range []string{"favicon.ico", "robots.txt", "vite.svg"} {
		p := filepath.Join(dist, f)
		if _, err := os.Stat(p); err == nil {
			file := f
			r.GET("/"+file, func(c *gin.Context) { c.File(filepath.Join(dist, file)) })
		}
	}
	// SPA fallback
	indexPath := filepath.Join(dist, "index.html")
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		// API / WS 路径不 fallback
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/ws/") || p == "/healthz" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if _, err := os.Stat(indexPath); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "frontend not built"})
			return
		}
		c.File(indexPath)
	})
}
