// fault-engine 入口。
//
// 装配范围：
//   - 管理面：PG 剧本仓储 -> DefectService -> admin CRUD/启停/排序/校验接口；
//   - 运行时（T1-26/27、T2-13）：kubectl exec 生产执行通道（CloudExec）
//   - Prometheus 指标抓取（PromFetcher）+ experiments 仓储
//     -> RuntimeService（剧本编排/临时注入/撤销/会话补偿）-> /api/v1/runtime 接口；
//   - SupervisorRegistry 统一管理每会话驱动循环，进程退出时 StopAll 撤销全部在注故障。
//
// 领域组件均为接口抽象；云上接线集中在本文件，领域代码不随环境变化。
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/adminstats"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"

	"github.com/quenching/fault-engine/internal/handler"
	"github.com/quenching/fault-engine/internal/repository"
	"github.com/quenching/fault-engine/internal/service"
)

func main() {
	v := config.MustLoad("configs", "config")
	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "fault-engine.log")
	gin.SetMode(v.GetString("server.mode"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPG(ctx, database.PGConfig{
		DSN:      v.GetString("db.dsn"),
		MaxConns: int32(v.GetInt("db.max_conns")),
	})
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer pool.Close()

	defectRepo := repository.NewContentDefectReader(v.GetString("content_service.base_url"), v.GetString("internal.token"))
	// ——— 运行时链路（T1-26/27、T2-13）———
	// 1) blade 执行通道：kubectl exec 进 ChaosBlade agent Pod。
	cloudExec, execErr := service.NewBladeK8sExec(service.BladeCloudExecConfig{
		Enabled:          v.GetBool("chaos.enabled"),
		OperatorName:     v.GetString("chaos.operator_name"),
		KubectlPath:      v.GetString("chaos.kubectl_path"),
		Kubeconfig:       v.GetString("chaos.kubeconfig"),
		AgentNamespace:   v.GetString("chaos.agent_namespace"),
		AgentPodSelector: v.GetString("chaos.agent_pod_selector"),
		Container:        v.GetString("chaos.container"),
		BladePath:        v.GetString("chaos.blade_path"),
	})

	if execErr != nil {
		log.Printf("[WARN] fault execution unavailable: %v", execErr)
	}

	// 2) Prometheus 指标抓取器。
	promFetcher := service.NewJudgePromFetcher(service.JudgePromFetcherConfig{
		BaseURL:       v.GetString("prometheus.url"),
		MetricQueries: v.GetStringMapString("prometheus.metric_queries"),
	})

	// 3) experiments 仓储 + 会话编排注册表 + 运行时服务。
	expRepo := repository.NewPGExperimentRepo(pool)
	registry := service.NewSupervisorRegistry()
	runtimeSvc := service.NewRuntimeService(
		ctx,
		cloudExec,
		defectRepo,
		expRepo,
		registry,
		promFetcher,
		service.RuntimeConfig{
			StableRequired:   time.Duration(v.GetInt("runtime.stable_required_seconds")) * time.Second,
			PollInterval:     time.Duration(v.GetInt("runtime.poll_interval_seconds")) * time.Second,
			DefaultNamespace: v.GetString("runtime.default_namespace"),
		},
	)
	defer runtimeSvc.Close()
	runtimeSvc.SetTemplateResolver(service.TemplateResolver(v.GetString("content_service.base_url")))
	runtimeSvc.SetTemplateTargetResolver(service.TemplateTargetResolver(v.GetString("sandbox_service.base_url"), v.GetString("internal.token")))
	runtimeSvc.SetAutoscalingGuard(service.HTTPAutoscalingGuard{BaseURL: v.GetString("sandbox_service.base_url"), Token: v.GetString("internal.token")})
	runtimeHandler := handler.NewRuntimeHandler(runtimeSvc, v.GetString("internal.token"))
	faultCommands := service.NewFaultCommandService(repository.NewPGFaultCommandRepo(pool).WithCatalog(v.GetString("content_service.base_url"), v.GetString("internal.token")), runtimeSvc, cloudExec, service.TemplateRevisionResolver(v.GetString("content_service.base_url")))
	faultCommandHandler := handler.NewFaultCommandHandler(faultCommands, v.GetString("internal.token"))

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/api/admin/v1/stats", adminstats.Handler(v.GetString("internal.token"), func(ctx context.Context) (map[string]interface{}, error) { return map[string]interface{}{}, nil }))

	handler.RegisterContentCompatibility(r, v.GetString("content_service.base_url"))
	handler.RegisterConfigValidation(r, v.GetString("internal.token"))
	// 服务间运行时路由 /api/v1/runtime。
	runtimeHandler.RegisterRoutes(r)
	faultCommandHandler.RegisterRoutes(r)

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		log.Printf("fault-engine 启动在 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("fault-engine 正在退出，先撤销全部进行中的故障注入")
	// 先停编排器：触发各会话 Orchestrator.Abort 撤销剧本故障。
	registry.StopAll()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
