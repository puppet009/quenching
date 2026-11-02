"""人机对照总结编排层单测：输入校验、mock 文案、真实分支流式聚合与错误帧。

覆盖 app/services/comparison_summary.py：
- _parse_comparison：空串/坏 JSON/顶层非对象/缺两侧数据一律 ValueError；
- mock 分支：事件序列 thinking -> delta* -> done，聚合文本数字全部取自输入；
- 真实分支：llm_client.stream 的增量/结束帧被转成对应事件；
- 真实分支流已开始后异常：只下发一次 error 帧，不向上抛。

运行：cd services/ai-service && python -m pytest tests/test_comparison_summary.py -q
"""

from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest

from app.adapters.llm_client import StreamChunk
from app.models.chat import ChatStreamEvent
from app.services import comparison_summary as cs

_SAMPLE = {
    "manual": {
        "sessionId": 1001,
        "defectName": "订单服务超时",
        "success": False,
        "totalTimeSec": 1800,
        "techScore": 55,
        "collabScore": None,
        "dangerousOps": 5,
        "verifyActions": 1,
        "aiQuestions": 0,
        "pitfallRecognized": 0,
        "pitfallTotal": 0,
    },
    "aiCollab": {
        "sessionId": 1002,
        "defectName": "订单服务超时",
        "success": True,
        "totalTimeSec": 1200,
        "techScore": 78,
        "collabScore": 82,
        "dangerousOps": 2,
        "verifyActions": 4,
        "aiQuestions": 6,
        "pitfallRecognized": 2,
        "pitfallTotal": 3,
    },
    "techRadar": {},
    "collabRadar": {},
}


def _collect(agen):
    async def run():
        return [ev async for ev in agen]

    return asyncio.run(run())


# ---- _parse_comparison -----------------------------------------------------


@pytest.mark.parametrize(
    "raw",
    [
        "",
        "   ",
        "not json",
        "[]",
        "{}",
        json.dumps({"manual": {}}),
        json.dumps({"manual": {}, "aiCollab": []}),
    ],
)
def test_parse_comparison_rejects_bad_input(raw):
    with pytest.raises(ValueError):
        cs._parse_comparison(raw)


def test_parse_comparison_accepts_pair():
    data = cs._parse_comparison(json.dumps(_SAMPLE, ensure_ascii=False))
    assert data["manual"]["sessionId"] == 1001
    assert data["aiCollab"]["success"] is True


# ---- mock 分支 -------------------------------------------------------------


def test_mock_stream_event_sequence(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=True))
    events = _collect(
        cs.comparison_summary_stream(json.dumps(_SAMPLE, ensure_ascii=False))
    )

    assert events[0].type == "thinking"
    assert events[-1].type == "done"
    assert all(ev.type in ("thinking", "delta", "done") for ev in events)

    text = "".join(ev.delta for ev in events if ev.type == "delta")
    # 四段式：空行分隔为 3 个间隔。
    assert text.count("\n\n") == 3
    # 数字全部来自输入。
    assert "55" in text and "78" in text
    assert "5" in text and "2" in text  # 危险操作
    assert "2/3" in text  # 坑点识别


def test_mock_stream_missing_scores_still_emits_four_paragraphs(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=True))
    sample = json.loads(json.dumps(_SAMPLE))
    sample["manual"]["techScore"] = None
    sample["aiCollab"]["techScore"] = None

    events = _collect(cs.comparison_summary_stream(json.dumps(sample)))
    text = "".join(ev.delta for ev in events if ev.type == "delta")
    assert text.count("\n\n") == 3
    assert "未全部评分" in text


def test_mock_stream_bad_input_raises_before_thinking(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=True))
    with pytest.raises(ValueError):
        _collect(cs.comparison_summary_stream("not-json"))


# ---- 真实分支 ---------------------------------------------------------------


def test_real_stream_maps_deltas_and_done(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=False))

    async def fake_stream(messages, temperature=0.3):
        assert [m["role"] for m in messages] == ["system", "user"]
        assert temperature == 0.3
        yield StreamChunk(delta="总体")
        yield StreamChunk(delta="结论")
        yield StreamChunk(
            final=True, prompt_tokens=120, completion_tokens=80, cost=0.01
        )

    monkeypatch.setattr(cs.llm_client, "stream", fake_stream)

    events = _collect(cs.comparison_summary_stream(json.dumps(_SAMPLE)))
    types = [ev.type for ev in events]
    assert types == ["thinking", "delta", "delta", "done"]
    assert events[1].delta == "总体"
    done = events[-1]
    assert done.prompt_tokens == 120 and done.completion_tokens == 80
    assert done.cost == pytest.approx(0.01)


def test_real_stream_error_after_start_emits_single_error_frame(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=False))

    async def fake_stream(messages, temperature=0.3):
        yield StreamChunk(delta="已有半句")
        raise RuntimeError("上游连接中断")

    monkeypatch.setattr(cs.llm_client, "stream", fake_stream)

    events = _collect(cs.comparison_summary_stream(json.dumps(_SAMPLE)))
    assert events[0].type == "thinking"
    assert events[1].type == "delta"
    assert events[-1].type == "error"
    assert "上游连接中断" in events[-1].error
    assert sum(1 for ev in events if ev.type == "error") == 1


def test_events_are_chat_stream_events(monkeypatch):
    monkeypatch.setattr(cs.llm_client, "s", SimpleNamespace(llm_mock=True))
    events = _collect(cs.comparison_summary_stream(json.dumps(_SAMPLE)))
    assert all(isinstance(ev, ChatStreamEvent) for ev in events)
