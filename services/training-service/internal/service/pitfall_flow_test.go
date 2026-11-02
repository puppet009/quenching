package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// ---- 可控坑点运行时 fake（实现 PitfallRuntime） ----

type fakePitfallRuntime struct {
	configs []*model.PitfallConfig
	snap    model.PitfallRuntimeContext
	counts  map[int64]int // configID -> 已注入次数

	// T3-11：按"发生时刻"排序的操作脚本，OperationsSince 只返回严格晚于 since 的部分。
	ops      []timedOp
	failOps  bool
	opsHits  int
	opsSince time.Time // 记录最近一次 OperationsSince 的 since 参数，供时序断言

	failConfigs      bool
	failSnapshot     bool
	failInjected     bool
	activeConfigHits int
	snapshotHits     int
}

// timedOp 一条带发生时刻的测试操作。
type timedOp struct {
	at time.Time
	op model.PitfallOperation
}

func (r *fakePitfallRuntime) ActiveConfigs(_ context.Context, _ int64) ([]*model.PitfallConfig, error) {
	r.activeConfigHits++
	if r.failConfigs {
		return nil, errors.New("config store down")
	}
	return r.configs, nil
}

func (r *fakePitfallRuntime) Snapshot(_ context.Context, _, _ int64, q string) (model.PitfallRuntimeContext, error) {
	r.snapshotHits++
	if r.failSnapshot {
		return model.PitfallRuntimeContext{}, errors.New("snapshot down")
	}
	snap := r.snap
	snap.Question = q
	if snap.TriggeredDefects == nil {
		snap.TriggeredDefects = map[int64]bool{}
	}
	return snap, nil
}

func (r *fakePitfallRuntime) InjectedCount(_ context.Context, _, configID int64) (int, error) {
	if r.failInjected {
		return 0, errors.New("count down")
	}
	return r.counts[configID], nil
}

func (r *fakePitfallRuntime) OperationsSince(
	_ context.Context, _ int64, since time.Time,
) ([]model.PitfallOperation, error) {
	r.opsHits++
	r.opsSince = since
	if r.failOps {
		return nil, errors.New("operations down")
	}
	out := make([]model.PitfallOperation, 0)
	for _, e := range r.ops {
		if e.at.After(since) {
			out = append(out, e.op)
		}
	}
	return out, nil
}

// ---- 夹具 ----

func pfConfig(id int64, defect int64, mutate ...func(*model.PitfallConfig)) *model.PitfallConfig {
	c := &model.PitfallConfig{
		ID:              id,
		DefectID:        defect,
		PitfallType:     model.TypeCausalMisjudgment,
		Name:            "重启误判坑",
		ErrorContent:    "直接重启服务就能解决，不用看日志",
		ConfidenceLevel: model.ConfidenceHigh, // 固定档位，避免测试断言受 random 影响
		SortOrder:       int(id),
		Status:          model.StatusActive,
		Trigger: model.TriggerCondition{
			When:        model.WhenAlways,
			Probability: 1,
			MaxTimes:    1,
		},
	}
	for _, m := range mutate {
		m(c)
	}
	return c
}

func triggeredSnap() model.PitfallRuntimeContext {
	return model.PitfallRuntimeContext{Turn: 1, TriggeredDefects: map[int64]bool{100: true}}
}

func startTrainingConv(t *testing.T, svc *ChatService, userID int64) int64 {
	t.Helper()
	sid := int64(500)
	convID, err := svc.StartConversation(context.Background(), userID, &sid, model.ConvTrainingAssistant)
	if err != nil {
		t.Fatalf("创建训练对话失败: %v", err)
	}
	return convID
}

func llmStreamer() *fakeStreamer {
	return &fakeStreamer{events: []StreamEvent{
		{Type: StreamDelta, Delta: "正常 LLM 回复"},
		{Type: StreamDone},
	}}
}

// ---- 命中：确定性模板直出，不经 LLM，落库 + 帧标记 ----

func TestPitfallHitDeterministicInjection(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100)},
		snap:    triggeredSnap(),
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil)) // p=1 必中，随机源不影响
	convID := startTrainingConv(t, svc, 7)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "现在怎么办"))

	if st.called != 0 {
		t.Fatalf("命中坑点时不应调用 LLM，实际调用 %d 次", st.called)
	}
	if len(evs) != 2 || evs[0].Type != StreamDelta || evs[1].Type != StreamDone {
		t.Fatalf("命中路径应为 delta+done 两帧: %+v", evs)
	}
	d, done := evs[0], evs[1]
	if !d.IsPitfall || d.PitfallConfigID != 1 || d.PitfallType != model.TypeCausalMisjudgment ||
		d.ConfidenceLevel != model.ConfidenceHigh {
		t.Fatalf("delta 帧坑点标记错误: %+v", d)
	}
	if !strings.Contains(d.Delta, "直接重启服务就能解决") {
		t.Fatalf("delta 应确定性直出错误建议模板: %q", d.Delta)
	}
	if !done.IsPitfall || done.PitfallConfigID != 1 || done.MessageID == 0 || done.ConversationID != convID {
		t.Fatalf("done 帧坑点标记/落库ID错误: %+v", done)
	}
	// 落库：用户消息仍在，坑点助手消息恰好一条，普通助手消息为零
	if store.upCount != 1 || store.pfCount != 1 || store.asCount != 0 {
		t.Fatalf("落库次数 user=%d pitfall=%d assistant=%d", store.upCount, store.pfCount, store.asCount)
	}
	pf := store.pitfalls[0]
	if pf.convID != convID || pf.configID != 1 || pf.pitfallType != model.TypeCausalMisjudgment ||
		pf.confidenceLevel != model.ConfidenceHigh || !strings.Contains(pf.content, "直接重启") {
		t.Fatalf("坑点落库记录错误: %+v", pf)
	}
	// SSE 直接序列化 StreamEvent：坑点字段的 JSON 通道必须真实存在
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"is_pitfall":true`, `"pitfall_config_id":1`, `"pitfall_type":"causal_misjudgment"`, `"confidence_level":"high"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("坑点帧 JSON 缺少 %s: %s", key, raw)
		}
	}
}

