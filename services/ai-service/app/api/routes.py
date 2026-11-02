"""路由定义。"""

from fastapi import APIRouter

from app.models.chat import ChatRequest, ChatResponse
from app.services import assistant

router = APIRouter(prefix="/api/v1/ai", tags=["ai"])


@router.post("/chat", response_model=ChatResponse)
async def chat(req: ChatRequest) -> ChatResponse:
    return await assistant.chat(req)
