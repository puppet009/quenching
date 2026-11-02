package handler

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigValidatorRequiresInternalTokenAndRejectsInvalidSpec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, v := range []struct {
		token, body string
		want        int
	}{{"", `{}`, 401}, {"wrong", `{}`, 401}, {"valid", `{"configuration":{"kind":"unsupported"},"pod_required":true}`, 400}, {"valid", `{"configuration":"bad"}`, 400}} {
		r := gin.New()
		RegisterConfigValidation(r, "valid")
		req := httptest.NewRequest("POST", "/internal/v1/faults/validate-config", strings.NewReader(v.body))
		req.Header.Set("X-Internal-Token", v.token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != v.want {
			t.Fatalf("%s: got %d body %s", v.token, w.Code, w.Body.String())
		}
	}
}
