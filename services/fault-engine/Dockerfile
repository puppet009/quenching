# fault-engine 镜像。构建上下文为仓库根目录：
#   docker build -f services/fault-engine/Dockerfile -t quenching/fault-engine:dev .
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY packages/proto/go.mod ./packages/proto/
COPY services/fault-engine/go.mod services/fault-engine/go.su[m] ./services/fault-engine/
RUN cd services/fault-engine && go mod download

COPY packages/go-common ./packages/go-common
COPY packages/proto ./packages/proto
COPY services/fault-engine ./services/fault-engine
RUN cd services/fault-engine && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY --from=builder /build/services/fault-engine/configs /app/configs
COPY --from=builder /build/services/fault-engine/migrations /app/migrations
USER app
EXPOSE 8088
ENTRYPOINT ["/app/server"]
