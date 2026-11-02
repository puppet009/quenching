package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
)

// CORS 返回跨域中间件。
// 功能：
//   - 处理 OPTIONS 预检请求
//   - 设置 Access-Control-* 响应头
//   - 附加安全响应头（X-Content-Type-Options 等）
func CORS(cfg config.CORSConfig) gin.HandlerFunc {
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeaders := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)

	// 全部允许的快捷方式
	allowAll := len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*"

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// 确定允许的源
		var allowOrigin string
		if allowAll {
			allowOrigin = "*"
		} else if origin != "" && isOriginAllowed(origin, cfg.AllowOrigins) {
			allowOrigin = origin
		} else if origin != "" {
			// 非允许源，直接拒绝（但不阻断，让业务层处理）
			allowOrigin = origin
		}

		// CORS 响应头
		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Access-Control-Allow-Methods", allowMethods)
			c.Header("Access-Control-Allow-Headers", allowHeaders)
			if exposeHeaders != "" {
				c.Header("Access-Control-Expose-Headers", exposeHeaders)
			}
			if cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
			c.Header("Access-Control-Max-Age", maxAge)
		}

		// 安全响应头（所有请求都加）
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// 预检请求直接返回 204
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// isOriginAllowed 检查 origin 是否在允许列表中。
// 支持精确匹配和子域匹配（如 *.example.com）。
func isOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == origin {
			return true
		}
		// 通配符子域匹配：*.example.com 匹配 foo.example.com
		if strings.HasPrefix(a, "*.") {
			suffix := a[1:] // .example.com
			if strings.HasSuffix(origin, suffix) {
				return true
			}
		}
	}
	return false
}
