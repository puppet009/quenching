"""T4-16 复盘编排层单测：信封识别/回退、Prompt 组装、真实模型失败不静默降级。

覆盖 app/services/review.py：
- _parse_envelope：空串/坏 JSON/裸时间线/错 kind 一律安全回退 None；
- _build_prompts：无信封走完整八章兜底指令，有信封按信封 instruction 并
  全量展开 difficulty/scripts/metrics/timeline，instruction 缺失再回退；
- generate_review：真实模型分支 JSON 损坏/顶层非对象必须抛 RuntimeError
  （交由 Go worker 退避重排），绝不静默降级成 [MOCK] 章节。

运行：cd services/ai-service && python -m pytest tests/test_review_t416.py -q
"""

from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest

from app.adapters.llm_client import LLMResult
from app.services import review

_EIGHT_CHAPTERS = (
    "root_cause",
    "timeline",
    "path_comparison",
    "highlights_pitfalls",
    "methodology",
    "knowledge",
    "pitfall_analysis",
    "collab_review",
)


# ---- _parse_envelope -------------------------------------------------------


@pytest.mark.parametrize(
    "raw",
    [
        "",
        "   ",
        "not a json",
        "[1, 2, 3]",  # 旧路径裸时间线数组
        "{}",  # 无 kind
        json.dumps({"kind": "something_else", "instruction": "x"}),
    ],
)
def test_parse_envelope_returns_none_for_non_envelope(raw):
    assert review._parse_envelope(raw) is None


def test_parse_envelope_returns_payload_when_kind_matches():
    payload = {
        "kind": review.ENVELOPE_KIND,
        "instruction": "信封内指令",
        "timeline": [{"ts": 1, "action": "重启服务"}],
        "scripts": ["demo.sh"],
        "metrics": {"cpu": 0.9},
    }
    parsed = review._parse_envelope(json.dumps(payload, ensure_ascii=False))
    assert parsed is not None
    # 额外字段必须原样保留，供 _build_prompts 全量拼接。
    assert parsed["scripts"] == ["demo.sh"]
    assert parsed["metrics"] == {"cpu": 0.9}
    assert parsed["instruction"] == "信封内指令"


# ---- _build_prompts --------------------------------------------------------


def test_build_prompts_bare_timeline_uses_full_system_prompt():
    system, user = review._build_prompts(
        session_id=7,
        fault_type="network_timeout",
        operation_timeline_json=json.dumps(
            [{"ts": 1, "action": "看监控"}], ensure_ascii=False
        ),
        pitfall_records_json="[]",
    )
    assert system == review.REVIEW_SYSTEM_PROMPT
    assert "session=7" in user
    assert "fault_type=network_timeout" in user
    assert "timeline=" in user
    assert "pitfalls=[]" in user


def test_build_prompts_envelope_expands_all_sections():
    envelope = {
        "kind": review.ENVELOPE_KIND,
        "instruction": "method-v3 完整指令，不得精简",
        "session_id": 42,
        "fault_type": "cpu_saturation",
        "difficulty": "hard",
        "scripts": ["fault-inject.sh", "restore.sh"],
        "metrics": {"cpu_usage": "92%"},
        "timeline": [{"ts": 10, "action": "top 观察到 load 高"}],
    }
    system, user = review._build_prompts(
        session_id=42,
        fault_type="ignored_when_envelope_has_fault_type",
        operation_timeline_json=json.dumps(envelope, ensure_ascii=False),
        pitfall_records_json='[{"type":"ai_misleading"}]',
    )
    assert system == "method-v3 完整指令，不得精简"
    assert "session=42" in user
    assert "fault_type=cpu_saturation" in user
    assert "difficulty=hard" in user
    assert "fault-inject.sh" in user
    assert "cpu_usage" in user
    assert "top 观察到 load 高" in user
    assert 'pitfalls=[{"type":"ai_misleading"}]' in user


