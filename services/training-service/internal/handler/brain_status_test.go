package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/quenching/training-service/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

type statusOwnerStore struct{ sid, uid int64 }

func (s *statusOwnerStore) PublicBrainStatus(_ context.Context, sid, uid int64) (*service.PublicBrainStatus, error) {
	s.sid = sid
	s.uid = uid
	return nil, nil
}
func TestBrainStatusUsesAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s := &statusOwnerStore{}
	NewBrainStatusHandler(s).RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/training/sessions/7/brain?user_id=99", nil)
	req.Header.Set("X-User-Id", "12")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if s.sid != 7 || s.uid != 12 || w.Code != 404 {
		t.Fatalf("owner=%d session=%d HTTP=%d", s.uid, s.sid, w.Code)
	}
}
