package service

import "github.com/quenching/training-service/internal/model"

// 置信度话术库（T3-10，配置化）
//
// 设计目标：
//   1. 每档置信度有多句开场白和结尾话术，随机选用，避免重复可预测
//   2. 置信度高低与建议正误无固定关联——正确建议也随机置信度，
//      错误建议也可能低置信，杜绝"越自信越是坑"的可习得信号
//   3. 配置化：新增/修改话术只需改这个文件，不改逻辑
//
// 使用方式：由 BuildInjection 调用，按实际置信度档位随机选一句开场+一句结尾
// （结尾话术可选，只在高置信档偶尔出现，避免过度干扰）

// confidencePhrases 各档位话术配置。
// key 为置信度档位：low / medium / high
var confidencePhrases = map[string]struct {
	// openings 开场白（必选其一，放在正文前）
	openings []string
	// closings 结尾话术（可选，按概率追加在正文后，强化置信度感知）
	closings []string
	// closingProbability 结尾话术出现概率，[0,1]
	closingProbability float64
}{
	model.ConfidenceLow: {
		openings: []string{
			"我不是很有把握，不过供你参考：",
			"这个方向我不太确定，你可以看看：",
			"只是一个初步想法，可能不对：",
			"我有一些疑问，但先说说我的看法：",
			"信息量有限，我的推测是：",
		},
		closings: []string{
			"以上只是我的推测，建议你多方验证一下。",
			"别完全按我说的来，最好再交叉确认一下。",
			"这个方向不一定对，你也可以看看别的可能性。",
		},
		closingProbability: 0.6,
	},
	model.ConfidenceMedium: {
		openings: []string{
			"根据目前的信息，我的判断是：",
			"从现有数据来看，大概率是：",
			"我分析了一下，应该是这样：",
			"综合各方面信息，我的结论是：",
			"从现象上推断，比较可能的原因是：",
		},
		closings: []string{
			"你可以先按这个方向排查，有新发现随时调整。",
			"建议结合实际情况再确认一下。",
			"这是基于现有信息的判断，供你参考。",
		},
		closingProbability: 0.4,
	},
	model.ConfidenceHigh: {
		openings: []string{
			"我比较确定，基本可以按这个方向处理：",
			"证据比较充分，结论很明确：",
			"可以比较有把握地说：",
			"数据指向很清晰，结论就是：",
			"我已经仔细分析过了，基本可以确定：",
		},
		closings: []string{
			"按这个方向处理应该没问题。",
			"结论很明确，不用太怀疑。",
			"证据链很完整，这个判断是站得住的。",
			"放心按这个思路走就行。",
		},
		closingProbability: 0.7,
	},
}

// pickOpening 随机选一句开场白。
func pickOpening(level string, roll confidenceRoller) string {
	phrases, ok := confidencePhrases[level]
	if !ok || len(phrases.openings) == 0 {
		return "根据目前的信息，我的判断是："
	}
	return phrases.openings[roll.Intn(len(phrases.openings))]
}

// pickClosing 按概率选一句结尾话术；不选时返回空字符串。
func pickClosing(level string, roll confidenceRoller) string {
	phrases, ok := confidencePhrases[level]
	if !ok || len(phrases.closings) == 0 || phrases.closingProbability <= 0 {
		return ""
	}
	// 用 Intn 模拟概率：roll.Intn(100) < probability*100 视为命中
	threshold := int(phrases.closingProbability * 100)
	if roll.Intn(100) >= threshold {
		return ""
	}
	return phrases.closings[roll.Intn(len(phrases.closings))]
}
