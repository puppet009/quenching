// Package service 中人机对照交叉分组的编排服务（T3-25）。
//
// 职责边界：本文件只做"首训前分组 → 按组与第几次训练决定 ai_mode →
// 第二次找首场配对并选同类同难度故障 → 两次会话双向绑定"的编排，
// 不负责创建会话/沙箱（属 T2-9 会话创建流，云上接线）。
// 外部依赖全部抽象为端口：用户分组经 user-service 内部 HTTP 端口读写，
// 会话在本服务 training_sessions，故障目录经 fault-engine 内部 HTTP 端口读取。
package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	"github.com/quenching/training-service/internal/model"
)

// ErrBeyondCrossGroup 用户已完成前两次对照训练，第 3 次起不再套用交叉模式。
// 调用方（会话创建编排）收到该错误应回退到普通训练流程（用户自选 ai_mode/难度）。
var ErrBeyondCrossGroup = errors.New("该用户已完成交叉分组的前两次训练")

// ErrCollabGroupStoreUnavailable 用户分组端口未配置或不可用。
var ErrCollabGroupStoreUnavailable = errors.New("用户交叉分组端口不可用")

// CollabGroupStore 用户级长期对照分组端口（数据属主为 user-service，
// training 侧只经内部 HTTP 接口访问）。
type CollabGroupStore interface {
	// GetCollabGroup 取用户分组；注册后尚未首训分配时返回 ("", nil)。
	GetCollabGroup(ctx context.Context, userID int64) (model.ControlGroup, error)
	// AssignCollabGroupIfAbsent 仅当用户尚无分组时条件写入，保证并发首训不串组：
	// 首次写入 applied=true；被并发抢先时不覆盖，applied=false 并返回已生效分组。
	AssignCollabGroupIfAbsent(
		ctx context.Context, userID int64, g model.ControlGroup,
	) (final model.ControlGroup, applied bool, err error)
}

// GroupSession 配对编排所需的会话最小视图（training_sessions 投影）。
type GroupSession struct {
	ID            int64
	UserID        int64
	ProjectID     int64
	Status        model.SessionStatus
	AIMode        model.AIMode
	ControlGroup  model.ControlGroup
	PairSessionID int64
}

// SessionPairStore 会话侧计数/配对端口（PG 实现在 internal/repository）。
type SessionPairStore interface {
	// CountSessions 用户历史训练会话数（排除软删），第几次训练 = 计数 + 1。
	CountSessions(ctx context.Context, userID int64) (int, error)
	// GetSession 取单个会话视图；不存在返回 (nil, nil)。
	GetSession(ctx context.Context, sessionID int64) (*GroupSession, error)
	// ListUserSessions 按 id 升序列出用户全部未删除会话。
	ListUserSessions(ctx context.Context, userID int64) ([]GroupSession, error)
	// LinkPair 把两次会话双向互指 pair_session_id（同事务、同条件更新，幂等安全）。
	LinkPair(ctx context.Context, sessionA, sessionB int64) error
}

// DefectCatalog 故障目录只读端口（fault-engine.defects 属主，共库只读，不建编译依赖）。
type DefectCatalog interface {
	// SessionDefects 取某次会话已挂载的全部故障（联 session_defects→defects，id 升序）。
	SessionDefects(ctx context.Context, sessionID int64) ([]PairingDefect, error)
	// ListProjectDefects 取某项目下全部在用故障（is_active、未删除），供配对筛选。
	ListProjectDefects(ctx context.Context, projectID int64) ([]PairingDefect, error)
}

// SessionPlan 一次训练开训前的交叉分组决策结果，供会话创建编排消费。
type SessionPlan struct {
	UserID  int64
	Attempt int // 1 或 2
	Group   model.ControlGroup
	AIMode  model.AIMode
	// 以下仅第二次训练有值：
	FirstSessionID int64          // 与之配对的首场会话
	PairedDefect   *PairingDefect // 第二次应挂载的校准故障（同类同难度、不同于首场）
}

// CrossGroupPlanner 交叉分组编排器，零外部硬依赖，可用内存 fake 全链路单测。
type CrossGroupPlanner struct {
	groups   CollabGroupStore
	sessions SessionPairStore
	defects  DefectCatalog
	rng      *rand.Rand
}

// NewCrossGroupPlanner 创建编排器；rng 传 nil 时用时间种子（生产），
// 单测注入固定 rand.Rand 保证确定性。
func NewCrossGroupPlanner(
	groups CollabGroupStore, sessions SessionPairStore, defects DefectCatalog, rng *rand.Rand,
) *CrossGroupPlanner {
	return &CrossGroupPlanner{groups: groups, sessions: sessions, defects: defects, rng: rng}
}

// EnsureGroup 返回用户分组；首次调用时随机分配并条件持久化（幂等、不可变）。
func (p *CrossGroupPlanner) EnsureGroup(ctx context.Context, userID int64) (model.ControlGroup, error) {
	if p.groups == nil {
		return "", ErrCollabGroupStoreUnavailable
	}
	g, err := p.groups.GetCollabGroup(ctx, userID)
	if err != nil {
		return "", err
	}
	if g == model.GroupA || g == model.GroupB {
		return g, nil
	}
	if g != "" {
		return "", fmt.Errorf("用户 %d 的对照分组为非法值 %q", userID, g)
	}
	desired := AssignRandomGroup(p.rng)
	final, _, err := p.groups.AssignCollabGroupIfAbsent(ctx, userID, desired)
	if err != nil {
		return "", err
	}
	return final, nil
}

