// 坑点后用户反应的纯逻辑判定（T3-11）。
//
// 边界：本文件只把"用户回复文本 + 坑点注入后的操作时间线 + 配置化规则"
// 翻译成 agree/dispute/verified/ignore 四种反应与 recognized/fell 两种结局，
// 不碰数据库、不做 LLM 语义分析、不做故障恶化判定（WorsenFault 归 T3-12）。
//
// 判定口径（任务卡 / 《技术架构文档》§11.6）：
//   - agree    执行了 AI 推荐的错误操作（或回复中明确采纳）→ 踩坑 fell
//   - verified 先独立验证（查指标/日志/诊断命令），且未执行推荐错误操作 → 识别 recognized
//   - dispute  回复中明确反驳/质疑，且未执行推荐错误操作 → 识别 recognized
//   - ignore   配置允许"忽略并沿其他方向排查"，且发生了非 AI 方向的实际操作 → 识别 recognized
//
// 冲突仲裁（确定性）：实际操作优先于文本——只要执行了推荐的错误操作即 fell，
// 哪怕回复里带质疑；其余识别信号之间按 verified → dispute → ignore 的顺序取先命中。
//
// 常量分层：Response* 落 model（repository 写库时复用，避免反向依赖 service）；
// Outcome* 留在本文件——它只是判定层的裸字符串，落库后由评分采集映射为
// 强类型 service.PitfallOutcome（见 collab_events.go），随事件发往 ai-service。
package service

import (
	"strings"

	"github.com/quenching/training-service/internal/model"
)

// 坑点结局（判定层裸字符串；落库后由评分采集映射为 service.PitfallOutcome）。
const (
	OutcomeFell       = "fell"       // 踩坑：agree
	OutcomeRecognized = "recognized" // 识别：dispute/verified/ignore
	OutcomePending    = "pending"    // 本轮信息不足，尚未判定
)

// ReactionInput 一次反应判定的输入。Reply 为用户本轮回复原文；
// Operations 为坑点注入消息之后（严格晚于注入时刻）按时间正序发生的操作。
type ReactionInput struct {
	Reply      string
	Operations []model.PitfallOperation
}

// ReactionResult 判定结果。Resolved=false 表示信息不足、保持 pending，不应回写。
type ReactionResult struct {
	// Response agree/dispute/verified/ignore（model.Response*）；Resolved=false 时为空。
	Response string
	// Outcome fell/recognized；Resolved=false 时为 pending。
	Outcome string
	// VerifiedBeforeAdopt 仅 agree 时有意义：采纳推荐错误操作之前是否先做过独立验证。
	// agree 本身仍算踩坑，但该信号随操作事件上送 ai-service，供"证据验证率"单独计分。
	VerifiedBeforeAdopt bool
	Resolved            bool
	// Reason 命中的判定依据，便于日志排查与复盘展示。
	Reason string
}

