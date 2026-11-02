package service

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// T3-12 踩坑后果传导的编排层测试。
//
// 纯逻辑（规格解析/severity 缺省强度）由 pitfall 包 consequence_test.go 覆盖；
// 本文件聚焦编排契约：
//   - agree/fell 首次判定后联动注入一次次生故障，且同一坑点只恶化一次；
//   - dispute/verified/ignore 不触发；worsen_fault=false 不触发；
//   - executor/store 未装配或失败时静默降级，绝不阻断 LLM 对话；
//   - 会话结束按登记幂等撤销。

// ---- 内存 fake：ConsequenceStore ----

type fakeConsequenceStore struct {
	records      []ConsequenceRecord
	nextID       int64
	failRegister bool
	failMark     bool
	failList     bool
}

func newFakeConsequenceStore() *fakeConsequenceStore {
	return &fakeConsequenceStore{nextID: 1}
}

var _ ConsequenceStore = (*fakeConsequenceStore)(nil)

func (f *fakeConsequenceStore) RegisterPending(
	_ context.Context, rec ConsequenceRecord,
) (*ConsequenceRecord, bool, error) {
	if f.failRegister {
		return nil, false, errors.New("register down")
	}
	for i := range f.records {
		if f.records[i].PitfallMessageID == rec.PitfallMessageID {
			existing := f.records[i]
			return &existing, false, nil
		}
	}
	rec.ID = f.nextID
	f.nextID++
	rec.Status = ConsequenceStatusPending
	now := time.Now()
	rec.CreatedAt, rec.UpdatedAt = now, now
	f.records = append(f.records, rec)
	saved := rec
	return &saved, true, nil
}

func (f *fakeConsequenceStore) MarkInjected(_ context.Context, id int64, uid string) error {
	if f.failMark {
		return errors.New("mark down")
	}
	for i := range f.records {
		if f.records[i].ID == id && f.records[i].Status == ConsequenceStatusPending {
			f.records[i].Status = ConsequenceStatusInjected
			f.records[i].ExperimentUID = uid
		}
	}
	return nil
}

func (f *fakeConsequenceStore) MarkFailed(_ context.Context, id int64, reason string) error {
	for i := range f.records {
		if f.records[i].ID == id && f.records[i].Status == ConsequenceStatusPending {
			f.records[i].Status = ConsequenceStatusFailed
			f.records[i].FailReason = reason
		}
	}
	return nil
}

func (f *fakeConsequenceStore) MarkRevoked(_ context.Context, id int64) error {
	if f.failMark {
		return errors.New("mark down")
	}
	for i := range f.records {
		if f.records[i].ID == id && f.records[i].Status == ConsequenceStatusInjected {
			f.records[i].Status = ConsequenceStatusRevoked
		}
	}
	return nil
}

func (f *fakeConsequenceStore) ListActiveBySession(
	_ context.Context, sessionID int64,
) ([]ConsequenceRecord, error) {
	if f.failList {
		return nil, errors.New("list down")
	}
	out := make([]ConsequenceRecord, 0)
	for _, r := range f.records {
		if r.SessionID == sessionID && r.Status == ConsequenceStatusInjected {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeConsequenceStore) byStatus(status string) int {
	n := 0
	for _, r := range f.records {
		if r.Status == status {
			n++
		}
	}
	return n
}

// ---- 内存 fake：ConsequenceExecutor ----

type fakeConsequenceExecutor struct {
	injected []ConsequenceInjectRequest
	uids     []string
	revoked  []string
	failNext bool
	uidSeq   int
}

var _ ConsequenceExecutor = (*fakeConsequenceExecutor)(nil)

func (e *fakeConsequenceExecutor) Inject(
	_ context.Context, req ConsequenceInjectRequest,
) (string, error) {
	if e.failNext {
		e.failNext = false
		return "", errors.New("chaos engine unavailable")
	}
	e.uidSeq++
	uid := "blade-uid-" + strconv.Itoa(e.uidSeq)
	e.injected = append(e.injected, req)
	e.uids = append(e.uids, uid)
	return uid, nil
}

func (e *fakeConsequenceExecutor) Revoke(_ context.Context, _ int64, uid string) error {
	if e.failNext {
		e.failNext = false
		return errors.New("revoke failed")
	}
	e.revoked = append(e.revoked, uid)
	return nil
}

// ---- 夹具：坑点已注入、待用户反应；并装好后果传导两依赖 ----

func consequenceConfig(id int64, mutate ...func(*model.PitfallConfig)) *model.PitfallConfig {
	c := reactionConfig(id)
	c.Severity = model.SeverityMedium
	c.PitfallRules.WorsenFault = true
	for _, m := range mutate {
		m(c)
	}
	return c
}

func consequenceHarness(t *testing.T, cfg *model.PitfallConfig) (
	*ChatService, *fakeStore, *fakeStreamer, *fakePitfallRuntime,
	*fakeConsequenceStore, *fakeConsequenceExecutor, int64, time.Time,
) {
	t.Helper()
	svc, store, st, rt, convID, at := reactionHarness(t, cfg)
	cs := newFakeConsequenceStore()
	ce := &fakeConsequenceExecutor{}
	svc.EnableConsequence(cs, ce)
	return svc, store, st, rt, cs, ce, convID, at
}

// ---- 核心：采纳错误建议 -> 注入次生故障（默认 CPU 满载，medium=80）----

func TestConsequenceInjectedOnAgree(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, store, st, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(10*time.Second), "restart_service", "action")}

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启试试"))

	if st.called != 1 {
		t.Fatalf("反应轮仍应走普通 LLM 路径，called=%d", st.called)
	}
	if evs[len(evs)-1].Type != StreamDone {
		t.Fatalf("对话应以 done 正常结束: %+v", evs)
	}
	if len(ce.injected) != 1 {
		t.Fatalf("采纳后应注入 1 次次生故障，实际 %d", len(ce.injected))
	}
	req := ce.injected[0]
	if req.Kind != model.ConsequenceCPU || req.CPUPercent != 80 || req.SessionID != 500 {
		t.Fatalf("默认应为 medium CPU 80%% 且带会话: %+v", req)
	}
	if cs.byStatus(ConsequenceStatusInjected) != 1 || cs.byStatus(ConsequenceStatusPending) != 0 {
		t.Fatalf("登记应落到 injected: %+v", cs.records)
	}
	if cs.records[0].PitfallMessageID != store.pitfalls[0].messageID {
		t.Fatal("登记应关联触发它的坑点消息")
	}
}

