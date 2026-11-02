package service

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
	"time"
)

type OperationSealer interface {
	Seal(context.Context, int64, string, time.Time) (oe.AuditSeal, error)
	SaveRemoteSeal(context.Context, int64, oe.AuditSeal) error
}
type RemoteOperationSealer interface {
	SealOperations(context.Context, int64) (oe.AuditSeal, error)
}
type OperationSealService struct {
	Local           OperationSealer
	Remote          RemoteOperationSealer
	ProjectionReady func(context.Context, int64) error
}

func (s *OperationSealService) Advance(ctx context.Context, sid int64, now time.Time) (oe.AuditSeal, error) {
	local, err := s.Local.Seal(ctx, sid, "training", now)
	if err != nil {
		return local, err
	}
	remote := oe.AuditSeal{SessionID: sid, Producer: "sandbox", Deadline: local.Deadline, State: "pending", Pending: 1}
	if s.Remote != nil {
		v, e := s.Remote.SealOperations(ctx, sid)
		if e == nil {
			remote = v
		}
	}
	if remote.Deadline.IsZero() || remote.Deadline.After(local.Deadline) {
		remote.Deadline = local.Deadline
	}
	if s.ProjectionReady != nil {
		// Canonical upload acknowledgement can precede the legacy operation's
		// projection into training. Every report waits here, including outcomes
		// that do not qualify for scoring. Retain the persisted audit deadline.
		projectionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		projectionErr := s.ProjectionReady(projectionCtx, sid)
		cancel()
		if projectionErr != nil {
			remote.Pending++
			if remote.State != "timed_out" {
				remote.State = "pending"
			}
			if !now.Before(remote.Deadline) {
				remote.Gaps = append(remote.Gaps, "legacy_projection_timeout")
			}
		}
	}
	remote = oe.EvaluateSeal(remote, now)
	if err = s.Local.SaveRemoteSeal(ctx, sid, remote); err != nil {
		return local, err
	}
	local.Pending += remote.Pending
	local.Gaps = append(local.Gaps, remote.Gaps...)
	if local.State == "timed_out" || remote.State == "timed_out" {
		local.State = "timed_out"
	} else if local.State != "ready" || remote.State != "ready" {
		local.State = "pending"
	}
	return local, nil
}
