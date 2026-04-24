# QuantSaaS 统一构建脚本。
#
# 常用目标：
#   make build           编译 saas + agent + 前端
#   make build-saas      只编译 SaaS 二进制
#   make build-agent     只编译 Agent 二进制
#   make build-frontend  构建前端 dist/
#   make test            运行单元测试（不含 integration）
#   make test-integration  运行整合测试（需 docker-compose up postgres + redis）
#   make lint            go vet + 铁律检查
#   make dev-up / dev-down  启动 / 关闭本地 docker-compose 服务
#   make run-saas        本地运行 SaaS（需要本机有 Postgres / Redis）
#   make clean           清理 bin / dist

.PHONY: build build-saas build-agent build-frontend test test-integration lint dev-up dev-down run-saas clean

BIN_DIR := bin
GO_LDFLAGS := -s -w

build: build-saas build-agent build-frontend

build-saas:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/saas ./cmd/saas

build-agent:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -ldflags="$(GO_LDFLAGS)" -o $(BIN_DIR)/agent ./cmd/agent

build-frontend:
	cd web-frontend && npm install && npm run build

test:
	go test ./... -race -timeout 300s

test-integration:
	go test -tags=integration -timeout 300s ./internal/saas/integration/...

lint:
	go vet ./...
	@echo ""
	@echo "=== 铁律 1: 禁止 isBacktest 分支 ==="
	@! grep -rn "isBacktest" internal/strategies/ internal/adapters/ internal/saas/ga/ 2>/dev/null | grep -v "_test.go" | grep -v "//"
	@echo "OK"
	@echo "=== 铁律 5: SaaS 侧禁止 API Key 字段 ==="
	@! grep -rnE "api_key|secret_key|passphrase" internal/saas/ 2>/dev/null | grep -v "_test.go" | grep -v "//" | grep -v "ANTHROPIC"
	@echo "OK"
	@echo "=== 铁律 4: 策略内核禁止依赖 quant.Bar ==="
	@! grep -rn "quant\.Bar" internal/strategies/ 2>/dev/null | grep -v "//"
	@echo "OK"

dev-up:
	docker compose up -d postgres redis
	@echo "Postgres + Redis started. Set env JWT_SECRET, DB_PASSWORD, then run: make run-saas"

dev-down:
	docker compose down

run-saas:
	./$(BIN_DIR)/saas -config config.yaml

run-agent:
	./$(BIN_DIR)/agent -config config.agent.yaml

clean:
	rm -rf $(BIN_DIR) web-frontend/dist web-frontend/node_modules
