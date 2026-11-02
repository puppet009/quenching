// Package tests 分析服务单测。
package tests

import (
	"context"
	"testing"
	"time"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
	"github.com/quenching/review-service/internal/service"
)

// ——— 辅助 ———

func newTestService() (*service.AnalyticsService, *repository.InMemoryEventRepo) {
	repo := repository.NewInMemoryEventRepo()
	svc := service.NewAnalyticsService(repo)
	return svc, repo
}

func trackEvent(svc *service.AnalyticsService, eventType string, userID int64) {
	svc.Track(context.Background(), &service.TrackRequest{
		EventType: eventType,
		UserID:    userID,
	})
}

// ——— 埋点采集测试 ———

func TestTrackEvent_Basic(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType:  "login",
		UserID:     100,
		SessionID:  1,
		ProjectID:  1,
		Properties: map[string]interface{}{"from": "home"},
	})
	if err != nil {
		t.Fatalf("上报失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}
}

// ========== T5-8 AI 专项看板测试 ==========

// ——— 辅助函数 ———

func trackEvidenceVerify(svc *service.AnalyticsService, userID int64) {
	svc.Track(context.Background(), &service.TrackRequest{
		EventType: model.EventEvidenceVerify,
		UserID:    userID,
		Properties: map[string]interface{}{
			"verify_type": "log_check",
		},
	})
}

func trackDangerousOp(svc *service.AnalyticsService, userID int64) {
	svc.Track(context.Background(), &service.TrackRequest{
		EventType: model.EventDangerousOp,
		UserID:    userID,
		Properties: map[string]interface{}{
			"op_type":  "force_restart",
			"severity": "high",
		},
	})
}

func trackABComplete(svc *service.AnalyticsService, userID int64, speedDiff float64, accDiff float64, dangerDiff float64, verifyDiff float64) {
	svc.Track(context.Background(), &service.TrackRequest{
		EventType: model.EventABComplete,
		UserID:    userID,
		Properties: map[string]interface{}{
			"speed_diff_pct":        speedDiff,
			"accuracy_diff":         accDiff,
			"dangerous_op_diff_pct": dangerDiff,
			"verify_diff_pct":       verifyDiff,
			"group":                 "A",
		},
	})
}

// ——— AI 专项核心指标 ———

func TestAISpecialtyMetrics_AllCategories(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 10 个 AI 活跃用户（用 AI 对话）
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventAIChatMessage, int64(i))
	}

	// 8 人遇到坑点，5 人识别
	for i := 1; i <= 8; i++ {
		trackEvent(svc, model.EventPitfallEncounter, int64(i))
	}
	for i := 1; i <= 5; i++ {
		trackEvent(svc, model.EventPitfallRecognize, int64(i))
	}

	// 6 人有验证行为
	for i := 1; i <= 6; i++ {
		trackEvidenceVerify(svc, int64(i))
	}

	// 7 人完成首训，4 人查看了协作评估
	for i := 1; i <= 7; i++ {
		trackEvent(svc, model.EventFirstComplete, int64(i))
	}
	for i := 1; i <= 4; i++ {
		trackEvent(svc, model.EventCollabView, int64(i))
	}

	// 危险操作：15 次
	for i := 1; i <= 15; i++ {
		trackDangerousOp(svc, int64(i%10+1))
	}

	metrics, err := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("AI 专项指标统计失败: %v", err)
	}

	// 识别率 = 5/8 = 62.5%
	if metrics.RecognitionRate != 62.5 {
		t.Errorf("识别率应为 62.5%%, got %.2f", metrics.RecognitionRate)
	}

	// 验证率 = 6/10 = 60%
	if metrics.VerificationRate != 60.0 {
		t.Errorf("验证率应为 60%%, got %.2f", metrics.VerificationRate)
	}

	// 协作查看率 = 4/7 ≈ 57.14%
	if metrics.CollabViewRate < 57 || metrics.CollabViewRate > 58 {
		t.Errorf("协作查看率应约 57.14%%, got %.2f", metrics.CollabViewRate)
	}

	// 危险操作总数
	if metrics.TotalDangerousOps != 15 {
		t.Errorf("危险操作总数应为 15, got %d", metrics.TotalDangerousOps)
	}

	// AI 活跃用户数
	if metrics.AIActiveUsers != 10 {
		t.Errorf("AI 活跃用户数应为 10, got %d", metrics.AIActiveUsers)
	}
}

func TestAISpecialtyMetrics_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	metrics, err := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	if metrics.RecognitionRate != 0 {
		t.Errorf("空数据识别率应为 0, got %.2f", metrics.RecognitionRate)
	}
	if metrics.VerificationRate != 0 {
		t.Errorf("空数据验证率应为 0, got %.2f", metrics.VerificationRate)
	}
	if metrics.CollabViewRate != 0 {
		t.Errorf("空数据协作查看率应为 0, got %.2f", metrics.CollabViewRate)
	}
}

// ——— 样本量提示 ———

func TestSampleInfo_LowConfidence(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 少量数据：10 用户，5 组对照
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventAIChatMessage, int64(i))
	}
	for i := 1; i <= 5; i++ {
		trackABComplete(svc, int64(i), -10.0, 5.0, -3.0, 10.0)
	}

	metrics, _ := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})

	if metrics.SampleInfo.EnoughUsers {
		t.Error("10 用户不应判定为样本充足")
	}
	if metrics.SampleInfo.EnoughAB {
		t.Error("5 组对照不应判定为样本充足")
	}
	if metrics.SampleInfo.OverallConfidence != "低" {
		t.Errorf("整体置信度应为'低', got %s", metrics.SampleInfo.OverallConfidence)
	}
}

