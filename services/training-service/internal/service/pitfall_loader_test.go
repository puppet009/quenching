package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ---- 测试用最小合法配置块 ----

const validTrigger = `{
  "when": "always",
  "probability": 1,
  "max_times": 1
}`
const validRecognition = `{
  "dispute_keywords": ["不对", "为什么"],
  "independent_verify_actions": ["view_log", "view_metric"]
}`
const validFall = `{
  "adopt_actions": ["exec_command"],
  "adopt_keywords": ["就按你说的"]
}`

func row(id, defectID int64, ptype, status string, order int, trigger, recog, fall []byte) model.PitfallStoredRow {
	return model.PitfallStoredRow{
		ID:               id,
		DefectID:         defectID,
		PitfallType:      ptype,
		Name:             "测试坑点",
		TriggerCondition: trigger,
		ErrorContent:     "错误建议模板",
		ConfidenceLevel:  model.ConfidenceRandom,
		RecognitionRules: recog,
		PitfallRules:     fall,
		Severity:         model.SeverityMedium,
		Status:           status,
		SortOrder:        order,
	}
}

type memStore struct {
	rows   []model.PitfallStoredRow
	err    error
	gotIDs []int64
}

func (m *memStore) ListActiveByDefectIDs(_ context.Context, ids []int64) ([]model.PitfallStoredRow, error) {
	m.gotIDs = ids
	if m.err != nil {
		return nil, m.err
	}
	return m.rows, nil
}

func validRow(id, defectID int64) model.PitfallStoredRow {
	r := row(id, defectID, model.TypeCausalMisjudgment, model.StatusActive, 0,
		[]byte(validTrigger), []byte(validRecognition), []byte(validFall))
	r.Name = "因果误判坑"
	return r
}

func TestLoadEmptyDefects(t *testing.T) {
	store := &memStore{rows: []model.PitfallStoredRow{validRow(1, 100)}}
	l := NewLoader(store)
	got, err := l.LoadByDefects(context.Background(), nil)
	if err != nil {
		t.Fatalf("空缺陷不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空缺陷应返回空 map，实际 %v", got)
	}
	if store.gotIDs != nil {
		t.Fatalf("空缺陷不应查询仓储")
	}
}

func TestLoadGroupsAndSorts(t *testing.T) {
	// defect 100 有两个坑（乱序给出），defect 200 有一个
	r1 := validRow(1, 100)
	r1.SortOrder = 20
	r2 := validRow(2, 100)
	r2.SortOrder = 10
	r3 := validRow(3, 200)
	store := &memStore{rows: []model.PitfallStoredRow{r1, r2, r3}}

	got, err := NewLoader(store).LoadByDefects(context.Background(), []int64{100, 200, 100})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(store.gotIDs) != 2 {
		t.Fatalf("缺陷ID应去重为2个，实际 %v", store.gotIDs)
	}
	if len(got[100]) != 2 || len(got[200]) != 1 {
		t.Fatalf("分组错误: %+v", got)
	}
	// 100 组内应按 sort_order 升序：id=2(order10) 在前
	if got[100][0].ID != 2 || got[100][1].ID != 1 {
		t.Fatalf("排序错误: %d, %d", got[100][0].ID, got[100][1].ID)
	}
}

func TestLoadSameSortOrderByID(t *testing.T) {
	r1 := validRow(5, 100)
	r2 := validRow(2, 100) // 同 sort_order=0，按 id 升序
	got, err := NewLoader(&memStore{rows: []model.PitfallStoredRow{r1, r2}}).
		LoadByDefects(context.Background(), []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	if got[100][0].ID != 2 || got[100][1].ID != 5 {
		t.Fatalf("同序应按 id 升序: %d,%d", got[100][0].ID, got[100][1].ID)
	}
}

func TestLoadSkipsNonActive(t *testing.T) {
	active := validRow(1, 100)
	draft := validRow(2, 100)
	draft.Status = model.StatusDraft
	inactive := validRow(3, 100)
	inactive.Status = model.StatusInactive
	got, err := NewLoader(&memStore{rows: []model.PitfallStoredRow{active, draft, inactive}}).
		LoadByDefects(context.Background(), []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[100]) != 1 || got[100][0].ID != 1 {
		t.Fatalf("应只保留 active，实际 %+v", got[100])
	}
}

func TestLoadDeduplicatesRows(t *testing.T) {
	r := validRow(1, 100)
	got, err := NewLoader(&memStore{rows: []model.PitfallStoredRow{r, r}}).
		LoadByDefects(context.Background(), []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[100]) != 1 {
		t.Fatalf("重复行应去重，实际 %d 条", len(got[100]))
	}
}

func TestLoadStoreError(t *testing.T) {
	store := &memStore{err: errors.New("db down")}
	_, err := NewLoader(store).LoadByDefects(context.Background(), []int64{100})
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("应透传仓储错误，实际 %v", err)
	}
}

