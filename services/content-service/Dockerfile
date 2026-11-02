# content-service 镜像。
# Monorepo 注意：go.mod 用 replace 指向 ../../packages/go-common，
# 因此构建上下文必须是仓库根目录：
#   docker build -f services/content-service/Dockerfile -t quenching/content-service:dev .
# 多阶段：Go 静态编译 -> alpine 运行。
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

# 先拷依赖（go-common + 本模块），利用层缓存。
COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY packages/proto/go.mod ./packages/proto/
COPY services/content-service/go.mod services/content-service/go.su[m] ./services/content-service/
RUN cd services/content-service && go mod download

# 再拷源码编译。
COPY packages/go-common ./packages/go-common
COPY packages/proto ./packages/proto
COPY services/content-service ./services/content-service
RUN cd services/content-service && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY --from=builder /build/services/content-service/configs /app/configs
COPY --from=builder /build/services/content-service/migrations /app/migrations
# 官方预置模板（清单 + chart），运行时由 go-common/templates 文件注册表扫描。
COPY templates /templates
ENV TEMPLATES_ROOT=/templates
USER app
EXPOSE 8082
ENTRYPOINT ["/app/server"]
