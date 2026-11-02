package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/pkg/eventbus"
)

// ——— 测试替身 ———
// 注：service 包内已存在 fakeStore/fakePublisher（会话、告警测试），
// 故主动线索的替身统一加 Clue 前缀。

type fakeClueStore struct {
	mu sync.Mutex

	ownerOK    bool
	ownerErr   error
	startedAt  time.Time
	anchor     time.Time
	anchorErr  error
	anchorZero bool
	ensureErr  error
	count      int
	countErr   error
	appendErr  error

	convID       int64
	appended     []appendedClue
	ensureCalls  int
	ownerSession int64
	anchorArgs   []anchorArg
}

type anchorArg struct {
	sessionID int64
	startedAt time.Time
	now       time.Time
}

type appendedClue struct {
	conversationID int64
	content        string
	reason         string
	anchor         time.Time
}

func (f *fakeClueStore) SessionOwner(_ context.Context, sessionID int64) (int64, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ownerSession = sessionID
	return 1001, f.startedAt, f.ownerOK, f.ownerErr
}

func (f *fakeClueStore) LastActivityAnchor(_ context.Context, sessionID int64, startedAt, now time.Time) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.anchorArgs = append(f.anchorArgs, anchorArg{sessionID, startedAt, now})
	if f.anchorErr != nil {
		return time.Time{}, f.anchorErr
	}
	if f.anchorZero {
		return time.Time{}, nil
	}
	if f.anchor.IsZero() {
		// 默认锚点：取一个稳定的过去时刻，模拟"最近活动在 8 分钟前"。
		return now.Add(-8 * time.Minute), nil
	}
	return f.anchor, nil
}

func (f *fakeClueStore) EnsureConversation(_ context.Context, _, _ int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureCalls++
	if f.ensureErr != nil {
		return 0, f.ensureErr
	}
	if f.convID == 0 {
		f.convID = 900
	}
	return f.convID, f.ensureErr
}

func (f *fakeClueStore) CountCluesFromAnchor(_ context.Context, _ int64, _ string, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count, f.countErr
}

func (f *fakeClueStore) AppendClue(_ context.Context, conversationID int64, content, reason string, anchor time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.appendErr != nil {
		return 0, f.appendErr
	}
	f.appended = append(f.appended, appendedClue{conversationID, content, reason, anchor})
	return int64(5000 + len(f.appended)), nil
}

func (f *fakeClueStore) snapshot() []appendedClue {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]appendedClue, len(f.appended))
	copy(out, f.appended)
	return out
}

// errUnexpectedMethod 推送器只允许走 PublishToUser，其余方法被调即失败。
var errUnexpectedMethod = errors.New("不应调用的发布方法")

type fakeCluePublisher struct {
	mu       sync.Mutex
	calls    []publishedEvent
	failOnce bool
}

type publishedEvent struct {
	userID    int64
	eventType eventbus.EventType
	data      eventbus.AIMessageEventData
}

func (p *fakeCluePublisher) Publish(context.Context, eventbus.TargetType, string, eventbus.EventType, interface{}) error {
	return errUnexpectedMethod
}

func (p *fakeCluePublisher) PublishToChannel(context.Context, string, eventbus.EventType, interface{}) error {
	return errUnexpectedMethod
}

func (p *fakeCluePublisher) Broadcast(context.Context, eventbus.EventType, interface{}) error {
	return errUnexpectedMethod
}

func (p *fakeCluePublisher) PublishToUser(_ context.Context, userID int64, eventType eventbus.EventType, data interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	msg, _ := data.(eventbus.AIMessageEventData)
	p.calls = append(p.calls, publishedEvent{userID, eventType, msg})
	if p.failOnce {
		p.failOnce = false
		return errors.New("网关不可用")
	}
	return nil
}

func (p *fakeCluePublisher) events() []publishedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]publishedEvent, len(p.calls))
	copy(out, p.calls)
	return out
}

func testStatus(reason model.StallReason) model.StallStatus {
	return model.StallStatus{
		SessionID:   42,
		State:       model.StallStateStalled,
		Reason:      reason,
		Since:       time.Now(), // 仅作扫描器内存态；推送频控不依赖它
		EvaluatedAt: time.Now(),
	}
}

