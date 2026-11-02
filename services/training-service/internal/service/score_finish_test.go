// FinishSessionScore（runtime finish 最小闭环）单测：
// 验证正常完成判定、幂等守卫、技术分先落库、协作评估降级与难度参考时长注入。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/quenching/proto/aiv1"

	"github.com/quenching/training-service/internal/model"
)

// fakeFinishStore 仅实现结束评分链路触及的端口方法，其余协作回写为空操作。
type fakeFinishStore struct {
	snapshot   *SessionFinishSnapshot
	snapErr    error
	actions    []model.OperationAction
	actionsErr error
	updateErr  error

	techUpdates []fakeTechUpdate
}

type fakeTechUpdate struct {
	sessionID     int64
	techScore     float64
	techRadar     model.RadarData
	faultResolved bool
}

func (f *fakeFinishStore) GetSessionFinishSnapshot(_ context.Context, _ int64) (*SessionFinishSnapshot, error) {
	return f.snapshot, f.snapErr
}

func (f *fakeFinishStore) ListSessionOperations(_ context.Context, _ int64) ([]model.OperationAction, error) {
	return f.actions, f.actionsErr
}

func (f *fakeFinishStore) UpdateSessionTechScore(
	_ context.Context,
	sessionID int64,
	techScore float64,
	techRadar model.RadarData,
	faultResolved bool,
) error {
	f.techUpdates = append(f.techUpdates, fakeTechUpdate{sessionID, techScore, techRadar, faultResolved})
	return f.updateErr
}

// fakeHintReader 可控的提示用量端口；usageErr 非空时透传错误。
type fakeHintReader struct {
	used     int
	usageErr error
}

func (r *fakeHintReader) Usage(_ context.Context, _ int64) (HintUsage, error) {
	if r.usageErr != nil {
		return HintUsage{}, r.usageErr
	}
	return HintUsage{Used: r.used}, nil
}

var _ HintUsageReader = (*fakeHintReader)(nil)

// 以下为满足 ScoreStore 接口的空实现（结束评分链路不触及）。
func (f *fakeFinishStore) CollectCollabInput(context.Context, int64) (CollabEvents, error) {
	return CollabEvents{}, nil
}
func (f *fakeFinishStore) UpdateSessionCollabScore(context.Context, int64, float64) error { return nil }
func (f *fakeFinishStore) UpdateReviewCollabScores(context.Context, int64, ReviewCollabDetail) error {
	return nil
}
func (f *fakeFinishStore) UpdateUserProfileCollab(context.Context, int64, model.RadarData, float64, string) error {
	return nil
}
func (f *fakeFinishStore) GetSessionScoreDetail(context.Context, int64, int64) (*SessionScoreResult, error) {
	return nil, nil
}

var _ ScoreStore = (*fakeFinishStore)(nil)

// fakeFinishEvaluator 可配返回响应或错误的协作评估端口。
type fakeFinishEvaluator struct {
	resp *aiv1.EvaluateCollabResponse
	err  error
}

func (f *fakeFinishEvaluator) EvaluateCollab(context.Context, *aiv1.EvaluateCollabRequest) (*aiv1.EvaluateCollabResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.resp != nil {
		return f.resp, nil
	}
	// 默认纯人工模式（available=false）：管线成功但无有效协作分。
	return &aiv1.EvaluateCollabResponse{Available: false}, nil
}

var _ CollabEvaluator = (*fakeFinishEvaluator)(nil)

func intPtr(v int) *int { return &v }

func newFinishService(store *fakeFinishStore, eval CollabEvaluator) *ScoreService {
	return NewScoreService(store, eval, nil)
}

// 会话不存在：不计分、不落库、不报错（供云端幂等重试）。
func TestFinishSessionScoreNotFound(t *testing.T) {
	store := &fakeFinishStore{snapshot: nil}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 1, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if res.Scored || res.Reason != "会话不存在" {
		t.Fatalf("应判为会话不存在，实际 %+v", res)
	}
	if len(store.techUpdates) != 0 {
		t.Fatal("会话不存在时不应回写技术分")
	}
}

// 已评分会话重复回调：幂等跳过。
func TestFinishSessionScoreIdempotent(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 2, UserID: 10, Status: "completed", TechScoreSet: true,
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 2, FinishOutcomeCompleted)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if res.Scored {
		t.Fatalf("已评分会话应跳过，实际 %+v", res)
	}
	if len(store.techUpdates) != 0 {
		t.Fatal("幂等跳过时不应回写技术分")
	}
}

