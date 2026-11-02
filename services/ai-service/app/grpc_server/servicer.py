"""AIService gRPC 实现（asyncio servicer）。

逻辑全部复用 HTTP/编排层（app.services），保证 gRPC 与 FastAPI 两条入口行为一致：
- Chat           → assistant.chat（非流式）
- ChatStream     → assistant.chat_stream（服务端流式，事件转 ChatChunk）
- GenerateReview → review.generate_review
- EvaluateCollab → collab.evaluate_collab（确定性规则，不调 LLM）
- GenerateComparisonSummary → comparison_summary.comparison_summary_stream（流式）
gRPC 层只做 proto 消息 ↔ 内部模型的转换，不写业务规则。
"""

from __future__ import annotations

import json
import logging
import secrets
from app.config import get_settings
from app.adapters.llm_client import LLMError
from app.services.brain import BrainRequest, decide_brain

import grpc

import app.grpc_gen  # noqa: F401  必须先导入：把生成目录加入 sys.path
from quenching.ai.v1 import ai_service_pb2, ai_service_pb2_grpc

from app.models.chat import ChatRequest, ToolDef, ToolResult
from app.services import assistant, collab, comparison_summary, review

logger = logging.getLogger(__name__)

# proto 坑点结局 -> 内部规则枚举；UNSPECIFIED 视为未决（不计入识别率分母）。
_PITFALL_OUTCOME = {
    ai_service_pb2.PITFALL_OUTCOME_RECOGNIZED: collab.PitfallOutcome.RECOGNIZED,
    ai_service_pb2.PITFALL_OUTCOME_FELL: collab.PitfallOutcome.FELL,
    ai_service_pb2.PITFALL_OUTCOME_PENDING: collab.PitfallOutcome.PENDING,
}

# proto 操作类型 -> 内部规则枚举；UNSPECIFIED 视为自主操作（不计入采纳分母）。
_ACTION_KIND = {
    ai_service_pb2.ACTION_KIND_FOLLOW_AI: collab.ActionKind.FOLLOW_AI,
    ai_service_pb2.ACTION_KIND_INDEPENDENT: collab.ActionKind.INDEPENDENT,
}

# 内部流式事件类型 -> proto 枚举。
_CHUNK_ENUM = {
    "thinking": ai_service_pb2.CHUNK_TYPE_THINKING,
    "delta": ai_service_pb2.CHUNK_TYPE_DELTA,
    "done": ai_service_pb2.CHUNK_TYPE_DONE,
    "error": ai_service_pb2.CHUNK_TYPE_ERROR,
    "tool_call": ai_service_pb2.CHUNK_TYPE_TOOL_CALL,
    "step": ai_service_pb2.CHUNK_TYPE_STEP,
}


def _to_chat_request(request) -> ChatRequest:
    return ChatRequest(
        session_id=request.session_id,
        conversation_id=request.conversation_id,
        message=request.message,
        agent_level=request.agent_level or "A1",
        history_json=request.history_json,
        memory_context_json=request.memory_context_json,
        conversation_type=request.conversation_type or "training_assistant",
        pitfall_instruction=request.pitfall_instruction,
        pitfall_type=request.pitfall_type,
        fault_context_json=request.fault_context_json,
        hint_level=request.hint_level,
        hint_remaining=request.hint_remaining,
        tools=[
            ToolDef(
                name=t.name,
                description=t.description,
                parameters_json=t.parameters_json,
                requires_approval=t.requires_approval,
            )
            for t in request.tools
        ],
        tool_results=[
            ToolResult(
                call_id=r.call_id,
                content_json=r.content_json,
                is_error=r.is_error,
            )
            for r in request.tool_results
        ],
    )


