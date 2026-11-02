// Package repository 注入实验数据访问层。
//
// ExperimentRepository 抽象 experiments 表的持久化。仓储只负责"登记与状态流转"，
// 真正的 blade create/destroy 由 blade.Executor 完成；编排器（scenario 链路）与
// runtime 服务（临时注入链路）通过下列方法把注入过程落库，崩溃重启后可按
// session_id 补偿撤销。
package repository

import (
	"context"
	"sync"
	"time"

	"github.com/quenching/fault-engine/internal/model"
)

// ExperimentRepository 注入实验仓储接口。
type ExperimentRepository interface {
	// Create 登记一条实验记录（status 取入参 e.Status，注入前通常为 created）。
	Create(ctx context.Context, e *model.Experiment) error

	// MarkRunning 注入成功：落 running + blade uid + 实际命令。
	MarkRunning(ctx context.Context, id int64, uid, command string) error
	// MarkRevoked 恢复成功：落 revoked。仅 running 记录生效，幂等（重复撤销不报错）。
	MarkRevoked(ctx context.Context, id int64) error
	// MarkFailed 注入/恢复失败：落 failed 与中文原因。
	MarkFailed(ctx context.Context, id int64, reason string) error

	// GetByUID 按 blade uid 查询（撤销时定位记录）。
	GetByUID(ctx context.Context, sessionID int64, uid string) (*model.Experiment, error)
	// ListActiveBySession 查询会话下未终结的实验（created/running）。
	ListActiveBySession(ctx context.Context, sessionID int64) ([]*model.Experiment, error)
	// ListBySession 查询会话下全部实验（运行时接口按会话展示注入历史）。
	ListBySession(ctx context.Context, sessionID int64) ([]*model.Experiment, error)
}

// ——— 内存实现（供单测 / 开发期使用）———

// InMemoryExperimentRepo 内存版实验仓储，线程安全。
type InMemoryExperimentRepo struct {
	mu     sync.RWMutex
	items  map[int64]*model.Experiment
	nextID int64
}

// NewInMemoryExperimentRepo 创建内存实验仓储。
func NewInMemoryExperimentRepo() *InMemoryExperimentRepo {
	return &InMemoryExperimentRepo{items: make(map[int64]*model.Experiment), nextID: 1}
}

// Create 登记实验。
func (r *InMemoryExperimentRepo) Create(_ context.Context, e *model.Experiment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e.ID = r.nextID
	r.nextID++
	e.CreatedAt, e.UpdatedAt = now, now
	clone := *e
	r.items[e.ID] = &clone
	return nil
}

// MarkRunning 置 running。
func (r *InMemoryExperimentRepo) MarkRunning(_ context.Context, id int64, uid, command string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.items[id]
	if !ok {
		return ErrExperimentNotFound
	}
	e.Status = model.StatusRunning
	e.ExperimentUID = uid
	e.Command = command
	e.FailReason = ""
	e.UpdatedAt = time.Now()
	return nil
}

// MarkRevoked 置 revoked，幂等。
func (r *InMemoryExperimentRepo) MarkRevoked(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.items[id]
	if !ok {
		return ErrExperimentNotFound
	}
	if e.Status == model.StatusRunning || e.Status == model.StatusCreated {
		e.Status = model.StatusRevoked
		e.UpdatedAt = time.Now()
	}
	return nil
}

// MarkFailed 置 failed。
func (r *InMemoryExperimentRepo) MarkFailed(_ context.Context, id int64, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.items[id]
	if !ok {
		return ErrExperimentNotFound
	}
	e.Status = model.StatusFailed
	e.FailReason = reason
	e.UpdatedAt = time.Now()
	return nil
}

// GetByUID 按 uid 查询（带会话归属校验）。
func (r *InMemoryExperimentRepo) GetByUID(_ context.Context, sessionID int64, uid string) (*model.Experiment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.items {
		if e.SessionID == sessionID && e.ExperimentUID == uid {
			clone := *e
			return &clone, nil
		}
	}
	return nil, ErrExperimentNotFound
}

// ListActiveBySession 列出未终结实验，按 ID 升序。
func (r *InMemoryExperimentRepo) ListActiveBySession(_ context.Context, sessionID int64) ([]*model.Experiment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.Experiment, 0)
	for id := int64(1); id < r.nextID; id++ {
		e := r.items[id]
		if e.SessionID == sessionID && (e.Status == model.StatusCreated || e.Status == model.StatusRunning) {
			clone := *e
			out = append(out, &clone)
		}
	}
	return out, nil
}

// ListBySession 列出会话下全部实验，按 ID 升序。
func (r *InMemoryExperimentRepo) ListBySession(_ context.Context, sessionID int64) ([]*model.Experiment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*model.Experiment, 0)
	for id := int64(1); id < r.nextID; id++ {
		if e := r.items[id]; e.SessionID == sessionID {
			clone := *e
			out = append(out, &clone)
		}
	}
	return out, nil
}
