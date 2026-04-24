package api

import (
	"net/http"
	"strings"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/gin-gonic/gin"
)

const (
	ctxKeyClaims = "auth_claims"
)

// AuthMiddleware 校验 JWT，成功后把 Claims 放入 context。
func AuthMiddleware(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		claims, err := authSvc.ParseToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Set(ctxKeyClaims, claims)
		c.Next()
	}
}

// RequireWrite 拦截 app_role=lab 的写接口（实例启停、交易下发）。
// saas / dev 通过；lab 返回 403。
func RequireWrite(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.IsWriteAllowed() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "write disabled in lab mode"})
			return
		}
		c.Next()
	}
}

// RequireEvolution 拦截 app_role=saas 的进化接口。
// lab / dev 通过；saas 返回 403。
func RequireEvolution(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.IsEvolutionAllowed() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "evolution disabled in saas mode"})
			return
		}
		c.Next()
	}
}

// getClaims 从 context 取出 Claims。auth 中间件确保非 nil。
func getClaims(c *gin.Context) *auth.Claims {
	v, ok := c.Get(ctxKeyClaims)
	if !ok {
		return nil
	}
	cl, _ := v.(*auth.Claims)
	return cl
}
