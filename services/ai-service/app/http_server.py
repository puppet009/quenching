"""ai-service HTTP(uvicorn) 进程入口。

以编程方式启动 uvicorn，先应用统一日志配置（stdout + 文件轮转、访问日志），
再拉起 FastAPI。与 gRPC 进程分开，日志写入独立文件。

用法：python -m app.http_server
"""

from __future__ import annotations

import uvicorn

from app.config import get_settings
from app.logging_setup import setup_logging


def main() -> None:
    settings = get_settings()
    setup_logging("ai-service-http.log")
    # log_config=None：阻止 uvicorn 覆盖我们上面配置好的 root logger；
    # access_log=True：保留每个 HTTP 请求的访问日志。
    config = uvicorn.Config(
        "app.main:app",
        host="0.0.0.0",
        port=settings.port,
        log_level=settings.log_level,
        access_log=True,
        log_config=None,
    )
    uvicorn.Server(config).run()


if __name__ == "__main__":
    main()
