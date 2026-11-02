// Package tests 网关 JWT 中间件单测（httptest，不依赖外部服务）。
package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/middleware"
	commonauth "github.com/quenching/go-common/auth"
)

func setupRouter(mgr *commonauth.Manager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.JWTAuth(mgr, nil))

	// 一个公开接口 + 一个受保护接口
	protected := r.Group("/api/v1")
	protected.GET("/me", func(c *gin.Context) {
		uid, ok := middleware.UserID(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "no uid"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"uid": uid})
	})
	return r
}

func newMgr() *commonauth.Manager { return commonauth.NewManager("gateway-secret") }

func do(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPublicPathBypassesAuth(t *testing.T) {
	r := setupRouter(newMgr())
	// /health 在默认白名单
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	w := do(r, http.MethodGet, "/health", "")
	if w.Code != http.StatusOK {
		t.Errorf("公开路径应放行，got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMissingTokenRejected(t *testing.T) {
	r := setupRouter(newMgr())
	w := do(r, http.MethodGet, "/api/v1/me", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无 token 应 401，got %d", w.Code)
	}
}

func TestMalformedHeaderRejected(t *testing.T) {
	r := setupRouter(newMgr())
	cases := []string{"", "Basic abc", "Bearer", "Bearer  ", "Token xxx"}
	for _, h := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		if h != "" {
			req.Header.Set("Authorization", h)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("头 %q 应 401，got %d", h, w.Code)
		}
	}
}

func TestInvalidTokenRejected(t *testing.T) {
	r := setupRouter(newMgr())
	w := do(r, http.MethodGet, "/api/v1/me", "not.a.jwt")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("非法 token 应 401，got %d", w.Code)
	}
}

func TestWrongSecretRejected(t *testing.T) {
	// 用别的密钥签发，网关密钥不同应拒绝
	other := commonauth.NewManager("other-secret").IssueAccess
	tok, _ := other(7)
	r := setupRouter(newMgr())
	w := do(r, http.MethodGet, "/api/v1/me", tok)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("异密钥 token 应 401，got %d", w.Code)
	}
}

func TestRefreshTokenRejected(t *testing.T) {
	mgr := newMgr()
	refresh, _ := mgr.IssueRefresh(9)
	r := setupRouter(mgr)
	w := do(r, http.MethodGet, "/api/v1/me", refresh)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("refresh 令牌访问业务接口应 401，got %d", w.Code)
	}
}

func TestValidAccessTokenPassesUID(t *testing.T) {
	mgr := newMgr()
	access, _ := mgr.IssueAccess(123)
	r := setupRouter(mgr)
	w := do(r, http.MethodGet, "/api/v1/me", access)
	if w.Code != http.StatusOK {
		t.Fatalf("合法 access 应放行，got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"uid":123`) {
		t.Errorf("应把 uid 注入上下文，body=%s", w.Body.String())
	}
}

func TestOptionsPreflightBypassed(t *testing.T) {
	r := setupRouter(newMgr())
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// 没有注册 OPTIONS handler，gin 可能 404，但绝不应是 401（预检不带token不应被鉴权挡）
	if w.Code == http.StatusUnauthorized {
		t.Error("CORS 预检不应被鉴权中间件 401")
	}
}

func TestCustomPublicPredicate(t *testing.T) {
	mgr := newMgr()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 自定义白名单：/public/* 全部放行
	r.Use(middleware.JWTAuth(mgr, func(_, path string) bool {
		return path == "/public/ping"
	}))
	r.GET("/public/ping", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	w := do(r, http.MethodGet, "/public/ping", "")
	if w.Code != http.StatusOK {
		t.Errorf("自定义公开路径应放行，got %d", w.Code)
	}
}
