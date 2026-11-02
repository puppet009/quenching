// 评分体系服务（T2-6）。
//
// 5 大评分维度：
//   - 排障速度（30%）：故障发生到恢复的时间
//   - 恢复质量（25%）：恢复验证得分
//   - 方法合理性（20%）：操作步骤是否合理
//   - 知识掌握（15%）：答题/选择正确性
//   - 团队协作（10%）：协作模式下的配合度
package service

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// defaultDimensions 默认评分维度配置。
var defaultDimensions = []model.DimensionConfig{
	{
		Dimension:   model.DimSpeed,
		Name:        "排障速度",
		Description: "从故障发生到完全恢复所用的时间，越快得分越高",
		Weight:      0.30,
		MaxScore:    100,
	},
	{
		Dimension:   model.DimRecovery,
		Name:        "恢复质量",
		Description: "系统恢复的完整程度，基于恢复验证结果",
		Weight:      0.25,
		MaxScore:    100,
	},
	{
		Dimension:   model.DimMethod,
		Name:        "方法合理性",
		Description: "排障过程中使用的方法和步骤是否合理、规范",
		Weight:      0.20,
		MaxScore:    100,
	},
	{
		Dimension:   model.DimKnowledge,
		Name:        "知识掌握",
		Description: "对相关技术知识的掌握程度（答题、选择）",
		Weight:      0.15,
		MaxScore:    100,
	},
	{
		Dimension:   model.DimCollaboration,
		Name:        "团队协作",
		Description: "团队协作模式下的配合度和沟通效率",
		Weight:      0.10,
		MaxScore:    100,
	},
}

var gradeTable = []model.ScoringGradeInfo{
	{Grade: model.GradeExcellent, Name: "优秀", MinScore: 90, MaxScore: 100, Color: "#52c41a", Description: "表现出色，全面掌握"},
	{Grade: model.GradeGood, Name: "良好", MinScore: 80, MaxScore: 89.99, Color: "#1890ff", Description: "表现不错，基本掌握"},
	{Grade: model.GradePass, Name: "及格", MinScore: 60, MaxScore: 79.99, Color: "#faad14", Description: "勉强通过，还需加强"},
	{Grade: model.GradeFail, Name: "不及格", MinScore: 0, MaxScore: 59.99, Color: "#ff4d4f", Description: "未能通过，需要努力"},
}

// getGrade 根据得分获取等级。
func getGrade(score float64) model.ScoringGradeInfo {
	for _, g := range gradeTable {
		if score >= g.MinScore && score <= g.MaxScore {
			return g
		}
	}
	return gradeTable[3] // 默认不及格
}

// ScoringService 评分服务。
type ScoringService struct {
	dimensions []model.DimensionConfig
	// 评分记录（内存存储，生产环境应落库）
	records map[string]*model.ScoreResult // sessionID -> result
	mu      sync.RWMutex
}

// NewScoringService 创建评分服务。
func NewScoringService() *ScoringService {
	return &ScoringService{
		dimensions: defaultDimensions,
		records:    make(map[string]*model.ScoreResult),
	}
}

// GetDimensions 获取评分维度配置。
func (s *ScoringService) GetDimensions() []model.DimensionConfig {
	return s.dimensions
}