// ---- 未命中：判定正常完成但条件不满足，回退普通 LLM 路径 ----

func TestPitfallMissFallsBackToLLM(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100)},
		snap:    model.PitfallRuntimeContext{Turn: 1, TriggeredDefects: map[int64]bool{}}, // 缺陷未触发
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "现在怎么办"))

	if st.called != 1 {
		t.Fatalf("未命中应回退调用 LLM 一次，实际 %d", st.called)
	}
	if store.pfCount != 0 || store.asCount != 1 {
		t.Fatalf("未命中不应落坑点消息: pitfall=%d assistant=%d", store.pfCount, store.asCount)
	}
	if evs[0].Type != StreamDelta || evs[0].IsPitfall || evs[0].Delta != "正常 LLM 回复" {
		t.Fatalf("回退路径应为普通 LLM 帧: %+v", evs)
	}
}

func TestPitfallNoActiveConfigsSkipsRuntime(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{configs: nil, snap: triggeredSnap()}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "问"))
	if rt.snapshotHits != 0 {
		t.Fatal("无活跃坑点配置时不应取快照")
	}
	if st.called != 1 {
		t.Fatal("无配置应直接走 LLM")
	}
}

// ---- 基础设施异常：静默回退 LLM，训练对话不被坑点子系统打挂 ----

func TestPitfallRuntimeErrorFallsBackToLLM(t *testing.T) {
	for name, mutate := range map[string]func(*fakePitfallRuntime){
		"配置加载失败": func(rt *fakePitfallRuntime) { rt.failConfigs = true },
		"快照失败":   func(rt *fakePitfallRuntime) { rt.failSnapshot = true },
		"次数读取失败": func(rt *fakePitfallRuntime) { rt.failInjected = true },
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			st := llmStreamer()
			rt := &fakePitfallRuntime{
				configs: []*model.PitfallConfig{pfConfig(1, 100)},
				snap:    triggeredSnap(),
				counts:  map[int64]int{},
			}
			mutate(rt)
			svc := NewChatService(store, st, "mock", "")
			svc.EnablePitfall(rt, NewEngine(nil))
			convID := startTrainingConv(t, svc, 7)

			evs := collect(svc.StreamReply(context.Background(), 7, convID, "问"))

			if st.called != 1 {
				t.Fatalf("%s：应回退调用 LLM，实际 %d", name, st.called)
			}
			if store.pfCount != 0 {
				t.Fatalf("%s：不应落坑点消息", name)
			}
			if evs[len(evs)-1].Type != StreamDone {
				t.Fatalf("%s：应以正常 done 结束: %+v", name, evs)
			}
		})
	}
}

// ---- 非训练对话（无 session）：根本不判定 ----

func TestPitfallDisabledForNonTrainingConversation(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100)},
		snap:    triggeredSnap(),
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID, err := svc.StartConversation(context.Background(), 7, nil, model.ConvKnowledgeQA)
	if err != nil {
		t.Fatal(err)
	}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "问"))

	if rt.activeConfigHits != 0 {
		t.Fatal("非训练对话不应查询坑点配置")
	}
	if st.called != 1 {
		t.Fatal("非训练对话应直接走 LLM")
	}
}

// ---- max_times：已注入次数从运行时计数重建，达上限后不再注入 ----

func TestPitfallMaxTimesAcrossTurns(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100)},
		snap:    triggeredSnap(),
		counts:  map[int64]int{1: 1}, // 库里该坑已注入过 1 次（max_times=1）
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "再问一次"))

	if store.pfCount != 0 {
		t.Fatal("达 max_times 后不应再次注入")
	}
	if st.called != 1 {
		t.Fatalf("达上限应回退 LLM，实际调用 %d 次", st.called)
	}
	if evs[0].IsPitfall {
		t.Fatalf("回退帧不应带坑点标记: %+v", evs[0])
	}
}

// ---- 一轮至多一坑：多个可命中配置时只注入排序第一的 ----

func TestPitfallAtMostOnePerTurn(t *testing.T) {
	store := newFakeStore()
	st := llmStreamer()
	rt := &fakePitfallRuntime{
		configs: []*model.PitfallConfig{pfConfig(1, 100), pfConfig(2, 100)},
		snap:    triggeredSnap(),
		counts:  map[int64]int{},
	}
	svc := NewChatService(store, st, "mock", "")
	svc.EnablePitfall(rt, NewEngine(nil))
	convID := startTrainingConv(t, svc, 7)

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "问"))

	if store.pfCount != 1 {
		t.Fatalf("一轮至多注入一坑，实际落库 %d 条", store.pfCount)
	}
	if evs[0].PitfallConfigID != 1 {
		t.Fatalf("应注入排序第一的坑(config=1)，实际 %d", evs[0].PitfallConfigID)
	}
	if st.called != 0 {
		t.Fatal("命中后不应再调 LLM")
	}
}
