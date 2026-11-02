package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func DashboardHandler(routes []config.RouteRule, token string) gin.HandlerFunc {
	bases := map[string]string{}
	for _, r := range routes {
		switch r.PathPrefix {
		case "/api/v1/users":
			bases["users"] = r.TargetURL
		case "/api/v1/projects":
			bases["content"] = r.TargetURL
		case "/api/admin/v1/defects":
			bases["faults"] = r.TargetURL
		case "/api/v1/training":
			bases["training"] = r.TargetURL
		case "/api/admin/v1/sandboxes":
			bases["sandboxes"] = r.TargetURL
		}
	}
	client := &http.Client{Timeout: 4 * time.Second}
	return func(c *gin.Context) {
		data := map[string]interface{}{}
		errs := []string{}
		for _, owner := range []string{"users", "content", "faults", "training", "sandboxes"} {
			if bases[owner] == "" {
				errs = append(errs, owner)
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		permissions, _ := c.Get("admin_permissions")
		ps, ok := permissions.([]string)
		if !ok {
			c.JSON(403, gin.H{"code": 20003, "msg": "没有管理权限"})
			return
		}
		canFeedback := false
		for _, p := range ps {
			if p == "*" || p == "feedback:view" {
				canFeedback = true
			}
		}
		for owner, base := range bases {
			wg.Add(1)
			go func(owner, base string) {
				defer wg.Done()
				path := "/api/admin/v1/stats"
				if owner == "sandboxes" {
					path = "/api/admin/v1/sandboxes/stats"
				}
				req, err := http.NewRequestWithContext(c.Request.Context(), "GET", strings.TrimRight(base, "/")+path, nil)
				var result struct {
					Code int                    `json:"code"`
					Data map[string]interface{} `json:"data"`
				}
				success := false
				if err == nil {
					req.Header.Set("X-Internal-Token", token)
					resp, err := client.Do(req)
					if err == nil {
						success = resp.StatusCode == 200 && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) == nil && result.Code == 0 && result.Data != nil
						resp.Body.Close()
					}
				}
				mu.Lock()
				defer mu.Unlock()
				if !success {
					errs = append(errs, owner)
					return
				}
				if owner == "sandboxes" {
					data["activeSandboxes"] = result.Data["active"]
					data["sandboxStats"] = result.Data
					return
				}
				for key, value := range result.Data {
					if key == "npsScore" && !canFeedback {
						continue
					}
					data[key] = value
				}
			}(owner, base)
		}
		wg.Wait()
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"stats": data, "unavailable": errs}})
	}
}