func TestSampleInfo_MediumConfidence(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 60 用户（充足），20 组对照（不足）→ 中置信度
	for i := 1; i <= 60; i++ {
		trackEvent(svc, model.EventAIChatMessage, int64(i))
	}
	for i := 1; i <= 20; i++ {
		trackABComplete(svc, int64(i), -5.0, 3.0, -2.0, 8.0)
	}

	metrics, _ := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})

	if !metrics.SampleInfo.EnoughUsers {
		t.Error("60 用户应判定为样本充足")
	}
	if metrics.SampleInfo.EnoughAB {
		t.Error("20 组对照不应判定为样本充足")
	}
	if metrics.SampleInfo.OverallConfidence != "中" {
		t.Errorf("整体置信度应为'中', got %s", metrics.SampleInfo.OverallConfidence)
	}
}

func TestSampleInfo_HighConfidence(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 100 用户，40 组对照 → 高置信度
	for i := 1; i <= 100; i++ {
		trackEvent(svc, model.EventAIChatMessage, int64(i))
	}
	for i := 1; i <= 40; i++ {
		trackABComplete(svc, int64(i), -15.0, 8.0, -5.0, 12.0)
	}

	metrics, _ := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})

	if !metrics.SampleInfo.EnoughUsers {
		t.Error("100 用户应判定为样本充足")
	}
	if !metrics.SampleInfo.EnoughAB {
		t.Error("40 组对应判定为样本充足")
	}
	if metrics.SampleInfo.OverallConfidence != "高" {
		t.Errorf("整体置信度应为'高', got %s", metrics.SampleInfo.OverallConfidence)
	}
}

// ——— 人机对照 A/B ———

func TestABComparison_WithData(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 3 组对照，速度差分别为 -10%、-20%、-15%（AI更快，所以负）
	// 正确率差：+5%、+10%、+15%
	trackABComplete(svc, 1, -10.0, 5.0, -3.0, 10.0)
	trackABComplete(svc, 2, -20.0, 10.0, -5.0, 15.0)
	trackABComplete(svc, 3, -15.0, 15.0, -2.0, 20.0)

	metrics, err := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("AI 专项指标统计失败: %v", err)
	}

	// 对照组数
	if metrics.ABPairCount != 3 {
		t.Errorf("对照组数应为 3, got %d", metrics.ABPairCount)
	}

	// 平均速度差 = (-10 + -20 + -15)/3 = -15%
	if metrics.ABSpeedDiff != -15.0 {
		t.Errorf("平均速度差应为 -15%%, got %.2f", metrics.ABSpeedDiff)
	}

	// 平均正确率差 = (5 + 10 + 15)/3 = 10%
	if metrics.ABAccuracyDiff != 10.0 {
		t.Errorf("平均正确率差应为 10%%, got %.2f", metrics.ABAccuracyDiff)
	}
}

func TestABComparison_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	metrics, _ := svc.GetAISpecialtyMetrics(ctx, time.Time{}, time.Time{})

	if metrics.ABPairCount != 0 {
		t.Errorf("空数据对照组数应为 0, got %d", metrics.ABPairCount)
	}
	if metrics.ABSpeedDiff != 0 {
		t.Errorf("空数据速度差应为 0, got %.2f", metrics.ABSpeedDiff)
	}
	if metrics.ABAccuracyDiff != 0 {
		t.Errorf("空数据正确率差应为 0, got %.2f", metrics.ABAccuracyDiff)
	}
}

// ——— AI 功能使用明细 ———

func TestAIFeatureBreakdown(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 10 个 AI 活跃用户
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventAIChatMessage, int64(i))
	}

	// 6 人用了 AI 提示
	for i := 1; i <= 6; i++ {
		trackEvent(svc, model.EventAIHintRequest, int64(i))
	}

	// 4 人查看了复盘
	for i := 1; i <= 4; i++ {
		trackEvent(svc, model.EventReviewView, int64(i))
	}

	breakdown, err := svc.GetAIFeatureBreakdown(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("AI 功能明细统计失败: %v", err)
	}

	// 应该有 8 个功能项
	if len(breakdown) != 8 {
		t.Fatalf("应有 8 个功能项, got %d", len(breakdown))
	}

	// 找 AI 对话项，使用率应该是 100%
	found := false
	for _, item := range breakdown {
		if item.Feature == "AI对话" {
			found = true
			if item.UserCount != 10 {
				t.Errorf("AI对话用户数应为 10, got %d", item.UserCount)
			}
			if item.UsageRate != 100.0 {
				t.Errorf("AI对话使用率应为 100%%, got %.2f", item.UsageRate)
			}
		}
	}
	if !found {
		t.Error("应包含 AI对话 功能项")
	}
}

func TestAIFeatureBreakdown_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	breakdown, err := svc.GetAIFeatureBreakdown(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(breakdown) != 8 {
		t.Errorf("空数据也应返回 8 个功能项, got %d", len(breakdown))
	}

	// 所有使用率应为 0
	for _, item := range breakdown {
		if item.UsageRate != 0 {
			t.Errorf("%s 使用率应为 0, got %.2f", item.Feature, item.UsageRate)
		}
	}
}

// ——— 验证行为事件上报 ———