func testPusher(store ClueStore, pub eventbus.Publisher) *CluePusher {
	return NewCluePusher(store, pub, model.ClueConfig{Enabled: true})
}

// ——— 用例 ———

// idle 卡顿：落库一条线索并发布一条 ai_message 事件，载荷字段正确。
func TestHandleStall_IdlePushes(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	st := testStatus(model.StallReasonIdle)
	p.HandleStall(context.Background(), st)

	wantAnchor := st.EvaluatedAt.Add(-8 * time.Minute)
	if got := store.snapshot(); len(got) != 1 {
		t.Fatalf("期望落库 1 条线索，实际 %d", len(got))
	} else {
		if got[0].reason != string(model.StallReasonIdle) || got[0].content != RenderClue(string(model.StallReasonIdle)) {
			t.Errorf("落库线索原因/文案不符：%+v", got[0])
		}
		if !got[0].anchor.Equal(wantAnchor) {
			t.Errorf("锚点期望 %v，实际 %v", wantAnchor, got[0].anchor)
		}
	}

	evs := pub.events()
	if len(evs) != 1 {
		t.Fatalf("期望发布 1 条事件，实际 %d", len(evs))
	}
	ev := evs[0]
	if ev.userID != 1001 || ev.eventType != eventbus.EventAIMessage {
		t.Errorf("事件目标/类型不符：%+v", ev)
	}
	d := ev.data
	if d.Role != "assistant" || d.Content != RenderClue(string(model.StallReasonIdle)) ||
		d.SessionID != "42" || d.MessageID == "" || d.IsStreaming || !d.IsEnd || d.Timestamp <= 0 {
		t.Errorf("ai_message 载荷字段不符：%+v", d)
	}
}

// no_progress 卡顿同样推送，文案为对应模板。
func TestHandleStall_NoProgressPushes(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonNoProgress))

	evs := pub.events()
	if len(evs) != 1 || evs[0].data.Content != RenderClue(string(model.StallReasonNoProgress)) {
		t.Fatalf("期望 1 条 no_progress 线索事件，实际 %+v", evs)
	}
}

// 频控：本周期已存在同类线索（count>0）时不打扰，既不落库也不发布。
func TestHandleStall_DedupSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, count: 1}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 {
		t.Error("频控命中时不应再落库")
	}
	if len(pub.events()) != 0 {
		t.Error("频控命中时不应再发布事件")
	}
	if store.ensureCalls != 0 {
		t.Error("频控命中时不应走到对话归属")
	}
}

// 属主不存在（会话已结束/查不到）：静默跳过。
func TestHandleStall_OwnerMissingSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: false}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("无属主时不应落库或推送")
	}
}

// 查属主失败：跳过，且不向上抛错。
func TestHandleStall_OwnerErrorSkips(t *testing.T) {
	store := &fakeClueStore{ownerErr: errors.New("db down")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("查属主失败时不应落库或推送")
	}
}

// 取卡顿周期锚点失败：保守跳过，不落库不发布。
func TestHandleStall_AnchorErrorSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, anchorErr: errors.New("db down")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("取锚点失败时应保守跳过")
	}
}

// 锚点缺失（未开训且从无活动）：保守跳过，宁可少打扰。
func TestHandleStall_AnchorZeroSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, anchorZero: true}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("锚点缺失时不应落库或推送")
	}
}

// 频控计数失败：保守跳过（宁可少打扰，不误轰炸）。
func TestHandleStall_CountErrorSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, countErr: errors.New("db down")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("频控查询失败时应保守跳过")
	}
}

// 对话归属失败：跳过，不落库不发布。
func TestHandleStall_EnsureConvErrorSkips(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, ensureErr: errors.New("db down")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("对话归属失败时不应落库或推送")
	}
}