def test_build_prompts_envelope_without_instruction_falls_back():
    envelope = {"kind": review.ENVELOPE_KIND, "fault_type": "disk_full"}
    system, user = review._build_prompts(
        session_id=1,
        fault_type="disk_full",
        operation_timeline_json=json.dumps(envelope),
        pitfall_records_json="[]",
    )
    assert system == review.REVIEW_SYSTEM_PROMPT
    # 信封没有 timeline 时也要稳定输出空数组段，不能拼出 "timeline=None"。
    assert "timeline=[]" in user


def test_build_prompts_envelope_fault_type_falls_back_to_argument():
    envelope = {"kind": review.ENVELOPE_KIND, "instruction": "指令"}
    _, user = review._build_prompts(
        session_id=3,
        fault_type="mem_leak",
        operation_timeline_json=json.dumps(envelope),
        pitfall_records_json="[]",
    )
    assert "fault_type=mem_leak" in user
    # 可选段落缺失时不应出现空标签。
    assert "difficulty=" not in user
    assert "scripts=" not in user
    assert "metrics=" not in user


def test_fallback_system_prompt_remains_full_eight_chapters():
    """锁死『Prompt 不精简』：兜底指令必须仍是完整八章结构。"""
    prompt = review.REVIEW_SYSTEM_PROMPT
    assert len(prompt) > 400
    for chapter in _EIGHT_CHAPTERS:
        assert chapter in prompt, f"兜底指令缺少章节: {chapter}"
    assert "只输出 JSON" in prompt


# ---- generate_review -------------------------------------------------------


def _force_real_model(monkeypatch, captured: dict, content: str):
    """把单例 client 切到真实分支并截获 complete 调用。"""
    monkeypatch.setattr(review.llm_client, "s", SimpleNamespace(llm_mock=False))

    async def fake_complete(messages, temperature=0.3, response_schema=None):
        captured["messages"] = messages
        captured["temperature"] = temperature
        captured["response_schema"] = response_schema
        return LLMResult(content=content, model="qwen3.8-flash")

    monkeypatch.setattr(review.llm_client, "complete", fake_complete)


def test_generate_review_real_branch_returns_parsed_dict(monkeypatch):
    captured: dict = {}
    _force_real_model(
        monkeypatch,
        captured,
        json.dumps({"root_cause": "连接池打满"}, ensure_ascii=False),
    )

    result = asyncio.run(review.generate_review(9, "db_pool", "[]", "[]"))

    assert result == {"root_cause": "连接池打满"}
    # 入参契约：system/user 两条消息、低温、八章 schema。
    assert [m["role"] for m in captured["messages"]] == ["system", "user"]
    assert captured["temperature"] == 0.2
    assert set(captured["response_schema"]["required"]) == set(_EIGHT_CHAPTERS)


def test_generate_review_real_branch_broken_json_raises(monkeypatch):
    captured: dict = {}
    _force_real_model(monkeypatch, captured, '{"root_cause": "截断在半截')

    with pytest.raises(RuntimeError, match="不是合法 JSON"):
        asyncio.run(review.generate_review(9, "db_pool", "[]", "[]"))


def test_generate_review_real_branch_non_dict_json_raises(monkeypatch):
    captured: dict = {}
    _force_real_model(monkeypatch, captured, '["a", "b", "c"]')

    with pytest.raises(RuntimeError, match="顶层不是对象"):
        asyncio.run(review.generate_review(9, "db_pool", "[]", "[]"))


def test_generate_review_mock_branch_returns_mock_sections(monkeypatch):
    # 显式锁定 mock 分支，避免云端注入 LLM_MOCK=False 环境变量时误打真实网络。
    monkeypatch.setattr(review.llm_client, "s", SimpleNamespace(llm_mock=True))
    result = asyncio.run(
        review.generate_review(
            11,
            "network_timeout",
            "[]",
            '[{"type":"ai_misleading","note":"误判根因"}]',
        )
    )
    assert result["mocked"] is True
    assert result["session_id"] == 11
    assert isinstance(result["pitfall_analysis"], list)
    assert result["pitfall_analysis"][0]["type"] == "ai_misleading"
    for chapter in _EIGHT_CHAPTERS:
        assert chapter in result
