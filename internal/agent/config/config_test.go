package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYaml = `
saas_url: http://localhost:8080
email: test@example.com
password: pw
exchange:
  name: binance
  api_key: key
  secret_key: sec
  sandbox: true
reconnect:
  initial_backoff_ms: 500
  max_backoff_ms: 60000
`

func writeTempYaml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	old, had := os.LookupEnv(k)
	require.NoError(t, os.Setenv(k, v))
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(k, old)
		} else {
			_ = os.Unsetenv(k)
		}
	})
}

func TestLoad_ValidYaml(t *testing.T) {
	path := writeTempYaml(t, validYaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", cfg.SaaSURL)
	assert.Equal(t, "binance", cfg.Exchange.Name)
	assert.True(t, cfg.Exchange.Sandbox)
}

func TestLoad_EnvOverrides(t *testing.T) {
	setEnv(t, "BINANCE_API_KEY", "env-key")
	setEnv(t, "BINANCE_SECRET_KEY", "env-secret")
	setEnv(t, "AGENT_PASSWORD", "env-pw")
	path := writeTempYaml(t, validYaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "env-key", cfg.Exchange.APIKey)
	assert.Equal(t, "env-secret", cfg.Exchange.SecretKey)
	assert.Equal(t, "env-pw", cfg.Password)
}

func TestLoad_RequiresAPIKey(t *testing.T) {
	// API Key 为空时拒绝
	setEnv(t, "BINANCE_API_KEY", "")
	incomplete := `
saas_url: http://localhost
email: a@b.com
password: p
exchange:
  name: binance
  api_key: ""
  secret_key: ""
`
	path := writeTempYaml(t, incomplete)
	_, err := Load(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}

func TestLoad_RequiresSaaSURL(t *testing.T) {
	incomplete := `
saas_url: ""
email: a@b.com
password: p
exchange:
  api_key: k
  secret_key: s
`
	path := writeTempYaml(t, incomplete)
	_, err := Load(path)
	assert.Error(t, err)
}

func TestLoad_AppliesBackoffDefaults(t *testing.T) {
	minYaml := `
saas_url: http://x
email: a@b
password: p
exchange:
  api_key: k
  secret_key: s
`
	path := writeTempYaml(t, minYaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 1000, cfg.Reconnect.InitialBackoffMs)
	assert.Equal(t, 5*60*1000, cfg.Reconnect.MaxBackoffMs)
}
