# ai-service 镜像（Python）。构建上下文为本服务目录：
#   docker build -f services/ai-service/Dockerfile -t quenching/ai-service:dev services/ai-service
# 容器内同时运行 FastAPI(HTTP/SSE,8000) 与 gRPC(9000) 两个进程，由 entrypoint 托管。
FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends tzdata && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -g 10001 app && useradd -u 10001 -g app app

COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY app ./app
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

EXPOSE 8000 9000
USER app
ENTRYPOINT ["docker-entrypoint.sh"]