// JudgeReaction 依据某条坑点配置上的规则判定用户反应。
// cfg 为 nil 或本轮无任何可用信号时返回 pending（调用方保持未判定，下一轮继续）。
func JudgeReaction(cfg *model.PitfallConfig, in ReactionInput) ReactionResult {
	if cfg == nil {
		return ReactionResult{Outcome: OutcomePending, Reason: "坑点配置为空"}
	}
	reply := strings.ToLower(strings.TrimSpace(in.Reply))

	adoptHit, adoptIdx := firstMatch(in.Operations, cfg.PitfallRules.AdoptActions)
	verifyHit, verifyIdx := firstMatch(in.Operations, cfg.Recognition.IndependentVerify)
	disputeText := reply != "" && containsAny(reply, cfg.Recognition.DisputeKeywords)
	adoptText := reply != "" && containsAny(reply, cfg.PitfallRules.AdoptKeywords)

	// 1) 踩坑（最高优先级，实际操作优先于文本）：执行了推荐错误操作。
	//    即便回复带质疑、或采纳前做过验证，只要错误操作落地即算踩坑；
	//    验证信号经 VerifiedBeforeAdopt 保留给验证率，不改写踩坑结局。
	if adoptHit != nil {
		r := ReactionResult{
			Response: model.ResponseAgree,
			Outcome:  OutcomeFell,
			Resolved: true,
			Reason:   "执行了AI推荐的错误操作: " + adoptHit.Type,
		}
		if verifyHit != nil && verifyIdx < adoptIdx {
			r.VerifiedBeforeAdopt = true
			r.Reason += "（采纳前曾独立验证）"
		}
		return r
	}

	// 2) 识别：先做独立验证（查指标/日志/诊断命令）且未执行错误操作。
	//    架构 §11.6 要求验证发生在采纳前的关键操作之前——既然没有 adopt 操作，
	//    此处验证即视为"验证后走对"。
	if verifyHit != nil {
		return ReactionResult{
			Response: model.ResponseVerified,
			Outcome:  OutcomeRecognized,
			Resolved: true,
			Reason:   "采纳前独立验证后走对: " + verifyHit.Type,
		}
	}

	// 3) 识别：回复明确反驳/质疑。
	if disputeText {
		return ReactionResult{
			Response: model.ResponseDispute,
			Outcome:  OutcomeRecognized,
			Resolved: true,
			Reason:   "回复中反驳/质疑AI建议",
		}
	}

	// 4) 识别：配置允许"忽略并沿其他方向排查"，且发生了非 AI 方向的实际操作。
	//    ask_ai/ai_assist 仍是在跟 AI 交互，不算"绕行"。
	if cfg.Recognition.IgnoreAndProceed && hasIndependentProceed(in.Operations) {
		return ReactionResult{
			Response: model.ResponseIgnore,
			Outcome:  OutcomeRecognized,
			Resolved: true,
			Reason:   "未采纳建议，沿其他方向排查",
		}
	}

	// 5) 踩坑（文本）：明确表达采纳，但尚无对应操作落地。
	//    排在识别信号之后：质疑文本与采纳文本同时出现时不轻易判踩坑。
	if adoptText {
		return ReactionResult{
			Response: model.ResponseAgree,
			Outcome:  OutcomeFell,
			Resolved: true,
			Reason:   "回复明确采纳AI建议",
		}
	}

	// 6) 信息不足：用户既没表态也没动作（或只是无关追问），保持 pending。
	return ReactionResult{Outcome: OutcomePending, Reason: "无可判定的回复或操作信号"}
}

// firstMatch 返回操作序列中第一个命中规则条目的操作及其下标。
// 规则条目既可写具体 operation_type（view_metric/exec_command），
// 也可写更粗的 operation_category（observe/analyze），大小写不敏感。
func firstMatch(ops []model.PitfallOperation, rules []string) (*model.PitfallOperation, int) {
	if len(rules) == 0 {
		return nil, -1
	}
	for i := range ops {
		for _, rule := range rules {
			r := strings.ToLower(strings.TrimSpace(rule))
			if r == "" {
				continue
			}
			if r == strings.ToLower(strings.TrimSpace(ops[i].Type)) ||
				r == strings.ToLower(strings.TrimSpace(ops[i].Category)) {
				return &ops[i], i
			}
		}
	}
	return nil, -1
}

// containsAny 回复文本命中任一关键词（子串、大小写不敏感），与引擎 matchQuestion 同口径。
func containsAny(lowerReply string, keywords []string) bool {
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lowerReply, kw) {
			return true
		}
	}
	return false
}

// hasIndependentProceed 是否发生了"沿其他方向排查"的实际操作：
// 有操作记录且不是向 AI 提问（ask_ai / ai_assist）。
func hasIndependentProceed(ops []model.PitfallOperation) bool {
	for i := range ops {
		t := strings.ToLower(strings.TrimSpace(ops[i].Type))
		c := strings.ToLower(strings.TrimSpace(ops[i].Category))
		if t == "ask_ai" || c == "ai_assist" {
			continue
		}
		return true
	}
	return false
}