// Calculate 计算评分。
func (s *ScoringService) Calculate(ctx context.Context, input model.ScoreInput) (*model.ScoreResult, error) {
	result := &model.ScoreResult{
		SessionID:  input.SessionID,
		UserID:     input.UserID,
		Dimensions: make([]model.DimensionScore, 0, len(s.dimensions)),
		ScoredAt:   time.Now().UnixMilli(),
	}

	// 计算各维度得分
	var totalWeighted float64

	for _, dim := range s.dimensions {
		ds := model.DimensionScore{
			Dimension: dim.Dimension,
			Name:      dim.Name,
			Weight:    dim.Weight,
			MaxScore:  dim.MaxScore,
		}

		switch dim.Dimension {
		case model.DimSpeed:
			s.calcSpeedScore(&ds, input)
		case model.DimRecovery:
			s.calcRecoveryScore(&ds, input)
		case model.DimMethod:
			s.calcMethodScore(&ds, input)
		case model.DimKnowledge:
			s.calcKnowledgeScore(&ds, input)
		case model.DimCollaboration:
			s.calcCollabScore(&ds, input)
		}

		ds.Weighted = ds.Score * dim.Weight
		totalWeighted += ds.Weighted
		result.Dimensions = append(result.Dimensions, ds)
	}

	// 总分
	result.TotalScore = round2(totalWeighted)

	// 等级
	result.Grade = getGrade(result.TotalScore)

	// 用时统计
	if input.FaultStartTime > 0 {
		endTime := input.RecoveryTime
		if endTime == 0 {
			endTime = time.Now().UnixMilli()
		}
		result.OverallTime = (endTime - input.FaultStartTime) / 1000
		result.RecoveryTime = result.OverallTime
	}

	// 总体评价
	result.Feedback = s.generateOverallFeedback(result)

	// 保存记录
	s.mu.Lock()
	s.records[input.SessionID] = result
	s.mu.Unlock()

	return result, nil
}

// GetResult 获取评分结果。
func (s *ScoringService) GetResult(sessionID string) (*model.ScoreResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.records[sessionID]
	return result, ok
}

