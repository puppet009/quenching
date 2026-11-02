package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrainInternalRejectsMissingOrWrongToken(t *testing.T) {
	for _, config := range []struct{ expected, supplied string }{{"", ""}, {"secret", ""}, {"secret", "wrong"}} {
		r := gin.New()
		NewBrainInternalHandler(nil, config.expected).RegisterRoutes(r)
		req := httptest.NewRequest("POST", "/internal/v1/brain/events", strings.NewReader(`{}`))
		req.Header.Set("X-Internal-Token", config.supplied)
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatalf("authorization: %d", out.Code)
		}
	}
}
