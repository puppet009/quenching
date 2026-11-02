// Package service 训练会话生命周期编排。
// 这里只做"领域规则 + 状态流转"，不直接操作 K8s/DB——外部依赖抽象成接口，
// 因此整个会话状态机可在无集群/无数据库下用 fake 单测。
// 真正调沙箱、故障引擎、复盘走下游接口（云上接线）。
package service

import (
	"fmt"
	"time"

	"github.com/quenching/training-service/internal/model"
)

// Clock 可注入时钟。
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// LifecycleError 非法状态流转错误。
type LifecycleError struct {
	From, To model.SessionStatus
}

func (e *LifecycleError) Error() string {
	return fmt.Sprintf("非法状态流转: %s -> %s", e.From, e.To)
}

// SessionLifecycle 对一个会话做状态推进。持有会话聚合根并强制不变量。
type SessionLifecycle struct {
	sess         *model.Session
	clock        Clock
	directorMode bool
}

func NewSessionLifecycle(sess *model.Session, clock Clock) *SessionLifecycle {
	if clock == nil {
		clock = systemClock{}
	}
	return &SessionLifecycle{sess: sess, clock: clock}
}

// Session 返回当前会话快照。
func (l *SessionLifecycle) Session() *model.Session { return l.sess }

func (l *SessionLifecycle) transition(to model.SessionStatus) error {
	if !model.CanTransition(l.sess.Status, to) {
		return &LifecycleError{From: l.sess.Status, To: to}
	}
	l.sess.Status = to
	l.sess.UpdatedAt = l.clock.Now()
	return nil
}

// DeployBegin 沙箱开始部署：pending -> deploying。
func (l *SessionLifecycle) DeployBegin(sandboxID int64) error {
	if err := l.transition(model.StatusDeploying); err != nil {
		return err
	}
	l.sess.SandboxID = sandboxID
	return nil
}

// Ready 沙箱就绪且故障武装完成：deploying -> armed。
func (l *SessionLifecycle) Ready() error {
	return l.transition(model.StatusArmed)
}

// Start 实训正式开始（用户进入）：armed -> running，记录开始时间。
// 部署态也允许直接开始（无武装延迟的场景）。
func (l *SessionLifecycle) Start() error {
	if l.sess.Status == model.StatusDeploying {
		if err := l.transition(model.StatusArmed); err != nil {
			return err
		}
	}
	if err := l.transition(model.StatusRunning); err != nil {
		return err
	}
	now := l.clock.Now()
	if l.sess.StartedAt == nil {
		l.sess.StartedAt = &now
	}
	return nil
}

// MarkFaultResolved 故障被判定修复：running -> resolved，记录时间、置修复标记。
func (l *SessionLifecycle) MarkFaultResolved() error {
	if l.directorMode {
		if l.sess.Status != model.StatusRunning {
			return &LifecycleError{From: l.sess.Status, To: model.StatusResolved}
		}
		return nil
	}
	if err := l.transition(model.StatusResolved); err != nil {
		return err
	}
	now := l.clock.Now()
	l.sess.ResolvedAt = &now
	l.sess.FaultResolved = true
	return nil
}

// Complete 实训正常结束进入复盘：running/resolved -> completed。
func (l *SessionLifecycle) Complete() error {
	if err := l.transition(model.StatusCompleted); err != nil {
		return err
	}
	now := l.clock.Now()
	l.sess.CompletedAt = &now
	return nil
}

// Abort 用户主动结束/超时：任意活跃态 -> aborted（需释放沙箱）。
func (l *SessionLifecycle) Abort() error {
	if model.IsTerminal(l.sess.Status) {
		return nil // 幂等
	}
	return l.transition(model.StatusAborted)
}

// Fail 异常失败：-> failed。
func (l *SessionLifecycle) Fail() error {
	if model.IsTerminal(l.sess.Status) {
		return nil
	}
	return l.transition(model.StatusFailed)
}

// ApplyScores 合并双维评分（技术分本服务算，协作分由 ai-service 提供）。
// 仅在实训进入 resolved/completed 后允许写最终分，避免中途评分被当成结论。
func (l *SessionLifecycle) ApplyScores(tech, collab *float64) error {
	if l.sess.Status != model.StatusResolved && l.sess.Status != model.StatusCompleted {
		return fmt.Errorf("当前状态 %s 不允许写入最终评分", l.sess.Status)
	}
	if tech != nil {
		l.sess.TechScore = tech
	}
	if collab != nil {
		l.sess.CollabScore = collab
	}
	l.sess.UpdatedAt = l.clock.Now()
	return nil
}

// AssignGroup 分配人机对照分组（首训开始前），仅在尚未分配时生效（幂等，不可改）。
func (l *SessionLifecycle) AssignGroup(g model.ControlGroup) error {
	if g != model.GroupA && g != model.GroupB {
		return fmt.Errorf("非法分组: %s", g)
	}
	if l.sess.ControlGroup != "" && l.sess.ControlGroup != g {
		return fmt.Errorf("分组已确定为 %s，不可修改", l.sess.ControlGroup)
	}
	l.sess.ControlGroup = g
	l.sess.UpdatedAt = l.clock.Now()
	return nil
}

// LinkPair 绑定人机对照的另一次会话。
func (l *SessionLifecycle) LinkPair(otherSessionID int64) error {
	if otherSessionID <= 0 {
		return fmt.Errorf("配对会话ID非法")
	}
	l.sess.PairSessionID = otherSessionID
	l.sess.UpdatedAt = l.clock.Now()
	return nil
}

func (l *SessionLifecycle) SetDirectorMode(enabled bool) { l.directorMode = enabled }
