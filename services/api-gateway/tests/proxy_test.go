package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
	"github.com/quenching/api-gateway/internal/proxy"
)

// closeNotifyRecorder 包装 httptest.ResponseRecorder 并实现 http.CloseNotifier。
// Go 1.26+ 的 httputil.ReverseProxy 会调用 CloseNotify（取消传播），
// 标准 ResponseRecorder 不实现该接口，直接走代理会 panic。
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool, 1),
	}
}

func (c *closeNotifyRecorder) CloseNotify() <-chan bool {
	return c.closeNotify
}

// 启动一个模拟后端服务
func newMockBackend(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestRouteMatchLongestPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 两个模拟后端
	userSrv := newMockBackend(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "user-service")
		w.WriteHeader(200)
		w.Write([]byte("user"))
	})
	defer userSrv.Close()

	authSrv := newMockBackend(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "auth-service")
		w.WriteHeader(200)
		w.Write([]byte("auth"))
	})
	defer authSrv.Close()

	rules := []config.RouteRule{
		{PathPrefix: "/api/v1/users", TargetURL: userSrv.URL, Auth: true},
		{PathPrefix: "/api/v1/auth", TargetURL: authSrv.URL, Auth: false},
	}

	router, err := proxy.NewRouter(rules)
	if err != nil {
		t.Fatalf("创建路由失败: %v", err)
	}

	r := gin.New()
	r.Any("/api/v1/*path", router.Handler())

	// /api/v1/auth/login 应匹配 auth（更长前缀）
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	w := newCloseNotifyRecorder()
	r.ServeHTTP(w, req)

	if w.Header().Get("X-Backend") != "auth-service" {
		t.Errorf("应路由到 auth-service，got %s", w.Header().Get("X-Backend"))
	}

	// /api/v1/users/me 应匹配 user
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	w2 := newCloseNotifyRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Header().Get("X-Backend") != "user-service" {
		t.Errorf("应路由到 user-service，got %s", w2.Header().Get("X-Backend"))
	}
}

func TestRouteNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	backend := newMockBackend(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	defer backend.Close()

	rules := []config.RouteRule{
		{PathPrefix: "/api/v1/users", TargetURL: backend.URL, Auth: true},
	}
	router, _ := proxy.NewRouter(rules)

	r := gin.New()
	r.Any("/api/v1/*path", router.Handler())

	// 不匹配的路径
	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown/path", nil)
	w := newCloseNotifyRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("未知路径应返回 404，got %d", w.Code)
	}
}

func TestRouteForwardsXForwardedFor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var gotXForwardedFor string
	backend := newMockBackend(func(w http.ResponseWriter, r *http.Request) {
		gotXForwardedFor = r.Header.Get("X-Forwarded-For")
		w.WriteHeader(200)
	})
	defer backend.Close()

	rules := []config.RouteRule{
		{PathPrefix: "/api/v1/test", TargetURL: backend.URL, Auth: false},
	}
	router, _ := proxy.NewRouter(rules)

	r := gin.New()
	r.Any("/api/v1/*path", router.Handler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/test/hello", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	w := newCloseNotifyRecorder()
	r.ServeHTTP(w, req)

	if gotXForwardedFor == "" {
		t.Error("应设置 X-Forwarded-For 头")
	}
}
