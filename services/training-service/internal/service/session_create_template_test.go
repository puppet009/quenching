package service

import (
	"context"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// templateCreateStore 仅实现 CreateSession 的最小 SessionStore，
// 其余方法经 nil 接口嵌入留空（模板创建路径不会调用它们）。
type templateCreateStore struct {
	SessionStore
	created []CreateSessionInput
	nextID  int64
}

func (s *templateCreateStore) CreateSession(_ context.Context, in CreateSessionInput) (int64, error) {
	s.created = append(s.created, in)
	s.nextID++
	return s.nextID, nil
}

// TestCreateTemplateSessionSkipsCalibration 复现线网现场：用户已有一场
// 无故障的首场会话（第二场 attempt），旧逻辑 PrepareSessionPlan 会报
// "首场会话未挂载任何故障，无法做难度校准"。修复后模板实训直接普通创建。
func TestCreateTemplateSessionSkipsCalibration(t *testing.T) {
	planner, groups, sessions, _ := newPlanner()

	// 预置：用户 2 分到 A 组，已有一场 AI 模式 none 的首场无故障会话。
	groups.set(2, model.GroupA)
	sessions.addSession(2, 0, model.GroupA, model.AIModeNone)

	store := &templateCreateStore{nextID: 10}
	svc := NewSessionService(store, planner, nil)

	out, err := svc.Create(context.Background(), 2, CreateSessionRequest{
		ProjectID:    0,
		TemplateSlug: "nginx-baseline",
		Difficulty:   "normal",
	})
	if err != nil {
		t.Fatalf("模板实训创建失败: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("期望落库 1 条会话，实际 %d", len(store.created))
	}
	got := store.created[0]
	if got.TemplateSlug != "nginx-baseline" {
		t.Fatalf("template_slug 透传错误: %q", got.TemplateSlug)
	}
	if got.AIMode != string(model.AIModeAssist) {
		t.Fatalf("模板实训默认应为 AI 协作模式，实际 %q", got.AIMode)
	}
	if out.AIMode != string(model.AIModeAssist) {
		t.Fatalf("响应 AIMode 错误: %q", out.AIMode)
	}
	if out.Status != string(model.StatusPending) {
		t.Fatalf("响应 Status 错误: %q", out.Status)
	}
}

// TestCreateMissingProjectAndTemplate 两者皆空时返回参数哨兵错误（校验在 planner 之前）。
func TestCreateMissingProjectAndTemplate(t *testing.T) {
	svc := NewSessionService(&templateCreateStore{}, nil, nil)
	if _, err := svc.Create(context.Background(), 2, CreateSessionRequest{}); err != ErrSessionProjectRequired {
		t.Fatalf("期望 ErrSessionProjectRequired，实际 %v", err)
	}
}
