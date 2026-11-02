// content-service 入口。
// 启动流程：加载配置 → 初始化日志/PG → 装配 repo→service→handler → 注册路由 → 起 HTTP 服务。
// 其他 Go 服务照此结构，只替换装配的领域组件。
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
	"go.uber.org/zap"

	"github.com/quenching/content-service/internal/handler"
	"github.com/quenching/content-service/internal/repository"
	"github.com/quenching/content-service/internal/service"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"
	"github.com/quenching/go-common/templates"
)

func main() {
	v := config.MustLoad("configs", "config")

	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "content-service.log")

	ctx := context.Background()
	pool, err := database.NewPG(ctx, database.PGConfig{
		DSN:      v.GetString("db.dsn"),
		MaxConns: int32(v.GetInt("db.max_conns")),
	})
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer pool.Close()

	// 依赖注入：repo -> service -> handler。
	projectRepo := repository.NewProjectRepo(pool)
	projectSvc := service.NewProjectService(projectRepo)
	projectHandler := handler.NewProjectHandler(projectSvc)

	projectServiceRepo := repository.NewProjectServiceRepo(pool)
	projectServiceSvc := service.NewProjectServiceService(projectServiceRepo)
	projectServiceHandler := handler.NewProjectServiceHandler(projectServiceSvc)

	deployConfigRepo := repository.NewProjectDeployConfigRepo(pool)
	deployConfigSvc := service.NewProjectDeployConfigService(deployConfigRepo)
	deployConfigHandler := handler.NewProjectDeployConfigHandler(deployConfigSvc)

	defectRepo := repository.NewPGDefectRepo(pool)
	defectSvc := service.NewDefectService(defectRepo)
	defectSvc.SetExecutionValidator(service.FaultExecutionValidator(v.GetString("fault_engine.base_url"), v.GetString("internal.token")))
	defectHandler := handler.NewDefectHandler(defectSvc)
	pitfallRepo := repository.NewPitfallRepo(pool)
	pitfallSvc := service.NewPitfallAdminService(pitfallRepo)
	// Project display still uses the existing white-list projection contract.
	faultDefectClient := repository.NewOwnedDefectReader(defectRepo)
	projectDefectRepo := repository.NewProjectDefectRepo(faultDefectClient)
	projectDefectSvc := service.NewProjectDefectService(projectDefectRepo)
	projectDefectHandler := handler.NewProjectDefectHandler(projectDefectSvc)

	// 服务间内部查询：项目 / 项目服务最小投影，供 training-service 等编排方调用。
	internalProjectRepo := repository.NewInternalProjectRepo(pool)
	internalProjectSvc := service.NewInternalProjectService(internalProjectRepo)
	internalProjectHandler := handler.NewInternalProjectHandler(
		internalProjectSvc, v.GetString("internal.token"))

	// 官方模板只读注册表：扫描模板目录（templates.root，环境变量 TEMPLATES_ROOT 覆盖）。
	// 目录不存在时返回空注册表，不影响服务启动。
	templateRoot := v.GetString("templates.root")
	templateRegistry := templates.Load(templateRoot)
	defectSvc.SetTemplateResolver(service.RegistryTemplateResolver(templateRegistry))
	templateHandler := handler.NewTemplateHandler(templateRegistry)
	logger.L().Info("模板注册表加载完成", zap.Int("templates", len(templateRegistry.List())))

	gin.SetMode(v.GetString("server.mode"))
	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())

	// 健康检查。
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/api/admin/v1/stats", adminstats.Handler(v.GetString("internal.token"), func(ctx context.Context) (map[string]interface{}, error) {
		data, err := repository.ReadAdminStats(ctx, pool)
		if err == nil {
			data["totalTemplates"] = len(templateRegistry.List())
		}
		return data, err
	}))

	// 业务路由。MVP 合并部署时由网关统一前缀 /api/v1，单体直连也可直接挂。
	api := r.Group("/api/v1")
	{
		// 官方预置模板（只读，数据来自 templates/ 目录扫描）。
		api.GET("/templates", templateHandler.List)
		api.GET("/templates/:slug", templateHandler.Get)
		api.GET("/template-media/:slug/:file", templateHandler.Media)
		adminProjects := api.Group("/admin/projects")
		adminProjects.GET("", projectHandler.List)
		adminProjects.POST("", projectHandler.Create)
		adminProjects.GET("/:id", projectHandler.Get)
		adminProjects.PUT("/:id", projectHandler.Update)
		adminProjects.DELETE("/:id", projectHandler.Delete)
		adminProjects.PATCH("/:id/status", projectHandler.Update)

		projects := api.Group("/projects")
		{
			projects.POST("", projectHandler.Create)
			projects.GET("", projectHandler.List)
			projects.GET("/:id", projectHandler.Get)
			projects.PUT("/:id", projectHandler.Update)
			projects.DELETE("/:id", projectHandler.Delete)

			// 子资源参数名必须与上面的 /:id 保持一致：Gin 路由树同一位置只允许
			// 同一个参数名，混用 :id 与 :projectId 会在启动时 panic（wildcard 冲突）。
			// 项目下的服务列表
			projects.GET("/:id/services", projectServiceHandler.ListByProject)
			// 项目下的故障概览（用户面只读）
			projects.GET("/:id/defects", projectDefectHandler.ListByProject)
			// 项目下的部署配置
			projects.GET("/:id/deploy-config", deployConfigHandler.GetByProject)
			projects.POST("/:id/deploy-config", deployConfigHandler.Save)
			projects.PUT("/:id/deploy-config", deployConfigHandler.Update)
			projects.DELETE("/:id/deploy-config", deployConfigHandler.Delete)
		}
		// 项目服务独立CRUD
		services := api.Group("/project-services")
		{
			services.POST("", projectServiceHandler.Create)
			services.GET("/:id", projectServiceHandler.Get)
			services.PUT("/:id", projectServiceHandler.Update)
			services.DELETE("/:id", projectServiceHandler.Delete)
		}
	}

	// 服务间路由（/internal/v1）：不挂网关、不对前端暴露，供集群后端直连。
	internalProjectHandler.RegisterRoutes(r)
	defectHandler.RegisterRoutes(r)
	handler.NewPitfallAdminHandler(pitfallSvc).RegisterRoutes(r)
	handler.NewInternalDefectHandler(service.NewInternalDefectService(defectRepo), v.GetString("internal.token")).RegisterRoutes(r)
	handler.NewInternalDefectScriptHandler(service.NewInternalDefectScriptService(defectRepo), v.GetString("internal.token")).RegisterRoutes(r)
	handler.RegisterCatalog(r, v.GetString("internal.token"), defectRepo, pitfallRepo)
	handler.RegisterCombinations(r, repository.NewCombinationRepo(pool), v.GetString("fault_engine.base_url"), v.GetString("internal.token"))

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		logger.L().Info("content-service 启动", zap.String("addr", v.GetString("server.addr")))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 优雅退出。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
