---
name: Go 后端专家
description: 负责 Go 包组织、GORM 模型、并发安全、错误处理与测试
scope: Go 语言层面的实现质量
---

# Go 后端专家 Skill

## 使命

确保 QuantSaaS 后端代码遵守以下质量基线：

1. **GORM Code-First**：所有 schema 变更通过修改 Go struct 完成，`AutoMigrate` 自动同步，**永不写 SQL migration 文件**。
2. **无循环依赖**：`strategy → ga → strategy` 是常见陷阱，通过将 `EvolvableStrategy` 实现放在 `internal/saas/ga/` 而非策略包内规避。
3. **纯函数策略**：`internal/strategies/*` 包内禁止任何 `http` / `database/sql` / `os` / `time.Now()` 等 I/O 调用。
4. **并发安全**：所有全局可变状态用 `sync.Map` / `sync.Mutex` 保护；WebSocket Hub 用 channel 或原子操作。

## 典型包组织

```
cmd/saas/main.go        - 组装 DI，不含业务逻辑
cmd/agent/main.go       - 同上，Agent 版本
internal/saas/
  config/               - 配置加载
  store/                - GORM models + DB/Redis 封装
  auth/                 - JWT 签发与校验
  ws/                   - WebSocket Hub
  cron/                 - 调度器
  instance/             - 实例生命周期
  ga/                   - GA 引擎 + 策略 Evolvable 实现
  epoch/                - 进化任务服务
  api/                  - Gin 路由与 Handler
internal/agent/         - LocalAgent 侧实现
internal/strategy/      - 策略公共契约
internal/strategies/*   - 具体策略（纯函数）
internal/quant/         - 策略共用数学工具
internal/adapters/      - 回测适配器
```

## 代码审查清单

- [ ] 包名是否与目录名一致（小写、无下划线）？
- [ ] 导出符号（首字母大写）是否都有 doc comment？
- [ ] error 是否 wrap 了原因（`fmt.Errorf("failed to X: %w", err)`）？
- [ ] goroutine 是否有明确的退出机制（context / done channel）？
- [ ] 测试用 `testify/require` 还是 `testify/assert`？断言是否明确？
- [ ] 策略包是否只依赖 `internal/quant` 与标准库？

## 验证命令

```bash
go list ./...
go test ./... -race -timeout 300s
go vet ./...
grep -r "isBacktest" internal/strategies/
grep -r "http\.\|database/sql\|os\.Open" internal/strategies/
```

## 参考

- CLAUDE.md 的目录职责表
- `docs/系统总体拓扑结构.md` 的模块边界
