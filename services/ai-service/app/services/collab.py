"""协作行为三维评估（确定性规则，不调 LLM）。

本模块自 training-service internal/collab 1:1 等价移植（2026-09 架构回迁）：
ai-service 是协作子分的唯一计算方，training-service 只负责事件采集、加权合并与回写。
任何阈值、关键词、权重、舍入规则都必须与 Go 侧历史行为保持一致，禁止单侧调整
（调整需先改设计文档，再两侧同步并更新对拍测试）。

三维：
- QuestionQuality 提问质量（具体性/针对性/完整度）
- InfoUtilization 信息利用率（有效追问、引用已知线索占比）
- Judgment 判断力（错误建议识别率 ×0.6 + 证据验证率 ×0.4）

纯函数、零 IO、零模型依赖，可直接单测复现。
"""

from __future__ import annotations

import math
from dataclasses import dataclass
from enum import Enum

# 提问评估阈值（对应 Go minGoodQuestionLen / minGreatQuestionLen）。
MIN_GOOD_QUESTION_LEN = 8
MIN_GREAT_QUESTION_LEN = 20

# 服务/指标/现象等"具体性"关键词，命中说明问题指向明确。
# 顺序与大小写规则须与 Go specificityKeywords 保持一致（匹配双方都转小写）。
SPECIFICITY_KEYWORDS: tuple[str, ...] = (
    "cpu",
    "内存",
    "memory",
    "延迟",
    "latency",
    "p99",
    "错误率",
    "error",
    "日志",
    "log",
    "服务",
    "service",
    "pod",
    "接口",
    "请求",
    "qps",
    "超时",
    "timeout",
    "磁盘",
    "disk",
    "网络",
    "network",
    "数据库",
    "mysql",
    "redis",
)

# 判断力合成权重：识别错误建议权重更高。
JUDGMENT_RECOGNITION_WEIGHT = 0.6
JUDGMENT_VERIFICATION_WEIGHT = 0.4


class PitfallOutcome(str, Enum):
    """单个坑点的用户反应。"""

    # 识别了坑（反驳/绕行/独立验证后走对：dispute/verified）。
    RECOGNIZED = "recognized"
    # 踩坑（采纳了 AI 错误建议：agree/fell）。
    FELL = "fell"
    # 未遭遇/未判定（不计入识别率分母）。
    PENDING = "pending"


class ActionKind(str, Enum):
    """排查操作归类。"""

    # 采纳 AI 建议执行的操作。
    FOLLOW_AI = "follow_ai"
    # 自主排查操作（查日志/指标/实验）。
    INDEPENDENT = "independent"


@dataclass(frozen=True, slots=True)
class Question:
    """一条用户提问。"""

    text: str = ""
    # 当前排查阶段关键词（如"信息收集/定位/修复"），可空。
    stage: str = ""
    # 是否引用了已知线索（上一条 AI 提示/监控/日志内容）。
    references_known_info: bool = False
    # 是否为基于前序回答的追问（而非重复提问）。
    follow_up: bool = False


@dataclass(frozen=True, slots=True)
class Pitfall:
    """一次坑点遭遇的判定结果。"""

    outcome: PitfallOutcome = PitfallOutcome.PENDING


@dataclass(frozen=True, slots=True)
class CollabAction:
    """一条排查操作（来自操作时间线）。"""

    kind: ActionKind = ActionKind.INDEPENDENT
    # 仅 kind=FOLLOW_AI 时有意义：采纳 AI 结论前是否先做了日志/指标/实验验证。
    verified_before_follow: bool = False


@dataclass(frozen=True, slots=True)
class CollabEvaluation:
    """协作评估结果。

    available=False 表示整场没有任何 AI 协作行为（纯人工 A0），
    调用方不得用三个 0 分子分拉低综合分。
    """

    available: bool
    question_quality: float = 0.0
    info_utilization: float = 0.0
    judgment: float = 0.0
    error_recognition_rate: float = 0.0
    evidence_validation_rate: float = 0.0
    pitfall_total: int = 0
    pitfall_recognized: int = 0
    follow_total: int = 0
    follow_verified: int = 0


def _clamp(v: float) -> float:
    """限制在 0-100（对应 Go clamp）。"""
    if v < 0:
        return 0.0
    if v > 100:
        return 100.0
    return v


def _contains_any_lower(text: str, keywords: tuple[str, ...]) -> bool:
    """双方转小写后的子串匹配（对应 Go containsAnyLower：strings.ToLower+Contains）。"""
    low = text.lower()
    return any(k.lower() in low for k in keywords)


def _round1(v: float) -> float:
    """正数四舍五入到 1 位小数（对应 Go round1：int(v*10+0.5)/10）。

    所有输入均为非负分值，故用 floor(v*10+0.5) 与 Go 向零取整等价；
    不能用 Python round（银行家舍入，2.675 类边界会产生偏差）。
    """
    return math.floor(v * 10 + 0.5) / 10


