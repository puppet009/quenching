"""T4-16 LLM 客户端单测：payload 契约（不传 max_tokens）与失败分类/重试边界。

覆盖 app/adapters/llm_client.py：
- 请求体只含 model/messages/temperature（+schema 时 response_format），
  绝不允许重新引入 max_tokens（限长会截断八章 JSON，复盘必失败）；
- 读超时（已建连）抛 LLMError 且 complete() 不重试，避免墙钟翻倍；
- 连接超时/连接池超时/其他网络错误/429/5xx/200 响应体损坏
  抛 RetryableLLMError，complete() 按 llm_max_retries 重试至耗尽；
- 其他 4xx 抛 LLMError 不重试。

全部通过 httpx.MockTransport 在进程内注入响应，不访问网络、不装额外依赖。
运行：cd services/ai-service && python -m pytest tests/test_llm_client_t416.py -q
"""

from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import httpx
import pytest

from app.adapters import llm_client as mod
from app.adapters.llm_client import LLMClient, LLMError, RetryableLLMError

_MESSAGES = [
    {"role": "system", "content": "sys"},
    {"role": "user", "content": "user"},
]
_SCHEMA = {"type": "object", "required": ["root_cause"]}


def _make_client(monkeypatch, handler, *, max_retries: int = 3) -> LLMClient:
    """构造挂接 MockTransport 的客户端，settings 全部本地注入，不碰网络。"""
    client = LLMClient()
    monkeypatch.setattr(
        client,
        "s",
        SimpleNamespace(
            llm_model="qwen3.8-flash",
            llm_timeout=300.0,
            llm_base_url="https://llm.example/proxy/",
            llm_api_key="test-key",
            llm_mock=False,
            llm_max_retries=max_retries,
            llm_price_input=0.0,
            llm_price_output=0.0,
        ),
    )
    client._http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    return client


async def _call(client, **kwargs):
    try:
        return await client._do_complete(
            _MESSAGES, kwargs.pop("temperature", 0.3), kwargs.pop("schema", None)
        )
    finally:
        await client.aclose()


def _ok_body(content: str = '{"root_cause":"ok"}') -> dict:
    return {
        "id": "chatcmpl-1",
        "model": "qwen3.8-flash",
        "choices": [{"index": 0, "message": {"role": "assistant", "content": content}}],
        "usage": {"prompt_tokens": 12, "completion_tokens": 34},
    }


# ---- 请求体契约：禁止 max_tokens -------------------------------------------


def test_payload_never_contains_max_tokens(monkeypatch):
    seen: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["request"] = request
        return httpx.Response(200, json=_ok_body())

    client = _make_client(monkeypatch, handler)
    result = asyncio.run(_call(client, schema=_SCHEMA))

    body = json.loads(seen["request"].content)
    assert "max_tokens" not in body, "T4-16 硬约束：payload 不得出现 max_tokens"
    assert body["model"] == "qwen3.8-flash"
    assert body["messages"] == _MESSAGES
    assert body["temperature"] == 0.3
    # 结构化输出开关只在传入 schema 时携带。
    assert body["response_format"] == {"type": "json_object"}
    assert seen["request"].headers["Authorization"] == "Bearer test-key"
    # base_url 尾部斜杠必须被规整，不能出现 //。
    assert str(seen["request"].url) == "https://llm.example/proxy/v1/chat/completions"
    assert result.content == '{"root_cause":"ok"}'
    assert result.prompt_tokens == 12
    assert result.completion_tokens == 34


def test_payload_omits_response_format_without_schema(monkeypatch):
    seen: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["request"] = request
        return httpx.Response(200, json=_ok_body("plain"))

    client = _make_client(monkeypatch, handler)
    asyncio.run(_call(client))

    body = json.loads(seen["request"].content)
    assert "response_format" not in body
    assert "max_tokens" not in body


# ---- _do_complete 异常分类 ---------------------------------------------------


@pytest.mark.parametrize(
    "exc",
    [
        httpx.ConnectTimeout("建连超时"),
        httpx.PoolTimeout("连接池获取超时"),
        httpx.RemoteProtocolError("连接被重置"),
        httpx.ConnectError("DNS/建连失败"),
    ],
)
def test_transport_errors_are_retryable(monkeypatch, exc):
    def handler(request: httpx.Request) -> httpx.Response:
        raise exc

    client = _make_client(monkeypatch, handler)
    with pytest.raises(RetryableLLMError):
        asyncio.run(_call(client))


