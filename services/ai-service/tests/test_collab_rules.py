"""协作三维评估确定性规则测试。

自 training-service tests/collab_test.go 1:1 移植（8 个场景），
分值期望与 Go 侧完全一致；本文件是规则回迁后"两侧行为等价"的锁定基线，
修改任何阈值/权重都必须同步对拍。
"""

from __future__ import annotations

import math

import pytest

from app.services.collab import (
    ActionKind,
    CollabAction,
    Pitfall,
    PitfallOutcome,
    Question,
    evaluate_collab,
    info_utilization,
    judgment_score,
    question_quality,
    recognition_rate,
    verification_rate,
)

R = PitfallOutcome.RECOGNIZED
F = PitfallOutcome.FELL
P = PitfallOutcome.PENDING


def fapprox(a: float, b: float) -> bool:
    """与 Go fapprox 一致：绝对差 < 1.0。"""
    return math.fabs(a - b) < 1.0


def test_question_quality() -> None:
    vague = [Question(text="怎么办")]
    specific = [
        Question(
            text="order-svc 的 CPU 使用率到95%了，是不是有死循环，怎么定位？",
            stage="定位",
        )
    ]

    lo = question_quality(vague)
    hi = question_quality(specific)
    assert hi > lo
    assert hi >= 70, f"具体+带阶段+命中关键词应高分，实际 {hi:.0f}"
    assert lo <= 30, f"'怎么办'这类空泛提问应低分，实际 {lo:.0f}"
    assert question_quality([]) == 0


def test_info_utilization() -> None:
    no_follow = [Question(text="q"), Question(text="q")]
    with_follow = [
        Question(text="q", references_known_info=True),
        Question(text="q", follow_up=True),
    ]
    assert info_utilization(no_follow) == 0
    assert fapprox(info_utilization(with_follow), 100)


def test_recognition_rate() -> None:
    # 4 个遭遇坑：识别 3、踩 1 -> 75%；第 5 个 pending 不计入。
    pitfalls = [Pitfall(R), Pitfall(R), Pitfall(R), Pitfall(F), Pitfall(P)]
    rate, n, rec = recognition_rate(pitfalls)
    assert n == 4 and rec == 3
    assert fapprox(rate, 75), f"识别率应75，实际 {rate:.0f}"
    _, n0, _ = recognition_rate([])
    assert n0 == 0


def test_verification_rate() -> None:
    actions = [
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True),
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=False),
        CollabAction(ActionKind.INDEPENDENT),  # 不计入采纳分母
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True),
    ]
    rate, follows, verified = verification_rate(actions)
    assert follows == 3 and verified == 2
    assert fapprox(rate, 66.7), f"验证率应约66.7，实际 {rate:.1f}"


def test_judgment_score() -> None:
    # 识别率 75 ×0.6 + 验证率 66.7 ×0.4
    pitfalls = [Pitfall(R), Pitfall(R), Pitfall(R), Pitfall(F)]
    actions = [
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True),
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=False),
        CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True),
    ]
    got, ok = judgment_score(pitfalls, actions)
    assert ok
    want = 75 * 0.6 + 66.7 * 0.4
    assert fapprox(got, want), f"判断力应 {want:.1f}，实际 {got:.1f}"
    # 全无数据
    _, ok2 = judgment_score([], [])
    assert not ok2


def test_collect_full() -> None:
    result = evaluate_collab(
        questions=[
            Question(
                text="order-svc CPU 95%，是不是死循环？",
                stage="定位",
                references_known_info=True,
            )
        ],
        pitfalls=[Pitfall(R), Pitfall(R), Pitfall(F)],
        actions=[CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True)],
    )
    assert result.available
    assert result.question_quality > 0
    assert result.info_utilization > 0
    assert result.judgment > 0
    # 识别 2/3≈66.7，验证 1/1=100 -> 判断≈80
    assert fapprox(result.judgment, 80), f"判断力约80，实际 {result.judgment:.1f}"


def test_collect_manual_mode_unavailable() -> None:
    # 纯人工：无提问/坑点/采纳。
    result = evaluate_collab()
    assert not result.available
    assert result.question_quality == 0
    assert result.info_utilization == 0
    assert result.judgment == 0
    # 计数也应全部归零（Go 侧该断言体现在 score.Evaluate 综合分不被拉低，
    # 这里直接锁定响应面：available=False 时调用方不读取子分）。
    assert result.pitfall_total == 0
    assert result.follow_total == 0


def test_collect_end_to_end_score() -> None:
    # 优秀协作者：提问具体、识别全部坑、采纳前都验证。
    result = evaluate_collab(
        questions=[
            Question(
                text="order-svc CPU和错误率都异常，先查哪个？",
                stage="定位",
                follow_up=True,
            )
        ],
        pitfalls=[Pitfall(R), Pitfall(R)],
        actions=[CollabAction(ActionKind.FOLLOW_AI, verified_before_follow=True)],
    )
    # 识别 100%×0.6 + 验证 100%×0.4 = 100。
    assert fapprox(
        result.judgment, 100
    ), f"全识别全验证判断力应100，实际 {result.judgment:.1f}"


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
