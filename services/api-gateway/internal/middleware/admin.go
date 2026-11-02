package middleware

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PermissionFor also protects the legacy content write paths.
func PermissionFor(method, path string) string {
	switch {
	case strings.HasPrefix(path, "/api/admin/v1/defects"), strings.HasPrefix(path, "/api/admin/v1/template-faults"):
		return "defect:manage"
	case strings.HasPrefix(path, "/api/admin/v1/sandboxes"), strings.HasPrefix(path, "/api/admin/v1/anomalies"):
		return "sandbox:manage"
	case strings.HasPrefix(path, "/api/v1/admin/pitfalls"):
		return "pitfall:manage"
	case strings.HasPrefix(path, "/api/v1/admin/feedback"):
		return "feedback:view"
	case strings.HasPrefix(path, "/api/v1/admin/projects"):
		return "project:manage"
	case strings.HasPrefix(path, "/api/v1/admin/dashboard"), strings.HasPrefix(path, "/api/admin/v1/stats"):
		return "content:view"
	case (strings.HasPrefix(path, "/api/v1/projects") || strings.HasPrefix(path, "/api/v1/project-services")) && method != http.MethodGet && method != http.MethodHead:
		return "project:manage"
	case strings.HasPrefix(path, "/api/v1/admin/") && path != "/api/v1/admin/profile", strings.HasPrefix(path, "/api/admin/v1/"):
		return "*"
	}
	return ""
}

func AdminAuth(profileURL string) gin.HandlerFunc {
	client := &http.Client{Timeout: 3 * time.Second}
	return func(c *gin.Context) {
		c.Request.Header.Del("X-User-ID")
		c.Request.Header.Del("X-Admin-Permissions")
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		required := PermissionFor(c.Request.Method, c.Request.URL.Path)
		if required == "" {
			c.Next()
			return
		}
		uid, ok := UserID(c)
		if !ok || uid <= 0 {
			c.AbortWithStatusJSON(401, gin.H{"code": 20001, "msg": "请先登录"})
			return
		}
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, profileURL, nil)
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"code": 20005, "msg": "权限服务不可用"})
			return
		}
		req.Header.Set("X-User-ID", strconv.FormatInt(uid, 10))
		req.Header.Set("Authorization", c.GetHeader("Authorization"))
		resp, err := client.Do(req)
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"code": 20005, "msg": "权限服务不可用"})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 403 || resp.StatusCode == 404 {
			c.AbortWithStatusJSON(403, gin.H{"code": 20003, "msg": "没有管理权限"})
			return
		}
		var body struct {
			Code int `json:"code"`
			Data *struct {
				Permissions []string `json:"permissions"`
			} `json:"data"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.Code != 0 || body.Data == nil {
			c.AbortWithStatusJSON(503, gin.H{"code": 20005, "msg": "权限服务不可用"})
			return
		}
		allowed := false
		for _, p := range body.Data.Permissions {
			if p == "*" || p == required {
				allowed = true
			}
		}
		if !allowed {
			c.AbortWithStatusJSON(403, gin.H{"code": 20003, "msg": "没有管理权限"})
			return
		}
		c.Set("admin_permissions", body.Data.Permissions)
		c.Next()
	}
}
