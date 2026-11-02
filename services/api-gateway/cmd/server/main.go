// api-gateway 入口。
// 功能：
//   - CORS 跨域 + 安全响应头
//   - JWT 鉴权（白名单放行）
//   - 限流（令牌桶，按用户/IP）
//   - 路由转发（按路径前缀到各后端服务）
//
// 中间件执行顺序：
//
//	Recovery → CORS → RateLimit → JWTAuth → Proxy
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	commonauth "github.com/quenching/go-common/auth"
	"github.com/quenching/go-common/logger"

	"github.com/quenching/api-gateway/internal/config"
	"github.com/quenching/api-gateway/internal/handler"
	"github.com/quenching/api-gateway/internal/middleware"
	"github.com/quenching/api-gateway/internal/pkg/hub"
	"github.com/quenching/api-gateway/internal/proxy"
)

func main() {
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 加载配置
	cfg := config.Load()
	// LOG_DIR 为空时仅 stdout（本地开发）；云端挂载 PVC 后同步落盘轮转。
	logger.InitWithFile(cfg.LogLevel, cfg.LogDir, "api-gateway.log")

	// JWT 管理器
	jwtMgr := commonauth.NewManager(cfg.JWTSecret)

	// 限流器
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit)

	// 路由代理
	router, err := proxy.NewRouter(cfg.Routes)
	if err != nil {
		log.Fatalf("初始化路由失败: %v", err)
	}

	// WebSocket 中枢（g7：ws-gateway 并入本进程）
	// Hub 负责连接注册/心跳/频道分发；EventHandler 负责内部事件发布。
	wsHub := hub.NewHub()
	go wsHub.Run()
	wsHandler := handler.NewWSHandler(wsHub, jwtMgr, cfg.WSOrigins)
	eventHandler := handler.NewEventHandler(wsHub, cfg.WSInternalAPIKey)
	clientLogs := handler.NewClientLogs(cfg.LogDir)

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())

	// 1. CORS（最外层，OPTIONS 直接返回）
	if cfg.CORS.Enabled {
		r.Use(middleware.CORS(cfg.CORS))
	}

	// 2. 限流（鉴权前，避免未登录请求打爆）
	r.Use(rateLimiter.Middleware())

	// 3. JWT 鉴权
	// 公开路径：除了登录注册，还要包含各服务无需鉴权的路径
	r.Use(middleware.JWTAuth(jwtMgr, func(method, path string) bool {
		if path == "/api/v1/client/time" && method == http.MethodGet || path == "/api/v1/client/logs" && method == http.MethodPost {
			return true
		}
		// 健康检查
		if path == "/health" {
			return true
		}
		// WebSocket 握手：网关 JWT 中间件放行，由 WSHandler 自行校验 query/ Bearer token
		if path == "/ws" {
			return true
		}
		// 内部事件发布：放行后由 EventHandler 用 X-Internal-API-Key 自行鉴权。
		// 仅限 publish/stats；其余 /api/v1/events* 是 review-service 埋点接口，需 JWT。
		if path == "/api/v1/events/publish" || path == "/api/v1/events/stats" {
			return true
		}
		// 检查路由表中标记为无需鉴权的前缀
		for _, rule := range cfg.Routes {
			if !rule.Auth && strings.HasPrefix(path, rule.PathPrefix) {
				return true
			}
		}
		return false
	}))

	profileURL := "http://localhost:8085/api/v1/admin/profile"
	for _, route := range cfg.Routes {
		if route.PathPrefix == "/api/v1/admin" {
			profileURL = strings.TrimRight(route.TargetURL, "/") + "/api/v1/admin/profile"
		}
	}
	r.Use(middleware.AdminAuth(profileURL))

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "api-gateway"})
	})

	// 鉴权自检接口（验证中间件链路）。
	// 必须挂在 /api/v1 之外：/api/v1/*path 是 catch-all 通配路由，
	// Gin（httprouter 树）不允许同层级再注册静态段 /api/v1/gateway/...，否则启动 panic。
	// 仍受 JWT 中间件保护（未在放行白名单内）。
	r.GET("/gateway/whoami", func(c *gin.Context) {
		uid, _ := middleware.UserID(c)
		c.JSON(http.StatusOK, gin.H{"uid": uid})
	})

	// WebSocket 连接入口（自行 JWT 鉴权，不走代理）
	r.GET("/ws", wsHandler.Handle)

	// 4. 路由转发（通配符，匹配用户面 /api/v1/* 与管理面 /api/admin/v1/* 请求）
	// /api/v1/events/publish|stats 已并入本进程，在代理前分流给 EventHandler
	// （内部 API Key 鉴权）；其余 /api/v1/events* 走 review-service 埋点接口。
	r.Any("/api/v1/*path", func(c *gin.Context) {
		// Keep these under the existing catch-all: static sibling routes panic in Gin.
		if c.Request.URL.Path == "/api/v1/client/time" && c.Request.Method == http.MethodGet {
			handler.ClientTime(c)
			return
		}
		if c.Request.URL.Path == "/api/v1/client/logs" && c.Request.Method == http.MethodPost {
			clientLogs(c)
			return
		}
		if c.Request.URL.Path == "/api/v1/admin/dashboard" && c.Request.Method == http.MethodGet {
			handler.DashboardHandler(cfg.Routes, cfg.InternalToken)(c)
			return
		}
		if c.Request.URL.Path == "/api/v1/events/publish" || c.Request.URL.Path == "/api/v1/events/stats" {
			eventHandler.Handle(c)
			return
		}
		router.Handler()(c)
	})
	r.Any("/api/admin/v1/*path", router.Handler())

	// 启动服务
	addr := cfg.Addr
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Printf("api-gateway 启动在 %s", addr)
		log.Printf("  CORS:      %v", cfg.CORS.Enabled)
		log.Printf("  限流:      %v (rate=%d/s, burst=%d)", cfg.RateLimit.Enabled, cfg.RateLimit.RatePerSec, cfg.RateLimit.Burst)
		log.Printf("  路由规则:   %d 条", len(cfg.Routes))
		for _, rule := range cfg.Routes {
			authFlag := "🔒"
			if !rule.Auth {
				authFlag = "🔓"
			}
			log.Printf("    %s %s → %s", authFlag, rule.PathPrefix, rule.TargetURL)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("网关启动失败: %v", err)
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭网关...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("网关强制关闭: %v", err)
	}
	log.Println("网关已关闭")
}
