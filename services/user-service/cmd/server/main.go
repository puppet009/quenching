// user-service 入口：注册/登录（账号密码，MVP 不做验证码）。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/adminstats"
	"github.com/quenching/go-common/analytics"
	commonauth "github.com/quenching/go-common/auth"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"
	"github.com/quenching/user-service/internal/handler"
	"github.com/quenching/user-service/internal/repository"
	"github.com/quenching/user-service/internal/service"
)

func main() {
	v := config.MustLoad("configs", "config")
	// 缺省保持严格策略；MVP 配置显式关闭，接入短信服务后才开启。
	v.SetDefault("auth.registration_sms_required", true)
	gin.SetMode(v.GetString("server.mode"))
	// log.dir 为空时仅 stdout（本地开发）；云端挂载 PVC 后日志同步落盘轮转。
	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "user-service.log")

	pool, err := database.NewPG(context.Background(), database.PGConfig{
		DSN:      v.GetString("db.dsn"),
		MaxConns: 20,
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "quenching-dev-secret" // 仅开发默认，生产必须通过环境变量注入
	}

	// 服务端埋点：直连 review-service 内部端点；base_url 为空时自动禁用。
	tracker := analytics.New(analytics.Config{
		BaseURL: v.GetString("review_service.base_url"),
		Token:   v.GetString("internal.token"),
	})

	authSvc := service.NewAuthService(
		repository.NewUserRepo(pool), commonauth.NewManager(secret),
		service.WithRegistrationSMSRequired(v.GetBool("auth.registration_sms_required")),
		service.WithAnalytics(tracker),
	)
	h := handler.NewAuthHandler(authSvc)

	profileSvc := service.NewProfileService(repository.NewProfileRepo(pool))
	profileH := handler.NewProfileHandler(profileSvc)

	internalProfileSvc := service.NewInternalProfileService(repository.NewInternalProfileRepo(pool))
	internalProfileH := handler.NewInternalProfileHandler(
		internalProfileSvc, v.GetString("internal.token"),
	)

	adminProfileSvc := service.NewAdminProfileService(repository.NewAdminProfileRepo(pool))
	adminProfileH := handler.NewAdminProfileHandler(adminProfileSvc)

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/api/admin/v1/stats", adminstats.Handler(v.GetString("internal.token"), func(ctx context.Context) (map[string]interface{}, error) { return repository.ReadAdminStats(ctx, pool) }))
	h.RegisterRoutes(r)
	profileH.RegisterRoutes(r)
	internalProfileH.RegisterRoutes(r)
	adminProfileH.RegisterRoutes(r)

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		log.Printf("user-service 启动在 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
