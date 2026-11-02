// Package middleware api-gateway 的鉴权中间件。
// 统一在入口校验 user-service 签发的 access token（HS256，与签发方共用 go-common/auth）。
// 公开路径（登录/注册/健康检查）放行；其余必须携带合法 Authorization: Bearer <token>。
package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/auth"
)

const (
	// ContextUID 存入 gin.Context 的用户ID键。
	ContextUID = "uid"
	bearer     = "Bearer "
)

// 网关层错误码（与平台错误码表对齐）。
const (
	codeUnauthorized = 20001
	codeTokenInvalid = 20002
)

// PublicPaths 无需登录即可访问的路径（精确匹配）。
// 注意：仅登录/注册等极少数接口公开，默认全部鉴权。
var defaultPublic = map[string]bool{
	"/health":               true,
	"/api/v1/auth/login":    true,
	"/api/v1/auth/register": true,
}

// JWTAuth 返回 JWT 鉴权中间件。isPublic 为 nil 时使用默认白名单。
func JWTAuth(mgr *auth.Manager, isPublic func(method, path string) bool) gin.HandlerFunc {
	if isPublic == nil {
		isPublic = func(_, path string) bool { return defaultPublic[path] }
	}
	return func(c *gin.Context) {
		// CORS 预检请求不带 token，直接放行（CORS 处理在 T1-33）。
		if c.Request.Method == http.MethodOptions || isPublic(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}

		token := extractToken(c.GetHeader("Authorization"))
		// WebSocket 握手无法自定义 Authorization 头，仅在升级请求时允许 query token，
		// 避免普通接口把令牌写进 URL/访问日志。
		if token == "" && isUpgradeRequest(c.Request) {
			token = c.Query("token")
		}
		if token == "" {
			abort401(c, codeUnauthorized, "未登录或缺少令牌")
			return
		}

		claims, err := mgr.ParseAccess(token)
		if err != nil {
			abort401(c, codeTokenInvalid, "登录已失效，请重新登录")
			return
		}

		c.Set(ContextUID, claims.UID)
		c.Next()
	}
}

// extractToken 从 Authorization 头取出 Bearer token；格式非法返回空。
func extractToken(header string) string {
	if len(header) <= len(bearer) || !strings.EqualFold(header[:len(bearer)], bearer) {
		return ""
	}
	return strings.TrimSpace(header[len(bearer):])
}

// isUpgradeRequest 判断是否为 WebSocket 升级请求（含 Upgrade: websocket 头）。
func isUpgradeRequest(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func abort401(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"code":     code,
		"msg":      msg,
		"trace_id": c.GetString("trace_id"),
	})
}

// UserID 从上下文取已认证用户ID；未认证返回 (0,false)。
func UserID(c *gin.Context) (int64, bool) {
	v, ok := c.Get(ContextUID)
	if !ok {
		return 0, false
	}
	uid, ok := v.(int64)
	return uid, ok
}
