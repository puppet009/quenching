// T3-18 坑点配置后台管理服务层单元测试。
package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
	"github.com/quenching/content-service/internal/service"
)

// ---------- 内存 fake 仓储 ----------

type fakePitfallRepo struct {
	mu     sync.Mutex
	items  map[int64]*model.PitfallStoredRow
	nextID int64
}

func newFakePitfallRepo() *fakePitfallRepo {
	return &fakePitfallRepo{
		items:  make(map[int64]*model.PitfallStoredRow),
		nextID: 100,
	}
}

// seedValid 植入一条合法配置，返回 ID
func (f *fakePitfallRepo) seedValid(defectID int64, name, status string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	now := time.Now()
	row := &model.PitfallStoredRow{
		ID:               f.nextID,
		DefectID:         defectID,
		PitfallType:      model.TypeCausalMisjudgment,
		Name:             name,
		Description:      "测试描述",
		ErrorContent:     "错误建议内容",
		ConfidenceLevel:  model.ConfidenceHigh,
		CorrectApproach:  "正确做法",
		Severity:         model.SeverityMedium,
		Status:           status,
		SortOrder:        1,
		CreatedBy:        1,
		CreatedAt:        now,
		UpdatedAt:        now,
		TriggerCondition: jsonMarshal(model.TriggerCondition{When: model.WhenAlways, Probability: 1, MaxTimes: 1}),
		RecognitionRules: jsonMarshal(model.RecognitionRules{DisputeKeywords: []string{"不对"}}),
		PitfallRules:     jsonMarshal(model.FallRules{AdoptKeywords: []string{"好的"}}),
	}
	f.items[row.ID] = row
	return row.ID
}