func TestTrackEvidenceVerify(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventEvidenceVerify,
		UserID:    100,
		ProjectID: 1,
		SessionID: 1,
		Properties: map[string]interface{}{
			"verify_type":    "log_check",
			"target_service": "order-service",
		},
	})
	if err != nil {
		t.Fatalf("上报验证行为失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}
}

// ——— 危险操作事件上报 ———

func TestTrackDangerousOp(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventDangerousOp,
		UserID:    100,
		ProjectID: 1,
		SessionID: 1,
		Properties: map[string]interface{}{
			"op_type":  "force_restart",
			"severity": "high",
			"cost":     10,
		},
	})
	if err != nil {
		t.Fatalf("上报危险操作失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}
}

// ——— 对照完成事件上报 ———

func TestTrackABComplete(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventABComplete,
		UserID:    100,
		Properties: map[string]interface{}{
			"speed_diff_pct":        -15.0,
			"accuracy_diff":         8.0,
			"dangerous_op_diff_pct": -5.0,
			"verify_diff_pct":       12.0,
			"group":                 "A",
			"project_id":            1,
		},
	})
	if err != nil {
		t.Fatalf("上报对照完成失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}
}

func TestTrackEvent_UniqueEventDedup(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// register 是唯一事件，同一用户只记一次
	recorded1, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventRegister,
		UserID:    100,
	})
	if err != nil || !recorded1 {
		t.Fatal("第一次注册事件应记录成功")
	}

	// 第二次应返回 false（重复）
	recorded2, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventRegister,
		UserID:    100,
	})
	if err != nil {
		t.Fatalf("第二次上报不应报错: %v", err)
	}
	if recorded2 {
		t.Error("唯一事件重复上报应返回 false")
	}
}

func TestTrackEvent_NonUniqueNoDedup(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// login 不是唯一事件，每次都记
	for i := 0; i < 5; i++ {
		recorded, err := svc.Track(ctx, &service.TrackRequest{
			EventType: model.EventLogin,
			UserID:    100,
		})
		if err != nil || !recorded {
			t.Errorf("第 %d 次 login 应记录成功", i+1)
		}
	}
}

func TestTrackEvent_MissingType(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.Track(context.Background(), &service.TrackRequest{
		EventType: "",
		UserID:    100,
	})
	if err == nil {
		t.Error("缺少事件类型应报错")
	}
}

// ——— 事件计数测试 ———

