package service

import (
	"context"
	"errors"
	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	"log"
	"time"
)

type AllocationStore interface {
	WithAllocationLock(context.Context, func(context.Context) error) error
	AllocationCandidates(context.Context) ([]model.Sandbox, error)
	ListReservations(context.Context) ([]model.Sandbox, error)
	Admit(context.Context, int64) (bool, error)
	SetAllocationReason(context.Context, int64, string) error
}
type ResourceQueue struct {
	Store    AllocationStore
	Snapshot func(context.Context) (model.PoolSnapshot, error)
	Deploy   func(context.Context, model.Sandbox) error
}

func (q *ResourceQueue) Tick(ctx context.Context) error {
	if q.Store == nil || q.Snapshot == nil || q.Deploy == nil {
		return errors.New("resource queue unavailable")
	}
	return q.Store.WithAllocationLock(ctx, func(ctx context.Context) error {
		list, e := q.Store.AllocationCandidates(ctx)
		if e != nil {
			return e
		}
		for _, sb := range list {
			if !sb.CompletedAt.IsZero() || sb.Status != model.StatusQueued && sb.Status != model.StatusDeploying || sb.IsExpired(time.Now()) {
				continue
			}
			if !sb.ResourcesAdmitted {
				snap, e := q.Snapshot(ctx)
				if e != nil {
					_ = q.Store.SetAllocationReason(ctx, sb.SessionID, "资源池暂不可读，等待恢复")
					return e
				}
				reservations, e := q.Store.ListReservations(ctx)
				if e != nil {
					return e
				}
				ok, reason := CanAdmit(snap, sb.ResourcePlan, reservations)
				if !ok {
					if e = q.Store.SetAllocationReason(ctx, sb.SessionID, reason); e != nil {
						return e
					}
					continue
				}
				ok, e = q.Store.Admit(ctx, sb.SessionID)
				if e != nil {
					return e
				}
				if !ok {
					continue
				}
				sb.ResourcesAdmitted = true
				sb.Status = model.StatusDeploying
			}
			if e = q.Deploy(ctx, sb); e != nil {
				_ = q.Store.SetAllocationReason(ctx, sb.SessionID, "部署未完成，请检查启动进度")
				log.Printf("allocation session %d: %v", sb.SessionID, e)
			}
		}
		return nil
	})
}
func (q *ResourceQueue) Run(ctx context.Context) {
	timer := time.NewTicker(5 * time.Second)
	defer timer.Stop()
	for {
		if e := q.Tick(ctx); e != nil && ctx.Err() == nil {
			log.Printf("resource queue: %v", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (s *SandboxService) EnableResourceQueue(q *ResourceQueue) { s.allocation = q }
func (s *SandboxService) ConvergeAllocation(ctx context.Context, sb model.Sandbox) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	getter, ok := s.db.(ProgressStore)
	if !ok {
		return errors.New("allocation state reader unavailable")
	}
	current, e := getter.GetBySessionID(ctx, sb.SessionID)
	if e != nil {
		return e
	}
	if current == nil || current.Status != model.StatusDeploying || !current.CompletedAt.IsZero() {
		return nil
	}
	sb = *current
	// Closing/releasing a queued or deploying session cancels this deployment.
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	ctx = watchCtx
	go func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				v, err := getter.GetBySessionID(ctx, sb.SessionID)
				if err == nil && (v == nil || v.Status != model.StatusDeploying || !v.CompletedAt.IsZero()) {
					watchCancel()
					return
				}
			}
		}
	}()
	manifest, ok := s.registry.Get(sb.TemplateSlug)
	if !ok || manifest.ArtifactDigest != sb.TemplateDigest {
		_ = s.db.MarkStatus(ctx, sb.SessionID, model.StatusFailed)
		return errors.New("模板已变更，需重新创建实训")
	}
	if s.provision == nil {
		return errors.New("real provisioner unavailable")
	}
	if _, e = s.rec.EnsureSandbox(ctx, &sb); e != nil {
		return e
	}
	exists, e := s.provision.deployer.Exists(ctx, sb.Namespace, manifest.Slug)
	if e != nil {
		return e
	}
	if exists {
		// Uncertain/partial recovery preserves the existing environment and reservation.
		if e = s.verifyAllocatedRelease(ctx, sb.Namespace, manifest.Slug); e != nil {
			return e
		}
	} else {
		e = s.deployChart(ctx, &sb, manifest)
	}
	if e != nil {
		if ctx.Err() == nil {
			_ = s.db.MarkStatus(ctx, sb.SessionID, model.StatusFailed)
		}
		return e
	}
	if !exists {
		if e = s.verifyAllocatedRelease(ctx, sb.Namespace, manifest.Slug); e != nil {
			return e
		}
	}
	writer, ok := s.db.(interface {
		MarkAllocationReady(context.Context, int64) error
	})
	if !ok {
		return errors.New("allocation readiness writer unavailable")
	}
	if e = writer.MarkAllocationReady(ctx, sb.SessionID); e != nil {
		return e
	}
	s.startBehavior(&sb, &manifest)
	s.registerDefaultAlerts(&sb)
	return nil
}
func (s *SandboxService) enqueue(ctx context.Context, uid, pid, sid int64, m templates.Manifest) (*model.Sandbox, error) {
	if m.Slug == "" {
		return nil, ErrSandboxTemplateNotFound
	}
	plan, e := BuildResourcePlan(m)
	if e != nil {
		return nil, e
	}
	sb := &model.Sandbox{SessionID: sid, UserID: uid, ProjectID: pid, Namespace: model.NamespaceName(sid), Status: model.StatusQueued, TemplateSlug: m.Slug, TemplateDigest: m.ArtifactDigest, ResourcePlan: plan}
	writer, ok := s.db.(interface {
		CreateQueued(context.Context, *model.Sandbox) error
	})
	if !ok {
		return nil, errors.New("durable allocation queue unavailable")
	}
	if getter, ok := s.db.(ProgressStore); ok {
		old, err := getter.GetBySessionID(ctx, sid)
		if err != nil {
			return nil, err
		}
		if old != nil {
			if err = ValidateQueuedReuse(*old, *sb, time.Now()); err != nil {
				return nil, err
			}
		}
	}
	if e = writer.CreateQueued(ctx, sb); e != nil {
		return nil, e
	}
	return sb, nil
}

// The session ID is a lifetime idempotency key; a closed environment needs a new session.
func ValidateQueuedReuse(old, want model.Sandbox, now time.Time) error {
	if old.UserID != want.UserID || old.ProjectID != want.ProjectID || old.TemplateSlug != want.TemplateSlug || old.TemplateDigest != want.TemplateDigest || old.Status == model.StatusReleased || old.Status == model.StatusReleasing || old.Status == model.StatusFailed || !old.CompletedAt.IsZero() || old.IsExpired(now) {
		return ErrSandboxConflict.Unwrap()
	}
	return nil
}
