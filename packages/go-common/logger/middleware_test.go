package logger

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestAccessRedactsQueryCredentialsWithoutChangingTheRequest(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	previous := std
	std = zap.New(core)
	t.Cleanup(func() { std = previous })
	router := gin.New()
	router.Use(Access())
	router.GET("/exec", func(c *gin.Context) {
		if c.Query("token") != "secret-jwt" || c.Query("cols") != "100" {
			t.Error("log redaction changed the real request")
		}
		c.Status(http.StatusNoContent)
	})
	raw := "cols=100&token=secret-jwt&Access_Token=secret-oauth&password=secret-password&api%5Fkey=secret-key"
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/exec?"+raw, nil))
	query := logs.All()[0].ContextMap()["query"].(string)
	if strings.Contains(query, "secret-") {
		t.Fatalf("credentials leaked: %s", query)
	}
	values, err := url.ParseQuery(query)
	if err != nil || values.Get("cols") != "100" || values.Get("token") != "[REDACTED]" {
		t.Fatalf("diagnostic query fields missing: %s %v", query, err)
	}
}
