package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScalingHoldEndpointRequiresInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ResourcesHandler{InternalToken: "private"}.Register(r)
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/v1/resources/scaling-holds/acquire", strings.NewReader(`{"namespace":"sandbox-1","hold_id":"abc","reason":"cpu"}`))
		req.Header.Set("X-Internal-Token", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/v1/resources/scaling-holds/acquire", strings.NewReader(`{}`))
	req.Header.Set("X-Internal-Token", "private")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("unavailable controller acknowledged freeze")
	}
}
