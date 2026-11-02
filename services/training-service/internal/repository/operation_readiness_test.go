package repository

import (
	"context"
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSourceReadinessRequiresAcknowledgedProjection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sid   int64
		stuck bool
		ok    bool
	}{{"late acknowledgement", 41, false, true}, {"foreign checkpoint", 42, false, false}, {"pending timeout", 41, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Internal-Token") != "secret" || r.URL.Path != "/internal/v1/operations/41/projection-checkpoint" {
					t.Error("scope or token lost")
				}
				pending := int64(1)
				if calls > 1 && !tc.stuck {
					pending = 0
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": oe.LegacyCheckpoint{SessionID: tc.sid, Watermark: 71, Pending: pending}})
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			e := LegacyProjectionReadiness(server.URL, "secret")(ctx, 41)
			if (e == nil) != tc.ok {
				t.Fatalf("checkpoint result: %v", e)
			}
			if tc.ok && calls < 2 {
				t.Fatal("scoring released before acknowledgement")
			}
		})
	}
}
