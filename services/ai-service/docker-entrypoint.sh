#!/usr/bin/env bash
# ai-service 容器入口：同容器托管 FastAPI(HTTP/SSE,8000) 与 gRPC(9000) 两个进程。
# MVP 形态；未来拆分微服务时以 python -m app.grpc_server.server 单独部署即可。
set -euo pipefail

terminate() {
  kill "$GRPC_PID" "$HTTP_PID" 2>/dev/null || true
}
trap terminate TERM INT

python -m app.grpc_server.server &
GRPC_PID=$!

# 经 http_server 启动 uvicorn，启用访问日志并落盘（stdout + 文件轮转）。
python -m app.http_server &
HTTP_PID=$!

# 任一子进程退出即结束容器（K8s 据此重启），退出码沿用先退出者。
wait -n "$GRPC_PID" "$HTTP_PID"
EXIT_CODE=$?
terminate
exit "$EXIT_CODE"
