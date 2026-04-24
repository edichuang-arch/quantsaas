// Package config LocalAgent 本地配置加载。
//
// 铁律 #5（API Key 物理隔离）：
//   - Binance API Key/Secret 只能存在于 config.agent.yaml
//   - 本文件必须在 .gitignore 中（已在项目根 .gitignore 配置）
//   - 永远不要把 ExchangeConfig 上传到 SaaS 或 DB
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentConfig Agent 启动配置。
type AgentConfig struct {
	SaaSURL   string           `yaml:"saas_url"`   // 如 "https://quantsaas.example.com" 或 "ws://localhost:8080"
	Email     string           `yaml:"email"`      // SaaS 用户 email
	Password  string           `yaml:"password"`   // SaaS 用户密码（仅本地持有；建议用环境变量覆盖）
	Exchange  ExchangeConfig   `yaml:"exchange"`
	Reconnect ReconnectConfig  `yaml:"reconnect"`
}

// ExchangeConfig Binance 现货 API 凭证。
type ExchangeConfig struct {
	Name      string `yaml:"name"`       // "binance"（未来可扩展）
	APIKey    string `yaml:"api_key"`
	SecretKey string `yaml:"secret_key"`
	Sandbox   bool   `yaml:"sandbox"`    // true 使用 testnet endpoint
	BaseURL   string `yaml:"base_url"`   // 可覆盖默认 endpoint（测试用）
}

// ReconnectConfig 重连策略。
type ReconnectConfig struct {
	InitialBackoffMs int `yaml:"initial_backoff_ms"`
	MaxBackoffMs     int `yaml:"max_backoff_ms"`
}

// Load 从 yaml + 环境变量载入配置。
// 环境变量优先级高于 yaml：
//   AGENT_SAAS_URL / AGENT_EMAIL / AGENT_PASSWORD
//   BINANCE_API_KEY / BINANCE_SECRET_KEY
func Load(path string) (*AgentConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := &AgentConfig{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	applyEnvOverrides(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *AgentConfig) {
	if v := os.Getenv("AGENT_SAAS_URL"); v != "" {
		cfg.SaaSURL = v
	}
	if v := os.Getenv("AGENT_EMAIL"); v != "" {
		cfg.Email = v
	}
	if v := os.Getenv("AGENT_PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv("BINANCE_API_KEY"); v != "" {
		cfg.Exchange.APIKey = v
	}
	if v := os.Getenv("BINANCE_SECRET_KEY"); v != "" {
		cfg.Exchange.SecretKey = v
	}
}

// Validate 检查所有必填字段。
func (c *AgentConfig) Validate() error {
	if c.SaaSURL == "" {
		return fmt.Errorf("saas_url required")
	}
	if c.Email == "" || c.Password == "" {
		return fmt.Errorf("email/password required")
	}
	if c.Exchange.APIKey == "" || c.Exchange.SecretKey == "" {
		return fmt.Errorf("exchange api_key/secret_key required (do not commit to git)")
	}
	if c.Exchange.Name == "" {
		c.Exchange.Name = "binance"
	}
	if c.Reconnect.InitialBackoffMs <= 0 {
		c.Reconnect.InitialBackoffMs = 1000
	}
	if c.Reconnect.MaxBackoffMs <= 0 {
		c.Reconnect.MaxBackoffMs = 5 * 60 * 1000
	}
	return nil
}

// InitialBackoff / MaxBackoff 给 ws client 直接取用。
func (c *AgentConfig) InitialBackoff() time.Duration {
	return time.Duration(c.Reconnect.InitialBackoffMs) * time.Millisecond
}

func (c *AgentConfig) MaxBackoff() time.Duration {
	return time.Duration(c.Reconnect.MaxBackoffMs) * time.Millisecond
}