func (f *fakePitfallRepo) GetByID(_ context.Context, id int64) (*model.PitfallStoredRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.items[id]
	if !ok {
		return nil, repository.ErrPitfallConfigNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakePitfallRepo) List(_ context.Context, q repository.PitfallQuery) (*repository.StoredPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	all := make([]model.PitfallStoredRow, 0)
	for _, row := range f.items {
		if q.DefectID > 0 && row.DefectID != q.DefectID {
			continue
		}
		if q.PitfallType != "" && row.PitfallType != q.PitfallType {
			continue
		}
		if q.Status != "" && row.Status != q.Status {
			continue
		}
		all = append(all, *row)
	}

	total := len(all)
	page := q.Page
	if page <= 0 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	return &repository.StoredPage{
		Total:    int64(total),
		Page:     page,
		PageSize: pageSize,
		Items:    all[start:end],
	}, nil
}

func (f *fakePitfallRepo) Create(_ context.Context, cfg *model.PitfallConfig, createdBy int64) (*model.PitfallStoredRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	now := time.Now()
	row := &model.PitfallStoredRow{
		ID:               f.nextID,
		DefectID:         cfg.DefectID,
		PitfallType:      cfg.PitfallType,
		Name:             cfg.Name,
		Description:      cfg.Description,
		ErrorContent:     cfg.ErrorContent,
		ConfidenceLevel:  cfg.ConfidenceLevel,
		CorrectApproach:  cfg.CorrectApproach,
		Severity:         cfg.Severity,
		Status:           cfg.Status,
		SortOrder:        cfg.SortOrder,
		CreatedBy:        createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
		TriggerCondition: jsonMarshal(cfg.Trigger),
		RecognitionRules: jsonMarshal(cfg.Recognition),
		PitfallRules:     jsonMarshal(cfg.PitfallRules),
	}
	f.items[row.ID] = row
	cp := *row
	return &cp, nil
}

func (f *fakePitfallRepo) Update(_ context.Context, id int64, cfg *model.PitfallConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.items[id]
	if !ok {
		return repository.ErrPitfallConfigNotFound
	}
	row.DefectID = cfg.DefectID
	row.PitfallType = cfg.PitfallType
	row.Name = cfg.Name
	row.Description = cfg.Description
	row.ErrorContent = cfg.ErrorContent
	row.ConfidenceLevel = cfg.ConfidenceLevel
	row.CorrectApproach = cfg.CorrectApproach
	row.Severity = cfg.Severity
	row.Status = cfg.Status
	row.SortOrder = cfg.SortOrder
	row.UpdatedAt = time.Now()
	row.TriggerCondition = jsonMarshal(cfg.Trigger)
	row.RecognitionRules = jsonMarshal(cfg.Recognition)
	row.PitfallRules = jsonMarshal(cfg.PitfallRules)
	return nil
}

func (f *fakePitfallRepo) UpdateStatus(_ context.Context, id int64, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.items[id]
	if !ok {
		return repository.ErrPitfallConfigNotFound
	}
	row.Status = status
	row.UpdatedAt = time.Now()
	return nil
}

func (f *fakePitfallRepo) Delete(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[id]; !ok {
		return repository.ErrPitfallConfigNotFound
	}
	delete(f.items, id)
	return nil
}

func jsonMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

// ---------- 测试用例 ----------

func TestPitfallAdminCreate(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	cfg := validConfigObj(101, "测试坑点")
	result, err := svc.Create(ctx, cfg, 1)
	if err != nil {
		t.Fatalf("创建应成功，got err: %v", err)
	}
	if result.ID == 0 {
		t.Fatal("创建后应有 ID")
	}
	if result.DefectID != 101 {
		t.Errorf("defect_id 应为 101，got %d", result.DefectID)
	}
	if result.Name != "测试坑点" {
		t.Errorf("name 不匹配: %s", result.Name)
	}
	if result.Status != model.StatusDraft {
		t.Errorf("新建默认应为 draft，got %s", result.Status)
	}
	if result.CreatedBy != 1 {
		t.Errorf("created_by 应为 1，got %d", result.CreatedBy)
	}
}

func TestPitfallAdminCreateInvalid(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	// 缺少 name
	cfg := validConfigObj(101, "")
	_, err := svc.Create(ctx, cfg, 1)
	if err == nil {
		t.Fatal("缺少 name 应报错")
	}
	if !errors.Is(err, service.ErrPitfallInvalid) {
		t.Errorf("应返回 ErrPitfallInvalid，got %v", err)
	}
}

func TestPitfallAdminGet(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "种子坑点", model.StatusActive)

	cfg, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("查询应成功，got err: %v", err)
	}
	if cfg.ID != id {
		t.Errorf("ID 不匹配: %d vs %d", cfg.ID, id)
	}
	if cfg.Name != "种子坑点" {
		t.Errorf("name 不匹配: %s", cfg.Name)
	}
	if cfg.PitfallType != model.TypeCausalMisjudgment {
		t.Errorf("type 不匹配: %s", cfg.PitfallType)
	}
}

func TestPitfallAdminGetNotFound(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	_, err := svc.Get(ctx, 999)
	if err == nil {
		t.Fatal("不存在的 ID 应报错")
	}
	if !errors.Is(err, service.ErrPitfallNotFound) {
		t.Errorf("应返回 ErrPitfallNotFound，got %v", err)
	}
}

func TestPitfallAdminUpdate(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "旧名称", model.StatusDraft)

	cfg := validConfigObj(101, "新名称")
	result, err := svc.Update(ctx, id, cfg)
	if err != nil {
		t.Fatalf("更新应成功，got err: %v", err)
	}
	if result.Name != "新名称" {
		t.Errorf("name 应为 '新名称'，got %s", result.Name)
	}
}

func TestPitfallAdminUpdateNotFound(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	cfg := validConfigObj(101, "测试")
	_, err := svc.Update(ctx, 999, cfg)
	if err == nil {
		t.Fatal("不存在的 ID 应报错")
	}
	if !errors.Is(err, service.ErrPitfallNotFound) {
		t.Errorf("应返回 ErrPitfallNotFound，got %v", err)
	}
}

func TestPitfallAdminUpdateInvalid(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "测试", model.StatusDraft)

	// 更新为空 name
	cfg := validConfigObj(101, "")
	_, err := svc.Update(ctx, id, cfg)
	if err == nil {
		t.Fatal("空 name 应报错")
	}
	if !errors.Is(err, service.ErrPitfallInvalid) {
		t.Errorf("应返回 ErrPitfallInvalid，got %v", err)
	}
}