func TestEventCount_TotalAndUnique(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 用户1：登录 3 次
	for i := 0; i < 3; i++ {
		trackEvent(svc, model.EventLogin, 1)
	}
	// 用户2：登录 2 次
	for i := 0; i < 2; i++ {
		trackEvent(svc, model.EventLogin, 2)
	}

	// 总次数
	total, err := svc.EventCount(ctx, model.EventLogin, false, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("总登录次数应为 5, got %d", total)
	}

	// 独立用户数
	unique, err := svc.EventCount(ctx, model.EventLogin, true, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if unique != 2 {
		t.Errorf("独立登录用户数应为 2, got %d", unique)
	}
}

// ——— 漏斗分析测试 ———

func TestFunnelAnalysis_FullFunnel(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 用户 1：走完完整漏斗（注册→首启→首完→复训）
	trackEvent(svc, model.EventRegister, 1)
	trackEvent(svc, model.EventFirstStart, 1)
	trackEvent(svc, model.EventFirstComplete, 1)
	trackEvent(svc, model.EventRetrain, 1)

	// 用户 2：注册→首启→首完（没复训）
	trackEvent(svc, model.EventRegister, 2)
	trackEvent(svc, model.EventFirstStart, 2)
	trackEvent(svc, model.EventFirstComplete, 2)

	// 用户 3：注册→首启（没完成）
	trackEvent(svc, model.EventRegister, 3)
	trackEvent(svc, model.EventFirstStart, 3)

	// 用户 4：只注册
	trackEvent(svc, model.EventRegister, 4)

	result, err := svc.AnalyzeFunnel(ctx, service.FunnelQuery{
		FunnelCode: "training_funnel",
	})
	if err != nil {
		t.Fatalf("漏斗分析失败: %v", err)
	}

	if result.TotalUsers != 4 {
		t.Errorf("第一步用户数应为 4, got %d", result.TotalUsers)
	}
	if result.FinalUsers != 1 {
		t.Errorf("最后一步用户数应为 1, got %d", result.FinalUsers)
	}

	// 各步骤用户数
	expected := []int{4, 3, 2, 1}
	for i, count := range expected {
		if result.Steps[i].UserCount != count {
			t.Errorf("步骤 %d 用户数应为 %d, got %d", i, count, result.Steps[i].UserCount)
		}
	}

	// 第一步转化率 100%
	if result.Steps[0].Conversion != 100.0 {
		t.Errorf("第一步转化率应为 100%%, got %.2f", result.Steps[0].Conversion)
	}

	// 第二步转化率：3/4 = 75%
	if result.Steps[1].Conversion != 75.0 {
		t.Errorf("第二步转化率应为 75%%, got %.2f", result.Steps[1].Conversion)
	}

	// 整体转化率：1/4 = 25%
	if result.OverallConversion != 25.0 {
		t.Errorf("整体转化率应为 25%%, got %.2f", result.OverallConversion)
	}
}

func TestFunnelAnalysis_EmptyFunnel(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	result, err := svc.AnalyzeFunnel(ctx, service.FunnelQuery{
		FunnelCode: "training_funnel",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalUsers != 0 {
		t.Errorf("空漏斗第一步应为 0, got %d", result.TotalUsers)
	}
	if result.OverallConversion != 0 {
		t.Errorf("整体转化率应为 0, got %.2f", result.OverallConversion)
	}
}

func TestFunnelAnalysis_NotFound(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.AnalyzeFunnel(context.Background(), service.FunnelQuery{
		FunnelCode: "nonexistent",
	})
	if err == nil {
		t.Error("不存在的漏斗应报错")
	}
}

func TestListFunnels(t *testing.T) {
	svc, _ := newTestService()
	funnels := svc.ListFunnels()
	if len(funnels) == 0 {
		t.Error("应至少有一个内置漏斗")
	}

	// 验证训练漏斗存在
	found := false
	for _, f := range funnels {
		if f.Code == "training_funnel" {
			found = true
			if len(f.Steps) != 4 {
				t.Errorf("训练漏斗应有 4 步, got %d", len(f.Steps))
			}
			break
		}
	}
	if !found {
		t.Error("应包含 training_funnel 漏斗")
	}
}

// ——— 看板统计测试 ———

func TestDashboardStats(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 5 个用户注册
	for i := 1; i <= 5; i++ {
		trackEvent(svc, model.EventRegister, int64(i))
	}
	// 3 个用户启动沙箱
	for i := 1; i <= 3; i++ {
		trackEvent(svc, model.EventFirstStart, int64(i))
	}
	// 2 个用户完成首训
	for i := 1; i <= 2; i++ {
		trackEvent(svc, model.EventFirstComplete, int64(i))
	}
	// 1 个用户复训
	trackEvent(svc, model.EventRetrain, 1)

	stats, err := svc.GetDashboardStats(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("看板统计失败: %v", err)
	}

	if stats.TotalUsers != 5 {
		t.Errorf("总用户数应为 5, got %d", stats.TotalUsers)
	}
	if stats.ActiveUsers != 3 {
		t.Errorf("活跃用户数应为 3, got %d", stats.ActiveUsers)
	}
	if stats.CompletedUsers != 2 {
		t.Errorf("完成用户数应为 2, got %d", stats.CompletedUsers)
	}
	if stats.RetrainUsers != 1 {
		t.Errorf("复训用户数应为 1, got %d", stats.RetrainUsers)
	}
	if stats.TrainingFunnel == nil {
		t.Error("应包含训练漏斗数据")
	}
}

// ——— 事件列表测试 ———

func TestListEvents_Pagination(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 上报 5 个事件
	for i := 0; i < 5; i++ {
		trackEvent(svc, model.EventLogin, 100)
	}

	events, total, err := svc.ListEvents(ctx, 100, "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Errorf("总数应为 5, got %d", total)
	}
	if len(events) != 2 {
		t.Errorf("第1页应有 2 条, got %d", len(events))
	}
}

func TestListEvents_FilterByType(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	trackEvent(svc, model.EventLogin, 1)
	trackEvent(svc, model.EventRegister, 1)
	trackEvent(svc, model.EventLogin, 1)
	trackEvent(svc, model.EventViewProject, 1)

	// 只查 login
	events, total, err := svc.ListEvents(ctx, 0, model.EventLogin, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Errorf("login 事件应有 2 条, got %d", total)
	}
	_ = events
}

// ——— 模型辅助方法测试 ———

func TestIsUniqueEvent(t *testing.T) {
	uniqueEvents := []string{
		model.EventRegister,
		model.EventFirstStart,
		model.EventFirstComplete,
	}
	for _, e := range uniqueEvents {
		if !model.IsUniqueEvent(e) {
			t.Errorf("%s 应是唯一事件", e)
		}
	}

	nonUnique := []string{
		model.EventLogin,
		model.EventStartTraining,
		model.EventSubmitFix,
		"custom_event",
	}
	for _, e := range nonUnique {
		if model.IsUniqueEvent(e) {
			t.Errorf("%s 不应是唯一事件", e)
		}
	}
}

func TestRegisterUniqueEvent(t *testing.T) {
	customType := "custom_unique_event"
	if model.IsUniqueEvent(customType) {
		t.Fatal("自定义事件初始不应是唯一事件")
	}

	model.RegisterUniqueEvent(customType)
	if !model.IsUniqueEvent(customType) {
		t.Error("注册后应是唯一事件")
	}
}

// ——— 时间范围测试 ———

func TestEventCount_WithTimeRange(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	now := time.Now()

	// 手动注入两个时间点的事件
	// 30 天前的事件
	repo.Track(ctx, &model.Event{
		EventType:  model.EventRegister,
		UserID:     1,
		OccurredAt: now.AddDate(0, 0, -30),
	})
	// 1 天前的事件
	repo.Track(ctx, &model.Event{
		EventType:  model.EventRegister,
		UserID:     2,
		OccurredAt: now.AddDate(0, 0, -1),
	})
	// 未来的事件（测试边界）
	repo.Track(ctx, &model.Event{
		EventType:  model.EventRegister,
		UserID:     3,
		OccurredAt: now.AddDate(0, 0, 1),
	})

	// 最近 7 天的注册数
	start := now.AddDate(0, 0, -7)
	end := now
	count, err := svc.EventCount(ctx, model.EventRegister, true, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("最近 7 天注册用户应为 1, got %d", count)
	}
}

// ——— 仓储层直接测试 ———

func TestInMemoryRepo_HasUserEvent(t *testing.T) {
	repo := repository.NewInMemoryEventRepo()
	ctx := context.Background()

	has, _ := repo.HasUserEvent(ctx, 100, model.EventRegister)
	if has {
		t.Error("初始不应有记录")
	}

	repo.Track(ctx, &model.Event{
		EventType: model.EventRegister,
		UserID:    100,
	})

	has, _ = repo.HasUserEvent(ctx, 100, model.EventRegister)
	if !has {
		t.Error("上报后应有记录")
	}
}

func TestInMemoryRepo_GetUniqueUsers(t *testing.T) {
	repo := repository.NewInMemoryEventRepo()
	ctx := context.Background()

	// 用户1：登录 2 次
	repo.Track(ctx, &model.Event{EventType: model.EventLogin, UserID: 1})
	repo.Track(ctx, &model.Event{EventType: model.EventLogin, UserID: 1})
	// 用户2：登录 1 次
	repo.Track(ctx, &model.Event{EventType: model.EventLogin, UserID: 2})

	users, err := repo.GetUniqueUsers(ctx, model.EventLogin, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Errorf("独立用户数应为 2, got %d", len(users))
	}
	if !users[1] || !users[2] {
		t.Error("用户集合应包含用户 1 和 2")
	}
}

// ——— 流失率计算验证 ———

func TestFunnelDropOff(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 10 人注册，5 人启动 → 转化率 50%，流失率 50%
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventRegister, int64(i))
	}
	for i := 1; i <= 5; i++ {
		trackEvent(svc, model.EventFirstStart, int64(i))
	}

	result, _ := svc.AnalyzeFunnel(ctx, service.FunnelQuery{
		FunnelCode: "training_funnel",
	})

	// 第二步转化率 50%
	if result.Steps[1].Conversion != 50.0 {
		t.Errorf("第二步转化率应为 50%%, got %.2f", result.Steps[1].Conversion)
	}
	// 第二步流失率 50%
	if result.Steps[1].DropOff != 50.0 {
		t.Errorf("第二步流失率应为 50%%, got %.2f", result.Steps[1].DropOff)
	}
}

// ========== T4-14 AI 行为埋点测试 ==========

// ——— AI 行为事件常量 ———

func TestAIBehaviorEvents_List(t *testing.T) {
	// AI 行为事件列表应包含 13 种事件
	if len(model.AIBehaviorEvents) != 13 {
		t.Errorf("AI 行为事件应有 13 种, got %d", len(model.AIBehaviorEvents))
	}

	// 验证关键事件存在
	expected := map[string]bool{
		model.EventReviewView:       false,
		model.EventReviewShare:      false,
		model.EventCollabView:       false,
		model.EventCollabEvaluate:   false,
		model.EventCollabComment:    false,
		model.EventPitfallEncounter: false,
		model.EventPitfallRecognize: false,
		model.EventPitfallFall:      false,
		model.EventAIChatMessage:    false,
		model.EventAIHintRequest:    false,
		model.EventAIExplanation:    false,
		model.EventEvidenceVerify:   false,
		model.EventDangerousOp:      false,
	}

	for _, evt := range model.AIBehaviorEvents {
		if _, ok := expected[evt]; ok {
			expected[evt] = true
		}
	}

	for evt, found := range expected {
		if !found {
			t.Errorf("AI 行为事件列表应包含 %s", evt)
		}
	}
}

// ——— AI 行为统计概览 ———

func TestAIBehaviorStats_AllCategories(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 用户1：完整使用 AI 功能
	trackEvent(svc, model.EventReviewView, 1)
	trackEvent(svc, model.EventReviewView, 1) // 2 次查看
	trackEvent(svc, model.EventCollabView, 1)
	trackEvent(svc, model.EventCollabEvaluate, 1)
	trackEvent(svc, model.EventPitfallEncounter, 1)
	trackEvent(svc, model.EventPitfallRecognize, 1)
	trackEvent(svc, model.EventPitfallFall, 1)
	trackEvent(svc, model.EventAIChatMessage, 1)
	trackEvent(svc, model.EventAIChatMessage, 1)
	trackEvent(svc, model.EventAIHintRequest, 1)

	// 用户2：只看了复盘
	trackEvent(svc, model.EventReviewView, 2)

	// 用户3：只遇到坑点
	trackEvent(svc, model.EventPitfallEncounter, 3)
	trackEvent(svc, model.EventPitfallFall, 3)

	stats, err := svc.GetAIBehaviorStats(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("AI 行为统计失败: %v", err)
	}

	// 复盘相关
	if stats.ReviewViews != 3 {
		t.Errorf("复盘查看次数应为 3, got %d", stats.ReviewViews)
	}
	if stats.ReviewViewUsers != 2 {
		t.Errorf("查看复盘的独立用户数应为 2, got %d", stats.ReviewViewUsers)
	}

	// 协作评估相关
	if stats.CollabViews != 1 {
		t.Errorf("协作评估查看次数应为 1, got %d", stats.CollabViews)
	}
	if stats.CollabEvaluates != 1 {
		t.Errorf("协作评估提交数应为 1, got %d", stats.CollabEvaluates)
	}

	// 坑点相关
	if stats.PitfallEncounters != 2 {
		t.Errorf("遇到坑点次数应为 2, got %d", stats.PitfallEncounters)
	}
	if stats.PitfallRecognizes != 1 {
		t.Errorf("识别坑点次数应为 1, got %d", stats.PitfallRecognizes)
	}
	if stats.PitfallFalls != 2 {
		t.Errorf("踩坑次数应为 2, got %d", stats.PitfallFalls)
	}
	if stats.PitfallUsers != 2 {
		t.Errorf("受坑点影响的用户数应为 2, got %d", stats.PitfallUsers)
	}

	// AI 对话相关
	if stats.AIChatMessages != 2 {
		t.Errorf("AI 对话消息数应为 2, got %d", stats.AIChatMessages)
	}
	if stats.AIHintRequests != 1 {
		t.Errorf("AI 提示请求数应为 1, got %d", stats.AIHintRequests)
	}

	// AI 活跃用户（使用过任意 AI 功能）
	if stats.AIActiveUsers != 3 {
		t.Errorf("AI 活跃用户数应为 3, got %d", stats.AIActiveUsers)
	}

	// 总事件数
	if stats.TotalAIEvents == 0 {
		t.Error("总 AI 事件数不应为 0")
	}
}

func TestAIBehaviorStats_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	stats, err := svc.GetAIBehaviorStats(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalAIEvents != 0 {
		t.Errorf("空数据总事件数应为 0, got %d", stats.TotalAIEvents)
	}
	if stats.AIActiveUsers != 0 {
		t.Errorf("空数据 AI 活跃用户数应为 0, got %d", stats.AIActiveUsers)
	}
}

// ——— 坑点转化漏斗 ———

func TestPitfallConversion(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 10 人遇到坑点
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventPitfallEncounter, int64(i))
	}
	// 6 人识别了坑点
	for i := 1; i <= 6; i++ {
		trackEvent(svc, model.EventPitfallRecognize, int64(i))
	}
	// 4 人踩了坑
	for i := 1; i <= 4; i++ {
		trackEvent(svc, model.EventPitfallFall, int64(i))
	}

	conv, err := svc.GetPitfallConversion(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("坑点转化统计失败: %v", err)
	}

	if conv.EncounterUsers != 10 {
		t.Errorf("遇到坑点用户数应为 10, got %d", conv.EncounterUsers)
	}
	if conv.RecognizeUsers != 6 {
		t.Errorf("识别坑点用户数应为 6, got %d", conv.RecognizeUsers)
	}
	if conv.FallUsers != 4 {
		t.Errorf("踩坑用户数应为 4, got %d", conv.FallUsers)
	}

	// 识别率 = 6/10 = 60%
	if conv.RecognitionRate != 60.0 {
		t.Errorf("识别率应为 60%%, got %.2f", conv.RecognitionRate)
	}

	// 踩坑率 = 4/10 = 40%
	if conv.FallRate != 40.0 {
		t.Errorf("踩坑率应为 40%%, got %.2f", conv.FallRate)
	}
}

