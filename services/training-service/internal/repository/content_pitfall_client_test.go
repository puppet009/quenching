package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentPitfallsRejectForeignDefectRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"ID": 7, "DefectID": 99, "Status": "active"}}})
	}))
	defer server.Close()
	if _, err := NewContentPitfallReader(server.URL, "token").ListActiveByDefectIDs(context.Background(), []int64{2}); err == nil {
		t.Fatal("foreign defect config accepted")
	}
}