func TestPitfallAdminDelete(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "待删除", model.StatusDraft)

	err := svc.Delete(ctx, id)
	if err != nil {
		t.Fatalf("删除应成功，got err: %v", err)
	}

	// 删除后查不到
	_, err = svc.Get(ctx, id)
	if !errors.Is(err, service.ErrPitfallNotFound) {
		t.Error("删除后应查不到")
	}
}

func TestPitfallAdminDeleteNotFound(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	err := svc.Delete(ctx, 999)
	if err == nil {
		t.Fatal("不存在的 ID 应报错")
	}
	if !errors.Is(err, service.ErrPitfallNotFound) {
		t.Errorf("应返回 ErrPitfallNotFound，got %v", err)
	}
}

func TestPitfallAdminUpdateStatusToActive(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "测试", model.StatusDraft)

	err := svc.UpdateStatus(ctx, id, model.StatusActive)
	if err != nil {
		t.Fatalf("启用应成功，got err: %v", err)
	}

	// 验证状态变更
	cfg, _ := svc.Get(ctx, id)
	if cfg.Status != model.StatusActive {
		t.Errorf("状态应为 active，got %s", cfg.Status)
	}
}

func TestPitfallAdminUpdateStatusInvalid(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "测试", model.StatusDraft)

	err := svc.UpdateStatus(ctx, id, "invalid_status")
	if err == nil {
		t.Fatal("非法状态应报错")
	}
	if !errors.Is(err, service.ErrPitfallInvalid) {
		t.Errorf("应返回 ErrPitfallInvalid，got %v", err)
	}
}

func TestPitfallAdminList(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	repo.seedValid(101, "坑点A", model.StatusActive)
	repo.seedValid(101, "坑点B", model.StatusDraft)
	repo.seedValid(102, "坑点C", model.StatusActive)

	// 全量查询
	page, err := svc.List(ctx, repository.PitfallQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询应成功，got err: %v", err)
	}
	if page.Total != 3 {
		t.Errorf("总数应为 3，got %d", page.Total)
	}

	// 按缺陷筛选
	page, _ = svc.List(ctx, repository.PitfallQuery{DefectID: 101, Page: 1, PageSize: 10})
	if page.Total != 2 {
		t.Errorf("缺陷 101 应有 2 条，got %d", page.Total)
	}

	// 按状态筛选
	page, _ = svc.List(ctx, repository.PitfallQuery{Status: model.StatusActive, Page: 1, PageSize: 10})
	if page.Total != 2 {
		t.Errorf("active 状态应有 2 条，got %d", page.Total)
	}
}

func TestPitfallAdminListPagination(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		repo.seedValid(101, "坑点"+itoa(i), model.StatusActive)
	}

	// 第一页
	page1, err := svc.List(ctx, repository.PitfallQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询应成功，got err: %v", err)
	}
	if page1.Total != 25 {
		t.Errorf("总数应为 25，got %d", page1.Total)
	}
	if len(page1.Items) != 10 {
		t.Errorf("第一页应有 10 条，got %d", len(page1.Items))
	}

	// 第三页（最后一页，5 条）
	page3, _ := svc.List(ctx, repository.PitfallQuery{Page: 3, PageSize: 10})
	if len(page3.Items) != 5 {
		t.Errorf("第三页应有 5 条，got %d", len(page3.Items))
	}
}