func TestPitfallConversion_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	conv, err := svc.GetPitfallConversion(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if conv.EncounterUsers != 0 {
		t.Errorf("空数据遇到坑点用户数应为 0, got %d", conv.EncounterUsers)
	}
	if conv.RecognitionRate != 0 {
		t.Errorf("空数据识别率应为 0, got %.2f", conv.RecognitionRate)
	}
	if conv.FallRate != 0 {
		t.Errorf("空数据踩坑率应为 0, got %.2f", conv.FallRate)
	}
}

// ——— AI 事件分类明细 ———

func TestAIEventsBreakdown(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 上报几种 AI 事件
	trackEvent(svc, model.EventReviewView, 1)
	trackEvent(svc, model.EventReviewView, 2)
	trackEvent(svc, model.EventAIChatMessage, 1)
	trackEvent(svc, model.EventAIChatMessage, 1)
	trackEvent(svc, model.EventPitfallEncounter, 3)

	breakdown, err := svc.GetAIEventsBreakdown(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("AI 事件明细失败: %v", err)
	}

	if breakdown.Total == 0 {
		t.Error("总事件数不应为 0")
	}

	// 明细条数应等于 AI 行为事件种类数
	if len(breakdown.Items) != len(model.AIBehaviorEvents) {
		t.Errorf("明细条数应为 %d, got %d", len(model.AIBehaviorEvents), len(breakdown.Items))
	}

	// 验证每个条目都有事件类型和名称
	for _, item := range breakdown.Items {
		if item.EventType == "" {
			t.Error("事件类型不应为空")
		}
		if item.EventName == "" {
			t.Errorf("事件 %s 应有名称", item.EventType)
		}
	}
}

