package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
	"github.com/quenching/content-service/internal/service"
)

// 辅助：创建测试服务
func newTestDefectService() *service.DefectService {
	repo := repository.NewInMemoryDefectRepo()
	s := service.NewDefectService(repo)
	s.SetExecutionValidator(func(_ context.Context, raw json.RawMessage, _ bool) error {
		var v map[string]any
		if json.Unmarshal(raw, &v) != nil {
			return fmt.Errorf("invalid JSON")
		}
		if v["kind"] == nil {
			return fmt.Errorf("kind required")
		}
		return nil
	})
	return s
}

// 辅助：创建一个有效剧本的请求（含完整 amplifier_config）
func validCreateReq(code string) *service.CreateDefectReq {
	return &service.CreateDefectReq{
		ProjectID:       1,
		Code:            code,
		Name:            "测试剧本" + code,
		DefectType:      "code",
		Category:        "database",
		Difficulty:      "easy",
		Description:     "测试描述",
		RootCause:       "测试根因",
		TargetServiceID: 1,
		TriggerConfig:   model.JSONMap{"type": "delay", "delay_seconds": 60},
		ManifestationConfig: model.JSONMap{
			"metrics": []interface{}{
				map[string]interface{}{"name": "p99_latency", "service": "svc", "op": "gt", "value": 500},
			},
		},
		FixConfig: model.JSONMap{
			"steps": []interface{}{
				map[string]interface{}{"step": 1, "description": "定位问题"},
			},
			"verify": "接口恢复正常",
		},
		AmplifierConfig: model.JSONMap{
			"code":  code,
			"kind":  "network_delay",
			"title": "测试延迟",
			"target": map[string]interface{}{
				"pod":               "test-svc",
				"network_interface": "eth0",
			},
			"spec": map[string]interface{}{
				"kind":         "network_delay",
				"delay_millis": 300,
			},
			"trigger": map[string]interface{}{
				"type":          "delay",
				"delay_seconds": 60,
			},
			"recovery": []interface{}{
				map[string]interface{}{"metric": "p99_latency", "service": "svc", "op": "lte", "value": 200},
			},
			"auto_revoke": true,
		},
		ScoreWeight:      1.0,
		EstimatedFixTime: 15,
	}
}

// TestCreateDefect_Success 成功创建剧本。
func TestCreateDefect_Success(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	defect, err := svc.CreateDefect(ctx, validCreateReq("test-001"))
	if err != nil {
		t.Fatalf("创建剧本失败: %v", err)
	}
	if defect.ID <= 0 {
		t.Error("ID 应大于 0")
	}
	if defect.Code != "test-001" {
		t.Errorf("code 不匹配: %s", defect.Code)
	}
	if !defect.IsActive {
		t.Error("新建剧本应默认启用")
	}
	if defect.ScoreWeight != 1.0 {
		t.Errorf("默认权重应为 1.0, got %f", defect.ScoreWeight)
	}
}

// TestCreateDefect_DuplicateCode code 重复应报错。
func TestCreateDefect_DuplicateCode(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	_, err := svc.CreateDefect(ctx, validCreateReq("dup-001"))
	if err != nil {
		t.Fatalf("第一次创建失败: %v", err)
	}

	_, err = svc.CreateDefect(ctx, validCreateReq("dup-001"))
	if err == nil {
		t.Error("重复 code 应返回错误")
	}
}

// TestCreateDefect_InvalidAmplifier 放大器配置不合法应报错。
func TestCreateDefect_InvalidAmplifier(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	req := validCreateReq("invalid-amp")
	req.AmplifierConfig = model.JSONMap{"code": "x"} // 缺少必要字段

	_, err := svc.CreateDefect(ctx, req)
	if err == nil {
		t.Error("非法放大器配置应返回错误")
	}
}

// TestGetDefect_Success 成功获取详情。
func TestGetDefect_Success(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	created, _ := svc.CreateDefect(ctx, validCreateReq("get-001"))
	defect, err := svc.GetDefect(ctx, created.ID)

	if err != nil {
		t.Fatalf("获取剧本失败: %v", err)
	}
	if defect.ID != created.ID {
		t.Errorf("ID 不匹配: %d vs %d", defect.ID, created.ID)
	}
}

// TestGetDefect_NotFound 不存在的ID返回错误。
func TestGetDefect_NotFound(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	_, err := svc.GetDefect(ctx, 9999)
	if err == nil {
		t.Error("不存在的 ID 应返回错误")
	}
}

