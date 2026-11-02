package service

import (
	"errors"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

func TestPlanAIMode(t *testing.T) {
	cases := []struct {
		group   model.ControlGroup
		attempt int
		want    model.AIMode
		wantErr bool
	}{
		{model.GroupA, AttemptFirst, model.AIModeNone, false},
		{model.GroupA, AttemptSecond, model.AIModeAssist, false},
		{model.GroupB, AttemptFirst, model.AIModeAssist, false},
		{model.GroupB, AttemptSecond, model.AIModeNone, false},
		{model.GroupA, 3, "", true},            // 第 3 次起不属交叉设计
		{model.GroupB, 0, "", true},            // 序号非法
		{model.ControlGroup("X"), 1, "", true}, // 分组非法
		{model.ControlGroup(""), 1, "", true},
	}
	for _, c := range cases {
		got, err := PlanAIMode(c.group, c.attempt)
		if c.wantErr {
			if err == nil {
				t.Errorf("PlanAIMode(%q,%d) 期望报错，实际 %s", c.group, c.attempt, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("PlanAIMode(%q,%d) 意外报错: %v", c.group, c.attempt, err)
		}
		if got != c.want {
			t.Errorf("PlanAIMode(%q,%d) = %s, 期望 %s", c.group, c.attempt, got, c.want)
		}
	}
}

func TestModesComplement(t *testing.T) {
	if !ModesComplement(model.AIModeNone, model.AIModeAssist) {
		t.Error("none/assistant 应互补")
	}
	if !ModesComplement(model.AIModeAssist, model.AIModeNone) {
		t.Error("assistant/none 应互补")
	}
	if ModesComplement(model.AIModeNone, model.AIModeNone) {
		t.Error("none/none 不互补")
	}
	if ModesComplement(model.AIModeAssist, model.AIModeAssist) {
		t.Error("assistant/assistant 不互补")
	}
}

func TestChoosePairedDefect(t *testing.T) {
	first := PairingDefect{ID: 1, Code: "db-slow-1", Category: "database", Difficulty: "easy"}
	used := map[int64]bool{1: true}
	candidates := []PairingDefect{
		{ID: 1, Code: "db-slow-1", Category: "database", Difficulty: "easy"}, // 自身，排除
		{ID: 2, Code: "net-loss", Category: "network", Difficulty: "easy"},   // 不同类
		{ID: 3, Code: "db-hard", Category: "database", Difficulty: "hard"},   // 不同难度
		{ID: 5, Code: "db-lock-5", Category: "database", Difficulty: "easy"}, // 合格但 id 大
		{ID: 4, Code: "db-lock-4", Category: "database", Difficulty: "easy"}, // 合格且 id 最小
	}
	got, err := ChoosePairedDefect(first, used, candidates)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if got.ID != 4 {
		t.Errorf("应选 id=4 的同类同难度最小 id 故障，实际 %d(%s)", got.ID, got.Code)
	}
}

func TestChoosePairedDefect_NoCandidate(t *testing.T) {
	first := PairingDefect{ID: 1, Category: "database", Difficulty: "easy"}
	// 只有不同类/不同难度候选，不得放宽校准条件。
	candidates := []PairingDefect{
		{ID: 2, Category: "network", Difficulty: "easy"},
		{ID: 3, Category: "database", Difficulty: "hard"},
	}
	if _, err := ChoosePairedDefect(first, map[int64]bool{1: true}, candidates); err == nil {
		t.Error("无同类同难度候选时应报错，而不是放宽条件")
	}

	// 首场故障缺失无法定锚点。
	if _, err := ChoosePairedDefect(PairingDefect{}, nil, candidates); err == nil {
		t.Error("首场故障为空时应报错")
	}
}

// 确保跨包可见的哨兵错误可 errors.Is 比较。
func TestErrBeyondCrossGroupSentinel(t *testing.T) {
	if !errors.Is(ErrBeyondCrossGroup, ErrBeyondCrossGroup) {
		t.Error("哨兵错误应支持 errors.Is")
	}
}
