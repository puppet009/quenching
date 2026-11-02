package service

import (
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// rcConfig 构造一份识别/踩坑规则齐备的配置（规则来自 recognition_rules/pitfall_rules）。
func rcConfig(mutate ...func(*model.PitfallConfig)) *model.PitfallConfig {
	c := &model.PitfallConfig{
		ID:              7,
		DefectID:        100,
		PitfallType:     model.TypeCausalMisjudgment,
		ConfidenceLevel: model.ConfidenceHigh,
		Status:          model.StatusActive,
		Recognition: model.RecognitionRules{
			DisputeKeywords:   []string{"不对", "为什么", "证据呢", "凭什么"},
			IndependentVerify: []string{"view_metric", "view_log", "exec_command"},
			IgnoreAndProceed:  true,
		},
		PitfallRules: model.FallRules{
			AdoptActions:  []string{"restart_service", "modify_config"},
			AdoptKeywords: []string{"好的", "就按你说的", "我执行了"},
		},
	}
	for _, m := range mutate {
		m(c)
	}
	return c
}

// ---- agree：执行推荐错误操作即踩坑（操作优先于文本） ----

func TestJudgeReactionAdoptByAction(t *testing.T) {
	r := JudgeReaction(rcConfig(), ReactionInput{
		Reply: "好的我重启",
		Operations: []model.PitfallOperation{
			{Type: "restart_service", Category: "action"},
		},
	})
	if !r.Resolved || r.Response != model.ResponseAgree || r.Outcome != OutcomeFell {
		t.Fatalf("执行推荐操作应判 agree/fell: %+v", r)
	}
	if r.VerifiedBeforeAdopt {
		t.Fatalf("无前置验证时 VerifiedBeforeAdopt 应为 false")
	}
}

func TestJudgeReactionAdoptActionOverridesDisputeText(t *testing.T) {
	// 回复里质疑，但实际还是执行了推荐错误操作 → 仍判踩坑。
	r := JudgeReaction(rcConfig(), ReactionInput{
		Reply: "凭什么让我重启？算了我重启了",
		Operations: []model.PitfallOperation{
			{Type: "restart_service", Category: "action"},
		},
	})
	if !r.Resolved || r.Response != model.ResponseAgree || r.Outcome != OutcomeFell {
		t.Fatalf("错误操作落地应覆盖质疑文本，判 agree/fell: %+v", r)
	}
}

func TestJudgeReactionVerifiedBeforeAdoptStillFell(t *testing.T) {
	// 先查日志（验证），随后仍执行推荐错误操作：结局是踩坑，但保留"采纳前验证"信号。
	r := JudgeReaction(rcConfig(), ReactionInput{
		Operations: []model.PitfallOperation{
			{Type: "view_log", Category: "observe"},
			{Type: "restart_service", Category: "action"},
		},
	})
	if r.Response != model.ResponseAgree || r.Outcome != OutcomeFell {
		t.Fatalf("验证后仍执行错误操作应为 agree/fell: %+v", r)
	}
	if !r.VerifiedBeforeAdopt {
		t.Fatalf("验证动作早于 adopt 动作时应置 VerifiedBeforeAdopt: %+v", r)
	}
}

func TestJudgeReactionVerifyAfterAdoptNotCredited(t *testing.T) {
	// 验证发生在错误操作之后：不算"采纳前验证"。
	r := JudgeReaction(rcConfig(), ReactionInput{
		Operations: []model.PitfallOperation{
			{Type: "restart_service", Category: "action"},
			{Type: "view_log", Category: "observe"},
		},
	})
	if r.Response != model.ResponseAgree || r.VerifiedBeforeAdopt {
		t.Fatalf("adopt 之后的验证不应计分: %+v", r)
	}
}

func TestJudgeReactionAdoptByKeywordOnly(t *testing.T) {
	r := JudgeReaction(rcConfig(), ReactionInput{Reply: "好的，就按你说的做"})
	if !r.Resolved || r.Response != model.ResponseAgree || r.Outcome != OutcomeFell {
		t.Fatalf("仅文本明确采纳应判 agree/fell: %+v", r)
	}
}

// ---- verified：先独立验证且未执行错误操作 ----

func TestJudgeReactionVerifiedByMetric(t *testing.T) {
	r := JudgeReaction(rcConfig(), ReactionInput{
		Operations: []model.PitfallOperation{{Type: "view_metric", Category: "observe"}},
	})
	if !r.Resolved || r.Response != model.ResponseVerified || r.Outcome != OutcomeRecognized {
		t.Fatalf("查看指标且未踩坑应判 verified/recognized: %+v", r)
	}
}

func TestJudgeReactionVerifiedByCategoryRule(t *testing.T) {
	// 规则写类别（analyze）时，同类别下任意具体类型都命中。
	c := rcConfig(func(c *model.PitfallConfig) {
		c.Recognition.IndependentVerify = []string{"analyze"}
	})
	r := JudgeReaction(c, ReactionInput{
		Operations: []model.PitfallOperation{{Type: "exec_command", Category: "analyze", Target: "EXPLAIN"}},
	})
	if r.Response != model.ResponseVerified || r.Outcome != OutcomeRecognized {
		t.Fatalf("按类别匹配验证动作应判 verified: %+v", r)
	}
}

// ---- dispute：回复反驳且无错误操作 ----

func TestJudgeReactionDisputeByKeyword(t *testing.T) {
	r := JudgeReaction(rcConfig(), ReactionInput{Reply: "不对吧，为什么要重启？"})
	if !r.Resolved || r.Response != model.ResponseDispute || r.Outcome != OutcomeRecognized {
		t.Fatalf("反驳应判 dispute/recognized: %+v", r)
	}
}

func TestJudgeReactionKeywordCaseInsensitive(t *testing.T) {
	c := rcConfig(func(c *model.PitfallConfig) { c.Recognition.DisputeKeywords = []string{"wrong"} })
	r := JudgeReaction(c, ReactionInput{Reply: "This is WRONG"})
	if r.Response != model.ResponseDispute {
		t.Fatalf("关键词匹配应大小写不敏感: %+v", r)
	}
}

// ---- ignore：配置允许绕行且发生非 AI 方向操作 ----

func TestJudgeReactionIgnoreAndProceed(t *testing.T) {
	r := JudgeReaction(rcConfig(), ReactionInput{
		Reply: "我先自己查查",
		Operations: []model.PitfallOperation{
			{Type: "check_architecture", Category: "analyze"},
		},
	})
	if !r.Resolved || r.Response != model.ResponseIgnore || r.Outcome != OutcomeRecognized {
		t.Fatalf("绕行排查应判 ignore/recognized: %+v", r)
	}
}

func TestJudgeReactionIgnoreDisabledStaysPending(t *testing.T) {
	// 关闭 ignore_and_proceed 后，非验证/非采纳/非反驳的中立操作不构成判定。
	c := rcConfig(func(c *model.PitfallConfig) {
		c.Recognition.IgnoreAndProceed = false
		c.Recognition.IndependentVerify = nil
	})
	r := JudgeReaction(c, ReactionInput{
		Operations: []model.PitfallOperation{{Type: "check_architecture", Category: "analyze"}},
	})
	if r.Resolved || r.Outcome != OutcomePending {
		t.Fatalf("未开启 ignore 且无其他信号时应 pending: %+v", r)
	}
}

func TestJudgeReactionAskAIisNotIndependentProceed(t *testing.T) {
	c := rcConfig(func(c *model.PitfallConfig) { c.Recognition.IndependentVerify = nil })
	r := JudgeReaction(c, ReactionInput{
		Operations: []model.PitfallOperation{{Type: "ask_ai", Category: "ai_assist"}},
	})
	if r.Resolved {
		t.Fatalf("继续向 AI 提问不算沿其他方向绕行，应 pending: %+v", r)
	}
}

// ---- pending：信息不足 ----

func TestJudgeReactionPendingWithNoSignal(t *testing.T) {
	for name, in := range map[string]ReactionInput{
		"空回复空操作": {},
		"无关追问":   {Reply: "今天天气怎么样"},
		"仅空白回复":  {Reply: "   "},
	} {
		t.Run(name, func(t *testing.T) {
			r := JudgeReaction(rcConfig(), in)
			if r.Resolved || r.Outcome != OutcomePending {
				t.Fatalf("应保持 pending: %+v", r)
			}
		})
	}
}

func TestJudgeReactionNilConfig(t *testing.T) {
	r := JudgeReaction(nil, ReactionInput{Reply: "好的"})
	if r.Resolved || r.Outcome != OutcomePending {
		t.Fatalf("nil 配置应 pending: %+v", r)
	}
}

// ---- 信号优先级：verified 先于 dispute 先于 ignore；adoptText 最后 ----

func TestJudgeReactionPrecedenceVerifiedOverDispute(t *testing.T) {
	// 同时有验证动作与反驳文本：取更"重"的行为证据 verified。
	r := JudgeReaction(rcConfig(), ReactionInput{
		Reply:      "凭什么",
		Operations: []model.PitfallOperation{{Type: "view_metric", Category: "observe"}},
	})
	if r.Response != model.ResponseVerified {
		t.Fatalf("验证动作应优先于反驳文本: %+v", r)
	}
}

func TestJudgeReactionDisputeBeatsAdoptKeyword(t *testing.T) {
	// 反驳与采纳文本同时出现：不轻易判踩坑，识别优先。
	r := JudgeReaction(rcConfig(), ReactionInput{Reply: "好的……不对，为什么"})
	if r.Response != model.ResponseDispute || r.Outcome != OutcomeRecognized {
		t.Fatalf("质疑与认同并存时应判 dispute: %+v", r)
	}
}
