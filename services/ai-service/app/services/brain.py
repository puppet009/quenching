"""Stateless director inference. Side effects remain in training-service."""

import json
from pathlib import Path
from typing import Literal
from pydantic import BaseModel, ConfigDict, Field, model_validator
from app.adapters.llm_client import llm_client

Action = Literal[
    "observe_session",
    "list_trainable_faults",
    "inspect_evidence",
    "run_user_behavior",
    "inject_fault",
    "revoke_fault",
    "verify_business",
    "set_hint_policy",
    "update_ability_assessment",
    "wait_for_event",
    "finish_training",
]
ACTIONS = set(Action.__args__)


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class BrainToolDef(StrictModel):
    name: Action
    description: str = ""
    parameters_json: str
    requires_approval: bool = False


class BrainCall(StrictModel):
    id: str = Field(min_length=1)
    name: Action
    arguments_json: str


class BrainResult(StrictModel):
    call_id: str = Field(min_length=1)
    content_json: str
    is_error: bool = False


class BrainRequest(StrictModel):
    session_id: int = Field(gt=0)
    phase: str
    observation_version: int = Field(ge=0)
    context_json: str = "{}"
    evidence_refs: list[str] = Field(default_factory=list)
    allowed_tools: list[BrainToolDef] = Field(default_factory=list)
    tool_results: list[BrainResult] = Field(default_factory=list)
    previous_calls: list[BrainCall] = Field(default_factory=list)


class BrainDecision(StrictModel):
    decision_id: str = Field(min_length=1, max_length=128)
    observation_version: int = Field(ge=0)
    learning_objective: str = Field(min_length=1)
    action: Action
    parameters_json: str
    evidence_refs: list[str] = Field(default_factory=list)
    reason_summary: str = Field(min_length=1)
    next_observation: str = Field(min_length=1)

    @model_validator(mode="after")
    def parameter_object(self):
        if not isinstance(json.loads(self.parameters_json), dict):
            raise ValueError("parameters must be a JSON object")
        return self


class BrainResponse(StrictModel):
    decision_json: str
    actual_model: str
    request_id: str = ""
    prompt_version: str = "director:v1"
    prompt_tokens: int = 0
    completion_tokens: int = 0
    latency_ms: float = 0
    mocked: bool = False


PROMPT = (
    Path(__file__)
    .parents[1]
    .joinpath("prompts/v1/brain/director.txt")
    .read_text(encoding="utf-8")
)


def build_brain_messages(request: BrainRequest) -> list[dict]:
    context = json.loads(request.context_json)
    if not isinstance(context, dict):
        raise ValueError("context must be an object")
    calls = {c.id: c for c in request.previous_calls}
    if len(calls) != len(request.previous_calls):
        raise ValueError("duplicate tool call id")
    results = {r.call_id: r for r in request.tool_results}
    if set(results) != set(calls) or len(results) != len(request.tool_results):
        raise ValueError("tool result must match each prior call exactly once")
    obs = request.model_dump(exclude={"previous_calls", "tool_results", "context_json"})
    obs["context"] = context
    messages = [
        {"role": "system", "content": PROMPT},
        {"role": "user", "content": json.dumps(obs, ensure_ascii=False)},
    ]
    if calls:
        messages.append(
            {
                "role": "assistant",
                "content": None,
                "tool_calls": [
                    {
                        "id": c.id,
                        "type": "function",
                        "function": {"name": c.name, "arguments": c.arguments_json},
                    }
                    for c in calls.values()
                ],
            }
        )
        messages.extend(
            {"role": "tool", "tool_call_id": r.call_id, "content": r.content_json}
            for r in request.tool_results
        )
    return messages


async def decide_brain(request: BrainRequest, *, client=None) -> BrainResponse:
    client = client or llm_client
    messages = build_brain_messages(request)
    allowed = {t.name: t for t in request.allowed_tools}
    if not allowed or len(allowed) != len(request.allowed_tools):
        raise ValueError("allowed tools must be nonempty and unique")
    schema = BrainDecision.model_json_schema()
    messages[0]["content"] += "\nOutput JSON satisfying this schema: " + json.dumps(
        schema
    )
    # One correction, never an execution retry. No hidden reasoning is persisted.
    totals = {"prompt_tokens": 0, "completion_tokens": 0, "latency_ms": 0.0}
    for attempt in range(2):
        result = await client.complete(
            messages, response_schema=schema, task="director"
        )
        for key in totals:
            totals[key] += getattr(result, key)
        try:
            decision = BrainDecision.model_validate_json(result.content)
            if decision.observation_version != request.observation_version:
                raise ValueError("stale observation version")
            if decision.action not in allowed:
                raise ValueError("action not allowed for observation")
            if not set(decision.evidence_refs) <= set(request.evidence_refs):
                raise ValueError("unknown evidence reference")
            params = json.loads(decision.parameters_json)
            tool_schema = json.loads(allowed[decision.action].parameters_json)
            if tool_schema.get("additionalProperties") is False:
                if set(params) - set(tool_schema.get("properties", {})):
                    raise ValueError("unexpected tool parameter")
            if not set(tool_schema.get("required", [])) <= set(params):
                raise ValueError("missing required tool parameter")
        except ValueError:
            if attempt:
                raise
            messages.append(
                {
                    "role": "user",
                    "content": "Invalid decision. Recheck schema, allowed tools, current observation version and evidence references; return corrected JSON.",
                }
            )
            continue
        return BrainResponse(
            decision_json=decision.model_dump_json(),
            actual_model=result.actual_model or result.model,
            request_id=result.request_id,
            prompt_version=result.prompt_version or "director:v1",
            mocked=result.mocked,
            **totals,
        )
    raise ValueError("no valid decision")