class AIServicer(ai_service_pb2_grpc.AIServiceServicer):
    async def GenerateGrowthReviewStep(self, request, context):
        expected = get_settings().internal_token
        supplied = dict(context.invocation_metadata()).get("x-internal-token", "")
        if not expected or not secrets.compare_digest(expected, supplied):
            await context.abort(
                grpc.StatusCode.UNAUTHENTICATED, "internal token required"
            )
            return
        try:
            from app.services.growth_review import generate_growth_review_step

            if len(request.input_json.encode()) > 81920:
                raise ValueError("input exceeds budget")
            data = json.loads(request.input_json)
            if (
                data.get("session_id") != request.session_id
                or data.get("mode") != request.mode
                or data.get("snapshot_hash") != request.snapshot_hash
            ):
                raise ValueError("input identity mismatch")
            result = await generate_growth_review_step(data)
            return ai_service_pb2.GrowthReviewStepResponse(**result)
        except (ValueError, TypeError, KeyError):
            await context.abort(
                grpc.StatusCode.INVALID_ARGUMENT, "invalid growth review contract"
            )
        except LLMError:
            await context.abort(
                grpc.StatusCode.UNAVAILABLE, "growth inference unavailable"
            )

    async def SummarizeSessionMemory(self, request, context):
        from app.services.session_memory import summarize_session_memory

        expected = get_settings().internal_token
        supplied = dict(context.invocation_metadata()).get("x-internal-token", "")
        if not expected or not secrets.compare_digest(expected, supplied):
            await context.abort(
                grpc.StatusCode.UNAUTHENTICATED, "internal authorization required"
            )
            return
        try:
            public = json.loads(request.public_input_json)
            if (
                not isinstance(public, dict)
                or public.get("session_id") != request.session_id
            ):
                raise ValueError("memory session mismatch")
            result = await summarize_session_memory(public)
            return ai_service_pb2.SessionMemoryResponse(**result)
        except ValueError:
            await context.abort(
                grpc.StatusCode.INVALID_ARGUMENT, "invalid memory input or summary"
            )
        except LLMError:
            await context.abort(
                grpc.StatusCode.UNAVAILABLE, "memory inference unavailable"
            )

    async def AssessAbility(self, request, context):
        from app.services.ability import AbilityRequest, assess_ability

        expected = get_settings().internal_token
        supplied = dict(context.invocation_metadata()).get("x-internal-token", "")
        if not expected or not secrets.compare_digest(expected, supplied):
            await context.abort(
                grpc.StatusCode.UNAUTHENTICATED, "internal authorization required"
            )
            return
        try:
            req = AbilityRequest(
                session_id=request.session_id,
                version=request.version,
                evidence_json=request.evidence_json,
                evidence_refs=list(request.evidence_refs),
            )
            result = await assess_ability(req)
            return ai_service_pb2.AssessAbilityResponse(**result.model_dump())
        except ValueError:
            await context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "invalid ability evidence or assessment",
            )
        except LLMError:
            await context.abort(
                grpc.StatusCode.UNAVAILABLE, "ability inference unavailable"
            )

    async def BrainDecide(self, request, context):
        expected = get_settings().internal_token
        supplied = dict(context.invocation_metadata()).get("x-internal-token", "")
        if not expected or not secrets.compare_digest(expected, supplied):
            await context.abort(
                grpc.StatusCode.UNAUTHENTICATED, "internal authorization required"
            )
            return
        try:
            req = BrainRequest(
                session_id=request.session_id,
                phase=request.phase,
                observation_version=request.observation_version,
                context_json=request.context_json or "{}",
                evidence_refs=list(request.evidence_refs),
                allowed_tools=[
                    dict(
                        name=t.name,
                        description=t.description,
                        parameters_json=t.parameters_json,
                        requires_approval=t.requires_approval,
                    )
                    for t in request.allowed_tools
                ],
                previous_calls=[
                    dict(id=t.id, name=t.name, arguments_json=t.arguments_json)
                    for t in request.previous_calls
                ],
                tool_results=[
                    dict(
                        call_id=t.call_id,
                        content_json=t.content_json,
                        is_error=t.is_error,
                    )
                    for t in request.tool_results
                ],
            )
            result = await decide_brain(req)
            return ai_service_pb2.BrainDecideResponse(**result.model_dump())
        except ValueError:
            await context.abort(
                grpc.StatusCode.INVALID_ARGUMENT,
                "invalid director observation or decision",
            )
        except LLMError:
            await context.abort(
                grpc.StatusCode.UNAVAILABLE, "director inference unavailable"
            )

    async def GenerateReview(self, request, context):
        logger.info(
            "gRPC GenerateReview session=%s fault=%s",
            request.session_id,
            request.fault_type,
        )
        try:
            sections = await review.generate_review(
                session_id=request.session_id,
                fault_type=request.fault_type,
                operation_timeline_json=request.operation_timeline_json,
                pitfall_records_json=request.pitfall_records_json,
            )
        except Exception as exc:  # 生成失败以 gRPC 错误返回，便于 Go 端按 code 重试
            logger.exception("GenerateReview failed")
            await context.abort(
                grpc.StatusCode.INTERNAL, f"generate review failed: {exc}"
            )

        # review_id 由 review-service 持久化时生成，AI 服务不持有，返回 0 由上层回填。
        return ai_service_pb2.GenerateReviewResponse(
            review_id=0,
            sections_json=json.dumps(sections, ensure_ascii=False),
        )

    async def EvaluateCollab(self, request, context):
        logger.info(
            "gRPC EvaluateCollab session=%s questions=%d pitfalls=%d actions=%d",
            request.session_id,
            len(request.questions),
            len(request.pitfalls),
            len(request.actions),
        )
        # 纯确定性计算、无 IO；异常仍以 INTERNAL 返回，training 端按严格模式标记 unavailable。
        try:
            result = collab.evaluate_collab(
                questions=[
                    collab.Question(
                        text=q.text,
                        stage=q.stage,
                        references_known_info=q.references_known_info,
                        follow_up=q.follow_up,
                    )
                    for q in request.questions
                ],
                pitfalls=[
                    collab.Pitfall(
                        _PITFALL_OUTCOME.get(p.outcome, collab.PitfallOutcome.PENDING)
                    )
                    for p in request.pitfalls
                ],
                actions=[
                    collab.CollabAction(
                        kind=_ACTION_KIND.get(a.kind, collab.ActionKind.INDEPENDENT),
                        verified_before_follow=a.verified_before_follow,
                    )
                    for a in request.actions
                ],
            )
        except Exception as exc:  # 规则异常以 gRPC 错误返回，便于调用方统一降级
            logger.exception("EvaluateCollab failed")
            await context.abort(
                grpc.StatusCode.INTERNAL, f"evaluate collab failed: {exc}"
            )

        return ai_service_pb2.EvaluateCollabResponse(
            available=result.available,
            question_quality_score=result.question_quality,
            info_utilization_score=result.info_utilization,
            judgment_score=result.judgment,
            error_recognition_rate=result.error_recognition_rate,
            evidence_validation_rate=result.evidence_validation_rate,
            pitfall_total=result.pitfall_total,
            pitfall_recognized=result.pitfall_recognized,
            follow_total=result.follow_total,
            follow_verified=result.follow_verified,
        )

    async def Chat(self, request, context):
        logger.info(
            "gRPC Chat session=%s level=%s", request.session_id, request.agent_level
        )
        try:
            resp = await assistant.chat(_to_chat_request(request))
        except Exception as exc:
            logger.exception("Chat failed")
            await context.abort(grpc.StatusCode.INTERNAL, f"chat failed: {exc}")

        # 坑点由 training-service 编排层决定并在消息历史上标记，AI 服务不自我宣称。
        return ai_service_pb2.ChatResponse(reply=resp.reply, is_pitfall=False)

    async def ChatStream(self, request, context):
        logger.info(
            "gRPC ChatStream session=%s level=%s",
            request.session_id,
            request.agent_level,
        )
        req = _to_chat_request(request)
        # 坑点标记在整条回复上保持一致，随每个 chunk 回传，前端可即时打标。
        is_pitfall = request.pitfall_instruction
        try:
            async for ev in assistant.chat_stream(req):
                chunk_kwargs = dict(
                    type=_CHUNK_ENUM.get(
                        ev.type, ai_service_pb2.CHUNK_TYPE_UNSPECIFIED
                    ),
                    delta=ev.delta,
                    is_pitfall=is_pitfall,
                    prompt_tokens=ev.prompt_tokens,
                    completion_tokens=ev.completion_tokens,
                    cost=ev.cost,
                    error=ev.error,
                    hint_level=ev.hint_level,
                    hint_remaining=ev.hint_remaining,
                )
                # 工具调用：转 proto ToolCall 列表。
                if ev.tool_calls:
                    chunk_kwargs["tool_calls"] = [
                        ai_service_pb2.ToolCall(
                            id=tc.id, name=tc.name, arguments_json=tc.arguments_json
                        )
                        for tc in ev.tool_calls
                    ]
                # 步骤时间线：转 proto StepEvent。
                if ev.step is not None:
                    st = ev.step
                    chunk_kwargs["step"] = ai_service_pb2.StepEvent(
                        step_id=st.step_id,
                        state=st.state,
                        tool=st.tool,
                        display=st.display,
                        summary=st.summary,
                        detail_json=st.detail_json,
                    )
                yield ai_service_pb2.ChatChunk(**chunk_kwargs)
        except Exception as exc:  # 兜底：流建立后的异常以错误帧下发，不直接中断
            logger.exception("ChatStream failed")
            yield ai_service_pb2.ChatChunk(
                type=ai_service_pb2.CHUNK_TYPE_ERROR, error=f"chat stream failed: {exc}"
            )

    async def GenerateComparisonSummary(self, request, context):
        logger.info(
            "gRPC GenerateComparisonSummary json_len=%d", len(request.comparison_json)
        )
        try:
            async for ev in comparison_summary.comparison_summary_stream(
                request.comparison_json
            ):
                yield ai_service_pb2.ComparisonSummaryChunk(
                    type=_CHUNK_ENUM.get(
                        ev.type, ai_service_pb2.CHUNK_TYPE_UNSPECIFIED
                    ),
                    delta=ev.delta,
                    prompt_tokens=ev.prompt_tokens,
                    completion_tokens=ev.completion_tokens,
                    cost=ev.cost,
                    error=ev.error,
                )
        except ValueError as exc:
            # 建立流之前的输入校验失败：错误帧作为唯一事件下发，由前端降级规则文案。
            logger.warning("GenerateComparisonSummary invalid input: %s", exc)
            yield ai_service_pb2.ComparisonSummaryChunk(
                type=ai_service_pb2.CHUNK_TYPE_ERROR, error=str(exc)
            )
        except Exception as exc:  # 兜底：未预期异常同样以错误帧终结
            logger.exception("GenerateComparisonSummary failed")
            yield ai_service_pb2.ComparisonSummaryChunk(
                type=ai_service_pb2.CHUNK_TYPE_ERROR,
                error=f"comparison summary failed: {exc}",
            )
