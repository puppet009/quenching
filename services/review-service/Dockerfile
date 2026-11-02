# review-service 镜像。构建上下文为仓库根目录：
#   docker build -f services/review-service/Dockerfile -t quenching/review-service:dev .
# 依赖 go-common + proto 两个本地包。
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY packages/proto/go.mod ./packages/proto/
COPY services/review-service/go.mod services/review-service/go.su[m] ./services/review-service/
RUN cd services/review-service && go mod download

COPY packages/go-common ./packages/go-common
COPY packages/proto ./packages/proto
COPY services/review-service ./services/review-service
RUN cd services/review-service && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY --from=builder /build/services/review-service/configs /app/configs
COPY --from=builder /build/services/review-service/migrations /app/migrations
USER app
EXPOSE 8087
ENTRYPOINT ["/app/server"]
