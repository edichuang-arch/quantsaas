package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Actor 类型区分 token 的使用场景。
// user  = 终端用户 JWT（前端访问 REST API）
// agent = LocalAgent JWT（建立 WebSocket 长连接用）
type Actor string

const (
	ActorUser  Actor = "user"
	ActorAgent Actor = "agent"
)

// Claims 自定义的 JWT payload。
// UserID 永远非零；Role 只在当前 token 被用作 SaaS 路由鉴权时判断。
type Claims struct {
	UserID uint   `json:"uid"`
	Email  string `json:"email"`
	Actor  Actor  `json:"actor"`
	Plan   string `json:"plan,omitempty"`
	jwt.RegisteredClaims
}

// Service 封装 JWT 签发与校验 + 密码哈希。
// 不持有 DB 引用——所有持久化由上层（user service）负责。
type Service struct {
	secret         []byte
	userTTL        time.Duration
	agentTTL       time.Duration
}

// NewService 用 config.JWT 构造 Service。
// secret 必须至少 32 字节；否则 panic（这是部署错误，不是运行时错误）。
func NewService(secret string, userTTLHours, agentTTLHours int) *Service {
	if len(secret) < 32 {
		panic("JWT secret must be at least 32 bytes; set JWT_SECRET env var")
	}
	if userTTLHours <= 0 {
		userTTLHours = 24
	}
	if agentTTLHours <= 0 {
		agentTTLHours = 24 * 7
	}
	return &Service{
		secret:   []byte(secret),
		userTTL:  time.Duration(userTTLHours) * time.Hour,
		agentTTL: time.Duration(agentTTLHours) * time.Hour,
	}
}

// SignUserToken 签发终端用户 token，用于前端 Bearer auth。
func (s *Service) SignUserToken(userID uint, email, plan string) (string, error) {
	return s.sign(userID, email, plan, ActorUser, s.userTTL)
}

// SignAgentToken 签发 LocalAgent 连接用 token，TTL 比用户 token 长。
func (s *Service) SignAgentToken(userID uint, email string) (string, error) {
	return s.sign(userID, email, "", ActorAgent, s.agentTTL)
}

func (s *Service) sign(userID uint, email, plan string, actor Actor, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		UserID: userID,
		Email:  email,
		Actor:  actor,
		Plan:   plan,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "quantsaas",
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(s.secret)
}

// ParseToken 解码并验证签名与过期。
// 返回完整 Claims 供上层进一步判断角色。
func (s *Service) ParseToken(tokenStr string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token claims")
	}
	if claims.UserID == 0 {
		return nil, errors.New("token missing user id")
	}
	return claims, nil
}

// HashPassword 用 bcrypt 生成密码哈希（cost=12，平衡安全与登录延迟）。
func HashPassword(plain string) (string, error) {
	if len(plain) < 6 {
		return "", errors.New("password must be at least 6 characters")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword 恒定时间比较，匹配失败返回 false+nil（不泄露是用户不存在还是密码错）。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
