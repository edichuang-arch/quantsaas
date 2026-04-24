# QuantSaaS

> 一個雲端驅動、由遺傳演算法自動調參、交易所 API Key 不離本地的量化交易 SaaS。

[![CI](https://github.com/edichuang-arch/quantsaas/actions/workflows/ci.yml/badge.svg)](https://github.com/edichuang-arch/quantsaas/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

## 一句話

**雲端只信上報,端側無腦執行**。你的 Binance API Key 永遠不離開本地機器,SaaS 只負責決策、GA 進化、排程、儀表板,實際下單由你本機的 LocalAgent 執行。

## 架構全景

```
┌─────────────────────┐           ┌──────────────────────────┐
│   Web 前端          │           │   LocalAgent (你的電腦)   │
│   React + Vite      │           │   持有 Binance API Key    │
│   Dashboard / GA    │           │   config.agent.yaml 本地  │
└──────────┬──────────┘           └──────────────┬───────────┘
           │ HTTP/REST                           │ WebSocket
           │ JWT Bearer                          │ 鑑權長連線
           ▼                                     ▼
┌─────────────────────────────────────────────────────────────┐
│                  SaaS 雲端 (cmd/saas)                        │
│                                                              │
│   ┌──────────┐  ┌───────────┐  ┌────────┐  ┌────────────┐   │
│   │  Cron    │  │ WebSocket │  │  REST  │  │ GA Engine  │   │
│   │ Scheduler│  │    Hub    │  │  API   │  │ (lab mode) │   │
│   └────┬─────┘  └─────┬─────┘  └────┬───┘  └──────┬─────┘   │
│        │              │             │             │         │
│        │     Step() 純函數(策略共用)│             │         │
│        │              │             │             │         │
│   ┌────▼──────────────▼─────────────▼─────────────▼──────┐  │
│   │       Postgres (quantsaas) + Redis (cache)          │  │
│   │  12 張表: Users / Instances / PortfolioStates /     │  │
│   │  SpotLots / TradeRecords / GeneRecords / KLines ... │  │
│   └─────────────────────────────────────────────────────┘  │
│                                                              │
│   ┌──────────────────────────────────────────────────────┐  │
│   │  K-line Collector (Binance 公開 API, 無需 API Key)   │  │
│   │  - 定期拉取最新 K 線                                 │  │
│   │  - cmd/collector 做一次性補歷史                      │  │
│   └──────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

### 三端物理部署

| 角色 | 跑在哪裡 | 持有什麼 |
|------|---------|---------|
| `saas` | 雲端 VPS | 決策大腦, 下發指令, **零 API Key** |
| `agent` | 你的電腦 | Binance API Key, **零策略邏輯** |
| `lab` | 本地算力機 | 跟 SaaS 連同一個 DB, 專跑 GA 進化 |

## 設計鐵律（不可妥協）

1. **策略同構**: 回測與實盤調用同一個 `Step()` 實現, 內部禁止 `if isBacktest` 分支
2. **策略純函數**: `Step()` 不得 I/O、不得 `time.Now()`、不得 DB 讀寫
3. **API Key 物理隔離**: 交易所密鑰只存在 `config.agent.yaml`, 永不上傳, 永不寫 DB
4. **GORM Code-First**: Schema 真源是 Go struct, `AutoMigrate` 自動同步, 永不寫 SQL migration
5. **無量綱計算**: 價格用對數收益率或比率, 禁止跨標的比較絕對價格
6. **單一 Postgres**: Redis 只做快取, 不承擔訊號傳遞

## 技術棧

| 層 | 選型 |
|----|-----|
| 後端語言 | Go 1.22+ |
| Web 框架 | Gin |
| ORM | GORM + Postgres driver |
| 快取 | Redis 7 (go-redis v9) |
| 進化演算法 | 自研 GA 引擎 (`internal/saas/ga/`) |
| WebSocket | gorilla/websocket |
| 日誌 | Uber zap |
| 排程 | robfig/cron/v3 |
| 測試 | testify + SQLite in-memory (整合測試) |
| 前端 | React 18 + TypeScript + Vite |
| 前端 UI | Tailwind CSS v3 + 自研元件 |
| 前端狀態 | Zustand + TanStack Query |
| 圖表 | Recharts |
| 動畫 | framer-motion |
| 容器 | Docker + docker-compose (Colima 相容) |

## Quick Start

### 前置條件

- Go 1.22+
- Node.js 20+
- Docker 或 Colima
- Git

### 5 分鐘起步

```bash
# 1. 啟動 Postgres + Redis
make dev-up

# 2. 設定環境變數（至少 32 byte 的 JWT secret）
export JWT_SECRET="your-long-random-string-at-least-32-bytes-long"
export DB_PASSWORD="quantsaas-dev-pw"
export APP_ROLE=dev

# 3. 編譯 + 啟動 SaaS（含前端靜態掛載）
make build
./bin/saas -config config.yaml

# 4. 開瀏覽器
open http://localhost:8080
```

### 補歷史 K 線

```bash
# 拉 BTCUSDT 5m, 過去 365 天（約 10.5 萬根 bar, 耗時 1-2 分鐘）
./bin/collector backfill -symbol BTCUSDT -interval 5m -days 365

# 查看資料現狀
./bin/collector status -symbol BTCUSDT -interval 5m
```

### 接真實 Binance Agent

```bash
# 1. 複製 config 模板
cp config.agent.yaml.example config.agent.yaml

# 2. 編輯填入 Binance API Key（建議從 testnet 開始）
vim config.agent.yaml

# 3. 啟動 Agent
./bin/agent -config config.agent.yaml
```

## 開發指南

### 專案目錄

```
quantsaas/
├── cmd/
│   ├── saas/          # 雲端 SaaS 進程入口
│   ├── agent/         # 本地 Agent 進程入口
│   └── collector/     # K 線補歷史 CLI
├── internal/
│   ├── saas/          # SaaS 業務邏輯
│   │   ├── api/       # REST API handlers + routes
│   │   ├── auth/      # JWT + bcrypt
│   │   ├── config/    # 配置載入 + env 覆蓋
│   │   ├── cron/      # 排程器
│   │   ├── epoch/     # 進化任務服務
│   │   ├── ga/        # GA 引擎 + EvolvableStrategy 適配器
│   │   ├── instance/  # 實例管理 + Tick 函數
│   │   ├── store/     # GORM models + DB / Redis 封裝
│   │   ├── integration/   # 整合測試 (build tag)
│   │   └── ws/        # WebSocket Hub + DeltaReport Reconciler
│   ├── agent/         # LocalAgent 邏輯
│   │   ├── config/    # API Key 本地配置
│   │   ├── exchange/  # Binance REST v3 + HMAC 簽名
│   │   └── ws/        # WebSocket Client + 指數退避重連
│   ├── collector/     # K 線收集器（公開 API, 無需 Key）
│   ├── quant/         # 策略共用數學（Sigmoid / Ghost DCA / Chromosome）
│   ├── strategies/
│   │   └── sigmoid-btc/   # 當前唯一策略: Sigmoid 動態天平
│   ├── adapters/
│   │   └── backtest/  # 回測適配器
│   └── wsproto/       # WebSocket 訊息類型（共用）
├── web-frontend/      # React 18 前端（10 頁面）
├── docs/              # 三份系統設計文件
├── .github/workflows/ # CI
├── saas.Dockerfile
├── agent.Dockerfile
├── docker-compose.yml
├── Makefile
└── README.md
```

### 常用命令

```bash
make build            # 編譯 saas + agent + collector + 前端
make test             # 單元測試（不含整合）
make test-integration # 整合測試（需 make dev-up 起容器）
make lint             # go vet + 鐵律檢查
make dev-up           # 起 Postgres + Redis
make dev-down         # 停所有 compose 服務
```

### 鐵律驗證

```bash
make lint
```

會跑以下檢查（任何一條違反即失敗）：

- `grep isBacktest internal/strategies/` 必須為空
- `grep api_key|secret_key internal/saas/` 必須為空
- `grep quant.Bar internal/strategies/` 必須為空
- 策略包不得匯入 net/http / database/sql / os/exec

## API 文件速查

### 認證

```http
POST /api/v1/auth/register    # 註冊
POST /api/v1/auth/login       # 登入（支援 actor=agent 取 Agent token）
GET  /api/v1/auth/me          # 當前用戶資訊
```

### 實例管理

```http
GET    /api/v1/instances            # 列出我的實例
POST   /api/v1/instances            # 建立實例（需 saas/dev 角色）
POST   /api/v1/instances/:id/start  # 啟動
POST   /api/v1/instances/:id/stop   # 暫停
DELETE /api/v1/instances/:id        # 刪除
GET    /api/v1/instances/:id/portfolio  # 帳戶快照
GET    /api/v1/instances/:id/trades     # 成交歷史
```

### 進化（僅 lab/dev）

```http
POST /api/v1/evolution/tasks              # 啟動 GA 任務
GET  /api/v1/evolution/tasks              # 任務狀態 + challenger 列表
POST /api/v1/evolution/tasks/:id/promote  # 人工晉升 challenger 為 champion
GET  /api/v1/genome/champion              # 當前冠軍參數
```

### WebSocket

```
GET /ws/agent   # Agent 建立長連線（需 Agent JWT）
```

訊息類型（`internal/wsproto/`）: `auth` / `auth_result` / `heartbeat` / `heartbeat_ack` / `command` / `command_ack` / `delta_report` / `report_ack`

## 核心設計亮點

### Sigmoid 動態天平（微觀引擎）

```
Signal       = (價格 - EMA) / σ              # 無量綱
InventoryBias= clamp(當前權重, 0, 1) - 0.5
Exponent     = β × Signal + γ × InventoryBias
TargetWeight = 1 / (1 + exp(Exponent))       # Sigmoid 夾在 [0,1]
```

價格偏離均線 → 自動調整目標倉位。楔形過濾避免安靜期粉塵訂單。

### GA 進化引擎

- 8-動詞 EvolvableStrategy 介面: `Sample / Mutate / Crossover / Fingerprint / Evaluate / DecodeElite / EncodeResult / StrategyID`
- 多時段坩堝評估: 6m / 2y / 5y / full 加權 (0.10 / 0.20 / 0.30 / 0.40)
- 級聯短路: MaxDD ≥ 88% 立即判 fatal, 不浪費長窗口算力
- 指紋快取（FNV-1a-64, 精度 1e-6）: 代內重複基因共用評估結果
- 變異斜坡（Mutation Ramp）: 收斂停滯時自動放大 mutProb / mutScale

## 測試現狀

- 單元 + 整合測試: **192 個**全綠
- 覆蓋率: quant 86% / auth 91% / config 83% / backtest 94% / strategies/sigmoid-btc 78% / ga 60% / ws 75%
- 鐵律自動檢查（`make lint`）全綠

## 授權

MIT. 詳見 [LICENSE](LICENSE)。

## 致謝

本專案以三份設計文件（`docs/`）為唯一功能真源，嚴格按規格落地。

---

*程式碼即產品, 鐵律即品管。*
