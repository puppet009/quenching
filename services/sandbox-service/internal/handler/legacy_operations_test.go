package handler

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/sandbox-service/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
)

type legacyRecorderSpy struct{ calls int }

func (s *legacyRecorderSpy) Record(_ context.Context, b oe.LegacyBatch) (oe.LegacyReceipt, error) {
	s.calls++
	return oe.LegacyReceipt{RequestID: b.RequestID, SessionID: b.SessionID, UserID: b.UserID, Written: 1, Total: 1}, nil
}
func TestLegacyReportRequiresTokenAndConfirmedUserOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, token string
		owner       service.OperationOwnerCheck
		want        int
	}{
		{"no token", "", func(context.Context, int64, int64) (bool, error) { return true, nil }, 401},
		{"other user", "secret", func(context.Context, int64, int64) (bool, error) { return false, nil }, 404},
		{"unavailable", "secret", func(context.Context, int64, int64) (bool, error) { return false, errors.New("offline") }, 503},
		{"confirmed", "secret", func(_ context.Context, s, u int64) (bool, error) { return s == 41 && u == 2, nil }, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			spy := &legacyRecorderSpy{}
			RegisterLegacyOperations(r, spy, tc.owner, "secret")
			req := httptest.NewRequest("POST", "/internal/v1/operations/report", strings.NewReader(`{"request_id":"r","session_id":41,"user_id":2,"operations":[{"operation_type":"view_log"}]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Internal-Token", tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if tc.want != 200 && spy.calls != 0 {
				t.Fatal("persisted before ownership confirmed")
			}
		})
	}
}
