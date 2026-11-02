package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/quenching/review-service/internal/model"
)

// validMethodSections 是 method-v3 六章齐全的合法 AI 返回。
const validMethodSections = `{"root_cause":"根因结论","timeline":"时间线结论","path_comparison":"路径对比结论","highlights_pitfalls":"亮点坑点结论","methodology":"方法论结论","knowledge":"知识点结论"}`

// ---------- ParseMethodSections ----------

func TestParseMethodSections_SixSections(t *testing.T) {
	s, err := ParseMethodSections(validMethodSections)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if s.RootCause != "根因结论" || s.Timeline != "时间线结论" || s.PathComparison != "路径对比结论" ||
		s.HighlightsPitfalls != "亮点坑点结论" || s.Methodology != "方法论结论" || s.Knowledge != "知识点结论" {
		t.Errorf("六章解析内容错误: %+v", s)
	}
}

func TestParseMethodSections_ExtraSectionsIgnored(t *testing.T) {
	// ai-service 八段式（含 pitfall_analysis/collab_review）：只取六章，其余容忍忽略。
	// pitfall_analysis 为数组（混合类型），解析器不应触碰它；collab_review 留待协作评估卡。
	raw := `{
		"root_cause":"根因",
		"timeline":"时间线",
		"path_comparison":"路径对比",
		"highlights_pitfalls":"亮点坑点",
		"methodology":"方法论",
		"knowledge":"知识点",
		"pitfall_analysis":[{"type":"因果误判","note":"误判索引"}],
		"collab_review":"协作"
	}`
	s, err := ParseMethodSections(raw)
	if err != nil {
		t.Fatalf("含额外章节应兼容: %v", err)
	}
	if !s.MethodFullValid() {
		t.Errorf("六章应齐全: %+v", s)
	}
}

func TestParseMethodSections_CodeFence(t *testing.T) {
	raw := "```json\n" + validMethodSections + "\n```"
	s, err := ParseMethodSections(raw)
	if err != nil {
		t.Fatalf("代码围栏应被剥离: %v", err)
	}
	if s.PathComparison != "路径对比结论" || s.Knowledge != "知识点结论" {
		t.Errorf("围栏内章节解析错误: %+v", s)
	}
}

