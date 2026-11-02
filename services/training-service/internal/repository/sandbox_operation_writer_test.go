package repository

import (
	"context"
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegacyReportsAreForwardedWithScopeAndAcknowledged(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var b oe.LegacyBatch
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Fatal("invalid batch")
		}
		if r.Header.Get("X-Internal-Token") != "token" || b.SessionID != 17 || b.UserID != 19 || b.RequestID == "" {
			t.Error("lost source scope")
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": oe.LegacyReceipt{RequestID: b.RequestID, SessionID: b.SessionID, UserID: b.UserID, Written: len(b.Operations), Total: 2}})
	}))
	defer server.Close()
	if _, err := NewSandboxOperationWriter(server.URL, "").Record(context.Background(), 17, 19, []service.OperationInput{{Type: "view_log"}}); err == nil || calls != 0 {
		t.Fatal("empty token allowed report")
	}
	receipt, err := NewSandboxOperationWriter(server.URL, "token").Record(context.Background(), 17, 19, []service.OperationInput{{Type: "view_log"}})
	if err != nil || receipt.Written != 1 {
		t.Fatal(receipt, err)
	}
}
func TestLegacyReportRejectsUnconfirmedOrForeignReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": oe.LegacyReceipt{SessionID: 999, UserID: 19, Written: 1, Total: 1}})
	}))
	defer server.Close()
	if _, err := NewSandboxOperationWriter(server.URL, "token").Record(context.Background(), 17, 19, []service.OperationInput{{Type: "view_log"}}); err == nil {
		t.Fatal("foreign receipt accepted")
	}
}
