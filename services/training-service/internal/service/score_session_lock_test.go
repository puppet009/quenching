package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quenching/proto/aiv1"
	"github.com/quenching/training-service/internal/model"
)

// The shared store models two service replicas contending on the repository's
// database lock contract. The first snapshot is paused before its score exists.
type concurrentFinishStore struct {
	*fakeFinishStore
	sessionLock sync.Mutex
	state       sync.Mutex
	readStarted chan struct{}
	allowRead   chan struct{}
	reads       atomic.Int32
	techWrites  atomic.Int32
	profiles    atomic.Int32
	locks       atomic.Int32
}

type heldCollabEvaluator struct {
	started chan struct{}
	proceed chan struct{}
}

func (e heldCollabEvaluator) EvaluateCollab(ctx context.Context, _ *aiv1.EvaluateCollabRequest) (*aiv1.EvaluateCollabResponse, error) {
	close(e.started)
	select {
	case <-e.proceed:
		return &aiv1.EvaluateCollabResponse{Available: true}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *concurrentFinishStore) WithScoreSessionLock(ctx context.Context, _ int64, run func(context.Context) error) (bool, error) {
	s.locks.Add(1)
	if !s.sessionLock.TryLock() {
		return false, nil
	}
	defer s.sessionLock.Unlock()
	return true, run(ctx)
}

func (s *concurrentFinishStore) GetSessionFinishSnapshot(ctx context.Context, _ int64) (*SessionFinishSnapshot, error) {
	s.state.Lock()
	snapshot := *s.snapshot
	s.state.Unlock()
	if s.reads.Add(1) == 1 {
		close(s.readStarted)
		select {
		case <-s.allowRead:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &snapshot, nil
}

func (s *concurrentFinishStore) UpdateSessionTechScore(context.Context, int64, float64, model.RadarData, bool) error {
	s.state.Lock()
	s.snapshot.TechScoreSet = true
	s.state.Unlock()
	s.techWrites.Add(1)
	return nil
}

func (s *concurrentFinishStore) UpdateUserProfileCollab(context.Context, int64, model.RadarData, float64, string) error {
	s.profiles.Add(1)
	return nil
}

func TestConcurrentFinishScoreWritesCollaborativeProfileOnce(t *testing.T) {
	store := &concurrentFinishStore{
		fakeFinishStore: &fakeFinishStore{snapshot: &SessionFinishSnapshot{SessionID: 7, UserID: 9, Status: "completed", FaultResolved: true, TotalDefectCount: 1, FixedDefectCount: 1}},
		readStarted:     make(chan struct{}), allowRead: make(chan struct{}),
	}
	evaluator := &fakeFinishEvaluator{resp: &aiv1.EvaluateCollabResponse{Available: true, QuestionQualityScore: 80, InfoUtilizationScore: 80, JudgmentScore: 80}}
	first := NewScoreService(store, evaluator, nil)
	second := NewScoreService(store, evaluator, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := first.FinishSessionScore(ctx, 7, FinishOutcomeCompleted)
		finished <- err
	}()
	select {
	case <-store.readStarted:
	case <-ctx.Done():
		t.Fatal("first score did not read its snapshot")
	}
	_, competingErr := second.FinishSessionScore(ctx, 7, FinishOutcomeCompleted)
	close(store.allowRead)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if competingErr == nil || store.techWrites.Load() != 1 || store.profiles.Load() != 1 {
		t.Fatalf("concurrent scoring duplicated non-idempotent effects: competing=%v tech=%d profiles=%d locks=%d", competingErr, store.techWrites.Load(), store.profiles.Load(), store.locks.Load())
	}
	// After the winner releases the lock, a retry must re-read the persisted
	// score inside the lock and skip the whole collaborative pipeline.
	result, err := second.FinishSessionScore(ctx, 7, FinishOutcomeCompleted)
	if err != nil || result.Scored || store.profiles.Load() != 1 || store.locks.Load() != 3 {
		t.Fatalf("retry did not recheck the score under the shared lock: result=%+v err=%v profiles=%d locks=%d", result, err, store.profiles.Load(), store.locks.Load())
	}
}

func TestClosedSessionWaitsForInFlightCollaborativeWrites(t *testing.T) {
	store := &concurrentFinishStore{
		fakeFinishStore: &fakeFinishStore{snapshot: &SessionFinishSnapshot{SessionID: 7, UserID: 9, Status: "completed", FaultResolved: true, TotalDefectCount: 1, FixedDefectCount: 1}},
		readStarted:     make(chan struct{}), allowRead: make(chan struct{}),
	}
	close(store.allowRead)
	evaluator := heldCollabEvaluator{started: make(chan struct{}), proceed: make(chan struct{})}
	first := NewScoreService(store, evaluator, nil)
	second := NewScoreService(store, &fakeFinishEvaluator{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := first.FinishSessionScore(ctx, 7, FinishOutcomeCompleted)
		finished <- err
	}()
	select {
	case <-evaluator.started:
	case <-ctx.Done():
		t.Fatal("first score did not reach its collaborative phase")
	}
	err := second.ScoreClosedSession(ctx, 7)
	close(evaluator.proceed)
	if firstErr := <-finished; firstErr != nil {
		t.Fatal(firstErr)
	}
	if err == nil {
		t.Fatal("review was allowed after tech score but before collaborative writes finished")
	}
}
