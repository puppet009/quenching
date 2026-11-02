"""Prompt 模板层单测：版本目录可加载、注册条目齐全、关键话术契约不被改坏。

对应 README §4.2：app/prompts/ 存放复盘/助教/坑点话术模板并按版本管理。
本测试把"模板存在且非空"从运行时提前到 CI——模板文件被误删/清空/漏登记时，
虽然 import app.prompts 本身就会快速失败，但这里再固化关键话术断言，
防止有人保留文件却删掉了安全约束（如"不直接宣布根因"）。

运行：cd services/ai-service && python -m pytest tests/test_prompts_templates.py -q
"""

from __future__ import annotations

import pytest

from app.prompts import (
    DEFAULT_VERSION,
    PromptNotFoundError,
    _REGISTRY,
    get_prompt,
    load_version,
    prompts,
)

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


def test_default_version_is_v1():
    assert DEFAULT_VERSION == "v1"


def test_registry_matches_files_on_disk():
    """清单里的每个条目都能从磁盘加载到非空文本（导入时已校验，这里固化契约）。"""
    pack = load_version(DEFAULT_VERSION)
    assert set(pack.keys()) == set(_REGISTRY.keys())
    for category, entries in _REGISTRY.items():
        assert set(pack[category].keys()) == set(entries.keys())
        for name, text in pack[category].items():
            assert isinstance(text, str) and text.strip(), f"{category}/{name} 为空"


def test_get_prompt_and_prompts_return_same_text():
    assert (
        get_prompt("assistant", "system_training_assistant")
        == prompts("assistant")["system_training_assistant"]
    )


def test_unknown_template_raises_prompt_not_found():
    with pytest.raises(PromptNotFoundError):
        get_prompt("assistant", "not_registered")
    with pytest.raises(PromptNotFoundError):
        get_prompt("not_a_category", "x")
    with pytest.raises(PromptNotFoundError):
        get_prompt("assistant", "system_training_assistant", version="v999")


def test_review_fallback_prompt_keeps_json_contract():
    """复盘兜底指令必须保持八章键、纯 JSON 输出约束与足够的指令长度。"""
    text = get_prompt("review", "fallback_system")
    assert len(text) > 400
    for chapter in _EIGHT_CHAPTERS:
        assert f'"{chapter}"' in text
    assert "只输出 JSON" in text


def test_training_assistant_prompt_keeps_no_leak_contract():
    text = get_prompt("assistant", "system_training_assistant")
    assert "故障排查训练的助教" in text
    assert "根因" in text  # 明确约束不直接给根因


@pytest.mark.parametrize(
    ("name", "expected"),
    [
        ("hint_level_1", "第 1 级提示"),
        ("hint_level_2", "第 2 级提示"),
        ("hint_level_3", "第 3 级提示"),
    ],
)
def test_hint_prompts_keep_level_markers(name, expected):
    text = get_prompt("assistant", name)
    assert expected in text


def test_hint_level_3_never_announces_root_cause():
    """第三级提示最接近答案，安全红线必须写死在模板里。"""
    assert "不直接宣布根因" in get_prompt("assistant", "hint_level_3")


@pytest.mark.parametrize(
    "name",
    [
        "pitfall_causal_misjudgment",
        "pitfall_info_omission",
        "pitfall_overconfidence",
    ],
)
def test_pitfall_prompts_present(name):
    assert get_prompt("assistant", name).startswith("本次为专项训练")


def test_fault_context_wrapper_prompts_present():
    assert "只含现象，不含根因" in get_prompt("assistant", "fault_context_prefix")
    assert "不要原样大段复述" in get_prompt("assistant", "fault_context_suffix")


def test_comparison_summary_prompt_keeps_contract():
    """人机对照总结指令：四段式、只用真实数字、纯文本输出。"""
    text = get_prompt("comparison", "system_summary")
    assert len(text) > 300
    assert "总体结论" in text
    assert "效率与结果" in text
    assert "风险与协作" in text
    assert "改进建议" in text
    assert "严禁编造" in text
