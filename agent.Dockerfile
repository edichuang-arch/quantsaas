# LocalAgent 二进制的 Docker 镜像（可选；更推荐用户直接在本机运行）。
# 若要用容器运行，必须将宿主机的 config.agent.yaml 挂入 /app/config.agent.yaml。

FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/agent ./cmd/agent

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10002 agent

WORKDIR /app
COPY --from=builder /out/agent /app/agent

USER agent
ENTRYPOINT ["/app/agent", "-config", "/app/config.agent.yaml"]
