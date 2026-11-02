"""Task-specific Qwen policy; no alternate model fallback."""

from dataclasses import dataclass

MODEL = "qwen3.8-flash"


@dataclass(frozen=True)
class QwenTaskPolicy:
    task: str
    timeout: float
    enable_thinking: bool
    prompt_version: str


def policy_for(task: str) -> QwenTaskPolicy:
    modes = {
        "director": (60.0, False),
        "assistant": (30.0, False),
        "memory": (60.0, False),
        "assessment": (90.0, True),
        "review": (300.0, True),
    }
    if task not in modes:
        raise ValueError("unknown inference task")
    timeout, thinking = modes[task]
    return QwenTaskPolicy(task, timeout, thinking, f"{task}:v1")


def validate_actual_model(model: str, mocked: bool, production: bool) -> None:
    if production and (mocked or model != MODEL):
        raise ValueError("production inference requires actual qwen3.8-flash")
