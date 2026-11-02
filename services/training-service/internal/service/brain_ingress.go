package service

import (
	"context"
	"errors"
	"github.com/quenching/training-service/internal/model"
)

type BrainEventIngress struct {
	store    BrainStore
	evidence *BrainEvidenceService
}

func NewBrainEventIngress(store BrainStore, evidence *BrainEvidenceService) *BrainEventIngress {
	return &BrainEventIngress{store, evidence}
}
func (i *BrainEventIngress) Ingest(ctx context.Context, event model.BrainEvent, evidence []model.BrainEvidence) error {
	if err := ValidateBrainEvent(event); err != nil {
		return err
	}
	for _, e := range evidence {
		if e.SessionID != event.SessionID {
			return errors.New("cross-session evidence submission")
		}
	}
	if err := i.store.Ensure(ctx, event.SessionID); err != nil {
		return err
	}
	for _, e := range evidence {
		if _, err := i.evidence.Append(ctx, e); err != nil {
			return err
		}
	}
	for _, ref := range event.EvidenceRefs {
		if _, err := i.evidence.Read(ctx, event.SessionID, ref); err != nil {
			return err
		}
	}
	state, err := i.store.State(ctx, event.SessionID)
	if err != nil {
		return err
	}
	if event.Kind == "terminal_record" || state.Phase == model.BrainClosing || state.Phase == model.BrainReviewing || state.Phase == model.BrainCompleted || state.Phase == model.BrainFailed {
		return nil
	}
	_, err = i.store.AppendEvent(ctx, event)
	return err
}
