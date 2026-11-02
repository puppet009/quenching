package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogPrivateReadRejectsMissingOrWrongToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, token := range []string{"", "wrong"} {
		r := gin.New()
		RegisterCatalog(r, "private", nil, nil)
		req := httptest.NewRequest("GET", "/internal/v1/catalog/defects/7", nil)
		req.Header.Set("X-Internal-Token", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	r := gin.New()
	RegisterCatalog(r, "", nil, nil)
	req := httptest.NewRequest("GET", "/internal/v1/catalog/defects/7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("empty configured token accepted")
	}
}

func TestLegacyCatalogReadsAlsoRejectEmptyConfiguredToken(t *testing.T) {
	for _, path := range []string{"/internal/v1/defects/batch", "/internal/v1/defects/script-detail"} {
		r := gin.New()
		NewInternalDefectHandler(nil, "").RegisterRoutes(r)
		NewInternalDefectScriptHandler(nil, "").RegisterRoutes(r)
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"ids":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatalf("%s accepted empty configured token: %d", path, w.Code)
		}
	}
}
