package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quenching/review-service/internal/model"
)

// ---------- 内存 fake ----------

type fakeInputProvider struct {
	in  *model.ReviewInput
	err error
}

func (f *fakeInputProvider) LoadReviewInput(_ context.Context, sessionID int64) (*model.ReviewInput, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.in != nil {
		f.in.SessionID = sessionID
	}
	return f.in, nil
}

type fakeAIClient struct {
	sections  string
	err       error
	gotID     int64
	gotPrompt Prompt
}

func (f *fakeAIClient) GenerateReview(_ context.Context, sessionID int64, p Prompt) (string, error) {
	f.gotID = sessionID
	f.gotPrompt = p
	if f.err != nil {
		return "", f.err
	}
	return f.sections, nil
}

// 编译期确认 fake 满足端口。
var (
	_ InputProvider  = (*fakeInputProvider)(nil)
	_ ReviewAIClient = (*fakeAIClient)(nil)
)

func sampleInput() *model.ReviewInput {
	return &model.ReviewInput{
		FaultType: "cpu",
		Timeline:  []model.TimelineOperation{{OffsetSec: 1, Type: "view_metric"}},
		Scripts: []model.FaultScript{
			{Name: "CPU 负载高", Category: "cpu"},
		},
	}
}

const validSections = `{"root_cause":"根因结论","timeline":"时间线结论"}`

func TestBasicReviewGenerator_Generate_Success(t *testing.T) {
	inputs := &fakeInputProvider{in: sampleInput()}
	ai := &fakeAIClient{sections: validSections}
	g := NewBasicReviewGenerator(inputs, ai)

	if g.PromptVersion() != PromptVersion {
		t.Fatalf("PromptVersion = %q, want %q", g.PromptVersion(), PromptVersion)
	}

	task := &ReviewTask{ReviewID: 1, SessionID: 100}
	title, summary, overall, tech, collab, duration, err := g.Generate(context.Background(), task)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}

	// 标题取首个剧本名称 + 复盘。
	if title != "CPU 负载高 复盘" {
		t.Errorf("title = %q, want %q", title, "CPU 负载高 复盘")
	}
	// 章节映射：summary=根因，overall=时间线。
	if summary != "根因结论" {
		t.Errorf("summary = %q, want 根因结论", summary)
	}
	if overall != "时间线结论" {
		t.Errorf("overall = %q, want 时间线结论", overall)
	}
	// 评分不属 T2-29，固定 0。
	if tech != 0 || collab != 0 {
		t.Errorf("评分应固定 0，实际 tech=%v collab=%v", tech, collab)
	}
	if duration < 0 {
		t.Errorf("duration = %d, 不应为负", duration)
	}
	// sessionID 透传到 AI 调用。
	if ai.gotID != 100 {
		t.Errorf("AI 入参 sessionID = %d, want 100", ai.gotID)
	}
	if ai.gotPrompt.FaultType != "cpu" || ai.gotPrompt.TimelineJSON == "" || ai.gotPrompt.PitfallJSON != "[]" {
		t.Errorf("AI Prompt 组装异常: %+v", ai.gotPrompt)
	}
}

func TestBasicReviewGenerator_Generate_DurationMeasured(t *testing.T) {
	// 用 nowFunc 注入时间，断言耗时被测量。
	orig := nowFunc
	defer func() { nowFunc = orig }()
	ticks := []time.Time{
		time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 10, 0, 1, 0, time.UTC), // +1s
	}
	idx := 0
	nowFunc = func() time.Time {
		tm := ticks[idx]
		idx++
		return tm
	}

	g := NewBasicReviewGenerator(&fakeInputProvider{in: sampleInput()}, &fakeAIClient{sections: validSections})
	_, _, _, _, _, duration, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if duration != 1000 {
		t.Errorf("duration = %d, want 1000ms", duration)
	}
}

func TestBasicReviewGenerator_TitleFallbacks(t *testing.T) {
	cases := []struct {
		name string
		in   *model.ReviewInput
		want string
	}{
		{"剧本名优先", &model.ReviewInput{
			FaultType: "network",
			Scripts:   []model.FaultScript{{Name: "网络抖动"}},
		}, "网络抖动 复盘"},
		{"无剧本名用故障类型", &model.ReviewInput{FaultType: "network"}, "network 故障复盘"},
		{"全空用固定标题", &model.ReviewInput{}, "故障演练复盘"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildTitle(tc.in)
			if got != tc.want {
				t.Errorf("buildTitle = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBasicReviewGenerator_InputError(t *testing.T) {
	boom := errors.New("pg down")
	g := NewBasicReviewGenerator(&fakeInputProvider{err: boom}, &fakeAIClient{sections: validSections})

	_, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1})
	if err == nil {
		t.Fatal("期望取数失败返回错误")
	}
	var ie *InputError
	if !errors.As(err, &ie) {
		t.Fatalf("期望 *InputError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("InputError 应 wrap 底层错误")
	}
}

func TestBasicReviewGenerator_AICallError(t *testing.T) {
	boom := errors.New("grpc unavailable")
	g := NewBasicReviewGenerator(&fakeInputProvider{in: sampleInput()}, &fakeAIClient{err: boom})

	_, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1})
	if err == nil {
		t.Fatal("期望 AI 调用失败返回错误")
	}
	var ae *AICallError
	if !errors.As(err, &ae) {
		t.Fatalf("期望 *AICallError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, boom) {
		t.Errorf("AICallError 应 wrap 底层错误")
	}
}

func TestBasicReviewGenerator_ParseErrorPropagates(t *testing.T) {
	// AI 返回坏 JSON，解析错误原样上抛（worker 据此重试）。
	g := NewBasicReviewGenerator(&fakeInputProvider{in: sampleInput()}, &fakeAIClient{sections: "{bad"})

	_, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1})
	if err == nil {
		t.Fatal("期望解析错误上抛")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("期望 *ParseError，实际 %T: %v", err, err)
	}
}
