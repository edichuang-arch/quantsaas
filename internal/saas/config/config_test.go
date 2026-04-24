package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYaml = `
app_role: dev
server:
  host: 0.0.0.0
  port: 8080
database:
  host: localhost
  port: 5432
  user: quantsaas
  password: ""
  dbname: quantsaas
  sslmode: disable
redis:
  addr: localhost:6379
  password: ""
  db: 0
jwt:
  secret: ""
  expire_hours: 24
  agent_token_ttl_hours: 168
`

// writeTempYaml 写入临时 yaml 文件并返回路径。测试结束自动删除。
func writeTempYaml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// setEnv 封装：测试结束后自动恢复。
func setEnv(t *testing.T, key, val string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	require.NoError(t, os.Setenv(key, val))
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// unsetEnv 在测试开始时确保某个环境变量为空，测试结束恢复。
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
		}
	})
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
}

func TestLoad_InvalidYaml(t *testing.T) {
	path := writeTempYaml(t, "this: is: not: yaml: [[")
	_, err := Load(path)
	assert.Error(t, err)
}

func TestLoad_MissingJWTSecretFails(t *testing.T) {
	unsetEnv(t, "JWT_SECRET")
	unsetEnv(t, "DB_PASSWORD")
	path := writeTempYaml(t, validYaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD")
}

func TestLoad_MissingDBPasswordFails(t *testing.T) {
	unsetEnv(t, "DB_PASSWORD")
	setEnv(t, "JWT_SECRET", "test-jwt-secret-at-least-32-bytes-here!!!")
	path := writeTempYaml(t, validYaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD")
}

func TestLoad_Success(t *testing.T) {
	setEnv(t, "JWT_SECRET", "test-jwt-secret-at-least-32-bytes-here!!!")
	setEnv(t, "DB_PASSWORD", "db-password-from-env")
	path := writeTempYaml(t, validYaml)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, RoleDev, cfg.AppRole)
	assert.Equal(t, "db-password-from-env", cfg.Database.Password)
	assert.Equal(t, "test-jwt-secret-at-least-32-bytes-here!!!", cfg.JWT.Secret)
	assert.Equal(t, 8080, cfg.Server.Port)
}

func TestLoad_AppRoleEnvOverride(t *testing.T) {
	setEnv(t, "APP_ROLE", "saas")
	setEnv(t, "JWT_SECRET", "test-jwt-secret-at-least-32-bytes-here!!!")
	setEnv(t, "DB_PASSWORD", "db-password")
	path := writeTempYaml(t, validYaml)

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, RoleSaaS, cfg.AppRole)
}

func TestLoad_InvalidAppRoleRejected(t *testing.T) {
	setEnv(t, "APP_ROLE", "banana")
	setEnv(t, "JWT_SECRET", "test-jwt-secret-at-least-32-bytes-here!!!")
	setEnv(t, "DB_PASSWORD", "db-password")
	path := writeTempYaml(t, validYaml)

	_, err := Load(path)
	assert.Error(t, err)
}

func TestDSN_Format(t *testing.T) {
	d := DatabaseConfig{
		Host: "h", Port: 5432, User: "u", Password: "p", DBName: "q", SSLMode: "disable",
	}
	assert.Equal(t, "host=h port=5432 user=u password=p dbname=q sslmode=disable", d.DSN())
}

// IsWriteAllowed：saas / dev 允许；lab 禁止。
func TestIsWriteAllowed(t *testing.T) {
	assert.True(t, (&Config{AppRole: RoleSaaS}).IsWriteAllowed())
	assert.True(t, (&Config{AppRole: RoleDev}).IsWriteAllowed())
	assert.False(t, (&Config{AppRole: RoleLab}).IsWriteAllowed())
}

// IsEvolutionAllowed：lab / dev 允许；saas 禁止。
func TestIsEvolutionAllowed(t *testing.T) {
	assert.True(t, (&Config{AppRole: RoleLab}).IsEvolutionAllowed())
	assert.True(t, (&Config{AppRole: RoleDev}).IsEvolutionAllowed())
	assert.False(t, (&Config{AppRole: RoleSaaS}).IsEvolutionAllowed())
}

func TestLoad_DefaultsAppliedWhenTTLZero(t *testing.T) {
	setEnv(t, "JWT_SECRET", "test-jwt-secret-at-least-32-bytes-here!!!")
	setEnv(t, "DB_PASSWORD", "db-password")

	yamlContent := `
app_role: dev
server:
  host: 0.0.0.0
  port: 8080
database:
  host: localhost
  port: 5432
  user: u
  password: ""
  dbname: q
  sslmode: disable
redis:
  addr: localhost:6379
  password: ""
  db: 0
jwt:
  secret: ""
  expire_hours: 0
  agent_token_ttl_hours: 0
`
	path := writeTempYaml(t, yamlContent)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 24, cfg.JWT.ExpireHours)
	assert.Equal(t, 168, cfg.JWT.AgentTokenTTLH)
}
