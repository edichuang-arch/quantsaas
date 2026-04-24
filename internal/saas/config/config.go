package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppRole 控制 SaaS 侧路由层的开放能力。
type AppRole string

const (
	RoleSaaS AppRole = "saas" // 云端生产：实例管理与交易下发，禁止进化/回测写接口
	RoleLab  AppRole = "lab"  // 本地算力：GA 进化与回测，禁止交易下发
	RoleDev  AppRole = "dev"  // 开发测试：全部打开
)

// Config 是 SaaS 进程启动所需的全部配置。
// 密钥字段（DB 密码、JWT secret）必须通过环境变量注入，yaml 中留空。
type Config struct {
	AppRole  AppRole        `yaml:"app_role"`
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"` // 建议留空，从 DB_PASSWORD 环境变量注入
	DBName   string `yaml:"dbname"`
	SSLMode  string `yaml:"sslmode"`
}

// DSN 组合成 GORM postgres driver 需要的连接字符串。
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"` // 留空，从 REDIS_PASSWORD 环境变量注入
	DB       int    `yaml:"db"`
}

type JWTConfig struct {
	Secret         string `yaml:"secret"` // 留空，从 JWT_SECRET 环境变量注入
	ExpireHours    int    `yaml:"expire_hours"`
	AgentTokenTTLH int    `yaml:"agent_token_ttl_hours"`
}

// Load 从 yaml 文件读取配置，然后用环境变量覆盖密钥字段。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	applyEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config invalid: %w", err)
	}
	return cfg, nil
}

// applyEnvOverrides 用环境变量覆盖密钥字段与 APP_ROLE。
// 环境变量总是比 yaml 更高优先级，方便 Docker/Kubernetes 注入。
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("APP_ROLE"); v != "" {
		cfg.AppRole = AppRole(strings.ToLower(v))
	}
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.Database.DBName = v
	}
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		cfg.Redis.Addr = v
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWT.Secret = v
	}
}

// Validate 检查所有必填字段与角色枚举的合法性。
func (c *Config) Validate() error {
	switch c.AppRole {
	case RoleSaaS, RoleLab, RoleDev:
		// ok
	default:
		return fmt.Errorf("app_role must be one of saas/lab/dev, got %q", c.AppRole)
	}
	if c.Database.Host == "" || c.Database.DBName == "" {
		return fmt.Errorf("database host/dbname required")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("DB_PASSWORD env var required (do not hardcode in yaml)")
	}
	if c.JWT.Secret == "" {
		return fmt.Errorf("JWT_SECRET env var required")
	}
	if c.JWT.ExpireHours <= 0 {
		c.JWT.ExpireHours = 24
	}
	if c.JWT.AgentTokenTTLH <= 0 {
		c.JWT.AgentTokenTTLH = 24 * 7
	}
	return nil
}

// IsWriteAllowed 检查当前 role 是否允许执行写操作（实例启停、交易下发）。
// SaaS 云端不允许写操作时，可以在 handler 层调用此函数做快速返回。
func (c *Config) IsWriteAllowed() bool {
	return c.AppRole == RoleSaaS || c.AppRole == RoleDev
}

// IsEvolutionAllowed 检查当前 role 是否允许 GA 进化与回测。
func (c *Config) IsEvolutionAllowed() bool {
	return c.AppRole == RoleLab || c.AppRole == RoleDev
}