// TestUpdateDefect_Success 成功更新剧本。
func TestUpdateDefect_Success(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	created, _ := svc.CreateDefect(ctx, validCreateReq("upd-001"))

	newName := "更新后的名称"
	newDesc := "更新后的描述"
	newDiff := "medium"
	req := &service.UpdateDefectReq{
		Name:        &newName,
		Description: &newDesc,
		Difficulty:  &newDiff,
	}

	updated, err := svc.UpdateDefect(ctx, created.ID, req)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("name 未更新: %s", updated.Name)
	}
	if updated.Description != newDesc {
		t.Errorf("description 未更新: %s", updated.Description)
	}
	if updated.Difficulty != newDiff {
		t.Errorf("difficulty 未更新: %s", updated.Difficulty)
	}
	// 未更新的字段保持原值
	if updated.Code != "upd-001" {
		t.Errorf("code 不应被修改: %s", updated.Code)
	}
}

// TestDeleteDefect_Success 成功删除剧本。
func TestDeleteDefect_Success(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	created, _ := svc.CreateDefect(ctx, validCreateReq("del-001"))

	if err := svc.DeleteDefect(ctx, created.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 删除后应查不到
	_, err := svc.GetDefect(ctx, created.ID)
	if err == nil {
		t.Error("删除后应查不到")
	}
}

// TestSetActive 启用/禁用切换。
func TestSetActive(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	created, _ := svc.CreateDefect(ctx, validCreateReq("active-001"))
	if !created.IsActive {
		t.Fatal("新建剧本应默认启用")
	}

	// 禁用
	if err := svc.SetActive(ctx, created.ID, false); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	d, _ := svc.GetDefect(ctx, created.ID)
	if d.IsActive {
		t.Error("应已禁用")
	}

	// 重新启用
	if err := svc.SetActive(ctx, created.ID, true); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	d, _ = svc.GetDefect(ctx, created.ID)
	if !d.IsActive {
		t.Error("应已启用")
	}
}

// TestListDefects_Pagination 分页查询。
func TestListDefects_Pagination(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	// 创建 5 个剧本
	for i := 0; i < 5; i++ {
		code := "page-" + string(rune('0'+i))
		svc.CreateDefect(ctx, validCreateReq(code))
	}

	// 第1页，每页2条
	result, err := svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		Page:      1,
		PageSize:  2,
	})
	if err != nil {
		t.Fatalf("列表查询失败: %v", err)
	}
	if result.Total != 5 {
		t.Errorf("总数应为 5, got %d", result.Total)
	}
	if len(result.Items) != 2 {
		t.Errorf("第1页应有 2 条, got %d", len(result.Items))
	}

	// 第3页（最后一页）
	result, _ = svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		Page:      3,
		PageSize:  2,
	})
	if len(result.Items) != 1 {
		t.Errorf("第3页应有 1 条, got %d", len(result.Items))
	}
}

// TestListDefects_FilterByDifficulty 按难度筛选。
func TestListDefects_FilterByDifficulty(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	easyReq := validCreateReq("diff-easy")
	easyReq.Difficulty = "easy"
	svc.CreateDefect(ctx, easyReq)

	medReq := validCreateReq("diff-med")
	medReq.Difficulty = "medium"
	svc.CreateDefect(ctx, medReq)

	hardReq := validCreateReq("diff-hard")
	hardReq.Difficulty = "hard"
	svc.CreateDefect(ctx, hardReq)

	// 只查 easy
	result, _ := svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID:  1,
		Difficulty: "easy",
		Page:       1,
		PageSize:   10,
	})
	if result.Total != 1 {
		t.Errorf("easy 难度应有 1 个, got %d", result.Total)
	}
	if result.Items[0].Difficulty != "easy" {
		t.Error("筛选结果应为 easy 难度")
	}
}

// TestListDefects_FilterByActive 按启用状态筛选。
func TestListDefects_FilterByActive(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	_, _ = svc.CreateDefect(ctx, validCreateReq("act-1"))
	d2, _ := svc.CreateDefect(ctx, validCreateReq("act-2"))
	svc.SetActive(ctx, d2.ID, false)
	svc.CreateDefect(ctx, validCreateReq("act-3"))

	// 只查启用的
	active := true
	result, _ := svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		IsActive:  &active,
		Page:      1,
		PageSize:  10,
	})
	if result.Total != 2 {
		t.Errorf("启用的应有 2 个, got %d", result.Total)
	}

	// 只查禁用的
	inactive := false
	result, _ = svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		IsActive:  &inactive,
		Page:      1,
		PageSize:  10,
	})
	if result.Total != 1 {
		t.Errorf("禁用的应有 1 个, got %d", result.Total)
	}
}

