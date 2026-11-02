package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResourceAdminAuthRejectsForgedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ResourcesHandler{JWTSecret: "test", ProfileURL: "http://unused"}.Register(r)
	req := httptest.NewRequest("GET", "/api/admin/v1/resources", nil)
	req.Header.Set("X-User-ID", "1")
	req.Header.Set("X-Admin-Permissions", "*")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestResourceAdminAuthUsesSignedIdentityAndFullPermission(t *testing.T) {
	permission := "*"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-ID") != "9" {
			t.Error("forged uid trusted")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"permissions":["` + permission + `"]}}`))
	}))
	defer server.Close()
	r := gin.New()
	ResourcesHandler{JWTSecret: "test", ProfileURL: server.URL}.Register(r)
	token, _ := auth.NewManager("test").IssueAccess(9)
	for _, code := range []int{503, 403} {
		req := httptest.NewRequest("GET", "/api/admin/v1/resources", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-User-ID", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != code {
			t.Fatal(w.Code, w.Body.String())
		}
		permission = "sandbox:manage"
	}
}
