package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestBehaviorCommandsRequireInternalToken(t *testing.T) {
	for _, value := range []string{"", "wrong"} {
		r := gin.New()
		NewBehaviorCommandsHandler(nil, "internal").RegisterRoutes(r)
		req := httptest.NewRequest("GET", "/internal/v1/sandboxes/1/behavior/commands/c1", nil)
		req.Header.Set("X-Internal-Token", value)
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatalf("unauthorized status %d", out.Code)
		}
	}
}
