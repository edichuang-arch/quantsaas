package api

import (
	"errors"
	"net/http"

	"github.com/edi/quantsaas/internal/saas/auth"
	"github.com/edi/quantsaas/internal/saas/store"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AuthHandler 处理 /api/v1/auth/* 路由。
type AuthHandler struct {
	DB   *store.DB
	Auth *auth.Service
}

// NewAuthHandler 构造 handler。
func NewAuthHandler(db *store.DB, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{DB: db, Auth: authSvc}
}

// RegisterRequest 注册请求体。
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenResponse 登录/注册共用的返回结构。
type TokenResponse struct {
	Token  string `json:"token"`
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	Plan   string `json:"plan"`
}

// Register POST /api/v1/auth/register。
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Email == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password required"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user := store.User{
		Email:        req.Email,
		PasswordHash: hash,
		Plan:         store.PlanFree,
		MaxInstances: 1,
	}
	if err := h.DB.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	token, err := h.Auth.SignUserToken(user.ID, user.Email, string(user.Plan))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, TokenResponse{
		Token:  token,
		UserID: user.ID,
		Email:  user.Email,
		Plan:   string(user.Plan),
	})
}

// Login POST /api/v1/auth/login。
// LocalAgent 也通过此 endpoint 拿 JWT（SignAgentToken 用于 Agent actor 的 token）。
// 这里简化：用户与 agent 用同一个 endpoint，响应里根据 `actor` 参数决定 token 类型。
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Actor    string `json:"actor,omitempty"` // "agent" 取 agent token；空或 "user" 取 user token
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var user store.User
	err := h.DB.WithContext(c.Request.Context()).
		Where("email = ?", req.Email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !auth.VerifyPassword(user.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	var token string
	if req.Actor == "agent" {
		token, err = h.Auth.SignAgentToken(user.ID, user.Email)
	} else {
		token, err = h.Auth.SignUserToken(user.ID, user.Email, string(user.Plan))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, TokenResponse{
		Token:  token,
		UserID: user.ID,
		Email:  user.Email,
		Plan:   string(user.Plan),
	})
}

// Me GET /api/v1/auth/me — 返回当前 token 对应的用户资料（前端启动时校验 token 有效性）。
func (h *AuthHandler) Me(c *gin.Context) {
	claims := getClaims(c)
	if claims == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no claims"})
		return
	}
	var user store.User
	if err := h.DB.WithContext(c.Request.Context()).First(&user, claims.UserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"user_id":       user.ID,
		"email":         user.Email,
		"plan":          user.Plan,
		"max_instances": user.MaxInstances,
	})
}