// ---- 显式 consequence：错误配置 -> 网络丢包（错误率上升映射）----

func TestConsequenceExplicitNetworkLoss(t *testing.T) {
	cfg := consequenceConfig(1, func(c *model.PitfallConfig) {
		c.PitfallRules.Consequence = &model.ConsequenceFault{
			Kind: model.ConsequenceNetLoss, LossPercent: 35,
		}
	})
	svc, _, _, rt, _, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "modify_config", "action")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我改了配置"))

	if len(ce.injected) != 1 {
		t.Fatalf("应注入 1 次，实际 %d", len(ce.injected))
	}
	if got := ce.injected[0]; got.Kind != model.ConsequenceNetLoss || got.LossPercent != 35 {
		t.Fatalf("应注入显式 network_loss 35%%: %+v", got)
	}
}

// ---- 同一坑点只恶化一次：跨轮重复进入不二次注入（登记幂等双保险）----

func TestConsequenceOnlyOncePerPitfall(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, _, _, rt, cs, ce, convID, at := consequenceHarness(t, cfg)

	// 第一轮：执行错误操作 -> agree -> 注入。
	rt.ops = []timedOp{opAt(at.Add(10*time.Second), "restart_service", "action")}
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))
	if len(ce.injected) != 1 {
		t.Fatalf("首轮应注入 1 次，实际 %d", len(ce.injected))
	}

	// 第二轮：该坑点消息已被标记 agree，ListPendingPitfalls 不再返回它，
	// 不会重复判定；即便登记被直接二次触发，RegisterPending 也应幂等拦截。
	rt.ops = []timedOp{opAt(at.Add(2*time.Minute), "restart_service", "action")}
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我又重启了一次"))
	if len(ce.injected) != 1 {
		t.Fatalf("同一坑点只应恶化一次，实际注入 %d", len(ce.injected))
	}
	if len(cs.records) != 1 {
		t.Fatalf("登记也应只有一条，实际 %d", len(cs.records))
	}
}

// ---- 识别了坑（dispute/verified/ignore）不触发后果传导 ----

func TestConsequenceNotTriggeredOnRecognition(t *testing.T) {
	cases := map[string]func(time.Time) (string, []timedOp){
		"dispute": func(at time.Time) (string, []timedOp) {
			return "凭什么重启？证据呢", []timedOp{opAt(at.Add(3*time.Second), "list_files", "observe")}
		},
		"verified": func(at time.Time) (string, []timedOp) {
			return "我先看看日志", []timedOp{opAt(at.Add(3*time.Second), "view_log", "observe")}
		},
		"ignore": func(at time.Time) (string, []timedOp) {
			return "我查下网络链路", []timedOp{opAt(at.Add(3*time.Second), "tcpdump_capture", "analyze")}
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := consequenceConfig(1)
			svc, _, _, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
			reply, ops := arrange(at)
			rt.ops = ops

			_ = collect(svc.StreamReply(context.Background(), 7, convID, reply))

			if len(ce.injected) != 0 {
				t.Fatalf("%s：识别了坑不应注入次生故障, got %d", name, len(ce.injected))
			}
			if len(cs.records) != 0 {
				t.Fatalf("%s：不应产生登记记录", name)
			}
		})
	}
}

