package service

import "testing"

func TestFeedbackValidation(t *testing.T) {
	score := 11
	in := FeedbackInput{TrainingID: 1, Score: &score}
	if ValidateFeedback(&in, true) == nil {
		t.Fatal("score out of range accepted")
	}
	score = 0
	if err := ValidateFeedback(&in, true); err != nil {
		t.Fatal(err)
	}
	in.TrainingID = 0
	if ValidateFeedback(&in, true) == nil {
		t.Fatal("missing training accepted")
	}
}