// 显式中止 / 超时：不计分不落库。
func TestFinishSessionScoreAbortedAndTimeout(t *testing.T) {
	for _, outcome := range []FinishOutcome{FinishOutcomeAborted, FinishOutcomeTimeout} {
		store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
			SessionID: 3, Status: "running",
			TotalDefectCount: 1, FixedDefectCount: 1, // 即便缺陷已修齐，显式结局优先
		}}
		svc := newFinishService(store, &fakeFinishEvaluator{})

		res, err := svc.FinishSessionScore(context.Background(), 3, outcome)
		if err != nil {
			t.Fatalf("%s 不应返回错误", outcome)
		}
		if res.Scored {
			t.Fatalf("%s 不应计分，实际 %+v", outcome, res)
		}
		if len(store.techUpdates) != 0 {
			t.Fatalf("%s 不应回写技术分", outcome)
		}
	}
}

// status 仍 running 且缺陷计数缺失（total=0）：保守判未完成，不计分。
func TestFinishSessionScoreRunningNoDefectData(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 4, Status: "running", TotalDefectCount: 0,
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 4, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if res.Scored {
		t.Fatalf("无缺陷登记证据时应保守不计分，实际 %+v", res)
	}
}

// status=completed 即便缺陷计数为 0 也按正常完成计分（状态机优先于启发式）。
func TestFinishSessionScoreCompletedStatus(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 5, UserID: 11, Status: "completed", Difficulty: "easy",
		ActualDuration: intPtr(200),
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 5, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if !res.Scored || res.Result == nil {
		t.Fatalf("completed 状态应计分，实际 %+v", res)
	}
	if len(store.techUpdates) != 1 {
		t.Fatalf("技术分应落库一次，实际 %d", len(store.techUpdates))
	}
	if store.techUpdates[0].faultResolved {
		t.Fatal("total=0 时故障修复结论应为 false")
	}
}

// running + fixed>=total 启发式判正常完成，修复结论透传到落库。
func TestFinishSessionScoreResolvedHeuristic(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 6, UserID: 12, Status: "running", Difficulty: "hard",
		TotalDefectCount: 3, FixedDefectCount: 3,
		ActualDuration: intPtr(800),
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 6, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if !res.Scored {
		t.Fatalf("缺陷修齐应启发式判完成: %s", res.Reason)
	}
	if len(store.techUpdates) != 1 || !store.techUpdates[0].faultResolved {
		t.Fatalf("修复结论应落库为 true，实际 %+v", store.techUpdates)
	}
}

// status=aborted 显式负向状态：即便回调未声明结局也不计分。
func TestFinishSessionScoreAbortedStatus(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 7, Status: "aborted",
		TotalDefectCount: 1, FixedDefectCount: 1,
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	res, err := svc.FinishSessionScore(context.Background(), 7, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if res.Scored {
		t.Fatal("aborted 状态不应计分")
	}
}

// 协作评估失败：技术分仍已落库，返回 TechOnly 降级且整体不报错。
func TestFinishSessionScoreTechOnlyOnEvaluatorError(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 8, UserID: 13, Status: "completed",
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{err: errors.New("ai-service 不可达")})

	res, err := svc.FinishSessionScore(context.Background(), 8, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("协作降级不应上抛错误，实际 %v", err)
	}
	if !res.Scored || !res.TechOnly || res.Result == nil {
		t.Fatalf("应返回技术分降级结果，实际 %+v", res)
	}
	if len(store.techUpdates) != 1 {
		t.Fatal("降级时技术分必须已落库")
	}
}

// 快照读取失败：错误上抛且不写任何分数。
func TestFinishSessionScoreSnapshotError(t *testing.T) {
	store := &fakeFinishStore{snapErr: errors.New("DB 连接断开")}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	if _, err := svc.FinishSessionScore(context.Background(), 9, FinishOutcomeUnspecified); err == nil {
		t.Fatal("快照读取失败应返回错误")
	}
	if len(store.techUpdates) != 0 {
		t.Fatal("出错路径不应回写技术分")
	}
}

// 操作时间线读取失败：错误上抛，技术分尚未落库。
func TestFinishSessionScoreActionsError(t *testing.T) {
	store := &fakeFinishStore{
		snapshot:   &SessionFinishSnapshot{SessionID: 10, Status: "completed"},
		actionsErr: errors.New("操作表查询超时"),
	}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	if _, err := svc.FinishSessionScore(context.Background(), 10, FinishOutcomeUnspecified); err == nil {
		t.Fatal("操作时间线失败应返回错误")
	}
	if len(store.techUpdates) != 0 {
		t.Fatal("代价计算前置失败时不应落技术分")
	}
}