func TestParsedTriggerShape(t *testing.T) {
	r := validRow(7, 100)
	got, err := NewLoader(&memStore{rows: []model.PitfallStoredRow{r}}).
		LoadByDefects(context.Background(), []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	c := got[100][0]
	if c.Trigger.EffectiveWhen() != model.WhenAlways {
		t.Fatalf("when 归一化错误: %s", c.Trigger.When)
	}
	if c.Trigger.EffectiveProbability() != 1 {
		t.Fatal("概率解析错误")
	}
	if c.Trigger.EffectiveMaxTimes() != 1 {
		t.Fatal("max_times 解析错误")
	}
	// 未配置 requires 时默认要求故障已触发
	if !c.Trigger.RequiresFaultTriggered() {
		t.Fatal("缺省应要求故障已触发")
	}
	if c.ConfidenceLevel != model.ConfidenceRandom || c.Severity != model.SeverityMedium {
		t.Fatal("默认值回填错误")
	}
}

// ---- 非法配置：fail-fast，且错误带配置ID ----

func TestLoadRejectsBadConfigs(t *testing.T) {
	cases := map[string]func(model.PitfallStoredRow) model.PitfallStoredRow{
		"非法坑点类型": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.PitfallType = "made_up"
			return r
		},
		"缺错误内容": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.ErrorContent = ""
			return r
		},
		"trigger JSON 非法": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.TriggerCondition = []byte("{not json")
			return r
		},
		"缺 recognition 块": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.RecognitionRules = nil
			return r
		},
		"概率越界": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.TriggerCondition = []byte(`{"when":"always","probability":1.5}`)
			return r
		},
		"question_match 缺关键词": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.TriggerCondition = []byte(`{"when":"question_match","probability":1}`)
			return r
		},
		"stage_enter 缺阶段": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.TriggerCondition = []byte(`{"when":"stage_enter","probability":1}`)
			return r
		},
		"时间窗非法": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.TriggerCondition = []byte(`{"when":"delay","window":{"start_seconds":30,"end_seconds":10}}`)
			return r
		},
		"识别规则为空": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.RecognitionRules = []byte(`{}`)
			return r
		},
		"踩坑规则为空": func(r model.PitfallStoredRow) model.PitfallStoredRow {
			r.PitfallRules = []byte(`{}`)
			return r
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := mutate(validRow(99, 100))
			_, err := NewLoader(&memStore{rows: []model.PitfallStoredRow{r}}).
				LoadByDefects(context.Background(), []int64{100})
			if err == nil {
				t.Fatalf("%s 应当被拒绝", name)
			}
			if !strings.Contains(err.Error(), "99") {
				t.Fatalf("%s 错误应带配置ID，实际: %v", name, err)
			}
		})
	}
}

func TestParseDirect(t *testing.T) {
	// 直接走 Parse 的最小合法整行
	raw := []byte(`{
	  "id": 1, "defect_id": 100, "pitfall_type": "info_omission",
	  "name": "信息遗漏", "error_content": "看起来没问题",
	  "trigger_condition": {"when": "stage_enter", "stages": ["detect"], "probability": 0.5},
	  "recognition_rules": {"dispute_keywords": ["再看看"]},
	  "pitfall_rules": {"adopt_keywords": ["好的"]},
	  "confidence_level": "random", "severity": "high", "status": "active"
	}`)
	c, err := model.Parse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if c.PitfallType != model.TypeInfoOmission || c.Trigger.When != model.WhenStageEnter {
		t.Fatalf("字段解析错误: %+v", c)
	}
	if c.Trigger.EffectiveProbability() != 0.5 || len(c.Trigger.Stages) != 1 {
		t.Fatal("触发条件解析错误")
	}
}

func TestParseMalformedJSON(t *testing.T) {
	if _, err := model.Parse([]byte("{bad")); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestDefaults(t *testing.T) {
	cond := model.TriggerCondition{}
	if cond.EffectiveWhen() != model.WhenAlways {
		t.Fatal("when 缺省应为 always")
	}
	if cond.EffectiveProbability() != 1 {
		t.Fatal("概率缺省应为 1（确定性）")
	}
	if cond.EffectiveMaxTimes() != 1 {
		t.Fatal("max_times 缺省应为 1")
	}
	if !cond.RequiresFaultTriggered() {
		t.Fatal("前置缺省应要求故障已触发")
	}
	noFault := false
	cond.Requires = &model.TriggerRequires{FaultTriggered: &noFault}
	if cond.RequiresFaultTriggered() {
		t.Fatal("显式 false 应生效")
	}
}
