package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/training-service/internal/service"
)

type finishedHistoryStore struct {
	service.SessionStore
	filter service.SessionListFilter
	calls  int
}

func (s *finishedHistoryStore) ListSessions(_ context.Context, f service.SessionListFilter) ([]service.SessionHistoryRow, int64, error) {
	s.filter = f
	s.calls++
	return nil, 41, nil
}

func TestFinishedHistoryPaginationUsesAuthenticatedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		query  string
		want   int
		called bool
	}{{"status=finished&page=2&pageSize=20", 200, true}, {"status=unsupported", 400, false}} {
		s := &finishedHistoryStore{}
		r := gin.New()
		NewSessionHandler(service.NewSessionService(s, nil, nil)).RegisterRoutes(r)
		req := httptest.NewRequest("GET", "/api/v1/training/sessions?"+tc.query, nil)
		req.Header.Set("X-User-Id", "17")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want || (s.calls > 0) != tc.called {
			t.Fatal(w.Code, w.Body.String(), s.calls)
		}
		if tc.called && (s.filter.UserID != 17 || s.filter.Status != "finished" || s.filter.Page != 2 || s.filter.PageSize != 20) {
			t.Fatal("history filter did not preserve scope/page", s.filter)
		}
	}
}
