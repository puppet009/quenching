package model

type FeedbackInput struct {
	TrainingID    int64   `json:"training_id"`
	Score         *int    `json:"score"`
	ValueFeedback *string `json:"value_feedback"`
	Value         *string `json:"value"`
	Comment       *string `json:"comment"`
}
