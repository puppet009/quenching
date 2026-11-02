// Package handler review-service 的 HTTP 中间件。
//
// 鉴权在 api-gateway 统一完成（JWT 校验）。网关认证通过后，通过内部请求头
// X-User-Id 把用户身份透传给本服务；本服务不再校验 JWT。
//
// 安全前提：网关必须在入口剥离/覆盖外部传入的 X-User-Id，禁止客户端伪造
// （该约束在网关联调 T1-39 落实）。本地开发可直接带此头模拟登录用户。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	// HeaderUserID 网关注入用户身份的内部请求头。
	HeaderUserID = "X-User-Id"
	// ContextUID gin.Context 中存放用户ID的键，与网关中间件保持同名。
	ContextUID = "uid"
)

// UserID 从受信请求头解析用户ID并注入上下文。
func UserID() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(HeaderUserID)
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
			return
		}
		uid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || uid <= 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 20002, "msg": "用户身份非法"})
			return
		}
		c.Set(ContextUID, uid)
		c.Next()
	}
}
