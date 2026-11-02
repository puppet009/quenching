package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestClientTimeIsUncachedAndCarriesServerInstant(t *testing.T) {
	r := gin.New()
	r.GET("/time", ClientTime)
	w := httptest.NewRecorder()
	before := time.Now().UnixMilli()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/time", nil))
	var out struct {
		Data struct {
			UnixMS int64 `json:"unix_ms"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || out.Data.UnixMS < before || out.Data.UnixMS > time.Now().UnixMilli() || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid clock response: %s", w.Body.String())
	}
}

func TestBrowserErrorsAreBoundedAndExcludeCredentialsFromURLs(t *testing.T) {
	core, seen := observer.New(zap.WarnLevel)
	r := gin.New()
	r.POST("/logs", ClientLogs(map[string]*zap.Logger{"web": zap.New(core)}))
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"app":"web","kind":"console","message":"failure token=secret","path":"/training/7?token=secret#private","stack":"Error at https://site/app.js?token=secret:1:2","client_time":1720000000000}`, 204},
		{`{"app":"other","kind":"error","message":"failure"}`, 400},
		{`{"app":"web","kind":"error","message":"failure","authorization":"secret"}`, 400},
		{`{"app":"web","kind":"error","message":"` + strings.Repeat("x", 20_000) + `"}`, 413},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/logs", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if seen.Len() != 1 {
		t.Fatalf("expected one accepted report, got %d", seen.Len())
	}
	fields := seen.All()[0].ContextMap()
	if fields["path"] != "/training/7" || strings.Contains(fields["message"].(string), "secret") || strings.Contains(fields["stack"].(string), "secret") {
		t.Fatalf("credentials persisted: %v", fields)
	}
}
