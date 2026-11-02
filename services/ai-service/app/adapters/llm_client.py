"""LLM 客户端基座。

导演可选择白名单工具，由训练域校验和执行；此层只负责推理协议。
导演、助教、评估、复盘使用同一 Qwen，任务上下文由调用方隔离。
显式 mock 仅限开发测试；生产验证实际响应模型，不以配置冒充调用。

T4-16 接入约定（qwen3.8-flash，OpenAI 兼容协议）：
- **不设置 max_tokens**：复盘六章一次返回，限制输出长度容易把 JSON 截断在半截，
  反而反复失败；输出长度完全交给模型。
- **Prompt 与输入全量保留，不做精简/截断**（效果优先）。
- 单次调用读超时 300s，且**超时不重试**：重试只会让墙钟翻倍到 600s 以上；
  仅连接失败/连接池获取超时/429/5xx/200 响应体损坏这类“快速失败”才重试。
- httpx.AsyncClient 进程内共享（连接池复用），不再每次调用新建。
"""

from __future__ import annotations

import json
import logging
import time

from app.adapters.qwen_policy import policy_for, validate_actual_model
from dataclasses import dataclass, field
from typing import AsyncIterator

import httpx
from tenacity import (
    AsyncRetrying,
    retry_if_exception_type,
    stop_after_attempt,
    wait_exponential,
)

from app.config import get_settings

logger = logging.getLogger(__name__)


@dataclass
class LLMResult:
    content: str
    model: str
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost: float = 0.0
    mocked: bool = False
    actual_model: str = ""
    request_id: str = ""
    latency_ms: float = 0.0
    prompt_version: str = ""


@dataclass
class StreamChunk:
    """流式补全的一个事件。

    delta 非空表示文本增量；delta 为空且 final=True 表示结束帧，
    prompt_tokens/completion_tokens/cost 在结束帧携带本次调用的真实用量
    （需请求时带 stream_options.include_usage，部分网关可能仍回 0）。
    tool_calls 非空表示模型请求工具调用（Function Calling）。
    """

    delta: str = ""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost: float = 0.0
    final: bool = False
    tool_calls: list = field(default_factory=list)
    actual_model: str = ""
    request_id: str = ""
    latency_ms: float = 0.0
    prompt_version: str = ""


@dataclass
class UsageAccumulator:
    """单次任务内累计 token/成本。"""

    prompt_tokens: int = 0
    completion_tokens: int = 0
    cost: float = 0.0
    calls: int = 0
    records: list = field(default_factory=list)

    def add(self, r: LLMResult) -> None:
        self.prompt_tokens += r.prompt_tokens
        self.completion_tokens += r.completion_tokens
        self.cost += r.cost
        self.calls += 1
        self.records.append(r)


class LLMError(Exception):
    """LLM 调用失败（默认不重试）。"""

    def __init__(self, message: str, status_code: int | None = None) -> None:
        super().__init__(message)
        self.status_code = status_code


class RetryableLLMError(LLMError):
    """可安全重试的快速失败：连接类异常、429、5xx、响应体损坏。"""


