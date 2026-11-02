"""T3-4/T3-5 助教引擎单测：脱敏上下文注入、三级提示、根因泄露双保险。

运行：cd services/ai-service && python -m pytest tests/test_assistant_t34.py -q
"""

from __future__ import annotations

import asyncio
import json

import pytest

from app.adapters.llm_client import LLMError, StreamChunk
from app.models.chat import (
    ChatRequest,
    PITFALL_CAUSAL_MISJUDGMENT,
)
from app.services import assistant


def _req(**overrides) -> ChatRequest:
    base = {
        "session_id": 1,
        "conversation_id": 10,
        "message": "服务超时了，我该从哪里查起？",
    }
    base.update(overrides)
    return ChatRequest(**base)


# ---- T3-4 输入侧：故障上下文白名单注入 -------------------------------------


def test_fault_context_injected_into_system_prompt():
    ctx = {
        "session_id": 1,
        "difficulty": "hard",
        "faults": [
            {
                "name": "订单接口偶发超时",
                "category": "网络",
                "difficulty": "hard",
                "description": "高峰期 P99 延迟升高到 3s",
                "status": "triggered",
            }
        ],
    }
    req = _req(fault_context_json=json.dumps(ctx, ensure_ascii=False))
    system = assistant.build_system_content(req)
    messages = assistant.build_messages(req)

    assert messages[0]["role"] == "system"
    assert "订单接口偶发超时" in system
    assert "网络" in system
    assert "高峰期 P99 延迟升高到 3s" in system
    assert "已发生" in system
    assert "困难" in system


def test_fault_context_strips_answer_fields_even_if_upstream_leaks_them():
    """第二道输入保险：上游误带答案列时，防御性白名单必须丢弃。"""
    poisoned = {
        "difficulty": "easy",
        "faults": [
            {
                "name": "故障A",
                "description": "CPU 飙高",
                "root_cause": "数据库连接池配置过小",
                "fix_config": "把 max_connections 改成 200",
                "trigger_config": {"delay": 5},
                "manifestation_config": {},
                "amplifier_config": {},
            }
        ],
    }
    req = _req(fault_context_json=json.dumps(poisoned, ensure_ascii=False))
    system = assistant.build_system_content(req)

    assert "故障A" in system
    assert "连接池" not in system
    assert "max_connections" not in system
    assert "200" not in system
    assert "trigger_config" not in system


def test_fault_context_tolerates_bad_json():
    assert assistant._parse_fault_context("") is None
    assert assistant._parse_fault_context("not-json") is None
    assert assistant._parse_fault_context(json.dumps([1, 2])) is None
    # 非法 JSON 不阻断，系统提示退化为基础人设。
    req = _req(fault_context_json="not-json")
    system = assistant.build_system_content(req)
    assert "故障排查训练的助教" in system


def test_fault_context_requires_name_or_description():
    ctx = {"difficulty": "", "faults": [{"category": "网络", "status": "triggered"}]}
    parsed = assistant._parse_fault_context(json.dumps(ctx, ensure_ascii=False))
    assert parsed is None


def test_status_dormant_not_in_label_map():
    """dormant 故障在 Go 侧已被过滤；即使传入也不产生误导性中文标签。"""
    ctx = {"faults": [{"name": "故障B", "status": "dormant"}]}
    block = assistant._fault_context_block(
        assistant._parse_fault_context(json.dumps(ctx, ensure_ascii=False))
    )
    assert "dormant" not in block


# ---- T3-4 输出侧：根因泄露过滤器 ------------------------------------------

LEAKING_REPLIES = [
    "根因是数据库连接池配置过小，调大即可。",
    "根本原因在于下游支付服务线程池耗尽。",
    "问题出在 Redis 的 maxmemory 参数设置过低。",
    "故障发生在 kube-proxy 的 iptables 规则上。",
    "你可以执行 systemctl restart mysqld 恢复服务。",
    "运行 `kubectl rollout restart deploy/api` 就能修复。",
    "修复命令如下：\n```\nALTER SYSTEM SET ...\n```",
]


@pytest.mark.parametrize("reply", LEAKING_REPLIES)
def test_reply_leaks_detects_assertions_and_commands(reply):
    assert assistant.reply_leaks(reply) is True


SAFE_REPLIES = [
    "你可以先想想，超时通常出现在链路的哪几个环节？",
    "根因可能是什么呢？先对比一下正常时段和异常时段的指标。",
    "建议先观察 P99 延迟在哪一跳开始升高，再判断问题出在哪里。",
    "你觉得是资源不足，还是依赖变慢？为什么？",
    "",
    "先别急，说说你目前看到了哪些现象？",
]


