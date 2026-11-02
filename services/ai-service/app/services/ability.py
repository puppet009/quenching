"""Evidence-based skill assessment, isolated from student conversation."""

import json
from pathlib import Path
from typing import Literal
from pydantic import BaseModel, ConfigDict, Field, model_validator
from app.adapters.llm_client import llm_client

DIMENSIONS = {
    "localization",
    "evidence",
    "mechanism",
    "repair",
    "regression",
    "architecture",
    "hint_dependence",
}


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class DimensionAssessment(StrictModel):
    level: Literal["unknown", "junior", "intermediate", "advanced"]
    confidence: float = Field(ge=0, le=1)
    evidence_refs: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def evidence_required(self):
        if self.level != "unknown" and not self.evidence_refs:
            raise ValueError("observed level requires evidence")
        if self.level == "unknown" and self.confidence != 0:
            raise ValueError("unknown dimension has zero confidence")
        return self


class AbilityAssessment(StrictModel):
    session_id: int = Field(gt=0)
    version: int = Field(gt=0)
    dimensions: dict[str, DimensionAssessment]
    evidence_refs: list[str]
    sufficiency: Literal["insufficient", "partial", "sufficient"]
    overall_level: Literal["unknown", "junior", "intermediate", "advanced"] = "unknown"

    @model_validator(mode="after")
    def seven_dimensions(self):
        if set(self.dimensions) != DIMENSIONS:
            raise ValueError("exactly seven skill dimensions required")
        return self


class AbilityRequest(StrictModel):
    session_id: int = Field(gt=0)
    version: int = Field(gt=0)
    evidence_json: str
    evidence_refs: list[str]


class AbilityResponse(StrictModel):
    assessment_json: str
    actual_model: str
    request_id: str = ""
    prompt_version: str = "assessment:v1"
    prompt_tokens: int = 0
    completion_tokens: int = 0
    latency_ms: float = 0
    mocked: bool = False


def validate_assessment(
    result: AbilityAssessment, allowed_refs: list[str], facts: dict
) -> AbilityAssessment:
    known = set(allowed_refs)
    if not set(result.evidence_refs) <= known:
        raise ValueError("foreign assessment evidence")
    for dimension in result.dimensions.values():
        if not set(dimension.evidence_refs) <= known:
            raise ValueError("foreign dimension evidence")
    repaired = [
        c for c in facts.get("challenges", []) if c.get("status") == "verified_repair"
    ]
    distinct_faults = {d for c in repaired for d in c.get("defect_ids", [])}
    sufficient = (
        result.sufficiency == "sufficient"
        and len(repaired) >= 2
        and len(distinct_faults) >= 2
    )
    sufficient = sufficient and all(
        d.level != "unknown" and d.confidence >= 0.7 and d.evidence_refs
        for d in result.dimensions.values()
    )
    if not sufficient:
        result.overall_level = "unknown"
        if result.sufficiency == "sufficient":
            result.sufficiency = "partial" if known else "insufficient"
    return result


async def assess_ability(request: AbilityRequest, client=llm_client) -> AbilityResponse:
    facts = json.loads(request.evidence_json)
    if not isinstance(facts, dict):
        raise ValueError("assessment evidence must be an object")
    prompt = (Path(__file__).parents[1] / "prompts/v1/brain/assessment.txt").read_text(
        encoding="utf-8"
    )
    schema = AbilityAssessment.model_json_schema()
    messages = [
        {
            "role": "system",
            "content": prompt + "\nReturn JSON matching: " + json.dumps(schema),
        },
        {
            "role": "user",
            "content": json.dumps(request.model_dump(), ensure_ascii=False),
        },
    ]
    totals = {"prompt_tokens": 0, "completion_tokens": 0, "latency_ms": 0.0}
    for attempt in range(2):
        inference = await client.complete(
            messages, response_schema=schema, task="assessment"
        )
        for key in totals:
            totals[key] += getattr(inference, key)
        try:
            result = AbilityAssessment.model_validate_json(inference.content)
            if (
                result.session_id != request.session_id
                or result.version != request.version
            ):
                raise ValueError("assessment scope/version mismatch")
            result = validate_assessment(result, request.evidence_refs, facts)
        except ValueError:
            if attempt:
                raise
            messages.append(
                {
                    "role": "user",
                    "content": "Invalid assessment. Recheck scope, exactly seven dimensions and valid evidence citations; unknown dimensions have confidence 0. Return corrected JSON.",
                }
            )
            continue
        return AbilityResponse(
            assessment_json=result.model_dump_json(),
            actual_model=inference.actual_model or inference.model,
            request_id=inference.request_id,
            prompt_version="assessment:v1",
            mocked=inference.mocked,
            **totals,
        )
    raise ValueError("assessment unavailable")
