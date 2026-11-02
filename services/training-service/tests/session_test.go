// Package tests training-service 会话状态机单测（无集群/无DB）。
package tests

import (
	"math/rand"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func newSession(status model.SessionStatus) *model.Session {
	return &model.Session{ID: 1, UserID: 10, ProjectID: 100, Status: status}
}

func TestStateTransitions(t *testing.T) {
	cases := []struct {
		name string
		from model.SessionStatus
		to   model.SessionStatus
		want bool
	}{
		{"pending->deploying", model.StatusPending, model.StatusDeploying, true},
		{"pending->running 跳级", model.StatusPending, model.StatusRunning, false},
		{"deploying->armed", model.StatusDeploying, model.StatusArmed, true},
		{"armed->running", model.StatusArmed, model.StatusRunning, true},
		{"running->resolved", model.StatusRunning, model.StatusResolved, true},
		{"resolved->completed", model.StatusResolved, model.StatusCompleted, true},
		{"running->completed", model.StatusRunning, model.StatusCompleted, true},
		{"任意活跃->aborted", model.StatusRunning, model.StatusAborted, true},
		{"completed终态不可回", model.StatusCompleted, model.StatusRunning, false},
		{"aborted终态不可回", model.StatusAborted, model.StatusRunning, false},
		{"failed终态不可回", model.StatusFailed, model.StatusRunning, false},
		{"同态幂等", model.StatusRunning, model.StatusRunning, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := model.CanTransition(c.from, c.to); got != c.want {
				t.Errorf("CanTransition(%s->%s)=%v want %v", c.from, c.to, got, c.want)
			}
		})
	}
}

func TestHappyPathLifecycle(t *testing.T) {
	clock := fixedClock{t: time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)}
	lc := service.NewSessionLifecycle(newSession(model.StatusPending), clock)

	if err := lc.DeployBegin(5001); err != nil {
		t.Fatal(err)
	}
	if lc.Session().SandboxID != 5001 {
		t.Error("沙箱ID未记录")
	}
	if err := lc.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := lc.Start(); err != nil {
		t.Fatal(err)
	}
	if lc.Session().StartedAt == nil {
		t.Error("开始时间应记录")
	}
	if err := lc.MarkFaultResolved(); err != nil {
		t.Fatal(err)
	}
	if !lc.Session().FaultResolved || lc.Session().ResolvedAt == nil {
		t.Error("修复标记/时间缺失")
	}
	if err := lc.Complete(); err != nil {
		t.Fatal(err)
	}
	if lc.Session().Status != model.StatusCompleted || lc.Session().CompletedAt == nil {
		t.Fatal("应完成并记录时间")
	}
}

func TestIllegalJumpRejected(t *testing.T) {
	lc := service.NewSessionLifecycle(newSession(model.StatusPending), fixedClock{})
	// pending 不能直接标修复
	if err := lc.MarkFaultResolved(); err == nil {
		t.Fatal("pending->resolved 应被拒绝")
	}
	if lc.Session().Status != model.StatusPending {
		t.Fatal("非法流转不应改变状态")
	}
}

func TestAbortIdempotentAndFromAny(t *testing.T) {
	clock := fixedClock{}
	lc := service.NewSessionLifecycle(newSession(model.StatusRunning), clock)
	if err := lc.Abort(); err != nil {
		t.Fatal(err)
	}
	if lc.Session().Status != model.StatusAborted {
		t.Fatal("应 aborted")
	}
	// 终态再 Abort 幂等不报错
	if err := lc.Abort(); err != nil {
		t.Fatalf("终态Abort应幂等: %v", err)
	}
}

func TestScoresOnlyAfterResolved(t *testing.T) {
	lc := service.NewSessionLifecycle(newSession(model.StatusRunning), fixedClock{})
	tech, collab := 80.0, 75.5
	// running 中途不允许写最终分
	if err := lc.ApplyScores(&tech, &collab); err == nil {
		t.Fatal("running 不应允许写最终评分")
	}
	if err := lc.MarkFaultResolved(); err != nil {
		t.Fatal(err)
	}
	if err := lc.ApplyScores(&tech, &collab); err != nil {
		t.Fatalf("resolved 应允许写分: %v", err)
	}
	if lc.Session().TechScore == nil || *lc.Session().TechScore != 80 {
		t.Error("技术分未写入")
	}
	if lc.Session().CollabScore == nil || *lc.Session().CollabScore != 75.5 {
		t.Error("协作分未写入")
	}
}

func TestControlGroupAssignmentImmutable(t *testing.T) {
	lc := service.NewSessionLifecycle(newSession(model.StatusPending), fixedClock{})
	if err := lc.AssignGroup(model.GroupA); err != nil {
		t.Fatal(err)
	}
	if err := lc.AssignGroup(model.GroupB); err == nil {
		t.Fatal("分组确定后不可改")
	}
	if err := lc.AssignGroup("X"); err == nil {
		t.Fatal("非法分组应报错")
	}
}

func TestAssignRandomGroupDistribution(t *testing.T) {
	a, b := 0, 0
	// 固定种子可复现；大样本下两组都应出现
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 1000; i++ {
		if service.AssignRandomGroup(r) == model.GroupA {
			a++
		} else {
			b++
		}
	}
	if a == 0 || b == 0 {
		t.Fatalf("A/B 两组都应被分到，A=%d B=%d", a, b)
	}
	if a < 400 || b < 400 {
		t.Fatalf("分组应大致均衡，A=%d B=%d", a, b)
	}
}

func TestOccupiesSandbox(t *testing.T) {
	active := []model.SessionStatus{
		model.StatusDeploying, model.StatusArmed, model.StatusRunning, model.StatusResolved,
	}
	for _, s := range active {
		sess := newSession(s)
		if !sess.OccupiesSandbox() {
			t.Errorf("%s 应占用沙箱", s)
		}
	}
	for _, s := range []model.SessionStatus{model.StatusPending, model.StatusCompleted, model.StatusAborted, model.StatusFailed} {
		sess := newSession(s)
		if sess.OccupiesSandbox() {
			t.Errorf("%s 不应占用沙箱", s)
		}
	}
}

func TestLinkPair(t *testing.T) {
	lc := service.NewSessionLifecycle(newSession(model.StatusPending), fixedClock{})
	if err := lc.LinkPair(0); err == nil {
		t.Fatal("配对ID为0应报错")
	}
	if err := lc.LinkPair(88); err != nil {
		t.Fatal(err)
	}
	if lc.Session().PairSessionID != 88 {
		t.Error("配对会话未绑定")
	}
}