// ---- worsen_fault=false：即便采纳也不恶化 ----

func TestConsequenceSkippedWhenWorsenDisabled(t *testing.T) {
	cfg := consequenceConfig(1, func(c *model.PitfallConfig) { c.PitfallRules.WorsenFault = false })
	svc, _, _, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}

	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))

	if len(ce.injected) != 0 || len(cs.records) != 0 {
		t.Fatalf("worsen_fault=false 不应恶化: injected=%d records=%d", len(ce.injected), len(cs.records))
	}
}

// ---- executor 未装配（云上未接线）：不登记、不注入，对话正常 ----

func TestConsequenceExecutorNilSkips(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, _, st, rt, convID, at := reactionHarness(t, cfg)
	cs := newFakeConsequenceStore()
	svc.EnableConsequence(cs, nil) // 执行通道未就绪
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))

	if st.called != 1 || evs[len(evs)-1].Type != StreamDone {
		t.Fatalf("执行通道缺失不应阻断对话: called=%d evs=%+v", st.called, evs)
	}
	if len(cs.records) != 0 {
		t.Fatalf("无执行通道不应产生登记（避免假装成功），实际 %d", len(cs.records))
	}
}

// ---- store 未装配：整条后果链路安静关闭，行为同旧版本 ----

func TestConsequenceStoreNilDisablesChain(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, store, st, rt, convID, at := reactionHarness(t, cfg) // 未调 EnableConsequence
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))

	if st.called != 1 || evs[len(evs)-1].Type != StreamDone {
		t.Fatalf("未装配后果链路应完全不影响对话")
	}
	if store.fallenCount != 1 {
		t.Fatalf("踩坑判定与计数仍应正常，fallen=%d", store.fallenCount)
	}
}

// ---- 注入失败：登记置 failed，对话不阻断，不返回句柄 ----

func TestConsequenceInjectFailureMarksFailedNeverBlocks(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, _, st, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}
	ce.failNext = true

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))

	if st.called != 1 || evs[len(evs)-1].Type != StreamDone {
		t.Fatalf("注入失败不应阻断 LLM 对话: called=%d evs=%+v", st.called, evs)
	}
	if cs.byStatus(ConsequenceStatusFailed) != 1 {
		t.Fatalf("应记录 1 条 failed，实际: %+v", cs.records)
	}
	if cs.byStatus(ConsequenceStatusInjected) != 0 {
		t.Fatal("失败时不应有 injected 记录")
	}
}

// ---- 登记失败：静默跳过，不调用执行器，对话正常 ----

func TestConsequenceRegisterFailureSkipsInject(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, _, st, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}
	cs.failRegister = true

	evs := collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))

	if st.called != 1 || evs[len(evs)-1].Type != StreamDone {
		t.Fatalf("登记失败不应阻断对话")
	}
	if len(ce.injected) != 0 {
		t.Fatalf("登记失败不应继续注入，实际 %d", len(ce.injected))
	}
}

// ---- 恢复：会话结束按 injected 登记幂等撤销，并置 revoked ----

func TestConsequenceRevokeOnSessionEnd(t *testing.T) {
	cfg := consequenceConfig(1)
	svc, _, _, rt, cs, ce, convID, at := consequenceHarness(t, cfg)
	rt.ops = []timedOp{opAt(at.Add(5*time.Second), "restart_service", "action")}
	_ = collect(svc.StreamReply(context.Background(), 7, convID, "我重启了"))
	injectedUID := ce.uids[0]

	if err := svc.RevokeSessionConsequences(context.Background(), 500); err != nil {
		t.Fatalf("撤销不应报错: %v", err)
	}
	if len(ce.revoked) != 1 || ce.revoked[0] != injectedUID {
		t.Fatalf("应按句柄撤销注入的实验: %+v", ce.revoked)
	}
	if cs.byStatus(ConsequenceStatusRevoked) != 1 {
		t.Fatalf("登记应置 revoked: %+v", cs.records)
	}

	// 再次撤销：已无 injected 记录，幂等无操作。
	if err := svc.RevokeSessionConsequences(context.Background(), 500); err != nil {
		t.Fatalf("重复撤销不应报错: %v", err)
	}
	if len(ce.revoked) != 1 {
		t.Fatalf("重复撤销不应再次调用执行器，实际 %d", len(ce.revoked))
	}
}

// ---- 恢复未装配依赖：no-op 不报错（会话结束主流程不能被拖垮）----

func TestConsequenceRevokeNoopWhenDisabled(t *testing.T) {
	// 未调 EnableConsequence 的普通服务：会话结束撤销应为 no-op，不拖垮结束主流程。
	plain := NewChatService(newFakeStore(), llmStreamer(), "mock", "")
	if err := plain.RevokeSessionConsequences(context.Background(), 500); err != nil {
		t.Fatalf("未装配时撤销应为 no-op: %v", err)
	}
}
