"""ai-service 入口（FastAPI）。

模块规划（MVP 不做自主 agent，均为代码编排的 pipeline）：
- app/api/        路由
- app/services/   复盘引擎、助教引擎、坑点产出、协作评估
- app/adapters/     LLM 客户端（mock/重试/成本/流式）
- app/prompts/      Prompt 模板，按版本管理
- app/models/       Pydantic 模型
- app/grpc_gen/     proto 生成代码（packages/proto 生成，禁止手改）
- app/grpc_server/  gRPC 入口（9000，与 FastAPI 双进程同容器托管）
"""

from fastapi import FastAPI

from app.api.routes import router
from app.config import get_settings

settings = get_settings()

app = FastAPI(title=settings.app_name, version="0.1.0-alpha.1")
app.include_router(router)


@app.get("/health")
async def health() -> dict:
    return {"status": "ok", "service": settings.app_name, "llm_mock": settings.llm_mock}
