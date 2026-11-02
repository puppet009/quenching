package service

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"sync"
	"testing"

	"github.com/quenching/training-service/internal/model"
)

// ——— 内存 fake：三个端口 ———

type fakeGroupStore struct {
	mu     sync.Mutex
	groups map[int64]model.ControlGroup
}

func newFakeGroupStore() *fakeGroupStore {
	return &fakeGroupStore{groups: map[int64]model.ControlGroup{}}
}

func (s *fakeGroupStore) GetCollabGroup(_ context.Context, userID int64) (model.ControlGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groups[userID], nil
}

func (s *fakeGroupStore) AssignCollabGroupIfAbsent(
	_ context.Context, userID int64, g model.ControlGroup,
) (model.ControlGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.groups[userID]; ok {
		return cur, false, nil // 已存在绝不覆盖
	}
	s.groups[userID] = g
	return g, true, nil
}

func (s *fakeGroupStore) set(userID int64, g model.ControlGroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups[userID] = g
}

type fakeSessionStore struct {
	mu       sync.Mutex
	sessions []GroupSession
	nextID   int64
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{nextID: 100}
}

func (s *fakeSessionStore) CountSessions(_ context.Context, userID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, gs := range s.sessions {
		if gs.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (s *fakeSessionStore) GetSession(_ context.Context, sessionID int64) (*GroupSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == sessionID {
			cp := s.sessions[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *fakeSessionStore) ListUserSessions(_ context.Context, userID int64) ([]GroupSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GroupSession, 0)
	for _, gs := range s.sessions {
		if gs.UserID == userID {
			out = append(out, gs)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *fakeSessionStore) LinkPair(_ context.Context, a, b int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ai, bj := -1, -1
	for i := range s.sessions {
		if s.sessions[i].ID == a {
			ai = i
		}
		if s.sessions[i].ID == b {
			bj = i
		}
	}
	if ai < 0 || bj < 0 {
		return errors.New("会话不存在")
	}
	if s.sessions[ai].PairSessionID > 0 || s.sessions[bj].PairSessionID > 0 {
		return errors.New("会话已被绑定")
	}
	s.sessions[ai].PairSessionID = b
	s.sessions[bj].PairSessionID = a
	return nil
}

// addSession 测试辅助：模拟一场已创建的训练会话。
func (s *fakeSessionStore) addSession(userID, projectID int64, group model.ControlGroup, mode model.AIMode) GroupSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	gs := GroupSession{
		ID: s.nextID, UserID: userID, ProjectID: projectID,
		Status: model.StatusRunning, AIMode: mode, ControlGroup: group,
	}
	s.nextID++
	s.sessions = append(s.sessions, gs)
	return gs
}

type fakeDefectCatalog struct {
	// sessionDefects: sessionID -> 故障列表
	sessionDefects map[int64][]PairingDefect
	// projectDefects: projectID -> 候选故障
	projectDefects map[int64][]PairingDefect
}

func newFakeDefectCatalog() *fakeDefectCatalog {
	return &fakeDefectCatalog{
		sessionDefects: map[int64][]PairingDefect{},
		projectDefects: map[int64][]PairingDefect{},
	}
}

func (c *fakeDefectCatalog) SessionDefects(_ context.Context, sessionID int64) ([]PairingDefect, error) {
	out := append([]PairingDefect(nil), c.sessionDefects[sessionID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *fakeDefectCatalog) ListProjectDefects(_ context.Context, projectID int64) ([]PairingDefect, error) {
	return append([]PairingDefect(nil), c.projectDefects[projectID]...), nil
}

// database 类故障：d1(首场用) / d2(同类同难度，配对目标) / d3(同类但 hard)
func seedDatabaseDefects(c *fakeDefectCatalog, projectID int64) {
	c.projectDefects[projectID] = []PairingDefect{
		{ID: 1, Code: "db-slow", Category: "database", Difficulty: "easy"},
		{ID: 2, Code: "db-lock", Category: "database", Difficulty: "easy"},
		{ID: 3, Code: "db-deadlock", Category: "database", Difficulty: "hard"},
		{ID: 4, Code: "net-loss", Category: "network", Difficulty: "easy"},
	}
}

func newPlanner() (*CrossGroupPlanner, *fakeGroupStore, *fakeSessionStore, *fakeDefectCatalog) {
	g := newFakeGroupStore()
	s := newFakeSessionStore()
	d := newFakeDefectCatalog()
	// 固定种子让首次分组确定；具体组别以实际返回为准，测试不写死。
	p := NewCrossGroupPlanner(g, s, d, rand.New(rand.NewSource(1)))
	return p, g, s, d
}

// ——— 编排测试 ———

func TestEnsureGroup_AssignOnceAndImmutable(t *testing.T) {
	p, groups, _, _ := newPlanner()
	ctx := context.Background()

	g1, err := p.EnsureGroup(ctx, 10)
	if err != nil {
		t.Fatalf("首次分组失败: %v", err)
	}
	if g1 != model.GroupA && g1 != model.GroupB {
		t.Fatalf("非法分组值: %s", g1)
	}
	g2, err := p.EnsureGroup(ctx, 10)
	if err != nil {
		t.Fatalf("二次读取分组失败: %v", err)
	}
	if g1 != g2 {
		t.Errorf("分组应不可变：首次 %s，再次 %s", g1, g2)
	}
	if groups.groups[10] != g1 {
		t.Error("分组应已持久化")
	}
}

// TestEnsureGroup_RandomDistribution 固定种子下两组都能出现，比例约 1:1。
func TestEnsureGroup_RandomDistribution(t *testing.T) {
	p, _, _, _ := newPlanner()
	ctx := context.Background()
	counts := map[model.ControlGroup]int{}
	for uid := int64(1); uid <= 1000; uid++ {
		g, err := p.EnsureGroup(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		counts[g]++
	}
	if counts[model.GroupA] < 400 || counts[model.GroupB] < 400 {
		t.Errorf("分组比例失衡: A=%d B=%d", counts[model.GroupA], counts[model.GroupB])
	}
}

// TestPrepare_FirstSessionMode 首训：分组随机，第 1 次 ai_mode 与组序一致。
func TestPrepare_FirstSessionMode(t *testing.T) {
	p, _, _, _ := newPlanner()
	plan, err := p.PrepareSessionPlan(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Attempt != 1 {
		t.Fatalf("首训序号应为 1，实际 %d", plan.Attempt)
	}
	want, err := PlanAIMode(plan.Group, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AIMode != want {
		t.Errorf("首训 ai_mode=%s，应与组 %s 的第 1 次 %s 一致", plan.AIMode, plan.Group, want)
	}
	if plan.FirstSessionID != 0 || plan.PairedDefect != nil {
		t.Error("首训不应有配对信息")
	}
}

// TestPrepare_SecondSessionPairedAndBound A/B 两组分别走一遍完整两次训练：
// 模式顺序正确、第二次选出不同的同类同难度故障、双向绑定成功。
func TestPrepare_SecondSessionPairedAndBound(t *testing.T) {
	for _, forcedGroup := range []model.ControlGroup{model.GroupA, model.GroupB} {
		g := newFakeGroupStore()
		s := newFakeSessionStore()
		d := newFakeDefectCatalog()
		p := NewCrossGroupPlanner(g, s, d, rand.New(rand.NewSource(1)))
		ctx := context.Background()
		const uid, pid = int64(77), int64(900)
		seedDatabaseDefects(d, pid)

		// 预置用户分组，使两组路径都被覆盖。
		g.set(uid, forcedGroup)

		// 第一场：模拟会话创建编排按计划建会话、挂首场故障 d1。
		firstPlan, err := p.PrepareSessionPlan(ctx, uid)
		if err != nil {
			t.Fatalf("[%s] 首训计划失败: %v", forcedGroup, err)
		}
		firstMode := model.AIModeNone
		if forcedGroup == model.GroupB {
			firstMode = model.AIModeAssist
		}
		if firstPlan.AIMode != firstMode {
			t.Errorf("[%s] 首场模式应为 %s，实际 %s", forcedGroup, firstMode, firstPlan.AIMode)
		}
		first := s.addSession(uid, pid, forcedGroup, firstPlan.AIMode)
		d.sessionDefects[first.ID] = []PairingDefect{
			{ID: 1, Code: "db-slow", Category: "database", Difficulty: "easy"},
		}

		// 第二场计划：应给出互补模式 + 校准故障 d2 + 指向首场。
		secondPlan, err := p.PrepareSessionPlan(ctx, uid)
		if err != nil {
			t.Fatalf("[%s] 第二次计划失败: %v", forcedGroup, err)
		}
		if secondPlan.Attempt != 2 {
			t.Fatalf("[%s] 第二次序号应为 2，实际 %d", forcedGroup, secondPlan.Attempt)
		}
		if !ModesComplement(firstPlan.AIMode, secondPlan.AIMode) {
			t.Errorf("[%s] 两次模式不互补: %s vs %s",
				forcedGroup, firstPlan.AIMode, secondPlan.AIMode)
		}
		if secondPlan.FirstSessionID != first.ID {
			t.Errorf("[%s] 配对首场 id=%d，期望 %d", forcedGroup, secondPlan.FirstSessionID, first.ID)
		}
		if secondPlan.PairedDefect == nil || secondPlan.PairedDefect.ID != 2 {
			t.Fatalf("[%s] 第二次校准故障应选 d2(id=2)，实际 %+v", forcedGroup, secondPlan.PairedDefect)
		}

		// 模拟第二场会话按计划创建并挂载 d2，随后绑定。
		second := s.addSession(uid, pid, forcedGroup, secondPlan.AIMode)
		d.sessionDefects[second.ID] = []PairingDefect{*secondPlan.PairedDefect}
		link, err := p.BindPair(ctx, second.ID)
		if err != nil {
			t.Fatalf("[%s] 绑定失败: %v", forcedGroup, err)
		}
		if link.FirstSessionID != first.ID || link.SecondSessionID != second.ID {
			t.Errorf("[%s] 绑定结果错误: %+v", forcedGroup, link)
		}
		gotFirst, _ := s.GetSession(ctx, first.ID)
		gotSecond, _ := s.GetSession(ctx, second.ID)
		if gotFirst.PairSessionID != second.ID || gotSecond.PairSessionID != first.ID {
			t.Errorf("[%s] 双向互指错误: %d<->%d",
				forcedGroup, gotFirst.PairSessionID, gotSecond.PairSessionID)
		}

		// 重复绑定幂等返回 AlreadyLinked。
		again, err := p.BindPair(ctx, second.ID)
		if err != nil {
			t.Fatalf("[%s] 重复绑定应幂等: %v", forcedGroup, err)
		}
		if !again.AlreadyLinked {
			t.Errorf("[%s] 重复绑定应标记 AlreadyLinked", forcedGroup)
		}
	}
}

// TestPrepare_ThirdSessionFallback 第三次起返回哨兵错误，供调用方回退普通训练。
func TestPrepare_ThirdSessionFallback(t *testing.T) {
	p, _, s, d := newPlanner()
	ctx := context.Background()
	const uid, pid = int64(55), int64(800)
	seedDatabaseDefects(d, pid)
	g, _ := p.EnsureGroup(ctx, uid)

	// 造两场已配对完成的会话（不经过 BindPair，直接造数据）。
	for i, mode := range []model.AIMode{model.AIModeNone, model.AIModeAssist} {
		_ = i
		gs := s.addSession(uid, pid, g, mode)
		s.sessions[len(s.sessions)-1].PairSessionID = gs.ID + 1000
	}
	_, err := p.PrepareSessionPlan(ctx, uid)
	if !errors.Is(err, ErrBeyondCrossGroup) {
		t.Fatalf("第三次应返回 ErrBeyondCrossGroup，实际 %v", err)
	}
}

// TestBind_RejectsSameFault 第二次若复用首场故障，绑定必须被拒绝。
func TestBind_RejectsSameFault(t *testing.T) {
	p, _, s, d := newPlanner()
	ctx := context.Background()
	const uid, pid = int64(33), int64(700)
	seedDatabaseDefects(d, pid)
	first := s.addSession(uid, pid, model.GroupA, model.AIModeNone)
	second := s.addSession(uid, pid, model.GroupA, model.AIModeAssist)
	same := PairingDefect{ID: 1, Code: "db-slow", Category: "database", Difficulty: "easy"}
	d.sessionDefects[first.ID] = []PairingDefect{same}
	d.sessionDefects[second.ID] = []PairingDefect{same} // 复用同一故障

	if _, err := p.BindPair(ctx, second.ID); err == nil {
		t.Fatal("复用首场故障时绑定应被拒绝")
	}
}

// TestBind_RejectsDifferentCategory 第二次故障类目不同也必须拒绝（不得放宽校准）。
func TestBind_RejectsDifferentCategory(t *testing.T) {
	p, _, s, d := newPlanner()
	ctx := context.Background()
	const uid, pid = int64(44), int64(600)
	first := s.addSession(uid, pid, model.GroupB, model.AIModeAssist)
	second := s.addSession(uid, pid, model.GroupB, model.AIModeNone)
	d.sessionDefects[first.ID] = []PairingDefect{
		{ID: 1, Code: "db-slow", Category: "database", Difficulty: "easy"},
	}
	d.sessionDefects[second.ID] = []PairingDefect{
		{ID: 4, Code: "net-loss", Category: "network", Difficulty: "easy"}, // 同难度不同类
	}
	if _, err := p.BindPair(ctx, second.ID); err == nil {
		t.Fatal("不同类目故障配对应被拒绝")
	}
}
