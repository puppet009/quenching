# api-gateway 镜像。构建上下文为仓库根目录：
#   docker build -f services/api-gateway/Dockerfile -t quenching/api-gateway:dev .
# 纯反向代理：无 configs/migrations，配置全部来自环境变量（.env / ROUTE_N）。
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY services/api-gateway/go.mod services/api-gateway/go.su[m] ./services/api-gateway/
RUN cd services/api-gateway && go mod download

COPY packages/go-common ./packages/go-common
COPY services/api-gateway ./services/api-gateway
RUN cd services/api-gateway && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/server /app/server
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
