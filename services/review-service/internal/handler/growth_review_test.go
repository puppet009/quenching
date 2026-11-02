package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
	"net/http/httptest"
	"testing"
)

type growthHandlerRepo struct{ service.ReviewRepo }

func (growthHandlerRepo) GetByID(context.Context, int64) (*model.Review, error) {
	return &model.Review{ID: 1, UserID: 2, SessionID: 3, ProjectID: 4}, nil
}

type growthHandlerJobs struct{}

func (growthHandlerJobs) Load(context.Context, int64) (service.GrowthJob, error) {
	return service.GrowthJob{Snapshot: rc.Snapshot{UserID: 2, SessionID: 3, ProjectID: 4, Version: 1}}, nil
}

type growthHandlerSources struct {
	service.GrowthSnapshotStore
	active bool
}

func (s growthHandlerSources) Eligibility(_ context.Context, u, id, p int64) (rc.Eligibility, error) {
	return rc.Eligibility{UserID: u, SessionID: id, ProjectID: p, CanGenerate: !s.active}, nil
}
func (growthHandlerSources) Read(context.Context, int64, int64, int64, string, int, int) (rc.Fragment, error) {
	return rc.Fragment{}, service.ErrNotFound
}
func TestGrowthHandlerValidatesRangeAndOwnership(t *testing.T) {
	for _, tc := range []struct {
		path   string
		user   int64
		active bool
		status int
	}{{"/1/source?reference=e:1&offset_bytes=-1", 2, false, 400}, {"/1/source?reference=e:1&offset_bytes=oops", 2, false, 400}, {"/1/source?reference=e:1&limit_bytes=8193", 2, false, 400}, {"/1/progress", 99, false, 404}, {"/1/source?reference=fault:1", 2, true, 404}, {"/1/source?reference=missing", 2, false, 404}} {
		svc := service.NewReviewService(growthHandlerRepo{}, nil, nil)
		growth := service.NewGrowthReportService(svc, growthHandlerJobs{}, growthHandlerSources{active: tc.active})
		h := NewGrowthReviewHandler(growth)
		router := gin.New()
		router.Use(func(c *gin.Context) { c.Set("uid", tc.user) })
		router.GET("/:id/source", h.Source)
		router.GET("/:id/progress", h.Progress)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, response.Code)
		}
	}
}
