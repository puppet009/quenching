"""Prompt 模板加载层（复盘 / 助教 / 坑点话术，按版本管理）。

约定（对应 README §4.2 app/prompts 职责）：
- 所有"静态话术"以纯文本文件存放在 ``app/prompts/<version>/<category>/<name>.txt``，
  业务代码不再内联大段提示词；只保留把运行时数据（故障现象列表、时间线等）
  拼进话术的那段逻辑。
- 模板按版本管理：当前默认版本为 :data:`DEFAULT_VERSION`（v1）。需要做不兼容改版
  （调整章节结构、变更输出契约）时，新建 v2/ 目录并在调用处显式切换，旧版本保留，
  便于灰度与回滚；纯文字润色直接改 v1 即可。
- :data:`_REGISTRY` 是模板的唯一清单：模块导入时即按清单全量加载，缺文件/空文件
  会在服务启动阶段直接抛错（快速失败），而不是等到某次请求才暴露。

运行时取值：
    from app.prompts import get_prompt, prompts
    system = get_prompt("assistant", "system_training_assistant")
    hint = prompts("assistant")["hint_level_1"]
"""

from __future__ import annotations

from importlib.resources import files

DEFAULT_VERSION = "v1"

# category -> (逻辑名 -> 相对版本目录的模板文件路径)。
# 新增模板必须在此登记，否则不会被启动校验覆盖。
_REGISTRY: dict[str, dict[str, str]] = {
    "review": {
        # 旧路径（training-service 直发裸时间线）的完整八章兜底指令。
        "fallback_system": "review/fallback_system.txt",
    },
    "assistant": {
        # 四种对话类型的基础人设。
        "system_training_assistant": "assistant/system_training_assistant.txt",
        "system_review_explainer": "assistant/system_review_explainer.txt",
        "system_knowledge_qa": "assistant/system_knowledge_qa.txt",
        "system_architecture_advisor": "assistant/system_architecture_advisor.txt",
        # 坑点引擎：三种误导模式的专项话术指令（编排层显式要求时才追加）。
        "pitfall_causal_misjudgment": "assistant/pitfall_causal_misjudgment.txt",
        "pitfall_info_omission": "assistant/pitfall_info_omission.txt",
        "pitfall_overconfidence": "assistant/pitfall_overconfidence.txt",
        # 渐进式提示三级话术：方向引导 / 缩小范围 / 接近答案。
        "hint_level_1": "assistant/hint_level_1.txt",
        "hint_level_2": "assistant/hint_level_2.txt",
        "hint_level_3": "assistant/hint_level_3.txt",
        # 脱敏故障上下文段落的首尾固定语（中间故障列表仍由业务层渲染）。
        "fault_context_prefix": "assistant/fault_context_prefix.txt",
        "fault_context_suffix": "assistant/fault_context_suffix.txt",
    },
    "comparison": {
        # 人机对照页 AI 总结的系统指令（输入为 /training/comparison 响应 JSON）。
        "system_summary": "comparison/system_summary.txt",
    },
}


class PromptNotFoundError(RuntimeError):
    """模板缺失或为空（启动校验 / 运行时取值都会抛）。"""


def _resource_path(version: str, rel_path: str):
    return files(__name__).joinpath(version, rel_path)


def _read(version: str, rel_path: str) -> str:
    path = _resource_path(version, rel_path)
    if not path.is_file():
        raise PromptNotFoundError(
            f"Prompt 模板不存在: app/prompts/{version}/{rel_path}"
        )
    # 仅去掉文件首尾空白（末尾换行）；模板内部的换行与缩进原样保留，
    # 因为复盘指令的 JSON 示例缩进、多行列章本身就是话术的一部分。
    content = path.read_text(encoding="utf-8").strip()
    if not content:
        raise PromptNotFoundError(f"Prompt 模板为空: app/prompts/{version}/{rel_path}")
    return content


def load_version(version: str = DEFAULT_VERSION) -> dict[str, dict[str, str]]:
    """加载某版本的全部模板（启动校验入口）；任一缺失/为空即抛错。"""
    loaded: dict[str, dict[str, str]] = {}
    for category, entries in _REGISTRY.items():
        loaded[category] = {name: _read(version, rel) for name, rel in entries.items()}
    return loaded


# 导入即校验：默认版本模板缺文件时，服务在 import 阶段直接起不来（快速失败）。
_PROMPTS: dict[str, dict[str, dict[str, str]]] = {
    DEFAULT_VERSION: load_version(DEFAULT_VERSION)
}


def get_prompt(category: str, name: str, version: str = DEFAULT_VERSION) -> str:
    """取单条模板文本。未预热的版本会按需加载并缓存。"""
    version_pack = _PROMPTS.get(version)
    if version_pack is None:
        version_pack = load_version(version)
        _PROMPTS[version] = version_pack
    try:
        return version_pack[category][name]
    except KeyError as exc:
        raise PromptNotFoundError(
            f"Prompt 模板未登记: category={category!r} name={name!r} version={version!r}"
        ) from exc


def prompts(category: str, version: str = DEFAULT_VERSION) -> dict[str, str]:
    """取某一类目的全部模板（逻辑名 -> 文本）。"""
    pack = _PROMPTS.get(version)
    if pack is None:
        pack = load_version(version)
        _PROMPTS[version] = pack
    if category not in pack:
        raise PromptNotFoundError(
            f"Prompt 类目不存在: category={category!r} version={version!r}"
        )
    return pack[category]
