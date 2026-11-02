package service

import (
	"context"
	"fmt"
	"github.com/quenching/training-service/internal/model"
)

type FeedbackInput = model.FeedbackInput

func ValidateFeedback(in *FeedbackInput, nps bool) error {
	if in.TrainingID <= 0 {
		return fmt.Errorf("训练 ID 非法")
	}
	if nps && in.Score == nil {
		return fmt.Errorf("请填写 NPS 分数")
	}
	if in.Score != nil && (*in.Score < 0 || *in.Score > 10) {
		return fmt.Errorf("分数应为 0–10")
	}
	if in.Value != nil {
		in.ValueFeedback = in.Value
	}
	if in.ValueFeedback != nil && *in.ValueFeedback != "valuable" && *in.ValueFeedback != "not_valuable" {
		return fmt.Errorf("价值评价非法")
	}
	if !nps && in.ValueFeedback == nil {
		return fmt.Errorf("请填写价值评价")
	}
	if in.Comment != nil && len([]rune(*in.Comment)) > 2000 {
		return fmt.Errorf("评论不能超过 2000 字")
	}
	return nil
}

type FeedbackStore interface {
	Save(context.Context, int64, FeedbackInput) (int64, error)
	List(context.Context, int, int, int, int, *bool) (map[string]interface{}, error)
}