// 落库失败：不发布事件（避免发出无落库事实、无法回看/去重的推送）。
func TestHandleStall_AppendErrorSkipsPublish(t *testing.T) {
	store := &fakeClueStore{ownerOK: true, appendErr: errors.New("db down")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(pub.events()) != 0 {
		t.Error("落库失败时不应发布事件")
	}
}

// publisher 为 nil（ws-gateway 未配置）：线索仍落库，不 panic。
func TestHandleStall_NilPublisherStillPersists(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	p := testPusher(store, nil)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 1 {
		t.Error("publisher 为 nil 时线索仍应落库")
	}
}

// 事件发布失败：不重试、不影响已落库事实，方法不返回错误。
func TestHandleStall_PublishErrorNoRetry(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	pub := &fakeCluePublisher{failOnce: true}
	p := testPusher(store, pub)

	p.HandleStall(context.Background(), testStatus(model.StallReasonIdle))

	if len(store.snapshot()) != 1 {
		t.Error("发布失败时线索应已落库")
	}
	if len(pub.events()) != 1 {
		t.Error("发布失败不应重试（避免重复打扰）")
	}
}

// 跨重启/跨周期：频控只看库内锚点，不看扫描器内存态 Since。
// 同一锚点重复进入（模拟重启首轮重触发）计数不变，被去重；
// 锚点前移（恢复后有新活动、再次卡顿）计数为 0，允许再推。
func TestHandleStall_AnchorDrivesDedup(t *testing.T) {
	t1 := time.Now().Add(-20 * time.Minute).Truncate(time.Second)
	t2 := time.Now().Add(-2 * time.Minute).Truncate(time.Second)

	// 同一锚点、两次 OnStall（第二次模拟进程重启后首轮，Since/EvaluatedAt 已变）。
	store := &fakeClueStore{ownerOK: true, anchor: t1, count: 1}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)
	st1 := testStatus(model.StallReasonIdle)
	st1.EvaluatedAt = time.Now().Add(-10 * time.Minute)
	p.HandleStall(context.Background(), st1)
	if len(store.snapshot()) != 0 {
		t.Fatal("同周期已有线索时不应再落库")
	}

	// 新卡顿周期：锚点前移，频控计数清零（fake 返回 0），应再推一条。
	store2 := &fakeClueStore{ownerOK: true, anchor: t2, count: 0}
	pub2 := &fakeCluePublisher{}
	p2 := testPusher(store2, pub2)
	st2 := testStatus(model.StallReasonIdle)
	p2.HandleStall(context.Background(), st2)
	if got := store2.snapshot(); len(got) != 1 {
		t.Fatalf("恢复后新周期应允许再推，实际落库 %d 条", len(got))
	} else if !got[0].anchor.Equal(t2) {
		t.Errorf("新周期落库锚点应为 %v，实际 %v", t2, got[0].anchor)
	}
	if len(pub2.events()) != 1 {
		t.Error("恢复后新周期应再发一条事件")
	}
}

// 配置开关关闭：OnStall 不派发，HandleStall 不受开关影响（便于直接测试）。
func TestOnStall_DisabledNoop(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	pub := &fakeCluePublisher{}
	p := NewCluePusher(store, pub, model.ClueConfig{Enabled: false})

	p.OnStall(context.Background(), testStatus(model.StallReasonIdle))
	p.Wait()

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("clue.enabled=false 时不应产生任何线索")
	}
}

// 无原因状态（防御）：不派发。
func TestOnStall_ReasonNoneNoop(t *testing.T) {
	store := &fakeClueStore{ownerOK: true}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.OnStall(context.Background(), model.StallStatus{SessionID: 1, State: model.StallStateStalled})
	p.Wait()

	if len(store.snapshot()) != 0 || len(pub.events()) != 0 {
		t.Error("无卡顿原因时不应派发线索")
	}
}

// OnStall 为异步派发：应在后台完成且 Wait 可收敛，store 异常 panic 不波及调用方。
func TestOnStall_AsyncAndRecover(t *testing.T) {
	store := &fakeClueStore{ownerErr: errors.New("触发内部异常也无妨")}
	pub := &fakeCluePublisher{}
	p := testPusher(store, pub)

	p.OnStall(context.Background(), testStatus(model.StallReasonIdle))
	p.Wait() // 不永久挂起即说明 goroutine 已收敛（含异常恢复路径）
}
