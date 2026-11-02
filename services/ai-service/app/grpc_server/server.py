"""gRPC server 启动入口。

与 FastAPI（HTTP/SSE）分开启动，各自独立端口：
    python -m app.grpc_server.server
未来拆分微服务时，直接以本入口作为 ai-service 的主进程。
"""

from __future__ import annotations

import asyncio
import logging
import signal

import grpc
from grpc_reflection.v1alpha import reflection

import app.grpc_gen  # noqa: F401  导入即配置生成代码路径
from quenching.ai.v1 import ai_service_pb2, ai_service_pb2_grpc

from app.config import get_settings
from app.grpc_server.servicer import AIServicer
from app.logging_setup import setup_logging

logger = logging.getLogger(__name__)


async def serve() -> None:
    settings = get_settings()
    setup_logging("ai-service-grpc.log")
    server = grpc.aio.server()
    ai_service_pb2_grpc.add_AIServiceServicer_to_server(AIServicer(), server)

    # 反射：便于 grpcurl 调试，生产可按 ENABLE_REFLECTION=false 关闭。
    if settings.enable_reflection:
        service_names = (
            ai_service_pb2.DESCRIPTOR.services_by_name["AIService"].full_name,
            reflection.SERVICE_NAME,
        )
        reflection.enable_server_reflection(service_names, server)

    address = f"{settings.grpc_host}:{settings.grpc_port}"
    server.add_insecure_port(address)
    await server.start()
    logger.info("ai-service gRPC listening on %s (mock=%s)", address, settings.llm_mock)

    # 优雅退出。
    stop_event = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, stop_event.set)
        except NotImplementedError:
            # Windows 上 add_signal_handler 不支持，依赖 Ctrl+C 抛 KeyboardInterrupt。
            pass

    try:
        await stop_event.wait()
    except (KeyboardInterrupt, asyncio.CancelledError):
        pass
    finally:
        await server.stop(grace=5)
        logger.info("ai-service gRPC stopped")


if __name__ == "__main__":
    asyncio.run(serve())