// ——— AI 事件上报测试 ———

func TestTrackAIEvent_ReviewView(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventReviewView,
		UserID:    100,
		SessionID: 1,
		ProjectID: 1,
		Properties: map[string]interface{}{
			"review_id": "r-123",
			"from":      "training_result",
		},
	})
	if err != nil {
		t.Fatalf("上报复盘查看事件失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}

	// 复盘查看不是唯一事件，可重复上报
	recorded2, _ := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventReviewView,
		UserID:    100,
	})
	if !recorded2 {
		t.Error("复盘查看应可重复记录")
	}
}

func TestTrackAIEvent_PitfallEncounter(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 遇到坑点不是唯一事件，每次都记
	for i := 0; i < 3; i++ {
		recorded, err := svc.Track(ctx, &service.TrackRequest{
			EventType: model.EventPitfallEncounter,
			UserID:    100,
			SessionID: 1,
			Properties: map[string]interface{}{
				"pitfall_code": "sql-injection-01",
			},
		})
		if err != nil || !recorded {
			t.Errorf("第 %d 次遇到坑点应记录成功", i+1)
		}
	}

	count, _ := svc.EventCount(ctx, model.EventPitfallEncounter, false, time.Time{}, time.Time{})
	if count != 3 {
		t.Errorf("遇到坑点次数应为 3, got %d", count)
	}
}

// ——— AI 事件都不是唯一事件 ———

func TestAIEvents_NoneAreUnique(t *testing.T) {
	for _, evt := range model.AIBehaviorEvents {
		if model.IsUniqueEvent(evt) {
			t.Errorf("%s 不应是唯一事件", evt)
		}
	}
}

// ========== T5-7 排训指标看板测试 ==========

// ——— 辅助函数 ———

func trackTrainingEvent(svc *service.AnalyticsService, eventType string, userID int64, projectID int64, durationSec int) {
	props := map[string]interface{}{}
	if durationSec > 0 {
		props["duration_seconds"] = durationSec
	}
	svc.Track(context.Background(), &service.TrackRequest{
		EventType:  eventType,
		UserID:     userID,
		ProjectID:  projectID,
		Properties: props,
	})
}

func trackReviewRating(svc *service.AnalyticsService, userID int64, score int) {
	svc.Track(context.Background(), &service.TrackRequest{
		EventType: model.EventReviewRating,
		UserID:    userID,
		Properties: map[string]interface{}{
			"score": score,
		},
	})
}

// ——— 排训核心指标 ———

