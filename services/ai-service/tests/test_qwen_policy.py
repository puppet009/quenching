import asyncio
import json

import httpx
import unittest

from app.adapters.llm_client import LLMClient, LLMError
from app.adapters.qwen_policy import policy_for, validate_actual_model
from app.config import Settings


def test_task_modes():
    seen = []

    def handler(request):
        seen.append(
            (json.loads(request.content), request.extensions["timeout"]["read"])
        )
        return httpx.Response(
            200,
            json={
                "id": "req-1",
                "model": "qwen3.8-flash",
                "choices": [
                    {
                        "message": {
                            "content": "{}",
                            "reasoning_content": "secret reasoning",
                        }
                    }
                ],
                "usage": {"prompt_tokens": 2, "completion_tokens": 3},
            },
        )

    async def run():
        client = LLMClient()
        client.s = Settings(llm_mock=False)
        client._http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        for task in ("director", "assistant", "assessment", "review"):
            result = await client.complete(
                [{"role": "user", "content": "JSON"}], task=task
            )
            assert result.content == "{}"
            assert result.actual_model == "qwen3.8-flash"
            assert result.request_id == "req-1"
            assert result.latency_ms >= 0
            assert result.prompt_version
        await client.aclose()

    asyncio.run(run())
    assert [b["enable_thinking"] for b, _ in seen] == [False, False, True, True]
    assert [t for _, t in seen] == [60, 30, 90, 300]
    assert all(b["model"] == "qwen3.8-flash" and "max_tokens" not in b for b, _ in seen)
    with unittest.TestCase().assertRaises(ValueError):
        policy_for("untrusted_task")


def test_production_rejects_mock():
    with unittest.TestCase().assertRaises(ValueError):
        validate_actual_model("mock", True, True)

    async def run():
        c = LLMClient()
        c.s = Settings(app_env="production", llm_mock=True)
        with unittest.TestCase().assertRaises(LLMError):
            await c.complete([{"role": "user", "content": "hi"}])
        await c.aclose()

    asyncio.run(run())


def test_actual_model_mismatch():
    actual = "other-model"

    def handler(request):
        return httpx.Response(
            200, json={"model": actual, "choices": [{"message": {"content": "hello"}}]}
        )

    async def run():
        c = LLMClient()
        c.s = Settings(app_env="production", llm_mock=False)
        c._http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        with unittest.TestCase().assertRaises(LLMError):
            await c.complete([{"role": "user", "content": "hi"}])
        await c.aclose()

    asyncio.run(run())


def test_stream_tool_fragments():
    frames = [
        {
            "id": "req-stream",
            "model": "qwen3.8-flash",
            "choices": [
                {
                    "delta": {
                        "reasoning_content": "hidden",
                        "tool_calls": [
                            {
                                "index": 0,
                                "id": "call-a",
                                "function": {"name": "observe_", "arguments": "{"},
                            }
                        ],
                    }
                }
            ],
        },
        {
            "choices": [
                {
                    "delta": {
                        "content": "公开",
                        "tool_calls": [
                            {
                                "index": 0,
                                "function": {"name": "session", "arguments": "}"},
                            }
                        ],
                    }
                }
            ]
        },
        {"choices": [], "usage": {"prompt_tokens": 5, "completion_tokens": 8}},
    ]

    def handler(request):
        body = json.loads(request.content)
        assert body["enable_thinking"] is False
        return httpx.Response(
            200,
            text="".join("data: " + json.dumps(f) + "\n\n" for f in frames)
            + "data: [DONE]\n\n",
        )

    async def run():
        c = LLMClient()
        c.s = Settings(app_env="production", llm_mock=False)
        c._http_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        chunks = [
            x
            async for x in c.stream(
                [{"role": "user", "content": "hi"}],
                tools=[
                    {
                        "type": "function",
                        "function": {
                            "name": "observe_session",
                            "parameters": {"type": "object"},
                        },
                    }
                ],
            )
        ]
        assert "".join(x.delta for x in chunks) == "公开"
        calls = [t for x in chunks for t in x.tool_calls]
        assert calls == [
            {
                "id": "call-a",
                "type": "function",
                "function": {"name": "observe_session", "arguments": "{}"},
            }
        ]
        assert chunks[-1].actual_model == "qwen3.8-flash"
        assert chunks[-1].request_id == "req-stream"
        assert chunks[-1].prompt_tokens == 5
        await c.aclose()

    asyncio.run(run())


class QwenProtocolTests(unittest.TestCase):
    test_task_modes = staticmethod(test_task_modes)
    test_production_rejects_mock = staticmethod(test_production_rejects_mock)
    test_actual_model_mismatch = staticmethod(test_actual_model_mismatch)
    test_stream_tool_fragments = staticmethod(test_stream_tool_fragments)
