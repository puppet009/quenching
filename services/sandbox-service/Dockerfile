# sandbox-service 镜像。构建上下文为仓库根目录：
#   docker build -f services/sandbox-service/Dockerfile -t quenching/sandbox-service:dev .
# scripts/ 下 steady_behavior.py 随镜像分发，供沙箱稳态行为场景使用。
FROM golang:1.27-alpine AS builder
WORKDIR /build
ENV GOWORK=off CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct

COPY packages/go-common/go.mod packages/go-common/go.su[m] ./packages/go-common/
COPY packages/proto/go.mod ./packages/proto/
COPY services/sandbox-service/go.mod services/sandbox-service/go.su[m] ./services/sandbox-service/
RUN cd services/sandbox-service && go mod download

COPY packages/go-common ./packages/go-common
COPY packages/proto ./packages/proto
COPY services/sandbox-service ./services/sandbox-service
RUN cd services/sandbox-service && go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# helm CLI：sandbox-service 通过 exec helm install/uninstall 部署沙箱（internal/service/helm.go）。
# 单独下载器阶段，版本固定，构建时在服务器拉取（本地不下载依赖）。
FROM alpine:3.20 AS helm
ARG HELM_VERSION=v3.16.3
# HELM_MIRROR 可覆盖为国内源目录（结尾带 /）。注意华为云路径含版本子目录，如：
#   --build-arg HELM_MIRROR=https://mirrors.huaweicloud.com/helm/v3.16.3/
# 官方源 https://get.helm.sh/ 无版本子目录，保持默认即可。
ARG HELM_MIRROR=https://get.helm.sh/
RUN apk add --no-cache curl tar && \
    arch="$(uname -m)" && \
    case "$arch" in x86_64) harch="linux-amd64" ;; aarch64) harch="linux-arm64" ;; *) echo "unsupported arch: $arch" && exit 1 ;; esac && \
    curl -fsSL "${HELM_MIRROR}helm-${HELM_VERSION}-${harch}.tar.gz" -o /tmp/helm.tgz && \
    tar -xzf /tmp/helm.tgz -C /tmp && \
    mv "/tmp/${harch}/helm" /out-helm

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 app
WORKDIR /app
COPY --from=helm /out-helm /usr/local/bin/helm
COPY --from=builder /out/server /app/server
COPY --from=builder /build/services/sandbox-service/configs /app/configs
COPY --from=builder /build/services/sandbox-service/migrations /app/migrations
COPY --from=builder /build/services/sandbox-service/scripts /app/scripts
# 官方预置模板（template.yaml 清单 + chart），helm install 的本地 chart 由注册表解析到此目录下。
# 源在构建上下文（仓库根），不在 builder 工作目录，故不带 --from。
# .dockerignore 已放行全部模板目录，README.md 作为锚文件保证空模板期 COPY 不失败。
COPY templates /templates
ENV TEMPLATES_ROOT=/templates
USER app
EXPOSE 8089
ENTRYPOINT ["/app/server"]