// TestListDefects_KeywordSearch 关键词搜索。
func TestListDefects_KeywordSearch(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	svc.CreateDefect(ctx, validCreateReq("kw-cpu-01"))
	svc.CreateDefect(ctx, validCreateReq("kw-mem-01"))

	// 搜索 cpu
	result, _ := svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		Keyword:   "cpu",
		Page:      1,
		PageSize:  10,
	})
	if result.Total != 1 {
		t.Errorf("搜索 cpu 应有 1 个, got %d", result.Total)
	}
}

// TestReorder 排序调整。
func TestReorder(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	d1, _ := svc.CreateDefect(ctx, validCreateReq("ord-1"))
	d2, _ := svc.CreateDefect(ctx, validCreateReq("ord-2"))
	d3, _ := svc.CreateDefect(ctx, validCreateReq("ord-3"))

	// 默认顺序：1, 2, 3（按 ID 排序）
	result, _ := svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		Page:      1,
		PageSize:  10,
	})
	if result.Items[0].ID != d1.ID || result.Items[2].ID != d3.ID {
		t.Error("默认顺序不正确")
	}

	// 交换 1 和 3：sort_order 由 1/2/3 变为 3/2/1，列表顺序变为 d3,d2,d1
	if err := svc.Reorder(ctx, 1, d1.ID, d3.ID); err != nil {
		t.Fatalf("排序失败: %v", err)
	}

	result, _ = svc.ListDefects(ctx, repository.DefectQuery{
		ProjectID: 1,
		Page:      1,
		PageSize:  10,
	})
	if result.Items[0].ID != d3.ID {
		t.Errorf("排序后第一个应为 d3 (ID=%d), got ID=%d", d3.ID, result.Items[0].ID)
	}
	if result.Items[1].ID != d2.ID {
		t.Errorf("排序后第二个应为 d2 (ID=%d), got ID=%d", d2.ID, result.Items[1].ID)
	}
	if result.Items[2].ID != d1.ID {
		t.Errorf("排序后第三个应为 d1 (ID=%d), got ID=%d", d1.ID, result.Items[2].ID)
	}
}

// TestValidateDefectConfig_Valid 校验合法剧本。
func TestValidateDefectConfig_Valid(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	created, _ := svc.CreateDefect(ctx, validCreateReq("val-001"))

	valid, errs, err := svc.ValidateDefectConfig(ctx, created.ID)
	if err != nil {
		t.Fatalf("校验出错: %v", err)
	}
	if !valid {
		t.Errorf("应校验通过, 错误: %v", errs)
	}
	if len(errs) != 0 {
		t.Errorf("应无错误, got %d 个", len(errs))
	}
}

// TestValidateDefectConfig_Invalid 校验缺少配置的剧本。
func TestValidateDefectConfig_Invalid(t *testing.T) {
	svc := newTestDefectService()
	ctx := context.Background()

	// 用基本字段创建，缺少 amplifier_config
	req := validCreateReq("val-invalid")
	req.AmplifierConfig = nil
	req.TriggerConfig = nil
	req.ManifestationConfig = nil
	req.FixConfig = nil

	// 因为 amplifier_config 为空所以不会触发校验，能成功创建
	created, _ := svc.CreateDefect(ctx, req)

	valid, errs, err := svc.ValidateDefectConfig(ctx, created.ID)
	if err != nil {
		t.Fatalf("校验出错: %v", err)
	}
	if valid {
		t.Error("应校验不通过")
	}
	// 应该至少缺少放大器、触发、表现、修复 4 项配置
	if len(errs) < 4 {
		t.Errorf("应至少有 4 个错误, got %d: %v", len(errs), errs)
	}
}

// TestInMemoryRepo_CountByProject 项目统计。
func TestInMemoryRepo_CountByProject(t *testing.T) {
	repo := repository.NewInMemoryDefectRepo()
	ctx := context.Background()

	// 项目1：3个
	for i := 0; i < 3; i++ {
		d := &model.Defect{
			ProjectID:  1,
			Code:       "p1-" + string(rune('0'+i)),
			Name:       "P1 Defect",
			DefectType: "code",
			Difficulty: "easy",
			IsActive:   true,
		}
		repo.Create(ctx, d)
	}
	// 项目2：1个
	d := &model.Defect{
		ProjectID:  2,
		Code:       "p2-1",
		Name:       "P2 Defect",
		DefectType: "code",
		Difficulty: "easy",
		IsActive:   true,
	}
	repo.Create(ctx, d)

	count1, _ := repo.CountByProject(ctx, 1)
	if count1 != 3 {
		t.Errorf("项目1应有 3 个剧本, got %d", count1)
	}

	count2, _ := repo.CountByProject(ctx, 2)
	if count2 != 1 {
		t.Errorf("项目2应有 1 个剧本, got %d", count2)
	}
}