@pytest.mark.parametrize("reply", SAFE_REPLIES)
def test_reply_leaks_allows_guiding_questions(reply):
    assert assistant.reply_leaks(reply) is False


def test_sanitize_replaces_leak_with_guiding_fallback():
    req = _req()
    out = assistant.sanitize_reply("根因是连接池太小。", req)
    assert out == assistant.LEAK_FALLBACK_REPLY
    assert assistant.reply_leaks(out) is False


def test_sanitize_keeps_safe_reply():
    req = _req()
    safe = "先看监控里哪一跳延迟开始升高，可以吗？"
    assert assistant.sanitize_reply(safe, req) == safe


def test_sanitize_skipped_for_non_training_conversation():
    req = _req(conversation_type="knowledge_qa")
    text = "根因是连接池太小。"
    # 知识问答不套训练场景约束（复盘已结束，可以直接讲结论）。
    assert assistant.sanitize_reply(text, req) == text


def test_sanitize_skipped_for_pitfall_instruction():
    req = _req(pitfall_instruction=True, pitfall_type=PITFALL_CAUSAL_MISJUDGMENT)
    text = "根因是连接池太小。"
    # 坑点专项训练按设计输出误导性结论，由编排层显式声明并在前端打标。
    assert assistant.sanitize_reply(text, req) == text


# ---- T3-5：三级提示话术 ---------------------------------------------------


@pytest.mark.parametrize("level", [1, 2, 3])
def test_hint_levels_add_distinct_instructions(level):
    system = assistant.build_system_content(_req(hint_level=level))
    assert f"第 {level} 级提示" in system


def test_hint_level_zero_adds_no_hint_instruction():
    system = assistant.build_system_content(_req(hint_level=0))
    assert "级提示" not in system


def test_level3_instruction_still_forbids_root_cause():
    system = assistant.build_system_content(_req(hint_level=3))
    assert "不直接宣布根因" in system


def test_invalid_hint_level_rejected_by_pydantic():
    with pytest.raises(ValueError):
        ChatRequest(session_id=1, message="x", hint_level=4)


# ---- 端到端：mock 流式/非流式 ---------------------------------------------


async def _collect_stream(req):
    events = []
    async for ev in assistant.chat_stream(req):
        events.append(ev)
    return events


def test_chat_stream_events_carry_hint_fields():
    import asyncio

    req = _req(hint_level=2, hint_remaining=4)
    events = asyncio.run(_collect_stream(req))
    types = [e.type for e in events]
    assert types[0] == "thinking"
    assert types[-1] == "done"
    assert "delta" in types
    for ev in events:
        assert ev.hint_level == 2
        assert ev.hint_remaining == 4


def test_chat_mock_reply_passes_filter():
    import asyncio

    req = _req()
    resp = asyncio.run(assistant.chat(req))
    assert resp.mocked is True
    assert resp.reply  # 非空
    assert assistant.reply_leaks(resp.reply) is False
    assert "[MOCK AI]" in resp.reply


def test_history_is_sanitized_to_role_content():
    history = json.dumps(
        [
            {"role": "user", "content": "怎么查？", "root_cause": "连接池"},
            {"role": "assistant", "content": "先看监控。"},
            {"role": "tool", "content": "应被丢弃"},
            "not-a-dict",
        ],
        ensure_ascii=False,
    )
    req = _req(history_json=history)
    messages = assistant.build_messages(req)
    # system + 2 条合法历史 + user
    assert len(messages) == 4
    assert all(set(m.keys()) == {"role", "content"} for m in messages[1:-1])
    assert all("root_cause" not in json.dumps(m, ensure_ascii=False) for m in messages)


# ---- T3-5：三级提示成稿确认（递进不泄露 + 越界成稿被兜底） ----------------

# 严格按 _HINT_INSTRUCTIONS 风格拟真的成稿：1 方向 -> 2 缩小范围 -> 3 点名可疑对象，
# 逐级递进，但均不宣布根因结论、不给可执行修复步骤。
HINT_REPLIES = {
    1: (
        "建议先站在整条请求链路的角度梳理一遍：把正常时段和超时高发时段的监控放在一起对比，"
        "确认耗时从哪一跳开始明显变大。你能先说说目前观察到的异常现象吗？"
    ),
    2: (
        "可以把范围收敛到数据库访问这一段：先看慢查询日志里有没有耗时突增的语句，"
        "再对比数据库连接池的等待计数和活跃连接数曲线。这两个指标你分别看到了什么？"
    ),
    3: (
        "目前最值得怀疑的对象是数据库连接池配置：若等待获取连接的耗时在高峰期同步抬升、"
        "而单条 SQL 本身并不慢，这个猜测就更有依据。"
        "你可以重点验证一下连接池等待时间与超时曲线是否吻合，好吗？"
    ),
}


