package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quenching/review-service/internal/model"
)

func TestBuildPrompt_FullInput(t *testing.T) {
	in := &model.ReviewInput{
		SessionID:  42,
		FaultType:  "cpu",
		Difficulty: "hard",
		Timeline: []model.TimelineOperation{
			{OffsetSec: 3, Type: "view_metric", Category: "observe", Target: "api", Result: "executed"},
		},
		Scripts: []model.FaultScript{
			{DefectID: 9, Code: "cpu-load-high", Name: "CPU 负载高", Category: "cpu", Status: "fixed"},
		},
		Metrics: []model.MetricSnapshot{
			{OffsetSec: 5, Name: "cpu_percent", Value: 95.2},
		},
	}

	p := BuildPrompt(in)
	if p.FaultType != "cpu" {
		t.Fatalf("FaultType = %q, want cpu", p.FaultType)
	}
	if p.PitfallJSON != "[]" {
		t.Fatalf("PitfallJSON = %q, want []", p.PitfallJSON)
	}

	var env promptEnvelope
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("envelope json invalid: %v, raw=%s", err, p.TimelineJSON)
	}
	if env.Kind != "basic_review_input" {
		t.Errorf("Kind = %q, want basic_review_input", env.Kind)
	}
	if env.Instruction != systemInstruction {
		t.Errorf("Instruction 未透传 systemInstruction")
	}
	if env.SessionID != 42 || env.FaultType != "cpu" || env.Difficulty != "hard" {
		t.Errorf("信封元数据错误: %+v", env)
	}
	if len(env.Timeline) != 1 || env.Timeline[0].Type != "view_metric" || env.Timeline[0].OffsetSec != 3 {
		t.Errorf("时间线未透传: %+v", env.Timeline)
	}
	if len(env.Scripts) != 1 || env.Scripts[0].Code != "cpu-load-high" {
		t.Errorf("剧本未透传: %+v", env.Scripts)
	}
	if len(env.Metrics) != 1 || env.Metrics[0].Value != 95.2 {
		t.Errorf("指标未透传: %+v", env.Metrics)
	}
}

func TestBuildPrompt_NilAndEmpty(t *testing.T) {
	// nil 输入也必须产出可解析的最小信封，不能 panic。
	p := BuildPrompt(nil)
	if p.FaultType != "" || p.PitfallJSON != "[]" {
		t.Fatalf("nil 输入兜底错误: %+v", p)
	}
	var env promptEnvelope
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("nil 输入信封非法: %v", err)
	}
	if env.Kind != "basic_review_input" {
		t.Errorf("Kind = %q", env.Kind)
	}
	// 空集合序列化为 [] 而非 null。
	if len(env.Timeline) != 0 || len(env.Scripts) != 0 {
		t.Errorf("空集合应为空切片: %+v", env)
	}

	// 空 ReviewInput 同样降级为空集合。
	p2 := BuildPrompt(&model.ReviewInput{SessionID: 7})
	var env2 promptEnvelope
	if err := json.Unmarshal([]byte(p2.TimelineJSON), &env2); err != nil {
		t.Fatalf("空输入信封非法: %v", err)
	}
	if env2.SessionID != 7 {
		t.Errorf("SessionID = %d, want 7", env2.SessionID)
	}
}

func TestBuildMethodPrompt_HintsAndV3Constraints(t *testing.T) {
	in := &model.ReviewInput{
		SessionID: 9,
		FaultType: "slow_sql",
		Timeline: []model.TimelineOperation{
			{OffsetSec: 10, Type: "view_metric", Category: "observe", Target: "db", Result: "executed"},
		},
	}
	tpl := NewStaticProvider().TemplateByFaultType("slow_sql")

	p := BuildMethodPrompt(in, tpl)
	var env promptEnvelope
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("信封非法: %v", err)
	}
	if env.Level != model.ReviewLevelMethod {
		t.Errorf("Level = %q, want %q", env.Level, model.ReviewLevelMethod)
	}
	// T3-3：按故障类型选中的模板要点必须注入 system instruction。
	for _, frag := range []string{"【按故障类型的复盘关注点】", "方法论提炼参考框架：", tpl.Methodology} {
		if !strings.Contains(env.Instruction, frag) {
			t.Errorf("method instruction 缺少片段 %q", frag)
		}
	}
	// T3-3 新增：方法论/知识点必须锚定本次证据，不得罗列无关知识清单。
	if !strings.Contains(env.Instruction, "不要罗列与本次故障无关的知识清单") {
		t.Error("method-v3 缺少证据绑定约束")
	}
}

func TestParseSections_TwoSection(t *testing.T) {
	s, err := ParseSections(`{"root_cause":"根因正文","timeline":"时间线正文"}`)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if s.RootCause != "根因正文" || s.Timeline != "时间线正文" {
		t.Errorf("解析内容错误: %+v", s)
	}
}

func TestParseSections_SixSectionCompatible(t *testing.T) {
	// ai-service 现行六段式：多余章节容忍，只取前两章。
	raw := `{
		"root_cause":"根因",
		"timeline":"时间线",
		"path_comparison":"路径对比",
		"methodology":"方法论",
		"pitfall_analysis":[],
		"collab_review":"协作"
	}`
	s, err := ParseSections(raw)
	if err != nil {
		t.Fatalf("六段式应兼容: %v", err)
	}
	if s.RootCause != "根因" || s.Timeline != "时间线" {
		t.Errorf("内容错误: %+v", s)
	}
}

func TestParseSections_CodeFence(t *testing.T) {
	raw := "```json\n{\"root_cause\":\"根因\",\"timeline\":\"时间线\"}\n```"
	s, err := ParseSections(raw)
	if err != nil {
		t.Fatalf("代码围栏应被剥离: %v", err)
	}
	if s.RootCause != "根因" || s.Timeline != "时间线" {
		t.Errorf("内容错误: %+v", s)
	}
}

func TestParseSections_Errors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"空字符串", "", ErrEmptySections},
		{"纯空白", "   \n\t ", ErrEmptySections},
		{"坏 JSON", `{not json`, nil}, // ParseError
		{"缺 root_cause", `{"timeline":"时间线"}`, nil},
		{"缺 timeline", `{"root_cause":"根因"}`, nil},
		{"root_cause 非字符串", `{"root_cause":123,"timeline":"x"}`, nil},
		{"root_cause 空白", `{"root_cause":"  ","timeline":"时间线"}`, ErrIncompleteSections},
		{"timeline 空白", `{"root_cause":"根因","timeline":""}`, ErrIncompleteSections},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSections(tc.raw)
			if err == nil {
				t.Fatalf("期望错误，实际 nil")
			}
			switch tc.name {
			case "坏 JSON":
				var pe *ParseError
				if !errors.As(err, &pe) {
					t.Fatalf("期望 *ParseError，实际 %T: %v", err, err)
				}
			case "缺 root_cause", "缺 timeline":
				var mf *MissingFieldError
				if !errors.As(err, &mf) {
					t.Fatalf("期望 *MissingFieldError，实际 %T: %v", err, err)
				}
			case "root_cause 非字符串":
				var ft *FieldTypeError
				if !errors.As(err, &ft) {
					t.Fatalf("期望 *FieldTypeError，实际 %T: %v", err, err)
				}
			default:
				if !errors.Is(err, tc.want) {
					t.Fatalf("期望 %v，实际 %v", tc.want, err)
				}
			}
		})
	}
}