func TestTrainingMetrics_AllCategories(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 10 个用户首次启动
	for i := 1; i <= 10; i++ {
		trackEvent(svc, model.EventFirstStart, int64(i))
	}
	// 7 个完成首训
	for i := 1; i <= 7; i++ {
		trackEvent(svc, model.EventFirstComplete, int64(i))
	}
	// 3 个复训
	for i := 1; i <= 3; i++ {
		trackEvent(svc, model.EventRetrain, int64(i))
	}

	// 训练完成事件（带时长）
	trackTrainingEvent(svc, model.EventTrainingComplete, 1, 1, 1800) // 30分钟
	trackTrainingEvent(svc, model.EventTrainingComplete, 2, 1, 2400) // 40分钟
	trackTrainingEvent(svc, model.EventTrainingComplete, 3, 2, 3600) // 60分钟

	// 复盘评价
	trackReviewRating(svc, 1, 5)
	trackReviewRating(svc, 2, 4)
	trackReviewRating(svc, 3, 3) // 不算好评
	trackReviewRating(svc, 4, 5)

	// 总训练场次
	for i := 1; i <= 15; i++ {
		trackEvent(svc, model.EventStartTraining, int64(i%10+1))
	}

	metrics, err := svc.GetTrainingMetrics(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("排训指标统计失败: %v", err)
	}

	// 首训完成率 = 7/10 = 70%
	if metrics.FirstCompletionRate != 70.0 {
		t.Errorf("首训完成率应为 70%%, got %.2f", metrics.FirstCompletionRate)
	}

	// 复训率 = 3/7 ≈ 42.86%
	if metrics.RetrainRate < 42 || metrics.RetrainRate > 43 {
		t.Errorf("复训率应约 42.86%%, got %.2f", metrics.RetrainRate)
	}

	// 复盘好评率 = 3/4 = 75%（5分、4分、5分算好评，3分不算）
	if metrics.ReviewSatisfaction != 75.0 {
		t.Errorf("复盘好评率应为 75%%, got %.2f", metrics.ReviewSatisfaction)
	}
	if metrics.PositiveRatings != 3 {
		t.Errorf("好评数应为 3, got %d", metrics.PositiveRatings)
	}

	// 平均训练时长 = (1800+2400+3600)/3 = 2600秒 ≈ 43.33分钟
	if metrics.AvgTrainingDuration != 2600 {
		t.Errorf("平均训练时长应为 2600秒, got %.2f", metrics.AvgTrainingDuration)
	}
	if metrics.AvgTrainingDurationMin < 43 || metrics.AvgTrainingDurationMin > 44 {
		t.Errorf("平均训练时长(分钟)应约 43.33, got %.2f", metrics.AvgTrainingDurationMin)
	}

	// 总量
	if metrics.TotalSessions != 15 {
		t.Errorf("总训练场次应为 15, got %d", metrics.TotalSessions)
	}
	if metrics.TotalCompleted != 3 {
		t.Errorf("总完成场次应为 3, got %d", metrics.TotalCompleted)
	}
}

func TestTrainingMetrics_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	metrics, err := svc.GetTrainingMetrics(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	if metrics.FirstCompletionRate != 0 {
		t.Errorf("空数据首训完成率应为 0, got %.2f", metrics.FirstCompletionRate)
	}
	if metrics.ReviewSatisfaction != 0 {
		t.Errorf("空数据好评率应为 0, got %.2f", metrics.ReviewSatisfaction)
	}
	if metrics.AvgTrainingDuration != 0 {
		t.Errorf("空数据平均时长应为 0, got %.2f", metrics.AvgTrainingDuration)
	}
}

// ——— 仓储层：数值属性统计 ———

