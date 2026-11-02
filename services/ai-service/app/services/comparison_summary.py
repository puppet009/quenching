"""人机对照 AI 总结（编排层，服务端流式）。

输入是 training-service 组装的对照数据 JSON（即 GET /training/comparison 的响应）：
manual / aiCollab 两侧指标 + techRadar / collabRadar 双雷达。

纪律（与 review.py 一致）：
- 输入 JSON 损坏、缺少两侧数据时显式失败（error 帧），禁止编造数字；
- 真实模型分支逐块转成 ChatStreamEvent；流已开始后中断只下发一次 error 帧；
- mock 模式给确定性四段式文案，供本地/联调，不依赖网络。

系统话术外置在 app/prompts/v1/comparison/system_summary.txt（_REGISTRY 已登记）。
"""

from __future__ import annotations

import json
from typing import Any, AsyncIterator

from app.adapters.llm_client import llm_client
from app.models.chat import ChatStreamEvent
from app.prompts import get_prompt

SYSTEM_PROMPT = get_prompt("comparison", "system_summary")


async def comparison_summary_stream(
    comparison_json: str,
) -> AsyncIterator[ChatStreamEvent]:
    """产出对照总结事件流：thinking -> delta* -> done；异常以 error 帧终结。

    建立流之前（输入校验失败）抛 ValueError，由 gRPC 层转 error 帧；
    已开始下发后的 LLM 异常在本函数内兜底为 error 帧，不直接抛出。
    """
    data = _parse_comparison(comparison_json)

    if llm_client.s.llm_mock:
        text = _mock_summary(data)
        yield ChatStreamEvent.mk_thinking()
        for i in range(0, len(text), 6):
            yield ChatStreamEvent.mk_delta(text[i : i + 6])
        yield ChatStreamEvent.mk_done()
        return

    yield ChatStreamEvent.mk_thinking()
    try:
        async for chunk in llm_client.stream(
            messages=[
                {"role": "system", "content": SYSTEM_PROMPT},
                {
                    "role": "user",
                    "content": "以下是本次人机对照数据，请按要求输出四段点评：\n"
                    + json.dumps(data, ensure_ascii=False),
                },
            ],
            temperature=0.3,
        ):
            if chunk.delta:
                yield ChatStreamEvent.mk_delta(chunk.delta)
            if chunk.final:
                yield ChatStreamEvent.mk_done(
                    prompt_tokens=chunk.prompt_tokens,
                    completion_tokens=chunk.completion_tokens,
                    cost=chunk.cost,
                )
    except Exception as exc:  # 流已开始：错误帧作为最后一个事件，不向上抛
        yield ChatStreamEvent.mk_error(f"对照总结生成失败: {exc}")


def _parse_comparison(comparison_json: str) -> dict[str, Any]:
    """校验输入：必须是含 manual/aiCollab 两侧数据的对象，否则显式报错。"""
    if not comparison_json or not comparison_json.strip():
        raise ValueError("对照数据为空，无法生成总结")
    try:
        data = json.loads(comparison_json)
    except json.JSONDecodeError as e:
        raise ValueError(f"对照数据不是合法 JSON，本次生成失败: {e}") from e
    if not isinstance(data, dict):
        raise ValueError("对照数据顶层不是对象，本次生成失败")
    if not isinstance(data.get("manual"), dict) or not isinstance(
        data.get("aiCollab"), dict
    ):
        raise ValueError("对照数据缺少 manual/aiCollab 两侧指标，本次生成失败")
    return data


# ---- mock 分支 -------------------------------------------------------------


def _fmt_minutes(seconds: Any) -> str:
    try:
        v = int(seconds)
    except (TypeError, ValueError):
        return "未知"
    if v <= 0:
        return "未知"
    return f"{v // 60} 分 {v % 60} 秒" if v % 60 else f"{v // 60} 分钟"


def _score(side: dict[str, Any], key: str) -> float | None:
    v = side.get(key)
    if isinstance(v, (int, float)):
        return float(v)
    return None


def _mock_summary(data: dict[str, Any]) -> str:
    """确定性四段式 mock 文案：数字全部取自输入，缺项显式说明。"""
    manual, ai = data["manual"], data["aiCollab"]

    mt, at = _score(manual, "techScore"), _score(ai, "techScore")
    if mt is not None and at is not None:
        overall = f"**技术得分 {at - mt:+.0f} 分**"
        lead = (
            f"本次对照中，AI 协作场的技术得分为 {at:.0f} 分，纯人工场为 {mt:.0f} 分，"
            f"总体变化为 {overall}。"
        )
    else:
        lead = "本次两侧技术得分未全部评分，暂不能给出总体得分结论，以下对比以过程数据为准。"

    ms_ok, ai_ok = manual.get("success"), ai.get("success")
    p2 = (
        f"效率与结果方面，纯人工场耗时 {_fmt_minutes(manual.get('totalTimeSec'))}、"
        f"修复{'成功' if ms_ok is True else '未成功' if ms_ok is False else '结果未记录'}；"
        f"AI 协作场耗时 {_fmt_minutes(ai.get('totalTimeSec'))}、"
        f"修复{'成功' if ai_ok is True else '未成功' if ai_ok is False else '结果未记录'}。"
    )
    if mt is not None and at is not None:
        p2 += f"两侧技术得分分别为 {mt:.0f} 分与 {at:.0f} 分。"
    else:
        p2 += "技术得分存在缺项，本段以耗时与修复结果为主。"

    p3 = (
        f"风险与协作方面，危险操作从纯人工场的 {manual.get('dangerousOps', 0)} 次"
        f"变为 AI 协作场的 {ai.get('dangerousOps', 0)} 次，"
        f"验证行为 {manual.get('verifyActions', 0)} 次对 {ai.get('verifyActions', 0)} 次；"
        f"坑点识别 {ai.get('pitfallRecognized', 0)}/{ai.get('pitfallTotal', 0)}，"
        f"共向 AI 提问 {ai.get('aiQuestions', 0)} 次。"
    )

    p4 = (
        "下一次训练建议：动手修复前先用监控与日志完成现象确认，每采纳一条 AI 建议都自己复核证据，"
        "修复后立即做业务验证；针对本场最弱的雷达维度做专项训练，再进入下一个故障。"
    )

    return "\n\n".join((lead, p2, p3, p4))
