package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestRuntimeAuthenticationFailsClosed(t *testing.T) {
	for _, token := range []string{"", "internal"} {
		engine := gin.New()
		NewRuntimeHandler(nil, token).RegisterRoutes(engine)
		req := httptest.NewRequest("POST", "/api/v1/runtime/inject", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("token=%q status=%d", token, rec.Code)
		}
	}
}