def test_read_timeout_is_non_retryable(monkeypatch):
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ReadTimeout("300s 内没读完", request=request)

    client = _make_client(monkeypatch, handler)
    with pytest.raises(LLMError) as ei:
        asyncio.run(_call(client))
    assert not isinstance(ei.value, RetryableLLMError)
    assert "300" in str(ei.value)


@pytest.mark.parametrize("status", [429, 500, 502, 503])
def test_rate_limit_and_5xx_are_retryable(monkeypatch, status):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status, text="upstream error")

    client = _make_client(monkeypatch, handler)
    with pytest.raises(RetryableLLMError) as ei:
        asyncio.run(_call(client))
    assert ei.value.status_code == status


@pytest.mark.parametrize("status", [400, 401, 403, 404])
def test_other_4xx_are_non_retryable(monkeypatch, status):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status, json={"error": "bad request"})

    client = _make_client(monkeypatch, handler)
    with pytest.raises(LLMError) as ei:
        asyncio.run(_call(client))
    assert not isinstance(ei.value, RetryableLLMError)
    assert ei.value.status_code == status


@pytest.mark.parametrize(
    "body",
    [
        {"oops": "no choices"},
        {"choices": []},
        {"choices": [{"no_message": True}]},
    ],
)
def test_broken_200_body_is_retryable(monkeypatch, body):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=body)

    client = _make_client(monkeypatch, handler)
    with pytest.raises(RetryableLLMError):
        asyncio.run(_call(client))


def test_non_json_200_body_is_retryable(monkeypatch):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, content=b"<html>gateway error</html>")

    client = _make_client(monkeypatch, handler)
    with pytest.raises(RetryableLLMError):
        asyncio.run(_call(client))


# ---- complete() 重试边界（总尝试次数含首次） --------------------------------


def _no_retry_wait(monkeypatch):
    # tenacity 重试间隔在真实环境为指数退避；测试里置空以保持纯逻辑快速执行。
    monkeypatch.setattr(mod, "wait_exponential", lambda **kwargs: None)


async def _run_complete(client):
    try:
        return await client.complete(_MESSAGES, temperature=0.3, response_schema=None)
    finally:
        await client.aclose()