class LLMClient:
    """薄封装：同步/流式补全 + mock + 分类重试 + 成本统计。

    Qwen 兼容协议；所有推理任务使用同一模型。
    """

    def __init__(self) -> None:
        self.s = get_settings()
        self._http_client: httpx.AsyncClient | None = None

    def _client(self) -> httpx.AsyncClient:
        """懒初始化共享客户端。

        读/写超时给满 llm_timeout（300s，长复盘输出）；连接与连接池获取
        只给 10s——这两类失败属于“快速失败”，应立刻重试而不是干等。
        """
        if self._http_client is None or self._http_client.is_closed:
            timeout = httpx.Timeout(
                self.s.llm_timeout,
                connect=10.0,
                pool=10.0,
            )
            limits = httpx.Limits(max_connections=100, max_keepalive_connections=20)
            self._http_client = httpx.AsyncClient(timeout=timeout, limits=limits)
        return self._http_client

    def _validate_config(self) -> None:
        production = getattr(self.s, "app_env", "development") == "production"
        try:
            validate_actual_model(self.s.llm_model, self.s.llm_mock, production)
            if production and getattr(self.s, "llm_provider", "qwen") != "qwen":
                raise ValueError("production provider must be qwen")
        except ValueError as exc:
            raise LLMError(str(exc)) from exc

    def _policy(self, task: str | None):
        policy = policy_for(task or "review")
        timeout = getattr(self.s, f"llm_{policy.task}_timeout", policy.timeout)
        return policy, httpx.Timeout(timeout, connect=10.0, pool=10.0)

    def _validate_response_model(self, actual: str) -> None:
        try:
            validate_actual_model(
                actual, False, getattr(self.s, "app_env", "development") == "production"
            )
        except ValueError as exc:
            raise LLMError(str(exc)) from exc

    async def aclose(self) -> None:
        """关闭共享连接池（测试或应用停机钩子调用；进程退出时非必须）。"""
        if self._http_client is not None and not self._http_client.is_closed:
            await self._http_client.aclose()

    async def complete(
        self,
        messages: list[dict],
        temperature: float = 0.3,
        response_schema: dict | None = None,
        *,
        task: str = "review",
    ) -> LLMResult:
        self._validate_config()
        if self.s.llm_mock:
            return self._mock(messages, response_schema)

        # 总尝试次数含首次；只重试 RetryableLLMError（见 _do_complete 的分类）。
        attempts = max(1, self.s.llm_max_retries)
        async for attempt in AsyncRetrying(
            stop=stop_after_attempt(attempts),
            wait=wait_exponential(multiplier=1, min=1, max=8),
            retry=retry_if_exception_type(RetryableLLMError),
            reraise=True,
            before_sleep=self._log_retry,
        ):
            with attempt:
                return await self._do_complete(
                    messages, temperature, response_schema, task=task
                )

    async def _do_complete(
        self,
        messages: list[dict],
        temperature: float,
        response_schema: dict | None,
        *,
        task: str | None = None,
    ) -> LLMResult:
        self._validate_config()
        policy, timeout = self._policy(task)
        started_at = time.monotonic()
        # 注意：刻意不传 max_tokens——限长会截断长 JSON 导致整体失败（T4-16）。
        payload: dict = {
            "model": self.s.llm_model,
            "messages": messages,
            "temperature": temperature,
        }
        # 结构化输出：要求模型返回 JSON，上层再用 pydantic 校验。
        payload["enable_thinking"] = policy.enable_thinking if task else False
        if response_schema is not None and not payload["enable_thinking"]:
            payload["response_format"] = {"type": "json_object"}

        base_url = self.s.llm_base_url.rstrip("/")
        try:
            resp = await self._client().post(
                f"{base_url}/v1/chat/completions",
                headers={"Authorization": f"Bearer {self.s.llm_api_key}"},
                json=payload,
                timeout=timeout,
            )
        except (httpx.ConnectTimeout, httpx.PoolTimeout) as e:
            # 建连/等连接池失败通常是网关抖动，快速失败可重试。
            raise RetryableLLMError(f"连接模型网关失败: {e}") from e
        except httpx.TimeoutException as e:
            # 已建连但模型在 llm_timeout 内没读完：重试只会翻倍墙钟，不重试。
            raise LLMError(
                f"模型在 {self.s.llm_timeout:.0f}s 内未返回完整结果，按策略不重试: {e}"
            ) from e
        except httpx.HTTPError as e:
            # 其他传输层错误（连接被重置、协议错误等）视为可重试。
            raise RetryableLLMError(f"调用模型网关发生网络错误: {e}") from e

        if resp.status_code == 429 or resp.status_code >= 500:
            raise RetryableLLMError(
                f"LLM 返回 {resp.status_code}: {resp.text[:200]}",
                status_code=resp.status_code,
            )
        if resp.status_code >= 400:
            # 鉴权失败、参数错误（含模型不支持 response_format）等 4xx 重试无意义。
            raise LLMError(
                f"LLM 返回 {resp.status_code}: {resp.text[:200]}",
                status_code=resp.status_code,
            )

        try:
            data = resp.json()
            content = data["choices"][0]["message"]["content"]
        except (json.JSONDecodeError, KeyError, IndexError, TypeError) as e:
            # 200 但响应体损坏多为网关瞬时异常，允许重试。
            raise RetryableLLMError(
                f"LLM 响应体无法解析: {e}; body={resp.text[:200]}"
            ) from e

        actual_model = data.get("model", "")
        self._validate_response_model(actual_model)
        usage = data.get("usage", {})
        return LLMResult(
            content=content,
            model=actual_model,
            actual_model=actual_model,
            request_id=data.get("id", "") or resp.headers.get("x-request-id", ""),
            latency_ms=(time.monotonic() - started_at) * 1000,
            prompt_version=policy.prompt_version,
            prompt_tokens=usage.get("prompt_tokens", 0),
            completion_tokens=usage.get("completion_tokens", 0),
            cost=self._estimate_cost(usage),
        )

    @staticmethod
    def _log_retry(retry_state) -> None:
        logger.warning(
            "LLM 调用快速失败，准备第 %s 次重试（共 %s 次）: %s",
            retry_state.attempt_number,
            get_settings().llm_max_retries,
            retry_state.outcome.exception() if retry_state.outcome else "unknown",
        )

    async def stream(
        self,
        messages: list[dict],
        temperature: float = 0.3,
        tools: list[dict] | None = None,
        *,
        task: str = "assistant",
    ) -> AsyncIterator[StreamChunk]:
        """流式补全：逐块产出 StreamChunk（文本增量 + 结束帧真实用量）。

        mock 模式：基于最后一条用户消息构造确定性回复，按小切片逐块返回，
        不依赖网络、不花钱，且能真实模拟多 chunk 的流式节奏；结束帧用量为 0。

        真实模式走 OpenAI 兼容 SSE（stream=True）。tools 非空时启用
        Function Calling，模型可能返回 tool_calls。重试边界与 complete() 对齐：
        - 建连/连接池超时、其他传输错误、429/5xx、首字节前流损坏属快速失败，
          在**尚未下发任何增量**时可安全重试（不会让下游收到重复内容）；
        - 已开始下发增量后发生读超时/连接中断，重试会导致下游重复，统一降级为
          不重试的 LLMError，由调用方下发错误帧。
        """
        self._validate_config()
        if self.s.llm_mock:
            async for chunk in self._mock_stream(messages, tools):
                yield chunk
            return

        attempts = max(1, self.s.llm_max_retries)
        async for attempt in AsyncRetrying(
            stop=stop_after_attempt(attempts),
            wait=wait_exponential(multiplier=1, min=1, max=8),
            retry=retry_if_exception_type(RetryableLLMError),
            reraise=True,
            before_sleep=self._log_retry,
        ):
            with attempt:
                async for chunk in self._do_stream(
                    messages, temperature, tools, task=task
                ):
                    yield chunk

    async def _mock_stream(
        self, messages: list[dict], tools: list[dict] | None
    ) -> AsyncIterator[StreamChunk]:
        """mock 模式：仅当显式提供工具时确定性返回一次 get_logs 调用，
        便于在不接真实模型时联调工具循环；未提供工具时维持原文本切片行为
        （保证既有 stream mock 测试不变）。"""
        if tools:
            call = {
                "id": "call-mock-1",
                "type": "function",
                "function": {
                    "name": "get_logs",
                    "arguments": json.dumps({"tail": 50}, ensure_ascii=False),
                },
            }
            yield StreamChunk(tool_calls=[call])
            yield StreamChunk(final=True)
            return
        text = self._mock(messages, None).content
        # 按约 6 个字符切片，模拟 token 流；中文也能平滑逐字出现。
        step = 6
        for i in range(0, len(text), step):
            yield StreamChunk(delta=text[i : i + step])
        yield StreamChunk(final=True)

    async def _do_stream(
        self,
        messages: list[dict],
        temperature: float,
        tools: list[dict] | None = None,
        *,
        task: str = "assistant",
    ) -> AsyncIterator[StreamChunk]:
        """单次 SSE 调用。刻意不传 max_tokens（T4-16 同 complete）。
        tools 非空时启用 Function Calling。"""
        self._validate_config()
        policy, timeout = self._policy(task)
        started_at = time.monotonic()
        actual_model = ""
        request_id = ""
        payload: dict = {
            "model": self.s.llm_model,
            "messages": messages,
            "temperature": temperature,
            "enable_thinking": policy.enable_thinking,
            "stream": True,
            # DashScope 兼容模式：流式下默认不回 usage，需显式索取。
            "stream_options": {"include_usage": True},
        }
        if tools:
            payload["tools"] = tools
        base_url = self.s.llm_base_url.rstrip("/")
        try:
            ctx = self._client().stream(
                "POST",
                f"{base_url}/v1/chat/completions",
                headers={"Authorization": f"Bearer {self.s.llm_api_key}"},
                json=payload,
                timeout=timeout,
            )
            resp = await ctx.__aenter__()
        except (httpx.ConnectTimeout, httpx.PoolTimeout) as e:
            raise RetryableLLMError(f"连接模型网关失败: {e}") from e
        except httpx.TimeoutException as e:
            # 建连阶段读超时极少见；按 complete() 策略不重试。
            raise LLMError(
                f"模型在 {self.s.llm_timeout:.0f}s 内未建立流式响应，按策略不重试: {e}"
            ) from e
        except httpx.HTTPError as e:
            raise RetryableLLMError(f"调用模型网关发生网络错误: {e}") from e

        # started：是否已向下游 yield 过增量。一旦开始，后续错误不可重试，
        # 否则 tenacity 重放整个异步生成器会让下游收到重复的前半段文本。
        started = False
        try:
            if resp.status_code == 429 or resp.status_code >= 500:
                body = (await resp.aread())[:200]
                raise RetryableLLMError(
                    f"LLM 流式返回 {resp.status_code}: {body!r}",
                    status_code=resp.status_code,
                )
            if resp.status_code >= 400:
                body = (await resp.aread())[:200]
                raise LLMError(
                    f"LLM 流式返回 {resp.status_code}: {body!r}",
                    status_code=resp.status_code,
                )

            last_usage: dict = {}
            # tool_calls 在 SSE 中分片到达（首片含 id/name，后续片补参数），
            # 按 index 累积，循环结束后一次性产出。
            tool_acc: dict[int, dict] = {}
            async for line in resp.aiter_lines():
                if not line or not line.startswith("data:"):
                    # SSE 注释行（: keep-alive）、空行与事件间分隔一律忽略。
                    continue
                data_str = line[len("data:") :].strip()
                if data_str == "[DONE]":
                    break
                try:
                    data = json.loads(data_str)
                    if data.get("model"):
                        actual_model = data["model"]
                        self._validate_response_model(actual_model)
                    request_id = data.get("id") or request_id
                    # 注意：含 usage 的结束帧 choices 为空数组，必须先判空再取 [0]。
                    choices = data.get("choices") or []
                    delta = None
                    raw_tc = None
                    if choices:
                        d0 = choices[0].get("delta") or {}
                        delta = d0.get("content")
                        raw_tc = d0.get("tool_calls")
                    usage = data.get("usage")
                except (
                    json.JSONDecodeError,
                    AttributeError,
                    KeyError,
                    IndexError,
                    TypeError,
                ) as e:
                    # 首字节前损坏（started=False）允许重试；已出增量后损坏不能重放。
                    if not started:
                        raise RetryableLLMError(
                            f"LLM 流式响应帧无法解析: {e}; frame={data_str[:200]}"
                        ) from e
                    raise LLMError(f"LLM 流式响应帧损坏，连接终止: {e}") from e
                if usage:
                    # 含 usage 的结束帧 choices 为空，仅记录用量。
                    last_usage = usage
                # 累积 tool_calls 分片。
                if raw_tc:
                    for piece in raw_tc:
                        idx = piece.get("index", 0)
                        slot = tool_acc.setdefault(
                            idx, {"id": "", "name": "", "arguments": ""}
                        )
                        fn = piece.get("function") or {}
                        if piece.get("id"):
                            slot["id"] = piece["id"]
                        if fn.get("name"):
                            slot["name"] += fn["name"]
                        if fn.get("arguments"):
                            slot["arguments"] += fn["arguments"]
                if delta:
                    self._validate_response_model(actual_model)
                    started = True
                    yield StreamChunk(delta=delta)

            # 输出累积完成的工具调用（规范化为统一结构）。
            calls: list[dict] = []
            for idx in sorted(tool_acc):
                slot = tool_acc[idx]
                if slot["name"]:
                    calls.append(
                        {
                            "id": slot["id"] or f"call-{idx+1}",
                            "type": "function",
                            "function": {
                                "name": slot["name"],
                                "arguments": slot["arguments"],
                            },
                        }
                    )
            self._validate_response_model(actual_model)
            if calls:
                started = True
                yield StreamChunk(tool_calls=calls)
            yield StreamChunk(
                final=True,
                prompt_tokens=last_usage.get("prompt_tokens", 0),
                completion_tokens=last_usage.get("completion_tokens", 0),
                cost=self._estimate_cost(last_usage),
                actual_model=actual_model,
                request_id=request_id or resp.headers.get("x-request-id", ""),
                latency_ms=(time.monotonic() - started_at) * 1000,
                prompt_version=policy.prompt_version,
            )
        except (httpx.ConnectTimeout, httpx.PoolTimeout) as e:
            # 流读取阶段的连接类异常：已出增量则不能重试。
            if not started:
                raise RetryableLLMError(f"读取模型流式响应失败: {e}") from e
            raise LLMError(f"流式响应中断（已开始下发，不重试）: {e}") from e
        except httpx.TimeoutException as e:
            # 已建连但流在 llm_timeout 内没读完：重试只会翻倍墙钟且造成重复，不重试。
            raise LLMError(
                f"模型流式响应在 {self.s.llm_timeout:.0f}s 内未完成，按策略不重试: {e}"
            ) from e
        except httpx.HTTPError as e:
            if not started:
                raise RetryableLLMError(f"读取模型流式响应发生网络错误: {e}") from e
            raise LLMError(f"流式响应网络中断（已开始下发，不重试）: {e}") from e
        finally:
            await ctx.__aexit__(None, None, None)

    def _mock(self, messages: list[dict], schema: dict | None) -> LLMResult:
        if schema is not None:
            content = json.dumps(
                {"mock": True, "note": "结构化占位输出"}, ensure_ascii=False
            )
        else:
            last = messages[-1]["content"] if messages else ""
            content = (
                f"[MOCK AI] 收到：{last[:50]}。（当前为 mock 模式，不调用真实模型）"
            )
        return LLMResult(content=content, model="mock", mocked=True)

    def _estimate_cost(self, usage: dict) -> float:
        # 单价（元/百万 token）由配置注入；默认 0 表示暂不计费。
        return (
            usage.get("prompt_tokens", 0) * self.s.llm_price_input
            + usage.get("completion_tokens", 0) * self.s.llm_price_output
        ) / 1_000_000


llm_client = LLMClient()