def _score_one_question(q: Question) -> float:
    """单条提问打分（对应 Go scoreOneQuestion）。"""
    text = q.text.strip()
    score = 0.0

    # 具体性：长度分档（按 rune 计字长，Python 3 字符串 len 即 Unicode 码点数）。
    length = len(text)
    if length >= MIN_GREAT_QUESTION_LEN:
        score += 35
    elif length >= MIN_GOOD_QUESTION_LEN:
        score += 22
    else:
        score += 8

    # 具体性：命中技术关键词 +25。
    if _contains_any_lower(text, SPECIFICITY_KEYWORDS):
        score += 25

    # 针对性 20 分：明确所处排查阶段。
    if q.stage.strip() != "":
        score += 20

    # 表述完整度 20 分：足够长（MVP 不做"纯关键词堆叠"识别，与 Go 一致）。
    if length >= MIN_GREAT_QUESTION_LEN:
        score += 20

    return _clamp(score)


def question_quality(questions: list[Question]) -> float:
    """提问质量分（0-100）：逐条打分后取平均；无提问返回 0。"""
    if not questions:
        return 0.0
    total = sum(_score_one_question(q) for q in questions)
    return _clamp(total / len(questions))


def info_utilization(questions: list[Question]) -> float:
    """信息利用率（0-100）：引用已知线索或有效追问的条数占比。"""
    if not questions:
        return 0.0
    good = sum(1 for q in questions if q.references_known_info or q.follow_up)
    return _clamp(good / len(questions) * 100)


def recognition_rate(pitfalls: list[Pitfall]) -> tuple[float, int, int]:
    """错误建议识别率 = 识别坑数 / 遭遇坑数（pending 不计分母）。

    返回 (率 0-100, 遭遇数, 识别数)；无遭遇返回 (0, 0, 0)。
    """
    encountered = 0
    recognized = 0
    for p in pitfalls:
        if p.outcome is PitfallOutcome.RECOGNIZED:
            encountered += 1
            recognized += 1
        elif p.outcome is PitfallOutcome.FELL:
            encountered += 1
    if encountered == 0:
        return 0.0, 0, 0
    return recognized / encountered * 100, encountered, recognized


def verification_rate(actions: list[CollabAction]) -> tuple[float, int, int]:
    """证据验证率 = 采纳前验证次数 / 采纳 AI 建议总次数。

    返回 (率 0-100, 采纳数, 验证数)；未采纳过 AI 建议返回 (0, 0, 0)。
    """
    follows = 0
    verified = 0
    for a in actions:
        if a.kind is not ActionKind.FOLLOW_AI:
            continue
        follows += 1
        if a.verified_before_follow:
            verified += 1
    if follows == 0:
        return 0.0, 0, 0
    return verified / follows * 100, follows, verified


def judgment_score(
    pitfalls: list[Pitfall], actions: list[CollabAction]
) -> tuple[float, bool]:
    """判断力综合分。

    识别率 ×0.6 + 验证率 ×0.4；只有一侧数据时用该侧；都没有返回 (0, False)。
    """
    rec_rate, rec_n, _ = recognition_rate(pitfalls)
    ver_rate, fol_n, _ = verification_rate(actions)

    if rec_n > 0 and fol_n > 0:
        return (
            _clamp(
                rec_rate * JUDGMENT_RECOGNITION_WEIGHT
                + ver_rate * JUDGMENT_VERIFICATION_WEIGHT
            ),
            True,
        )
    if rec_n > 0:
        return _clamp(rec_rate), True
    if fol_n > 0:
        return _clamp(ver_rate), True
    return 0.0, False


def evaluate_collab(
    questions: list[Question] | None = None,
    pitfalls: list[Pitfall] | None = None,
    actions: list[CollabAction] | None = None,
) -> CollabEvaluation:
    """计算一次训练会话的协作评估结果（对应 Go collab.Collect + 两个 Rate）。

    无提问且无判断力数据时 available=False（纯人工 A0）。
    三个子分各自四舍五入到 1 位小数；两个原始比率不做舍入（供雷达/明细展示）。
    """
    questions = questions or []
    pitfalls = pitfalls or []
    actions = actions or []

    judgment, has_judgment = judgment_score(pitfalls, actions)

    if not questions and not has_judgment:
        return CollabEvaluation(available=False)

    rec_rate, encountered, recognized = recognition_rate(pitfalls)
    ver_rate, follows, verified = verification_rate(actions)

    # 提问质量/信息利用率在完全没提问时为 0；有判断力数据时仍出协作分，
    # 缺提问的两项保持 0（该用户本就没怎么向 AI 提问），由加权自然体现。
    return CollabEvaluation(
        available=True,
        question_quality=_round1(question_quality(questions)),
        info_utilization=_round1(info_utilization(questions)),
        judgment=_round1(judgment),
        error_recognition_rate=rec_rate,
        evidence_validation_rate=ver_rate,
        pitfall_total=encountered,
        pitfall_recognized=recognized,
        follow_total=follows,
        follow_verified=verified,
    )


__all__ = [
    "PitfallOutcome",
    "ActionKind",
    "Question",
    "Pitfall",
    "CollabAction",
    "CollabEvaluation",
    "question_quality",
    "info_utilization",
    "recognition_rate",
    "verification_rate",
    "judgment_score",
    "evaluate_collab",
]