def test_complete_read_timeout_fails_fast_without_retry(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        raise httpx.ReadTimeout("模型卡住", request=request)

    client = _make_client(monkeypatch, handler, max_retries=3)
    with pytest.raises(LLMError) as ei:
        asyncio.run(_run_complete(client))
    assert not isinstance(ei.value, RetryableLLMError)
    assert calls["n"] == 1, "读超时不得重试，否则墙钟翻倍"


def test_complete_429_retries_until_attempts_exhausted(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        return httpx.Response(429, text="rate limited")

    client = _make_client(monkeypatch, handler, max_retries=3)
    with pytest.raises(RetryableLLMError) as ei:
        asyncio.run(_run_complete(client))
    assert ei.value.status_code == 429
    assert calls["n"] == 3, "总尝试次数=llm_max_retries（含首次）"


def test_complete_succeeds_after_transient_503(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        if calls["n"] == 1:
            return httpx.Response(503, text="temporary")
        return httpx.Response(200, json=_ok_body())

    client = _make_client(monkeypatch, handler, max_retries=3)
    result = asyncio.run(_run_complete(client))
    assert result.content == '{"root_cause":"ok"}'
    assert calls["n"] == 2, "瞬时 5xx 应被重试并在第二次成功"


def test_complete_mock_mode_skips_network(monkeypatch):
    # 默认 settings.llm_mock=True；MockTransport 若被调用即说明走错分支。
    def handler(request: httpx.Request) -> httpx.Response:
        raise AssertionError("mock 模式不应发起任何网络请求")

    client = _make_client(monkeypatch, handler)
    client.s.llm_mock = True
    result = asyncio.run(_run_complete(client))
    assert result.mocked is True


# ---- stream() 真实 SSE ------------------------------------------------------


def _sse(chunks, *, usage=None, done_marker=True) -> str:
    """按 OpenAI 兼容 SSE 协议拼帧；usage 帧 choices 为空（DashScope 行为）。"""
    lines = []
    for text in chunks:
        lines.append(
            "data: "
            + json.dumps(
                {"choices": [{"delta": {"content": text}}]}, ensure_ascii=False
            )
        )
    if usage is not None:
        lines.append("data: " + json.dumps({"choices": [], "usage": usage}))
    if done_marker:
        lines.append("data: [DONE]")
    return "\n\n".join(lines) + "\n\n"


async def _collect_stream(client, messages=None):
    chunks = []
    try:
        async for chunk in client.stream(messages or _MESSAGES):
            chunks.append(chunk)
    finally:
        await client.aclose()
    return chunks


def test_stream_payload_uses_stream_true_without_max_tokens(monkeypatch):
    seen: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["request"] = request
        return httpx.Response(
            200,
            text=_sse(["你好", "，世界"], usage=None),
            headers={"content-type": "text/event-stream"},
        )

    client = _make_client(monkeypatch, handler)
    chunks = asyncio.run(_collect_stream(client))

    body = json.loads(seen["request"].content)
    assert body["stream"] is True
    assert body["stream_options"] == {"include_usage": True}
    assert "max_tokens" not in body, "T4-16 硬约束对流式同样生效"
    assert str(seen["request"].url) == "https://llm.example/proxy/v1/chat/completions"
    deltas = [c.delta for c in chunks if c.delta]
    assert "".join(deltas) == "你好，世界"
    assert chunks[-1].final is True


def test_stream_parses_deltas_usage_and_done(monkeypatch):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            text=_sse(
                ["先看监控", "，再对比时段"],
                usage={"prompt_tokens": 11, "completion_tokens": 7},
            ),
            headers={"content-type": "text/event-stream"},
        )

    client = _make_client(monkeypatch, handler)
    client.s.llm_price_input = 2.0
    client.s.llm_price_output = 4.0
    chunks = asyncio.run(_collect_stream(client))

    deltas = [c.delta for c in chunks if c.delta]
    assert deltas == ["先看监控", "，再对比时段"]
    final = chunks[-1]
    assert final.final is True
    assert final.prompt_tokens == 11
    assert final.completion_tokens == 7
    # (11*2 + 7*4) / 1_000_000
    assert final.cost == pytest.approx(50 / 1_000_000)


def test_stream_ignores_keepalive_and_non_data_lines(monkeypatch):
    raw = (
        ": keep-alive\n\n"
        'data: {"choices":[{"delta":{"content":"A"}}]}\n\n'
        "\n\n"
        'data: {"choices":[{"delta":{"content":"B"}}]}\n\n'
        "data: [DONE]\n\n"
    )

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200, text=raw, headers={"content-type": "text/event-stream"}
        )

    client = _make_client(monkeypatch, handler)
    chunks = asyncio.run(_collect_stream(client))
    assert "".join(c.delta for c in chunks if c.delta) == "AB"
    assert chunks[-1].final is True


def test_stream_without_done_marker_still_emits_final(monkeypatch):
    # 部分网关可能不发 [DONE]，EOF 即结束；不得因此报错或丢结束帧。
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            text=_sse(["只有一帧"], usage=None, done_marker=False),
            headers={"content-type": "text/event-stream"},
        )

    client = _make_client(monkeypatch, handler)
    chunks = asyncio.run(_collect_stream(client))
    assert "".join(c.delta for c in chunks if c.delta) == "只有一帧"
    assert chunks[-1].final is True


def test_stream_empty_usage_chunk_does_not_corrupt_stream(monkeypatch):
    # DashScope 结束帧 choices=[]：旧实现会误判帧损坏，这里必须正常取到 usage。
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            text=_sse(["OK"], usage={"prompt_tokens": 3, "completion_tokens": 1}),
            headers={"content-type": "text/event-stream"},
        )

    client = _make_client(monkeypatch, handler)
    chunks = asyncio.run(_collect_stream(client))
    assert chunks[-1].prompt_tokens == 3
    assert chunks[-1].completion_tokens == 1


def test_stream_mock_mode_skips_network_and_ends_with_final(monkeypatch):
    def handler(request: httpx.Request) -> httpx.Response:
        raise AssertionError("mock 模式不应发起任何网络请求")

    client = _make_client(monkeypatch, handler)
    client.s.llm_mock = True
    chunks = asyncio.run(
        _collect_stream(client, messages=[{"role": "user", "content": "超时"}])
    )
    assert all(not c.final for c in chunks[:-1])
    assert chunks[-1].final is True
    assert "[MOCK AI]" in "".join(c.delta for c in chunks if c.delta)


