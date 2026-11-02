// 服务间用户协作档案服务：校验调用参数，并在本服务内完成滚动平均计算。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/quenching/user-service/internal/repository"
)

// collabRollupAlpha 协作历史汇总的滚动平均权重。
const collabRollupAlpha = 0.2

// ErrInternalProfileNotFound 用户或协作档案不存在。
var ErrInternalProfileNotFound = errors.New("用户协作档案不存在")

// ErrInvalidCollabInput 调用方提交的协作档案参数非法。
var ErrInvalidCollabInput = errors.New("协作档案参数无效")

// RadarData 协作雷达数据，字段与 training-service 的前端契约保持一致。
type RadarData struct {
	Dimensions []string  `json:"dimensions"`
	Values     []float64 `json:"values"`
}

// CollabRollupInput 一次协作评估产生的最新雷达、总分和等级。
type CollabRollupInput struct {
	Radar RadarData
	Score float64
	Level string
}

// InternalProfileService 用户协作档案内部服务。
type InternalProfileService struct {
	profiles repository.InternalProfileStore
}

// NewInternalProfileService 创建用户协作档案内部服务。
func NewInternalProfileService(profiles repository.InternalProfileStore) *InternalProfileService {
	return &InternalProfileService{profiles: profiles}
}

// GetCollabProfile 返回用户协作档案。
func (s *InternalProfileService) GetCollabProfile(
	ctx context.Context, userID int64,
) (*repository.InternalCollabProfileView, error) {
	if userID <= 0 {
		return nil, invalidCollabInput("用户ID非法")
	}
	profile, err := s.profiles.GetCollabProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, ErrInternalProfileNotFound
	}
	return profile, nil
}

// AssignCollabGroupIfAbsent 校验分组后执行条件分配。
func (s *InternalProfileService) AssignCollabGroupIfAbsent(
	ctx context.Context, userID int64, group string,
) (string, bool, error) {
	if userID <= 0 {
		return "", false, invalidCollabInput("用户ID非法")
	}
	g := strings.TrimSpace(group)
	if g != "A" && g != "B" {
		return "", false, invalidCollabInput("协作分组只能为 A 或 B")
	}
	finalGroup, applied, err := s.profiles.AssignCollabGroupIfAbsent(ctx, userID, g)
	if err != nil {
		return "", false, err
	}
	if finalGroup == "" {
		return "", false, ErrInternalProfileNotFound
	}
	return finalGroup, applied, nil
}

// SubmitCollabRollup 将本次协作评估并入历史滚动平均后写回。
func (s *InternalProfileService) SubmitCollabRollup(
	ctx context.Context, userID int64, input CollabRollupInput,
) error {
	if userID <= 0 {
		return invalidCollabInput("用户ID非法")
	}
	level := strings.TrimSpace(input.Level)
	if level == "" {
		return invalidCollabInput("协作等级不能为空")
	}
	if len(level) > 10 {
		return invalidCollabInput("协作等级长度不能超过 10")
	}
	if err := validateScore(input.Score); err != nil {
		return err
	}
	if err := validateRadar(input.Radar); err != nil {
		return err
	}

	old, err := s.profiles.GetCollabProfile(ctx, userID)
	if err != nil {
		return err
	}
	if old == nil {
		return ErrInternalProfileNotFound
	}

	avgScore := input.Score
	if old.AvgCollabScore != nil {
		avgScore = *old.AvgCollabScore*(1-collabRollupAlpha) + input.Score*collabRollupAlpha
	}
	newRadar := computeRollingRadar(old.CollabRadar, input.Radar, collabRollupAlpha)

	radarJSON, err := json.Marshal(newRadar)
	if err != nil {
		return fmt.Errorf("序列化协作雷达失败: %w", err)
	}
	found, err := s.profiles.UpdateCollabRollup(ctx, userID, radarJSON, avgScore, level)
	if err != nil {
		return err
	}
	if !found {
		return ErrInternalProfileNotFound
	}
	return nil
}

// validateScore 校验协作分是 0-100 的有限数值。
func validateScore(score float64) error {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return invalidCollabInput("协作分必须是有限数值")
	}
	if score < 0 || score > 100 {
		return invalidCollabInput("协作分必须在 0 到 100 之间")
	}
	return nil
}

// validateRadar 校验雷达维度和数值一一对应且非空，并拒绝非法浮点值。
func validateRadar(r RadarData) error {
	if len(r.Dimensions) == 0 || len(r.Values) == 0 {
		return invalidCollabInput("协作雷达不能为空")
	}
	if len(r.Dimensions) != len(r.Values) {
		return invalidCollabInput("协作雷达维度和数值数量不一致")
	}
	for _, value := range r.Values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return invalidCollabInput("协作雷达数值必须是有限数值")
		}
	}
	return nil
}

// invalidCollabInput 构造参数类错误，供处理器映射为 400。
func invalidCollabInput(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidCollabInput, msg)
}

// computeRollingRadar 计算雷达滚动平均；旧值缺失、解析失败或维度变化时采用新值。
func computeRollingRadar(oldJSON json.RawMessage, latest RadarData, alpha float64) RadarData {
	if len(oldJSON) == 0 || string(oldJSON) == "null" {
		return latest
	}
	var old RadarData
	if err := json.Unmarshal(oldJSON, &old); err != nil {
		return latest
	}
	if len(old.Values) != len(latest.Values) {
		return latest
	}
	values := make([]float64, len(latest.Values))
	for i := range latest.Values {
		values[i] = old.Values[i]*(1-alpha) + latest.Values[i]*alpha
	}
	return RadarData{Dimensions: latest.Dimensions, Values: values}
}