func TestParseMethodSections_Errors(t *testing.T) {
	six := func(overrides map[string]string) string {
		vals := map[string]string{
			"root_cause":          "根因",
			"timeline":            "时间线",
			"path_comparison":     "路径对比",
			"highlights_pitfalls": "亮点坑点",
			"methodology":         "方法论",
			"knowledge":           "知识点",
		}
		for k, v := range overrides {
			if v == "" {
				delete(vals, k)
			} else {
				vals[k] = v
			}
		}
		parts := make([]string, 0, len(vals))
		for k, v := range vals {
			b, _ := json.Marshal(v)
			parts = append(parts, fmt.Sprintf("%q:%s", k, string(b)))
		}
		return "{" + strings.Join(parts, ",") + "}"
	}

	cases := []struct {
		name string
		raw  string
		want error  // want 非 nil 时按 errors.Is 断言
		as   string // "parse"/"missing"/"type" 时按具体错误类型断言
	}{
		{"空字符串", "", ErrEmptySections, ""},
		{"纯空白", "  \n\t", ErrEmptySections, ""},
		{"坏 JSON", `{not json`, nil, "parse"},
		{"缺 path_comparison", six(map[string]string{"path_comparison": ""}), nil, "missing"},
		{"缺 highlights_pitfalls", six(map[string]string{"highlights_pitfalls": ""}), nil, "missing"},
		{"缺 methodology", six(map[string]string{"methodology": ""}), nil, "missing"},
		{"缺 knowledge", six(map[string]string{"knowledge": ""}), nil, "missing"},
		{"缺 root_cause", six(map[string]string{"root_cause": ""}), nil, "missing"},
		{"highlights_pitfalls 非字符串", six(map[string]string{"highlights_pitfalls": "`123`"}), nil, "type"},
		{"knowledge 非字符串", six(map[string]string{"knowledge": "`123`"}), nil, "type"},
		{"path_comparison 空白", six(map[string]string{"path_comparison": "   "}), ErrIncompleteMethodSections, ""},
		{"methodology 空串", six(map[string]string{"methodology": ""}), nil, "missing"},
		{"knowledge 空白", six(map[string]string{"knowledge": "   "}), ErrIncompleteMethodSections, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			if strings.Contains(tc.name, "非字符串") {
				// 把占位反引号串转成裸数字，构造类型错误。
				raw = strings.ReplaceAll(raw, "\"`123`\"", "123")
			}
			_, err := ParseMethodSections(raw)
			if err == nil {
				t.Fatalf("期望错误，实际 nil")
			}
			switch tc.as {
			case "parse":
				var pe *ParseError
				if !errors.As(err, &pe) {
					t.Fatalf("期望 *ParseError，实际 %T: %v", err, err)
				}
			case "missing":
				var mf *MissingFieldError
				if !errors.As(err, &mf) {
					t.Fatalf("期望 *MissingFieldError，实际 %T: %v", err, err)
				}
			case "type":
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

// ---------- BuildMethodPrompt ----------

func TestBuildMethodPrompt_Envelope(t *testing.T) {
	tpl := NewStaticProvider().TemplateByFaultType("cpu")
	if tpl.IsGeneric() {
		t.Fatalf("fault_type=cpu 应命中专属模板，实际 generic")
	}
	p := BuildMethodPrompt(sampleInput(), tpl)
	if p.FaultType != "cpu" || p.PitfallJSON != "[]" {
		t.Fatalf("Prompt 基础字段错误: %+v", p)
	}

	var env promptEnvelope
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("信封 JSON 非法: %v, raw=%s", err, p.TimelineJSON)
	}
	if env.Kind != "basic_review_input" {
		t.Errorf("Kind = %q，仍须复用 basic_review_input（Python 编排层零改动）", env.Kind)
	}
	if env.Level != model.ReviewLevelMethod {
		t.Errorf("Level = %q, want %q", env.Level, model.ReviewLevelMethod)
	}
	wantInstruction := fmt.Sprintf(methodSystemInstructionTemplate, tpl.PromptHints())
	if env.Instruction != wantInstruction {
		t.Errorf("Instruction 未按故障模板注入：%q", env.Instruction)
	}
	// 六章键名与模板要点都应出现在指令里。
	for _, want := range []string{
		"highlights_pitfalls", "methodology", "knowledge",
		"方法论提炼参考框架", "知识点梳理关注点", "亮点坑点评判关注点",
	} {
		if !strings.Contains(env.Instruction, want) {
			t.Errorf("指令缺少 %q", want)
		}
	}
	if len(env.Timeline) != 1 || len(env.Scripts) != 1 {
		t.Errorf("时间线/剧本未透传: %+v", env)
	}
}

func TestBuildMethodPrompt_GenericFallback(t *testing.T) {
	// 未知故障类型：Provider 回退 generic，构建不报错且指令仍含六章。
	tpl := NewStaticProvider().TemplateByFaultType("某种未登记故障")
	if !tpl.IsGeneric() {
		t.Fatalf("未登记类型应回退 generic，实际 key=%s", tpl.Key)
	}
	p := BuildMethodPrompt(sampleInput(), tpl)
	var env promptEnvelope
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("信封 JSON 非法: %v", err)
	}
	if !strings.Contains(env.Instruction, `"highlights_pitfalls"`) ||
		!strings.Contains(env.Instruction, `"knowledge"`) {
		t.Errorf("generic 模板的指令仍须要求六章: %s", env.Instruction)
	}
}

func TestBuildPrompt_BasicOmitsLevel(t *testing.T) {
	// 回归：基础复盘信封不得出现 level 键（omitempty）。
	p := BuildPrompt(sampleInput())
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(p.TimelineJSON), &env); err != nil {
		t.Fatalf("信封 JSON 非法: %v", err)
	}
	if _, ok := env["level"]; ok {
		t.Errorf("基础复盘信封不应包含 level 键")
	}
}

// ---------- MethodReviewGenerator ----------

func TestMethodReviewGenerator_PromptVersion(t *testing.T) {
	g := NewMethodReviewGenerator(&fakeInputProvider{in: sampleInput()}, &fakeAIClient{sections: validMethodSections}, NewStaticProvider())
	if g.PromptVersion() != MethodPromptVersion {
		t.Fatalf("PromptVersion = %q, want %q", g.PromptVersion(), MethodPromptVersion)
	}
	if MethodPromptVersion != "method-v3" {
		t.Fatalf("MethodPromptVersion = %q, want method-v3", MethodPromptVersion)
	}
}

func TestMethodReviewGenerator_Generate_Success(t *testing.T) {
	inputs := &fakeInputProvider{in: sampleInput()}
	ai := &fakeAIClient{sections: validMethodSections}
	g := NewMethodReviewGenerator(inputs, ai, NewStaticProvider())

	task := &ReviewTask{ReviewID: 7, SessionID: 100}
	title, summary, overall, tech, collab, duration, err := g.Generate(context.Background(), task)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if title != "CPU 负载高 复盘" {
		t.Errorf("title = %q", title)
	}
	// 七返回值仍只承载两章：summary=根因、overall=时间线。
	if summary != "根因结论" || overall != "时间线结论" {
		t.Errorf("summary=%q overall=%q", summary, overall)
	}
	if tech != 0 || collab != 0 || duration < 0 {
		t.Errorf("评分/耗时异常: tech=%v collab=%v duration=%d", tech, collab, duration)
	}

	// 其余四章经扩展端口取走，六章齐全。
	s := g.MethodSections()
	if s == nil {
		t.Fatal("成功后 MethodSections 不应为 nil")
	}
	if s.RootCause != "根因结论" || s.Timeline != "时间线结论" || s.PathComparison != "路径对比结论" ||
		s.HighlightsPitfalls != "亮点坑点结论" || s.Methodology != "方法论结论" || s.Knowledge != "知识点结论" {
		t.Errorf("缓存六章内容错误: %+v", s)
	}

	// 发给 ai-service 的信封必须带 level=method，且按 cpu 故障注入了模板要点。
	var env promptEnvelope
	if err := json.Unmarshal([]byte(ai.gotPrompt.TimelineJSON), &env); err != nil {
		t.Fatalf("AI 入参信封非法: %v", err)
	}
	if env.Level != model.ReviewLevelMethod {
		t.Errorf("AI 信封 level=%q, want method", env.Level)
	}
	tpl := NewStaticProvider().TemplateByFaultType("cpu")
	if env.Instruction != fmt.Sprintf(methodSystemInstructionTemplate, tpl.PromptHints()) {
		t.Errorf("AI 信封指令未注入 cpu 模板要点")
	}
	if ai.gotID != 100 {
		t.Errorf("AI 入参 sessionID = %d, want 100", ai.gotID)
	}
}

// TestMethodReviewGenerator_NilTemplatesFallback 验证传 nil Provider 时内部兜底，
// 装配层漏配模板也不影响生成（防御性）。
func TestMethodReviewGenerator_NilTemplatesFallback(t *testing.T) {
	g := NewMethodReviewGenerator(&fakeInputProvider{in: sampleInput()}, &fakeAIClient{sections: validMethodSections}, nil)
	if _, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1}); err != nil {
		t.Fatalf("nil Provider 应回退静态注册表，实际错误: %v", err)
	}
}