// PrepareSessionPlan 在开训前计算本次训练的分组/模式与配对故障。
// 第 3 次及以后返回 ErrBeyondCrossGroup，调用方回退普通训练流程。
func (p *CrossGroupPlanner) PrepareSessionPlan(ctx context.Context, userID int64) (*SessionPlan, error) {
	group, err := p.EnsureGroup(ctx, userID)
	if err != nil {
		return nil, err
	}
	count, err := p.sessions.CountSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	attempt := count + 1
	if attempt > AttemptSecond {
		return nil, fmt.Errorf("%w：当前第 %d 次", ErrBeyondCrossGroup, attempt)
	}
	mode, err := PlanAIMode(group, attempt)
	if err != nil {
		return nil, err
	}
	plan := &SessionPlan{UserID: userID, Attempt: attempt, Group: group, AIMode: mode}

	if attempt == AttemptSecond {
		first, err := p.findUnpairedFirst(ctx, userID, group, mode)
		if err != nil {
			return nil, err
		}
		d, err := p.pickCalibratedDefect(ctx, first)
		if err != nil {
			return nil, err
		}
		plan.FirstSessionID = first.ID
		plan.PairedDefect = &d
	}
	return plan, nil
}

// PairLink BindPair 的结果。
type PairLink struct {
	FirstSessionID  int64
	SecondSessionID int64
	AlreadyLinked   bool // 第二次会话此前已绑定（幂等返回）
}

// BindPair 在第二次会话创建（已挂校准故障）后，校验对照有效性并双向绑定。
// 校验：两次模式互补（none↔assistant）、故障互不重复且同类同难度。
func (p *CrossGroupPlanner) BindPair(ctx context.Context, secondSessionID int64) (*PairLink, error) {
	second, err := p.sessions.GetSession(ctx, secondSessionID)
	if err != nil {
		return nil, err
	}
	if second == nil {
		return nil, fmt.Errorf("会话 %d 不存在", secondSessionID)
	}
	if second.PairSessionID > 0 {
		return &PairLink{
			FirstSessionID: second.PairSessionID, SecondSessionID: secondSessionID, AlreadyLinked: true,
		}, nil
	}
	if second.ControlGroup != model.GroupA && second.ControlGroup != model.GroupB {
		return nil, fmt.Errorf("会话 %d 缺少对照分组，无法配对", secondSessionID)
	}

	first, err := p.findUnpairedFirst(ctx, second.UserID, second.ControlGroup, second.AIMode)
	if err != nil {
		return nil, err
	}

	firstDefects, err := p.defects.SessionDefects(ctx, first.ID)
	if err != nil {
		return nil, err
	}
	secondDefects, err := p.defects.SessionDefects(ctx, second.ID)
	if err != nil {
		return nil, err
	}
	if err := validateCalibratedPair(firstDefects, secondDefects); err != nil {
		return nil, err
	}

	if err := p.sessions.LinkPair(ctx, first.ID, second.ID); err != nil {
		return nil, err
	}
	return &PairLink{FirstSessionID: first.ID, SecondSessionID: second.ID}, nil
}

// findUnpairedFirst 找该用户同组、尚未配对、且与本次模式互补的最早一场会话。
func (p *CrossGroupPlanner) findUnpairedFirst(
	ctx context.Context, userID int64, group model.ControlGroup, secondMode model.AIMode,
) (*GroupSession, error) {
	sessions, err := p.sessions.ListUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	var first *GroupSession
	for i := range sessions {
		s := &sessions[i]
		if s.ControlGroup != group || s.PairSessionID > 0 {
			continue
		}
		if !ModesComplement(s.AIMode, secondMode) {
			continue
		}
		if first == nil || s.ID < first.ID {
			first = s
		}
	}
	if first == nil {
		return nil, fmt.Errorf("用户 %d 的 %s 组缺少模式互补的未配对首场会话", userID, group)
	}
	return first, nil
}

// pickCalibratedDefect 依据首场会话故障选出第二次的校准故障。
func (p *CrossGroupPlanner) pickCalibratedDefect(ctx context.Context, first *GroupSession) (PairingDefect, error) {
	firstDefects, err := p.defects.SessionDefects(ctx, first.ID)
	if err != nil {
		return PairingDefect{}, err
	}
	if len(firstDefects) == 0 {
		return PairingDefect{}, fmt.Errorf("首场会话 %d 未挂载任何故障，无法做难度校准", first.ID)
	}
	anchor := firstDefects[0] // SessionDefects 已按 id 升序，以最早挂载故障为校准锚点
	used := make(map[int64]bool, len(firstDefects))
	for _, d := range firstDefects {
		used[d.ID] = true
	}
	candidates, err := p.defects.ListProjectDefects(ctx, first.ProjectID)
	if err != nil {
		return PairingDefect{}, err
	}
	return ChoosePairedDefect(anchor, used, candidates)
}

// validateCalibratedPair 绑定前复核两次会话实际挂载的故障满足配对约束。
func validateCalibratedPair(firstDefects, secondDefects []PairingDefect) error {
	if len(firstDefects) == 0 || len(secondDefects) == 0 {
		return fmt.Errorf("配对双方都必须已挂载故障")
	}
	anchor := firstDefects[0]
	used := make(map[int64]bool, len(firstDefects))
	for _, d := range firstDefects {
		used[d.ID] = true
	}
	for _, d := range secondDefects {
		if used[d.ID] {
			return fmt.Errorf("第二次训练复用了首场故障 %s，对照配对要求不同故障", d.Code)
		}
		if d.Category != anchor.Category || d.Difficulty != anchor.Difficulty {
			return fmt.Errorf("故障 %s 与首场 %s 不是同类同难度（%s/%s vs %s/%s）",
				d.Code, anchor.Code, d.Category, d.Difficulty, anchor.Category, anchor.Difficulty)
		}
	}
	return nil
}
