package tests

import (
	"context"
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
	"sync"
	"testing"
)

type growthEligibility struct{ active bool }

func (e growthEligibility) Eligibility(_ context.Context, u, s, p int64) (rc.Eligibility, error) {
	return rc.Eligibility{UserID: u, SessionID: s, ProjectID: p, CanGenerate: !e.active}, nil
}

func TestTrainingChallengeRepairDoesNotGenerateReview(t *testing.T) {
	repo := newFakeReviewRepo()
	producer := newFakeProducer()
	svc := service.NewReviewService(repo, producer, repo).WithReviewEligibility(growthEligibility{active: true})
	for i := 0; i < 2; i++ {
		_, _ = svc.StartGeneration(context.Background(), 1, 2, 0)
	}
	if len(producer.published) != 0 || len(repo.reviews) != 0 {
		t.Fatal("per-fault review published")
	}
}

type growthConcurrentRepo struct {
	*fakeReviewRepo
	mu sync.Mutex
}

func (r *growthConcurrentRepo) GetBySessionID(ctx context.Context, s int64) (*model.Review, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fakeReviewRepo.GetBySessionID(ctx, s)
}
func (r *growthConcurrentRepo) Create(ctx context.Context, rv *model.Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.reviews {
		if v.SessionID == rv.SessionID {
			return errors.New("unique session")
		}
	}
	err := r.fakeReviewRepo.Create(ctx, rv)
	if err == nil {
		cp := *rv
		r.reviews[rv.ID] = &cp
	}
	return err
}

type growthConcurrentProducer struct {
	mu    sync.Mutex
	count int
}

func (p *growthConcurrentProducer) Publish(context.Context, *service.ReviewTask) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	return nil
}
func TestConcurrentClosureCreatesOneReview(t *testing.T) {
	repo := &growthConcurrentRepo{fakeReviewRepo: newFakeReviewRepo()}
	producer := &growthConcurrentProducer{}
	svc := service.NewReviewService(repo, producer, repo).WithReviewEligibility(growthEligibility{})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := svc.StartGeneration(context.Background(), 1, 2, 0); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.reviews) != 1 || producer.count != 1 {
		t.Fatal("duplicate closure report")
	}
}
func TestPublicAndInternalGenerationRejectActiveSession(t *testing.T) {
	repo := newFakeReviewRepo()
	svc := service.NewReviewService(repo, &fakeProducer{}, repo).WithReviewEligibility(growthEligibility{active: true})
	if _, err := svc.StartGeneration(context.Background(), 1, 2, 0); err == nil {
		t.Fatal("active session accepted")
	}
	if len(repo.reviews) != 0 {
		t.Fatal("created report for active session")
	}
}
func TestGrowthSourceOwnershipAndLegacyCompatibility(t *testing.T) {
	repo := newFakeReviewRepo()
	_ = repo.Create(context.Background(), &model.Review{UserID: 2, SessionID: 3})
	svc := service.NewReviewService(repo, &fakeProducer{}, repo).WithReviewEligibility(growthEligibility{})
	if _, err := svc.StartGeneration(context.Background(), 3, 99, 0); !errors.Is(err, service.ErrNotFound) {
		t.Fatal("foreign existing report", err)
	}
	detail, err := svc.GetDetail(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if detail.GrowthReport != nil {
		t.Fatal("invented legacy growth")
	}
}
