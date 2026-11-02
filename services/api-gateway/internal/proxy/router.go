// Package proxy 网关反向代理。
// 按路径前缀匹配路由规则，转发到对应后端服务。
//
// MVP 阶段使用标准库 net/http/httputil.ReverseProxy，
// 不引入额外依赖，性能足够。
package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
)

// Router 路由转发器。
type Router struct {
	rules []config.RouteRule
	// 预解析的 URL，避免每次请求解析
	targets map[string]*url.URL
}

// NewRouter 创建路由器。
func NewRouter(rules []config.RouteRule) (*Router, error) {
	targets := make(map[string]*url.URL, len(rules))
	for _, r := range rules {
		u, err := url.Parse(r.TargetURL)
		if err != nil {
			return nil, err
		}
		targets[r.PathPrefix] = u
	}
	return &Router{
		rules:   rules,
		targets: targets,
	}, nil
}

// match 匹配最长前缀路由规则。
func (r *Router) match(path string) *config.RouteRule {
	var matched *config.RouteRule
	maxLen := 0
	for i := range r.rules {
		rule := &r.rules[i]
		if strings.HasPrefix(path, rule.PathPrefix) && len(rule.PathPrefix) > maxLen {
			matched = rule
			maxLen = len(rule.PathPrefix)
		}
	}
	return matched
}

// Handler 返回 Gin 处理函数，负责转发请求到后端服务。
func (r *Router) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		rule := r.match(path)
		if rule == nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"code": 20004,
				"msg":  "路由不存在",
			})
			return
		}

		target := r.targets[rule.PathPrefix]
		proxy := httputil.NewSingleHostReverseProxy(target)

		// 修改请求路径（如果需要剥离前缀）
		if rule.Strip {
			// 保留前缀后的路径
			newPath := strings.TrimPrefix(path, rule.PathPrefix)
			if !strings.HasPrefix(newPath, "/") {
				newPath = "/" + newPath
			}
			c.Request.URL.Path = newPath
		}

		// 透传用户信息（如果已鉴权）
		if uid, ok := c.Get(ContextUID); ok {
			if uidInt, ok := uid.(int64); ok && uidInt > 0 {
				c.Request.Header.Set("X-User-ID", itoa(uidInt))
			}
		}

		// 透传 trace_id
		if traceID, ok := c.Get("trace_id"); ok {
			c.Request.Header.Set("X-Trace-Id", traceID.(string))
		}

		// 设置 X-Forwarded-* 头
		c.Request.Header.Set("X-Forwarded-For", c.ClientIP())
		c.Request.Header.Set("X-Forwarded-Proto", scheme(c))

		// 转发
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}

// scheme 获取请求协议。
func scheme(c *gin.Context) string {
	if c.Request.TLS != nil {
		return "https"
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

// ContextUID 从上下文取用户ID（复制自 middleware 包，避免循环引用）。
const ContextUID = "uid"

// itoa 简单的 int64 转字符串。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
