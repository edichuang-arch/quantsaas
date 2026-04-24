---
name: 部署与运维专家
description: 负责 Docker 多阶段构建、docker-compose、环境变量注入、优雅停机
scope: 部署与运行时配置
---

# 部署与运维专家 Skill

## 使命

确保 QuantSaaS 的运行环境与部署流程遵守以下原则：

1. **API Key 不进镜像**：`config.agent.yaml` 必须在 `.gitignore` 中，Agent 二进制设计为**用户本地直接运行**，不打包进 Docker 镜像。
2. **SaaS 通过环境变量注入密钥**：`ANTHROPIC_API_KEY`、JWT secret、DB 密码等从 env 读取，不硬编码。
3. **三端 app_role 切换**：`saas` / `lab` / `dev` 通过环境变量 `APP_ROLE` 控制，路由层做权限拦截。
4. **优雅停机**：收到 `SIGTERM` / `SIGINT` 时停止接受新请求、等待进行中的 tick 完成、持久化 RuntimeState、关闭连接。

## Docker 结构

```
saas.Dockerfile     多阶段：golang:1.22 builder → alpine runtime
agent.Dockerfile    同上（但 agent 二进制主要给用户本地直接运行）
docker-compose.yml  postgres:15 + redis:7-alpine + saas（APP_ROLE=dev 默认）
```

## 环境变量清单

| 变量 | 用途 | 默认 |
|------|------|------|
| `APP_ROLE` | saas / lab / dev | `dev` |
| `DB_HOST` | Postgres 主机 | `localhost` |
| `DB_USER` | Postgres 用户 | `quantsaas` |
| `DB_PASSWORD` | Postgres 密码 | **必填** |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `JWT_SECRET` | JWT 签名密钥 | **必填** |
| `ANTHROPIC_API_KEY` | Claude API（Phase 13 AI 层） | — |

## 验收检查清单

- [ ] `git status` 中 `config.agent.yaml` 未被追踪
- [ ] Docker 镜像中无 `config.agent.yaml`
- [ ] `grep -r "ANTHROPIC_API_KEY\|api_key" internal/` 无硬编码密钥
- [ ] `docker-compose up` 后 saas 能成功连接 postgres 并 AutoMigrate 通过
- [ ] `kill -TERM <pid>` 后 SaaS 能在 30 秒内优雅退出

## 参考

- `docs/系统总体拓扑结构.md` 第 6 章「优雅停机」