@pytest.mark.parametrize("level", [1, 2, 3])
def test_hint_style_replies_survive_sanitizer(level):
    """合规的三级提示成稿必须原样送达，过滤器不得误伤引导式表达。"""
    reply = HINT_REPLIES[level]
    req = _req(hint_level=level, hint_remaining=3 - level)
    assert assistant.reply_leaks(reply) is False
    assert assistant.sanitize_reply(reply, req) == reply


def test_level3_can_name_suspect_without_asserting_root_cause():
    """第 3 级允许点名最可疑对象（递进价值），但'怀疑/验证'式表达不构成根因断言。"""
    reply = HINT_REPLIES[3]
    assert "连接池" in reply  # 比第 1 级更接近答案，递进成立
    assert "根因" not in reply
    assert assistant.reply_leaks(reply) is False


# 模型不遵守级别约束、在提示轮直接给根因/修复命令的越界成稿。
LEAKING_HINT_REPLIES = [
    "这一级直接说结论：根因是数据库连接池的最大连接数配置过小。",
    "你可以执行 ALTER SYSTEM SET max_connections=500，然后观察效果。",
    "问题就出在 kube-proxy 的 iptables 规则被写满了。",
]


@pytest.mark.parametrize("reply", LEAKING_HINT_REPLIES)
def test_leaking_hint_replies_replaced_by_fallback(reply):
    """即使在最高级提示轮，成稿一旦宣布根因/给出修复命令，仍被统一兜底替换。"""
    req = _req(hint_level=3)
    assert assistant.reply_leaks(reply) is True
    out = assistant.sanitize_reply(reply, req)
    assert out == assistant.LEAK_FALLBACK_REPLY
    assert assistant.reply_leaks(out) is False


# ---- StreamLeakGate：句界缓冲 + 逐句过滤 ---------------------------------


def _drain(gate, *chunks):
    """依次喂入增量并在最后 flush，返回整场下发的全部句子。"""
    out: list[str] = []
    for chunk in chunks:
        out.extend(gate.feed(chunk))
    out.extend(gate.flush())
    return out


def test_gate_emits_only_at_sentence_boundary_and_joins_losslessly():
    """安全文本跨 chunk 拆分：到句界才出片，残句 flush 补齐，拼接无损。"""
    gate = assistant.StreamLeakGate(_req())
    mid: list[str] = []
    mid.extend(gate.feed("先看监控"))
    assert mid == [], "未到句界不得提前下发"
    mid.extend(gate.feed("里哪一跳延迟升高。再对"))
    assert mid == ["先看监控里哪一跳延迟升高。"]
    mid.extend(gate.feed("比正常与异常时段的曲线。"))
    assert mid == [
        "先看监控里哪一跳延迟升高。",
        "再对比正常与异常时段的曲线。",
    ]
    # flush 无残句
    assert gate.flush() == []


def test_gate_safe_text_joins_losslessly():
    """多 chunk 任意切分，安全文本整场拼接必须等于原文。"""
    text = "先看监控里哪一跳延迟升高。再对比正常与异常时段的曲线，可以吗？"
    for cut in range(1, len(text)):
        gate = assistant.StreamLeakGate(_req())
        assert "".join(_drain(gate, text[:cut], text[cut:])) == text


def test_gate_leaking_sentence_replaced_once_then_dropped():
    """首个泄露句换兜底且只换一次；其后泄露句丢弃，安全句继续正常下发。"""
    gate = assistant.StreamLeakGate(_req())
    out = _drain(
        gate,
        "根因是数据库连接池配置过小。",
        "你可以执行 systemctl restart mysqld。",
        "先说说你看到了哪些现象？",
    )
    assert out == [
        assistant.LEAK_FALLBACK_REPLY,
        "先说说你看到了哪些现象？",
    ]


def test_gate_question_spanning_chunks_is_exempt():
    """疑问词与问号分散在不同 chunk，句子到界后整体按提问豁免，不得误杀。"""
    gate = assistant.StreamLeakGate(_req())
    out = _drain(gate, "你觉得根因可能出在哪", "里呢？")
    assert out == ["你觉得根因可能出在哪里呢？"]