// ListResults 列出评分记录（按时间倒序）。
func (s *ScoringService) ListResults(userID int64, limit int) []*model.ScoreResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*model.ScoreResult
	for _, r := range s.records {
		if r.UserID == userID {
			results = append(results, r)
		}
	}

	// 按评分时间倒序
	sort.Slice(results, func(i, j int) bool {
		return results[i].ScoredAt > results[j].ScoredAt
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

// ========== 各维度评分计算 ==========

// calcSpeedScore 计算排障速度得分。
//
// 规则：
//   - 在预期时间内完成 → 100 分
//   - 超过预期时间 → 线性扣分，最低 0 分
//   - 未恢复 → 0 分
func (s *ScoringService) calcSpeedScore(ds *model.DimensionScore, input model.ScoreInput) {
	if input.RecoveryTime == 0 {
		ds.Score = 0
		ds.Feedback = "未完成恢复"
		ds.Deductions = append(ds.Deductions, model.Deduction{
			Reason: "未在限定时间内完成恢复",
			Points: 100,
		})
		return
	}

	if input.ExpectedSeconds == 0 {
		input.ExpectedSeconds = 600 // 默认 10 分钟
	}

	actualSeconds := float64(input.RecoveryTime-input.FaultStartTime) / 1000.0

	if actualSeconds <= 0 {
		ds.Score = 100
		ds.Feedback = "秒级恢复，非常快"
		return
	}

	if actualSeconds <= float64(input.ExpectedSeconds) {
		// 提前完成，满分
		ds.Score = 100
		ds.Feedback = fmt.Sprintf("用时 %d 秒，在预期 %d 秒内完成",
			int(actualSeconds), input.ExpectedSeconds)
	} else {
		// 超时，线性扣分
		ratio := float64(input.ExpectedSeconds) / actualSeconds
		ds.Score = round2(100 * ratio)
		if ds.Score < 0 {
			ds.Score = 0
		}
		deduct := 100 - ds.Score
		ds.Feedback = fmt.Sprintf("用时 %d 秒，超出预期 %d 秒",
			int(actualSeconds), int(actualSeconds)-input.ExpectedSeconds)
		ds.Deductions = append(ds.Deductions, model.Deduction{
			Reason: fmt.Sprintf("恢复用时超过预期时间（%.0f 秒 / %d 秒）",
				actualSeconds, input.ExpectedSeconds),
			Points: deduct,
		})
	}
}

// calcRecoveryScore 计算恢复质量得分。
//
// 直接使用恢复验证的得分。
func (s *ScoringService) calcRecoveryScore(ds *model.DimensionScore, input model.ScoreInput) {
	score := input.RecoveryScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	ds.Score = round2(score)

	switch {
	case score >= 90:
		ds.Feedback = "系统完全恢复，非常好"
	case score >= 70:
		ds.Feedback = "基本恢复，还有一些小问题"
	case score >= 50:
		ds.Feedback = "部分恢复，核心问题已解决"
	default:
		ds.Feedback = "恢复不彻底，还有较多问题"
	}

	if score < 100 {
		ds.Deductions = append(ds.Deductions, model.Deduction{
			Reason: fmt.Sprintf("恢复验证得分 %.1f，未达满分", score),
			Points: 100 - score,
		})
	}
}

// calcMethodScore 计算方法合理性得分。
//
// 直接使用传入的方法得分（由 AI 或人工评估）。
func (s *ScoringService) calcMethodScore(ds *model.DimensionScore, input model.ScoreInput) {
	score := input.MethodScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	ds.Score = round2(score)

	switch {
	case score >= 90:
		ds.Feedback = "排障方法专业、规范"
	case score >= 70:
		ds.Feedback = "排障方法基本合理"
	case score >= 50:
		ds.Feedback = "排障方法有待改进"
	default:
		ds.Feedback = "排障方法不太合理"
	}
}

// calcKnowledgeScore 计算知识掌握得分。
//
// 直接使用传入的知识得分（答题正确率等）。
func (s *ScoringService) calcKnowledgeScore(ds *model.DimensionScore, input model.ScoreInput) {
	score := input.KnowledgeScore
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	ds.Score = round2(score)

	switch {
	case score >= 90:
		ds.Feedback = "知识掌握扎实"
	case score >= 70:
		ds.Feedback = "知识掌握良好"
	case score >= 50:
		ds.Feedback = "知识掌握一般"
	default:
		ds.Feedback = "知识掌握不足"
	}
}

// calcCollabScore 计算团队协作得分。
//
// 直接使用传入的协作得分。
// 单人模式下默认满分。
func (s *ScoringService) calcCollabScore(ds *model.DimensionScore, input model.ScoreInput) {
	score := input.CollabScore
	if score <= 0 {
		// 单人模式默认满分
		score = 100
		ds.Feedback = "单人模式，默认满分"
	} else {
		if score > 100 {
			score = 100
		}
		switch {
		case score >= 90:
			ds.Feedback = "团队配合默契"
		case score >= 70:
			ds.Feedback = "团队配合良好"
		default:
			ds.Feedback = "团队配合有待加强"
		}
	}

	ds.Score = round2(score)
}

// ========== 反馈生成 ==========

// generateOverallFeedback 生成总体评价。
func (s *ScoringService) generateOverallFeedback(result *model.ScoreResult) string {
	grade := result.Grade

	switch grade.Grade {
	case model.GradeExcellent:
		return fmt.Sprintf("恭喜！你获得了 %.1f 分（%s）的好成绩。排障迅速、方法得当、系统完全恢复，"+
			"展现了扎实的技术功底和优秀的问题解决能力。继续保持！",
			result.TotalScore, grade.Name)

	case model.GradeGood:
		return fmt.Sprintf("表现不错！得分 %.1f 分（%s）。你成功解决了问题，系统基本恢复。"+
			"建议关注一下细节问题，争取下次拿到优秀！",
			result.TotalScore, grade.Name)

	case model.GradePass:
		return fmt.Sprintf("勉强通过，得分 %.1f 分（%s）。你找到了问题的核心，但还有不少改进空间。"+
			"建议多练习排障技巧，提升排障速度和恢复质量。",
			result.TotalScore, grade.Name)

	default:
		return fmt.Sprintf("很遗憾，本次得分 %.1f 分（%s），未能通过。"+
			"不要灰心，建议回顾相关知识，学习正确的排障方法，再试一次！",
			result.TotalScore, grade.Name)
	}
}

// ========== 工具函数 ==========

// round2 保留两位小数。
// 包级共享：scoring 与 ab_stats 服务共同使用。
func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
