package service

import (
	"github.com/quenching/training-service/internal/model"
	"testing"
)

func TestFirstCalibrationIsOneEasyFault(t *testing.T) {
	list := []BrainTrainableFault{{DefectID: 1, Difficulty: "easy"}, {DefectID: 2, Difficulty: "hard"}}
	if ValidateCalibrationFaults(0, []int64{1}, list) != nil {
		t.Fatal("easy calibration refused")
	}
	for _, ids := range [][]int64{{2}, {1, 2}} {
		if ValidateCalibrationFaults(0, ids, list) == nil {
			t.Fatal("difficult first calibration accepted")
		}
	}
}
func TestDirectorRepairKeepsSessionRunning(t *testing.T) {
	s := &model.Session{Status: model.StatusRunning}
	l := NewSessionLifecycle(s, nil)
	l.SetDirectorMode(true)
	if e := l.MarkFaultResolved(); e != nil {
		t.Fatal(e)
	}
	if s.Status != model.StatusRunning {
		t.Fatal("first challenge ended the session")
	}
}