// TestPitfallAdminListItemsParsed 验证列表返回的是解析后的强类型 Config
// （T3-18 列表契约修复）：Items 元素三块 JSONB 已结构化，可直接取 Trigger/Recognition；
// 序列化后为 snake_case 字段、trigger_condition 等是对象而非 base64 字符串，
// 且不出现 StoredRow 的 PascalCase 字段名（DefectID/CreatedAt 等）。
func TestPitfallAdminListItemsParsed(t *testing.T) {
	repo := newFakePitfallRepo()
	svc := service.NewPitfallAdminService(repo)
	ctx := context.Background()

	id := repo.seedValid(101, "契约坑点", model.StatusActive)

	page, err := svc.List(ctx, repository.PitfallQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("查询应成功，got err: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("应有 1 条，got %d", len(page.Items))
	}

	// Items 必须是解析后的 *Config，三块配置结构化可取
	item := page.Items[0]
	if item.ID != id {
		t.Errorf("ID 不匹配: %d vs %d", item.ID, id)
	}
	if item.Trigger.When != model.WhenAlways {
		t.Errorf("trigger_condition.when 应为 always，got %q", item.Trigger.When)
	}
	if item.Trigger.Probability != 1 {
		t.Errorf("trigger_condition.probability 应为 1，got %v", item.Trigger.Probability)
	}
	if len(item.Recognition.DisputeKeywords) != 1 || item.Recognition.DisputeKeywords[0] != "不对" {
		t.Errorf("recognition_rules.dispute_keywords 未正确解析: %+v", item.Recognition.DisputeKeywords)
	}
	if len(item.PitfallRules.AdoptKeywords) != 1 || item.PitfallRules.AdoptKeywords[0] != "好的" {
		t.Errorf("pitfall_rules.adopt_keywords 未正确解析: %+v", item.PitfallRules.AdoptKeywords)
	}

	// JSON 形态：snake_case 外层 + 结构化三块对象
	data, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("序列化分页失败: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		`"total":1`,
		`"page_size":10`,
		`"items":[{`,
		`"defect_id":101`,
		`"pitfall_type":"causal_misjudgment"`,
		`"trigger_condition":{"when":"always"`,
		`"recognition_rules":{"dispute_keywords":["不对"]}`,
		`"pitfall_rules":{"adopt_keywords":["好的"]}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表 JSON 缺少 %s；实际: %s", want, body)
		}
	}
	// 不得出现 StoredRow 序列化产物：PascalCase 字段名
	for _, bad := range []string{`"DefectID"`, `"PitfallType"`, `"TriggerCondition"`, `"CreatedAt"`} {
		if strings.Contains(body, bad) {
			t.Errorf("列表 JSON 不应出现 PascalCase 字段 %s；实际: %s", bad, body)
		}
	}
}

// TestPitfallAdminListCorruptRow 任一行的 JSONB 损坏（缺块/非法 JSON）时，
// 整页返回 ErrPitfallInvalid，避免前端拿到无法解析的半成品列表。
func TestPitfallAdminListCorruptRow(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		tamper func(row *model.PitfallStoredRow)
	}{
		{
			name:   "缺少 trigger_condition 块",
			tamper: func(row *model.PitfallStoredRow) { row.TriggerCondition = nil },
		},
		{
			name:   "trigger_condition 非法 JSON",
			tamper: func(row *model.PitfallStoredRow) { row.TriggerCondition = []byte("{坏JSON") },
		},
		{
			name:   "缺少 recognition_rules 块",
			tamper: func(row *model.PitfallStoredRow) { row.RecognitionRules = nil },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakePitfallRepo()
			svc := service.NewPitfallAdminService(repo)

			id := repo.seedValid(101, "脏行", model.StatusActive)
			repo.mu.Lock()
			tc.tamper(repo.items[id])
			repo.mu.Unlock()

			_, err := svc.List(ctx, repository.PitfallQuery{Page: 1, PageSize: 10})
			if err == nil {
				t.Fatal("脏行存在时列表应报错")
			}
			if !errors.Is(err, service.ErrPitfallInvalid) {
				t.Errorf("应返回 ErrPitfallInvalid，got %v", err)
			}
		})
	}
}

// ---------- 辅助函数 ----------

func validConfigObj(defectID int64, name string) *model.PitfallConfig {
	return &model.PitfallConfig{
		DefectID:        defectID,
		PitfallType:     model.TypeCausalMisjudgment,
		Name:            name,
		Description:     "测试描述",
		ErrorContent:    "错误建议内容",
		ConfidenceLevel: model.ConfidenceHigh,
		CorrectApproach: "正确做法",
		Severity:        model.SeverityMedium,
		Status:          model.StatusDraft,
		SortOrder:       1,
		Trigger: model.TriggerCondition{
			When:        model.WhenAlways,
			Probability: 1,
			MaxTimes:    1,
		},
		Recognition: model.RecognitionRules{
			DisputeKeywords: []string{"不对", "为什么"},
		},
		PitfallRules: model.FallRules{
			AdoptKeywords: []string{"好的", "执行"},
		},
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
