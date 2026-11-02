package service

import (
	"context"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"time"
)

type ToolRecovery struct {
	Store   oe.Store
	Orphans func(context.Context) ([]oe.Evidence, error)
}

func (r ToolRecovery) Recover(ctx context.Context) error {
	if r.Store == nil || r.Orphans == nil {
		return errors.New("tool recovery not configured")
	}
	events, err := r.Orphans(ctx)
	if err != nil {
		return err
	}
	for _, e := range events {
		if e.Kind != "tool" || oe.Terminal(e.Status) {
			continue
		}
		v := oe.Next(e, "unknown")
		v.ExitCode = nil
		v.Coverage.Execution = "unknown"
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "result_unconfirmed")
		if v.Approval == "requested" {
			v.Approval = "expired"
		}
		if err = r.Store.Append(ctx, v, nil); err != nil && !errors.Is(err, oe.ErrConflict) {
			return err
		}
	}
	return nil
}
func (r ToolRecovery) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		_ = r.Recover(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