func TestRepo_GetAverageNumericProperty(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	// 上报 3 个完成事件，不同时长
	trackTrainingEvent(svc, model.EventTrainingComplete, 1, 1, 1000)
	trackTrainingEvent(svc, model.EventTrainingComplete, 2, 1, 2000)
	trackTrainingEvent(svc, model.EventTrainingComplete, 3, 2, 3000)

	avg, err := repo.GetAverageNumericProperty(ctx, model.EventTrainingComplete, "duration_seconds", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if avg != 2000 {
		t.Errorf("平均时长应为 2000, got %.2f", avg)
	}
}

func TestRepo_GetAverageNumericProperty_Empty(t *testing.T) {
	_, repo := newTestService()
	ctx := context.Background()

	avg, err := repo.GetAverageNumericProperty(ctx, model.EventTrainingComplete, "duration_seconds", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if avg != 0 {
		t.Errorf("空数据平均值应为 0, got %.2f", avg)
	}
}

func TestRepo_CountByPropertyValue_Gte(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	// 5 个评分：2个5分、1个4分、1个3分、1个2分
	trackReviewRating(svc, 1, 5)
	trackReviewRating(svc, 2, 5)
	trackReviewRating(svc, 3, 4)
	trackReviewRating(svc, 4, 3)
	trackReviewRating(svc, 5, 2)

	// >= 4 分的有 3 个
	count, err := repo.CountByPropertyValue(ctx, model.EventReviewRating, "score", "gte", 4.0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("评分>=4的数量应为 3, got %d", count)
	}
}

func TestRepo_CountByPropertyValue_Eq(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	trackReviewRating(svc, 1, 5)
	trackReviewRating(svc, 2, 5)
	trackReviewRating(svc, 3, 3)

	count, err := repo.CountByPropertyValue(ctx, model.EventReviewRating, "score", "eq", 5.0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("5分评价数应为 2, got %d", count)
	}
}

func TestRepo_CountByPropertyValue_Lt(t *testing.T) {
	svc, repo := newTestService()
	ctx := context.Background()

	trackReviewRating(svc, 1, 5)
	trackReviewRating(svc, 2, 3)
	trackReviewRating(svc, 3, 2)
	trackReviewRating(svc, 4, 1)

	count, err := repo.CountByPropertyValue(ctx, model.EventReviewRating, "score", "lt", 3.0, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("评分<3的数量应为 2, got %d", count)
	}
}

// ——— 日趋势 ———

func TestDailyTrends_MultipleDays(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 在 3 天内分别上报事件
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	day3 := time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC)

	// 第1天：3人启动，2人完成
	for i := 1; i <= 3; i++ {
		svc.Track(ctx, &service.TrackRequest{EventType: model.EventFirstStart, UserID: int64(i), OccurredAt: day1})
	}
	for i := 1; i <= 2; i++ {
		svc.Track(ctx, &service.TrackRequest{EventType: model.EventFirstComplete, UserID: int64(i), OccurredAt: day1})
	}

	// 第2天：5人启动，4人完成
	for i := 10; i <= 14; i++ {
		svc.Track(ctx, &service.TrackRequest{EventType: model.EventFirstStart, UserID: int64(i), OccurredAt: day2})
	}
	for i := 10; i <= 13; i++ {
		svc.Track(ctx, &service.TrackRequest{EventType: model.EventFirstComplete, UserID: int64(i), OccurredAt: day2})
	}

	// 第3天：2人启动，0人完成
	for i := 20; i <= 21; i++ {
		svc.Track(ctx, &service.TrackRequest{EventType: model.EventFirstStart, UserID: int64(i), OccurredAt: day3})
	}

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	trends, err := svc.GetDailyTrends(ctx, start, end)
	if err != nil {
		t.Fatalf("日趋势统计失败: %v", err)
	}

	// 应该有 3 天数据
	if len(trends) != 3 {
		t.Fatalf("应有 3 天数据, got %d", len(trends))
	}

	// 第1天完成率 = 2/3 ≈ 66.67%
	if trends[0].CompletionRate < 66 || trends[0].CompletionRate > 67 {
		t.Errorf("第1天完成率应约 66.67%%, got %.2f", trends[0].CompletionRate)
	}

	// 第2天完成率 = 4/5 = 80%
	if trends[1].CompletionRate != 80.0 {
		t.Errorf("第2天完成率应为 80%%, got %.2f", trends[1].CompletionRate)
	}

	// 第3天完成率 = 0%
	if trends[2].CompletionRate != 0 {
		t.Errorf("第3天完成率应为 0%%, got %.2f", trends[2].CompletionRate)
	}

	// 日期格式正确
	if trends[0].Date != "2024-01-01" {
		t.Errorf("第1天日期应为 2024-01-01, got %s", trends[0].Date)
	}
}

func TestDailyTrends_Empty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	trends, err := svc.GetDailyTrends(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(trends) != 0 {
		t.Errorf("空时间范围应返回空数组, got %d 条", len(trends))
	}
}

// ——— 项目排行榜 ———

func TestProjectRankings_Basic(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 项目1：3个用户启动，2个完成，时长分别30/40分钟
	trackTrainingEvent(svc, model.EventStartTraining, 1, 1, 0)
	trackTrainingEvent(svc, model.EventStartTraining, 2, 1, 0)
	trackTrainingEvent(svc, model.EventStartTraining, 3, 1, 0)
	trackTrainingEvent(svc, model.EventTrainingComplete, 1, 1, 1800)
	trackTrainingEvent(svc, model.EventTrainingComplete, 2, 1, 2400)

	// 项目2：2个用户启动，1个完成，时长60分钟
	trackTrainingEvent(svc, model.EventStartTraining, 4, 2, 0)
	trackTrainingEvent(svc, model.EventStartTraining, 5, 2, 0)
	trackTrainingEvent(svc, model.EventTrainingComplete, 4, 2, 3600)

	rankings, err := svc.GetProjectRankings(ctx, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("项目排行榜统计失败: %v", err)
	}

	if len(rankings) != 2 {
		t.Fatalf("应有 2 个项目, got %d", len(rankings))
	}

	// 按启动用户数降序，项目1(3) > 项目2(2)
	if rankings[0].ProjectID != 1 {
		t.Errorf("第一名应是项目1, got project %d", rankings[0].ProjectID)
	}
	if rankings[0].StartUsers != 3 {
		t.Errorf("项目1启动用户数应为 3, got %d", rankings[0].StartUsers)
	}
	if rankings[0].CompleteUsers != 2 {
		t.Errorf("项目1完成用户数应为 2, got %d", rankings[0].CompleteUsers)
	}
}

func TestProjectRankings_TopN(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	// 创建 5 个项目
	for pid := 1; pid <= 5; pid++ {
		trackTrainingEvent(svc, model.EventStartTraining, int64(pid*10), int64(pid), 0)
	}

	// Top 3
	rankings, err := svc.GetProjectRankings(ctx, time.Time{}, time.Time{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rankings) != 3 {
		t.Errorf("Top 3 应返回 3 条, got %d", len(rankings))
	}
}

// ——— 复盘评价事件上报 ———

func TestTrackReviewRating(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventReviewRating,
		UserID:    100,
		Properties: map[string]interface{}{
			"score":     5,
			"review_id": "r-001",
			"comment":   "很有帮助",
		},
	})
	if err != nil {
		t.Fatalf("上报复盘评价失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}

	// 复盘评价不是唯一事件，同一用户可多次评价不同复盘
	recorded2, _ := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventReviewRating,
		UserID:    100,
		Properties: map[string]interface{}{
			"score": 4,
		},
	})
	if !recorded2 {
		t.Error("同一用户不同复盘应可重复评价")
	}
}

// ——— 训练完成事件上报 ———

func TestTrackTrainingComplete(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	recorded, err := svc.Track(ctx, &service.TrackRequest{
		EventType: model.EventTrainingComplete,
		UserID:    100,
		ProjectID: 1,
		SessionID: 1,
		Properties: map[string]interface{}{
			"duration_seconds": 2400,
			"score":            85,
			"difficulty":       "medium",
		},
	})
	if err != nil {
		t.Fatalf("上报训练完成失败: %v", err)
	}
	if !recorded {
		t.Error("应记录成功")
	}
}
