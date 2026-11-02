package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	oe "github.com/quenching/proto/operationevidence"
)

type legacyClosedStore struct {
	rows  []ClosedLegacySession
	batch int
}

func (s *legacyClosedStore) ListClosedLegacySessions(ctx context.Context, afterID int64, limit int) ([]ClosedLegacySession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.batch > 0 && s.batch < limit {
		limit = s.batch
	}
	var out []ClosedLegacySession
	for _, row := range s.rows {
		if row.ID > afterID && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

type legacyReviewClient struct {
	existing  map[int64]int64
	fail      map[int64]bool
	lookupErr error
	created   []int64
}

func (r *legacyReviewClient) ListReviewExistence(context.Context, []int64) ([]ReviewExistenceItem, error) {
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	var out []ReviewExistenceItem
	for sid, id := range r.existing {
		idCopy := id
		out = append(out, ReviewExistenceItem{SessionID: sid, ReviewID: &idCopy})
	}
	return out, nil
}

func (r *legacyReviewClient) TriggerGeneration(ctx context.Context, sid, _, _ int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.fail[sid] {
		return errors.New("review temporarily unavailable")
	}
	if r.existing[sid] == 0 {
		r.existing[sid] = sid + 100
		r.created = append(r.created, sid)
	}
	return nil
}

type legacyCompletedSandbox struct {
	SandboxLifecycle
	fail  bool
	calls []int64
}

func (s *legacyCompletedSandbox) MarkCompleted(ctx context.Context, sid int64, _ string) error {
	s.calls = append(s.calls, sid)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.fail {
		return errors.New("sandbox temporarily unavailable")
	}
	return nil
}

func legacyReadySeals() *OperationSealService {
	seal := oe.AuditSeal{State: "ready", Deadline: time.Now().Add(time.Minute)}
	return &OperationSealService{Local: &sealFake{seal: seal}, Remote: &sealFake{seal: seal}}
}

func TestLegacyReviewResumesAfterRestartWithoutBrowser(t *testing.T) {
	store := &legacyClosedStore{rows: []ClosedLegacySession{{ID: 7, UserID: 9, ProjectID: 2, Status: "completed"}, {ID: 8, UserID: 9, ProjectID: 2, Status: "aborted"}}}
	reviews := &legacyReviewClient{existing: map[int64]int64{8: 108}}
	sandbox := &legacyCompletedSandbox{fail: true}
	seals := legacyReadySeals()
	first := NewLegacyReviewDispatcher(store, reviews, reviews, sandbox, seals)
	if err := first.Dispatch(context.Background()); err == nil || len(reviews.created) != 0 {
		t.Fatalf("remote completion failure was not retryable: err=%v created=%v", err, reviews.created)
	}

	// Recreate the dispatcher: only persisted terminal sessions and audit seals remain.
	sandbox.fail = false
	remote := seals.Remote.(*sealFake)
	remote.seal.State = "pending"
	remote.seal.Pending = 1
	second := NewLegacyReviewDispatcher(store, reviews, reviews, sandbox, seals)
	if err := second.Dispatch(context.Background()); err != nil || len(reviews.created) != 0 {
		t.Fatalf("review started before audit collection: err=%v created=%v", err, reviews.created)
	}
	remote.seal.State = "ready"
	remote.seal.Pending = 0
	third := NewLegacyReviewDispatcher(store, reviews, reviews, sandbox, seals)
	if err := third.Dispatch(context.Background()); err != nil || !reflect.DeepEqual(reviews.created, []int64{7}) {
		t.Fatalf("terminal session did not resume without a browser: err=%v created=%v", err, reviews.created)
	}
	if err := third.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := third.Dispatch(context.Background()); err != nil || len(reviews.created) != 1 {
		t.Fatalf("already queued review was generated twice: err=%v created=%v", err, reviews.created)
	}
	if len(sandbox.calls) != 8 {
		t.Fatalf("persisted completion must be retried even for existing reviews: %v", sandbox.calls)
	}
}

func TestLegacyReviewScanDoesNotStarveLaterSessions(t *testing.T) {
	store := &legacyClosedStore{batch: 1, rows: []ClosedLegacySession{{ID: 1}, {ID: 2}, {ID: 3}}}
	reviews := &legacyReviewClient{existing: map[int64]int64{1: 101}, fail: map[int64]bool{2: true}}
	dispatcher := NewLegacyReviewDispatcher(store, reviews, reviews, &legacyCompletedSandbox{}, legacyReadySeals())
	for range 4 {
		_ = dispatcher.Dispatch(context.Background())
	}
	if !reflect.DeepEqual(reviews.created, []int64{3}) {
		t.Fatalf("earlier records starved later work: %v", reviews.created)
	}
	delete(reviews.fail, 2)
	for range 2 {
		_ = dispatcher.Dispatch(context.Background())
	}
	if !reflect.DeepEqual(reviews.created, []int64{3, 2}) {
		t.Fatalf("failed record was not revisited: %v", reviews.created)
	}
}

func TestLegacyCompletionDoesNotDependOnReviewService(t *testing.T) {
	store := &legacyClosedStore{rows: []ClosedLegacySession{{ID: 7}}}
	reviews := &legacyReviewClient{lookupErr: errors.New("review unavailable")}
	sandbox := &legacyCompletedSandbox{}
	dispatcher := NewLegacyReviewDispatcher(store, reviews, reviews, sandbox, legacyReadySeals())
	if err := dispatcher.Dispatch(context.Background()); err == nil || !reflect.DeepEqual(sandbox.calls, []int64{7}) {
		t.Fatalf("review outage stopped resource completion: err=%v calls=%v", err, sandbox.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := dispatcher.Dispatch(ctx); !errors.Is(err, context.Canceled) || len(sandbox.calls) != 1 {
		t.Fatalf("cancellation was ignored: err=%v calls=%v", err, sandbox.calls)
	}
}
