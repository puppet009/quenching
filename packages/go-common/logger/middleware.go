// Gin 日志中间件：Trace（透传/生成 trace_id）、Access（结构化访问日志）、
// Recovery（panic 恢复并记录堆栈）。各服务统一引用，避免每个服务重复实现。
package logger

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	HeaderTraceID = "X-Trace-Id"
	ctxTraceID    = "trace_id"
)

// Trace 透传上游 X-Trace-Id；缺失时生成一个，并写入 context、响应头。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := c.GetHeader(HeaderTraceID)
		if tid == "" {
			tid = time.Now().Format("20060102150405") + "-" + c.Request.URL.Path
		}
		c.Set(ctxTraceID, tid)
		c.Header(HeaderTraceID, tid)
		c.Request = c.Request.WithContext(WithTrace(c.Request.Context(), tid))
		c.Next()
	}
}

// clientIP 优先取网关透传的 X-Forwarded-For（InCluster 默认直连为空）。
func clientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		return xff
	}
	return c.ClientIP()
}

// Access 记录每个 HTTP 请求的结构化访问日志（stdout + 文件）。
func Access() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := logQuery(c.Request.URL.RawQuery)

		c.Next()

		latency := time.Since(start)
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.Int("status", c.Writer.Status()),
			zap.Int("size", c.Writer.Size()),
			zap.Duration("latency", latency),
			zap.String("ip", clientIP(c)),
			zap.String("ua", c.Request.UserAgent()),
		}
		if v, ok := c.Get(ctxTraceID); ok {
			if s, ok := v.(string); ok {
				fields = append(fields, zap.String("trace_id", s))
			}
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()))
		}

		L().Info("HTTP 访问", fields...)
	}
}

// Keep diagnostic filters while excluding credentials used by websocket URLs.
// Parsing creates a copy: authentication still receives the original query.
func logQuery(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[REDACTED]"
	}
	for key := range values {
		normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
		for _, credential := range []string{"token", "secret", "password", "passwd", "authorization", "apikey", "credential", "sessionid"} {
			if strings.Contains(normalized, credential) {
				values.Set(key, "[REDACTED]")
				break
			}
		}
	}
	return values.Encode()
}

// Recovery 捕获 handler panic，记录堆栈并返回 500，避免进程崩溃。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				var brokenPipe bool
				if err, ok := r.(error); ok {
					var ne *net.OpError
					if errors.As(err, &ne) {
						var se *os.SyscallError
						if errors.As(ne.Err, &se) &&
							(errors.Is(se.Err, syscall.EPIPE) || errors.Is(se.Err, syscall.ECONNRESET)) {
							brokenPipe = true
						}
					}
				}
				L().Error("panic 恢复",
					zap.Any("error", r),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.String("ip", clientIP(c)),
					zap.String("stack", string(debug.Stack())),
					zap.Bool("broken_pipe", brokenPipe),
				)
				if brokenPipe {
					if err, ok := r.(error); ok {
						_ = c.Error(err)
					}
					c.Abort()
					return
				}
				c.AbortWithStatusJSON(http.StatusInternalServerError,
					gin.H{"code": 10000, "msg": "服务器内部错误"})
			}
		}()
		c.Next()
	}
}