// 技术分回写失败：错误上抛。
func TestFinishSessionScoreUpdateError(t *testing.T) {
	store := &fakeFinishStore{
		snapshot:  &SessionFinishSnapshot{SessionID: 11, Status: "completed"},
		updateErr: errors.New("UPDATE 死锁"),
	}
	svc := newFinishService(store, &fakeFinishEvaluator{})

	if _, err := svc.FinishSessionScore(context.Background(), 11, FinishOutcomeUnspecified); err == nil {
		t.Fatal("技术分回写失败应返回错误")
	}
}

// 难度参考时长可由装配层覆盖：同一耗时下不同 ExpectedSec 应产出不同技术分。
func TestFinishSessionScoreExpectedSecOverride(t *testing.T) {
	mkStore := func() *fakeFinishStore {
		return &fakeFinishStore{snapshot: &SessionFinishSnapshot{
			SessionID: 12, Status: "completed", Difficulty: "normal",
			ActualDuration: intPtr(700), // 默认 expected=600 处衰减段；放宽到 1400 则满分
		}}
	}

	storeDefault := mkStore()
	svcDefault := newFinishService(storeDefault, &fakeFinishEvaluator{})
	resDefault, err := svcDefault.FinishSessionScore(context.Background(), 12, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("默认配置评分失败: %v", err)
	}

	storeRelaxed := mkStore()
	svcRelaxed := newFinishService(storeRelaxed, &fakeFinishEvaluator{})
	svcRelaxed.SetExpectedSecByDifficulty(map[string]int{"normal": 1400})
	resRelaxed, err := svcRelaxed.FinishSessionScore(context.Background(), 12, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("放宽配置评分失败: %v", err)
	}

	if resDefault.Result.Tech >= resRelaxed.Result.Tech {
		t.Fatalf("放宽参考时长应提高耗时维度得分：默认 %.2f 放宽 %.2f",
			resDefault.Result.Tech, resRelaxed.Result.Tech)
	}
}

// 提示用量接线：已用次数进入技术雷达 info_retrieval 维（第 3 维，索引 2），
// 雷达 6 维顺序与 TechRadarDimensions 对齐并随技术分一并落库。
func TestFinishSessionScoreHintRadar(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 13, UserID: 14, Status: "completed", Difficulty: "easy",
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})
	svc.SetHintUsageReader(&fakeHintReader{used: 2})

	res, err := svc.FinishSessionScore(context.Background(), 13, FinishOutcomeUnspecified)
	if err != nil {
		t.Fatalf("不应返回错误，实际 %v", err)
	}
	if !res.Scored || len(store.techUpdates) != 1 {
		t.Fatalf("应正常计分并落库一次，实际 %+v", res)
	}
	radar := store.techUpdates[0].techRadar
	if len(radar.Dimensions) != 6 || len(radar.Values) != 6 {
		t.Fatalf("技术雷达应为 6 维，实际 %+v", radar)
	}
	if radar.Dimensions[2] != "info_retrieval" {
		t.Fatalf("第 3 维应为 info_retrieval，实际 %s", radar.Dimensions[2])
	}
	// 每次提示扣 hintPenaltyPerUse，2 次后该维应低于满分。
	if radar.Values[2] >= 100 {
		t.Fatalf("使用 2 次提示后信息检索维应低于 100，实际 %.2f", radar.Values[2])
	}
}

// 提示用量读取失败：错误上抛，技术分不落库（评分前置数据不完整不硬算）。
func TestFinishSessionScoreHintUsageError(t *testing.T) {
	store := &fakeFinishStore{snapshot: &SessionFinishSnapshot{
		SessionID: 14, UserID: 15, Status: "completed",
	}}
	svc := newFinishService(store, &fakeFinishEvaluator{})
	svc.SetHintUsageReader(&fakeHintReader{usageErr: errors.New("提示用量查询失败")})

	if _, err := svc.FinishSessionScore(context.Background(), 14, FinishOutcomeUnspecified); err == nil {
		t.Fatal("提示用量读取失败应返回错误")
	}
	if len(store.techUpdates) != 0 {
		t.Fatal("前置数据失败时不应落技术分")
	}
}