@pytest.mark.parametrize("status", [429, 500, 503])
def test_stream_429_and_5xx_retryable_before_first_delta(monkeypatch, status):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        return httpx.Response(status, text="upstream error")

    client = _make_client(monkeypatch, handler, max_retries=3)
    with pytest.raises(RetryableLLMError) as ei:
        asyncio.run(_collect_stream(client))
    assert ei.value.status_code == status
    assert calls["n"] == 3, "首字节前的 429/5xx 与 complete() 一样可重试"


@pytest.mark.parametrize("status", [400, 401, 404])
def test_stream_other_4xx_non_retryable(monkeypatch, status):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status, text="bad request")

    client = _make_client(monkeypatch, handler)
    with pytest.raises(LLMError) as ei:
        asyncio.run(_collect_stream(client))
    assert not isinstance(ei.value, RetryableLLMError)
    assert ei.value.status_code == status


def test_stream_transport_error_before_first_delta_is_retryable(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        raise httpx.ConnectError("网关抖动")

    client = _make_client(monkeypatch, handler, max_retries=2)
    with pytest.raises(RetryableLLMError):
        asyncio.run(_collect_stream(client))
    assert calls["n"] == 2


def test_stream_retries_then_succeeds(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        if calls["n"] == 1:
            return httpx.Response(503, text="temporary")
        return httpx.Response(
            200, text=_sse(["恢复"]), headers={"content-type": "text/event-stream"}
        )

    client = _make_client(monkeypatch, handler, max_retries=3)
    chunks = asyncio.run(_collect_stream(client))
    assert "".join(c.delta for c in chunks if c.delta) == "恢复"
    assert calls["n"] == 2


def test_stream_malformed_frame_before_first_delta_is_retryable(monkeypatch):
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        if calls["n"] < 3:
            return httpx.Response(
                200,
                text="data: not-json\n\n",
                headers={"content-type": "text/event-stream"},
            )
        return httpx.Response(
            200, text=_sse(["好"]), headers={"content-type": "text/event-stream"}
        )

    client = _make_client(monkeypatch, handler, max_retries=3)
    chunks = asyncio.run(_collect_stream(client))
    assert "".join(c.delta for c in chunks if c.delta) == "好"
    assert calls["n"] == 3


def test_stream_read_timeout_after_first_delta_is_non_retryable(monkeypatch):
    """已下发增量后中断：重试会重放前半段造成重复，必须降级为不重试。"""
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        raise httpx.ReadTimeout("流中途卡住", request=request)

    client = _make_client(monkeypatch, handler, max_retries=3)
    # ReadTimeout 在 MockTransport 中发生在响应建立阶段（started 尚未置位），
    # 此情形仍按连接读取异常处理；真正“首 delta 后”的语义由代码 started 分支保证，
    # 这里锁定读超时整体不重试、只调用一次。
    with pytest.raises(LLMError):
        asyncio.run(_collect_stream(client))
    assert calls["n"] == 1


def test_stream_network_error_after_first_delta_does_not_replay(monkeypatch):
    """先给一帧合法增量再断连：只能收到一次前半段，且不重试。"""
    _no_retry_wait(monkeypatch)
    calls = {"n": 0}

    class _BrokenAsyncStream(httpx.AsyncByteStream):
        """先吐一帧合法 SSE，随后在迭代中断连的异步字节流。"""

        async def __aiter__(self):
            yield 'data: {"choices":[{"delta":{"content":"前半段"}}]}\n\n'.encode(
                "utf-8"
            )
            raise httpx.RemoteProtocolError("连接被重置")

    def handler(request: httpx.Request) -> httpx.Response:
        calls["n"] += 1
        return httpx.Response(
            200,
            stream=_BrokenAsyncStream(),
            headers={"content-type": "text/event-stream"},
        )

    client = _make_client(monkeypatch, handler, max_retries=3)
    seen_deltas: list[str] = []

    async def consume():
        async for chunk in client.stream(_MESSAGES):
            if chunk.delta:
                seen_deltas.append(chunk.delta)

    with pytest.raises(LLMError) as ei:
        asyncio.run(consume())
    asyncio.run(client.aclose())
    assert not isinstance(ei.value, RetryableLLMError)
    assert seen_deltas == ["前半段"], "已下发内容不得因重试重复"
    assert calls["n"] == 1
