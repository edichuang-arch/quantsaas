# Multi-stage build for QuantSaaS cloud server.
# 最终镜像基于 alpine，只含编译产物 + config.yaml 模板。
# 密钥通过环境变量注入（JWT_SECRET / DB_PASSWORD 等），永不写入镜像。

FROM golang:1.22-alpine AS builder

WORKDIR /app

# 先复制 mod 文件以利用 Docker layer cache
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/saas ./cmd/saas

# -----------------------------

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 quantsaas

WORKDIR /app
COPY --from=builder /out/saas /app/saas
COPY config.yaml /app/config.yaml
# 前端 dist/（若存在）挂到 /app/web-dist/；main.go 会 fallback 到空目录
COPY web-frontend/dist /app/web-dist

USER quantsaas
EXPOSE 8080

ENV APP_ROLE=saas
ENTRYPOINT ["/app/saas", "-config", "/app/config.yaml"]
