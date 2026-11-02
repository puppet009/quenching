package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"
)

// ClientTime supplies an instant, not a wall-clock string interpreted by the browser.
func ClientTime(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"unix_ms": time.Now().UnixMilli(), "timezone": "Asia/Shanghai"}})
}

func NewClientLogs(dir string) gin.HandlerFunc {
	sinks := make(map[string]*zap.Logger)
	for _, app := range []string{"web", "admin"} {
		sinks[app] = logger.NewFile("warn", logger.FileConfig{Dir: dir, Name: app + "-browser.log", MaxSizeMB: 100, MaxBackups: 7, MaxAgeDays: 30})
	}
	return ClientLogs(sinks)
}

// ClientLogs records only a small, fixed diagnostic schema. It deliberately does
// not accept cookies, request bodies, headers or user-supplied identity fields.
// The normal gateway IP rate limit also applies to this public login-page endpoint.
func ClientLogs(sinks map[string]*zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var report struct {
			App        string `json:"app"`
			Kind       string `json:"kind"`
			Message    string `json:"message"`
			Stack      string `json:"stack"`
			Path       string `json:"path"`
			ClientTime int64  `json:"client_time"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12*1024)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.Status(http.StatusRequestEntityTooLarge)
			} else {
				c.Status(http.StatusBadRequest)
			}
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			c.Status(http.StatusBadRequest)
			return
		}
		sink, validApp := sinks[report.App]
		validKind := report.Kind == "error" || report.Kind == "rejection" || report.Kind == "react" || report.Kind == "request" || report.Kind == "resource" || report.Kind == "console"
		if !validApp || !validKind || strings.TrimSpace(report.Message) == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		path := "/"
		if parsed, err := url.Parse(report.Path); err == nil && strings.HasPrefix(parsed.Path, "/") {
			path = parsed.Path
		}
		sink.Warn("浏览器异常", zap.String("app", report.App), zap.String("kind", report.Kind),
			zap.String("message", redactClientText(report.Message, 1000)), zap.String("stack", redactClientText(report.Stack, 6000)),
			zap.String("path", redactClientText(path, 250)), zap.Int64("client_time", report.ClientTime), zap.String("trace_id", c.GetString("trace_id")))
		c.Status(http.StatusNoContent)
	}
}

var clientURL = regexp.MustCompile(`https?://[^\s)]+`)
var clientBearer = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/=-]+`)
var clientCredential = regexp.MustCompile(`(?i)\b(token|access_token|refresh_token|authorization|password|secret|api[_-]?key)\s*[:=]\s*["']?[^,;\s"'&]+`)

func redactClientText(value string, limit int) string {
	value = clientURL.ReplaceAllStringFunc(value, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[url]"
		}
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return u.String()
	})
	value = clientBearer.ReplaceAllString(value, "Bearer [redacted]")
	value = clientCredential.ReplaceAllString(value, "$1=[redacted]")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}
