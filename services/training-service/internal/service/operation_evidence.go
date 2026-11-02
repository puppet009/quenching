package service

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
)

type OperationEvidenceService struct {
	Store oe.Store
	Owned func(context.Context, int64, int64) error
}

func (s *OperationEvidenceService) ListOwned(ctx context.Context, uid, sid, before int64, limit int) (oe.Page, error) {
	if err := s.Owned(ctx, uid, sid); err != nil {
		return oe.Page{}, err
	}
	return s.Store.List(ctx, sid, before, limit)
}
func (s *OperationEvidenceService) ReadOwned(ctx context.Context, uid, sid int64, ref string, offset, limit int) (oe.Fragment, error) {
	if err := s.Owned(ctx, uid, sid); err != nil {
		return oe.Fragment{}, err
	}
	t, err := s.Store.Read(ctx, sid, ref)
	if err != nil {
		return oe.Fragment{}, err
	}
	return oe.ReadFragment(t, offset, limit)
}
