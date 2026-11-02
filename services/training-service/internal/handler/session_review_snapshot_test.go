package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/training-service/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

type snapshotHandlerFake struct {
	service.SnapshotStore
	called bool
}

func (f *snapshotHandlerFake) Eligibility(context.Context, int64, int64, int64) (rc.Eligibility, error) {
	f.called = true
	return rc.Eligibility{}, nil
}
func TestSnapshotHandlerRequiresConfiguredInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, token := range []string{"", "internal-test"} {
		f := &snapshotHandlerFake{}
		r := gin.New()
		NewSessionReviewSnapshotHandler(service.NewSessionReviewSnapshotService(f), token).RegisterRoutes(r)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/v1/sessions/7/review-eligibility?user_id=10&project_id=0", nil))
		if w.Code != http.StatusUnauthorized || f.called {
			t.Fatal(w.Code, f.called)
		}
	}
}
func TestSnapshotHandlerRejectsInvalidIdentityAndCursor(t *testing.T) {
	f := &snapshotHandlerFake{}
	r := gin.New()
	NewSessionReviewSnapshotHandler(service.NewSessionReviewSnapshotService(f), "internal-test").RegisterRoutes(r)
	for _, path := range []string{"/internal/v1/sessions/7/review-eligibility?user_id=0&project_id=0", "/internal/v1/sessions/7/review-snapshot/sources?user_id=10&version=1&offset_bytes=-1", "/internal/v1/sessions/7/review-snapshot/source?user_id=10&version=1&reference=x&offset_bytes=bad"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Internal-Token", "internal-test")
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
