"""对话相关模型与常量。"""

from __future__ import annotations

from dataclasses import dataclass, field

from pydantic import BaseModel, Field

# 对话类型（对应 ai_conversations.conversation_type）。
CONV_TRAINING_ASSISTANT = "training_assistant"
CONV_REVIEW_EXPLAINER = "review_explainer"
CONV_KNOWLEDGE_QA = "knowledge_qa"
CONV_ARCHITECTURE_ADVISOR = "architecture_advisor"

VALID_CONV_TYPES = {
    CONV_TRAINING_ASSISTANT,
    CONV_REVIEW_EXPLAINER,
    CONV_KNOWLEDGE_QA,
    CONV_ARCHITECTURE_ADVISOR,
}

# 坑点类型（对应 ai_messages.pitfall_type）。MVP 训练对话不启用，T3 坑点引擎使用。
PITFALL_CAUSAL_MISJUDGMENT = "causal_misjudgment"
PITFALL_INFO_OMISSION = "info_omission"
PITFALL_OVERCONFIDENCE = "overconfidence"


class ChatRequest(BaseModel):
    session_id: int = Field(..., description="训练会话 ID")
    conversation_id: int = Field(default=0, description="AI 对话 ID")
    message: str = Field(..., min_length=1, max_length=4000)
    agent_level: str = Field(default="A1", description="A0=纯人工 A1=AI助手；A2+ 预留")
    memory_context_json: str = Field(
        default="", description="Owned untrusted session memory data"
    )
    history_json: str = Field(
        default="", description="多轮历史 JSON（已脱敏，不含根因）"
    )
    conversation_type: str = Field(default=CONV_TRAINING_ASSISTANT)
    pitfall_instruction: bool = Field(default=False, description="编排层声明的坑点意图")
    pitfall_type: str = Field(default="")
    # T3-4：脱敏故障上下文（编排层白名单 SELECT 后传入，不含根因/修复/触发配置）。
    fault_context_json: str = Field(
        default="", description="脱敏故障上下文 JSON（不含根因）"
    )
    # T3-5：渐进式提示。0=普通应答，1/2/3=方向引导/缩小范围/接近答案；
    # hint_remaining 为本次请求后剩余提示次数，-1 表示不限/未知。
    hint_level: int = Field(default=0, ge=0, le=3)
    hint_remaining: int = Field(default=-1)
    # Agent 工具调用（Function Calling）。
    tools: list["ToolDef"] = Field(default_factory=list)
    tool_results: list["ToolResult"] = Field(default_factory=list)


@dataclass
class ToolDef:
    """工具定义：LLM 可调用的白名单工具（OpenAI 风格 function schema）。"""

    name: str
    description: str
    parameters_json: str = ""
    requires_approval: bool = False


@dataclass
class ToolCall:
    """LLM 请求的一次工具调用。"""

    id: str
    name: str
    arguments_json: str


@dataclass
class ToolResult:
    """工具执行结果（由 training-service 执行后回传）。"""

    call_id: str
    content_json: str
    is_error: bool = False


@dataclass
class StepEvent:
    """步骤时间线事件：让用户看到助手正在执行哪一步。"""

    step_id: str
    state: str  # started/progress/completed/failed/awaiting_approval
    tool: str = ""
    display: str = ""
    summary: str = ""
    detail_json: str = ""


class ChatResponse(BaseModel):
    reply: str
    mocked: bool = True
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost: float = 0.0


# ---- 流式事件（assistant 编排层产出，gRPC/HTTP 各自转换为 ChatChunk / SSE） ----


@dataclass
class ChatStreamEvent:
    """流式对话内部事件。type: thinking/delta/done/error。

    注意：工厂方法统一用 mk_ 前缀。字段名 error 不能再定义同名类方法，
    否则类方法会覆盖 dataclass 字段描述符，导致实例上 ev.error 取到的是方法而非字符串。
    """

    type: str
    delta: str = ""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost: float = 0.0
    error: str = ""
    # T3-5：提示级别与剩余次数随帧回传编排层（0/-1 表示普通应答）。
    hint_level: int = 0
    hint_remaining: int = -1
    # 工具调用：LLM 选择调用工具（tool_calls 非空）。
    tool_calls: list["ToolCall"] = field(default_factory=list)
    # 步骤时间线事件（透传给前端显示执行过程）。
    step: "StepEvent | None" = None

    @classmethod
    def mk_thinking(
        cls, delta: str = "", hint_level: int = 0, hint_remaining: int = -1
    ) -> "ChatStreamEvent":
        return cls(
            type="thinking",
            delta=delta,
            hint_level=hint_level,
            hint_remaining=hint_remaining,
        )

    @classmethod
    def mk_delta(
        cls, text: str, hint_level: int = 0, hint_remaining: int = -1
    ) -> "ChatStreamEvent":
        return cls(
            type="delta",
            delta=text,
            hint_level=hint_level,
            hint_remaining=hint_remaining,
        )

    @classmethod
    def mk_done(
        cls,
        prompt_tokens: int = 0,
        completion_tokens: int = 0,
        cost: float = 0.0,
        hint_level: int = 0,
        hint_remaining: int = -1,
    ) -> "ChatStreamEvent":
        return cls(
            type="done",
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            cost=cost,
            hint_level=hint_level,
            hint_remaining=hint_remaining,
        )

    @classmethod
    def mk_error(
        cls, msg: str, hint_level: int = 0, hint_remaining: int = -1
    ) -> "ChatStreamEvent":
        return cls(
            type="error",
            error=msg,
            hint_level=hint_level,
            hint_remaining=hint_remaining,
        )

    @classmethod
    def mk_tool_calls(cls, calls: list["ToolCall"]) -> "ChatStreamEvent":
        return cls(type="tool_call", tool_calls=calls)

    @classmethod
    def mk_step(cls, step: "StepEvent") -> "ChatStreamEvent":
        return cls(type="step", step=step)