// TestMethodReviewGenerator_FailureClearsCache 验证失败不残留：
// 在同一生成器上先成功一次写入缓存，再改写 fake 触发各类失败，
// MethodSections 必须回到 nil（Generate 开头 reset + 失败不 store，重试安全）。
func TestMethodReviewGenerator_FailureClearsCache(t *testing.T) {
	cases := []struct {
		name   string
		induce func(inputs *fakeInputProvider, ai *fakeAIClient)
		wantAs func(error) bool
	}{
		{
			name: "取数失败",
			induce: func(inputs *fakeInputProvider, _ *fakeAIClient) {
				inputs.err = errors.New("pg down")
			},
			wantAs: func(err error) bool { var e *InputError; return errors.As(err, &e) },
		},
		{
			name: "AI 调用失败",
			induce: func(_ *fakeInputProvider, ai *fakeAIClient) {
				ai.err = errors.New("grpc down")
			},
			wantAs: func(err error) bool { var e *AICallError; return errors.As(err, &e) },
		},
		{
			name: "章节解析失败",
			induce: func(_ *fakeInputProvider, ai *fakeAIClient) {
				ai.sections = "{bad"
			},
			wantAs: func(err error) bool { var e *ParseError; return errors.As(err, &e) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputs := &fakeInputProvider{in: sampleInput()}
			ai := &fakeAIClient{sections: validMethodSections}
			g := NewMethodReviewGenerator(inputs, ai, NewStaticProvider())

			// 先成功一次，制造残留缓存。
			if _, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1}); err != nil {
				t.Fatalf("前置成功生成失败: %v", err)
			}
			if g.MethodSections() == nil {
				t.Fatal("前置成功后缓存应非 nil")
			}

			// 改写同一批 fake 触发失败。
			tc.induce(inputs, ai)

			_, _, _, _, _, _, err := g.Generate(context.Background(), &ReviewTask{SessionID: 1})
			if err == nil {
				t.Fatal("期望失败上抛")
			}
			if !tc.wantAs(err) {
				t.Errorf("错误类型不符: %T: %v", err, err)
			}
			if g.MethodSections() != nil {
				t.Errorf("失败后 MethodSections 必须为 nil，实际 %+v", g.MethodSections())
			}
		})
	}
}
