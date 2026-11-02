package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentDefectReaderRequiresTokenAndOwnCatalog(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Internal-Token") != "catalog-token" || r.URL.Path != "/internal/v1/catalog/defects/7" {
			t.Errorf("bad request: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 7, "name": "owned config", "root_cause": "server-only"}})
	}))
	defer server.Close()
	if _, err := NewContentDefectReader(server.URL, "").GetByID(context.Background(), 7); err == nil {
		t.Fatal("empty token accepted")
	}
	if calls != 0 {
		t.Fatal("called content without authentication")
	}
	got, err := NewContentDefectReader(server.URL, "catalog-token").GetByID(context.Background(), 7)
	if err != nil || got.ID != 7 || got.RootCause != "server-only" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestContentDefectReaderRejectsMismatchedIdentityAndFailure(t *testing.T) {
	for _, status := range []int{200, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 8}})
		}))
		if _, err := NewContentDefectReader(server.URL, "catalog-token").GetByID(context.Background(), 7); err == nil {
			t.Fatal("accepted failed or mismatched content response")
		}
		server.Close()
	}
}