def test_gate_flush_leaking_tail_falls_back_once():
    """流结束时无句界的残句同样过判定：泄露则兜底，再泄露则丢弃。"""
    gate = assistant.StreamLeakGate(_req())
    assert gate.flush() == []  # 空残句
    gate = assistant.StreamLeakGate(_req())
    assert gate.feed("根因是连接池太小") == []
    assert gate.flush() == [assistant.LEAK_FALLBACK_REPLY]
    # 兜底已用过，新的泄露残句直接丢弃
    assert gate.feed("运行 kubectl rollout restart") == []
    assert gate.flush() == []


def test_gate_flush_safe_tail_emitted():
    gate = assistant.StreamLeakGate(_req())
    assert gate.feed("先别急") == []
    assert gate.flush() == ["先别急"]
    assert gate.flush() == []  # flush 幂等


def test_gate_disabled_passes_through_verbatim():
    """非训练对话与坑点指令不过滤：增量原样透传，不做句界缓冲。"""
    gate = assistant.StreamLeakGate(_req(conversation_type="knowledge_qa"))
    assert gate.feed("根因是") == ["根因是"]
    assert gate.feed("连接池太小") == ["连接池太小"]
    assert gate.flush() == []

    gate = assistant.StreamLeakGate(
        _req(pitfall_instruction=True, pitfall_type=PITFALL_CAUSAL_MISJUDGMENT)
    )
    assert gate.feed("根因是连接池太小。") == ["根因是连接池太小。"]


# ---- chat_stream：真实 StreamChunk 透传与错误帧 ---------------------------


async def _fake_stream(chunks):
    for chunk in chunks:
        yield chunk


def test_chat_stream_passes_real_usage_through_done(monkeypatch):
    """真实模式结束帧的 token 用量与成本必须透传到 done 事件。"""
    chunks = [
        StreamChunk(delta="先看监控"),
        StreamChunk(delta="里哪一跳变慢。"),
        StreamChunk(delta="根因是什么呢？"),
        StreamChunk(final=True, prompt_tokens=21, completion_tokens=9, cost=0.00012),
    ]
    monkeypatch.setattr(
        assistant.llm_client,
        "stream",
        lambda messages, tools=None: _fake_stream(chunks),
    )

    events = asyncio.run(_collect_stream(_req(hint_level=2, hint_remaining=3)))
    assert events[0].type == "thinking"
    assert events[-1].type == "done"
    deltas = [e.delta for e in events if e.type == "delta"]
    # 句界缓冲：前两个 chunk 拼成一句到界才下发；提问句无句界，flush 补齐。
    assert deltas == ["先看监控里哪一跳变慢。", "根因是什么呢？"]
    done = events[-1]
    assert done.prompt_tokens == 21
    assert done.completion_tokens == 9
    assert done.cost == pytest.approx(0.00012)
    for ev in events:
        assert ev.hint_level == 2
        assert ev.hint_remaining == 3


def test_chat_stream_replaces_leak_but_keeps_safe_parts(monkeypatch):
    """集成：泄露句被兜底替换，安全句保留，整场不泄露且 done 照常结束。"""
    chunks = [
        StreamChunk(delta="问题出在 Redis 的 maxmemory 参数。"),
        StreamChunk(delta="先对比命中率曲线看看？"),
        StreamChunk(final=True),
    ]
    monkeypatch.setattr(
        assistant.llm_client,
        "stream",
        lambda messages, tools=None: _fake_stream(chunks),
    )

    events = asyncio.run(_collect_stream(_req()))
    deltas = [e.delta for e in events if e.type == "delta"]
    assert deltas == [
        assistant.LEAK_FALLBACK_REPLY,
        "先对比命中率曲线看看？",
    ]
    assert events[-1].type == "done"
    assert assistant.reply_leaks("".join(deltas)) is False


def test_chat_stream_emits_error_frame_when_stream_raises(monkeypatch):
    """首增量后流中断：错误被收敛成 error 帧，流正常收尾，不抛异常给传输层。"""

    async def broken_stream(messages, tools=None):
        yield StreamChunk(delta="先看一半")
        raise LLMError("流式响应网络中断（已开始下发，不重试）")

    monkeypatch.setattr(assistant.llm_client, "stream", broken_stream)

    events = asyncio.run(_collect_stream(_req()))
    assert events[0].type == "thinking"
    assert any(e.type == "delta" and e.delta == "先看一半" for e in events)
    assert events[-1].type == "error"
    assert "llm stream failed" in events[-1].error
    assert all(e.type != "done" for e in events)
