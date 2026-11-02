# user-service 镜像。构建上下文为仓库根目录：
#   docker build -f services/user-service/Dockerfile -t quenching/user-service:dev .
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY services/user-service/go.mod services/user-service/go.su[m] ./services/user-service/
RUN cd services/user-service && go mod download

COPY packages/go-common ./packages/go-common
COPY services/user-service ./services/user-service
RUN cd services/user-service && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=builder /out/server /app/server
COPY --from=builder /build/services/user-service/configs /app/configs
COPY --from=builder /build/services/user-service/migrations /app/migrations
USER app
EXPOSE 8085
ENTRYPOINT ["/app/server"]
