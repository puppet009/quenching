package handler

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"net/http/httptest"
	"testing"
	"time"
)

type readinessStore struct {
	service.ActivityPort
	sb *model.Sandbox
}

func (s readinessStore) GetBySessionID(context.Context, int64) (*model.Sandbox, error) {
	return s.sb, nil
}
func TestResourceReadinessRequiresOwnerLiveReadyAndToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, status, token   string
		owner                 int64
		closed, expired, want bool
		http                  int
	}{
		{"queued", "queued", "internal", 2, false, false, false, 200},
		{"wrong_owner", "ready", "internal", 3, false, false, false, 200},
		{"live_ready", "ready", "internal", 2, false, false, true, 200},
		{"closed", "ready", "internal", 2, true, false, false, 200},
		{"expired", "ready", "internal", 2, false, true, false, 200},
		{"missing_token", "ready", "", 2, false, false, false, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := &model.Sandbox{SessionID: 9, UserID: tc.owner, Status: tc.status, CreatedAt: time.Now()}
			if tc.closed {
				sb.CompletedAt = time.Now()
			}
			if tc.expired {
				sb.CreatedAt = time.Now().Add(-time.Hour)
			}
			r := gin.New()
			h := NewInternalLifecycleHandler(readinessStore{sb: sb}, nil, nil, "internal")
			h.RegisterRoutes(r)
			req := httptest.NewRequest("GET", "/internal/v1/sandboxes/9/readiness?user_id=2", nil)
			req.Header.Set("X-Internal-Token", tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.http {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Code != 200 {
				return
			}
			var body struct {
				Data struct {
					Ready bool `json:"ready"`
				} `json:"data"`
			}
			if e := json.Unmarshal(w.Body.Bytes(), &body); e != nil || body.Data.Ready != tc.want {
				t.Fatal(e, w.Body.String())
			}
		})
	}
}
