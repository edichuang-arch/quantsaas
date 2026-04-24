# QuantSaaS Project — AI 工作宪法

## 唯一功能真源

当前功能的唯一定义源是 `docs/` 下的三份文档：

- `docs/系统总体拓扑结构.md` — 系统物理端、模块、状态流转与生命周期
- `docs/策略数学引擎.md` — Step() 的数学规格与资产三态语义
- `docs/进化计算引擎.md` — GA 遗传算法引擎的完整规格

**三份文档未定义的功能不进入实现。** 任何代码实现与文档不符时，以文档为准；若文档本身需要调整，先改文档再改代码。

---

## 工作顺序

1. **涉及策略和回测**：先读 `docs/策略数学引擎.md` 与 `docs/进化计算引擎.md`，再动代码。
2. **涉及 Go 后端**：遵守 GORM Code-First 原则，只用 `AutoMigrate`，永不写 SQL migration 文件。
3. **涉及价格计算**：优先使用对数收益率、比率等无量纲表达，禁止跨标的比较绝对价格。
4. **涉及架构边界**：保持 SaaS / Strategy / Agent 三层分工，不做预防性解耦或过度抽象。

---

## 核心约束（铁律）

以下六条铁律违反任何一条都要立即停下来报告：

1. **策略复利前置条件**：策略设计必须能清楚说明复利如何发生（资金规模随权益正反馈滚动）。
2. **策略同构原则**：回测与实盘必须调用同一个 `Step()` 实现，`Step()` 内部禁止 `if isBacktest` 分支。
3. **Step() 只在 SaaS 侧执行**：Agent 二进制不包含任何策略代码。
4. **策略包内部纯净**：策略包禁止任何定时器、网络请求（HTTP/WebSocket/gRPC）、数据库读写、文件 I/O。
5. **API Key 物理隔离**：Binance API Key / Secret 只允许存在于 LocalAgent 的 `config.agent.yaml`，永不进入 SaaS，永不写入数据库，永不通过网络传输到云端。
6. **单一 Postgres，无分库**：Redis 仅作缓存（冠军基因、会话），不承担信号传递职责。

---

## 代码目录

| 目录 | 职责 |
|------|------|
| `cmd/saas/` | SaaS 云端可执行程序入口，`main.go` 负责组装 Config / DB / Redis / WebSocket Hub / Cron / HTTP Server |
| `cmd/agent/` | LocalAgent 执行端可执行程序入口，连接 Binance 并上报 SaaS |
| `internal/saas/` | SaaS 侧所有业务逻辑：Config / Store / Auth / WebSocket Hub / Cron / GA Engine / REST API |
| `internal/agent/` | LocalAgent 侧业务逻辑：WebSocket Client / Binance 封装 / 本地配置加载 |
| `internal/strategy/` | 策略公共契约与注册表（策略无关的基础设施） |
| `internal/strategies/sigmoid-btc/` | sigmoid-btc 具体策略实现，包含 `Step()` 主函数（纯函数） |
| `internal/quant/` | 所有策略共用的数学工具（EMA/StdDev/Sigmoid 动态天平/Ghost DCA/Chromosome 定义） |
| `internal/adapters/backtest/` | 回测适配器，将历史 K 线翻译为 StrategyInput 并驱动 Step() |

---

## 验证命令

```bash
# 列出所有 Go 包
go list ./...

# 运行全部测试（含数据竞态检测）
go test ./... -race -timeout 300s

# 铁律检查（必须全部无输出）
grep -r "isBacktest" internal/strategies/       # 禁止回测分支
grep -r "api_key\|secret_key" internal/saas/    # 禁止 SaaS 侧出现 API Key
grep -r "quant\.Bar" internal/strategies/       # 策略内核禁止依赖 Bar 结构体
grep -r "http\.\|database/sql\|os\.Open\|time\.Now" internal/strategies/  # 策略包禁止 I/O
```

---

## 对 Edi 的沟通原则

- 一律使用繁体中文台湾用语。
- 语气自然，像朋友对话。
- 技术术语附简单解释（Edi 非工程师背景）。
- 重要开发动作前先输出简要计划，等 Edi 确认。
- 金额保留两位小数，BTC 数量保留 6 位小数。
