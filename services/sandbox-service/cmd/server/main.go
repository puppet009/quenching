// sandbox-service 入口（本地进程级联调 / MVP 合并部署）。
//
// 装配范围：
//   - 用户面：POST 启动沙箱（配额校验 -> DB 落库 -> 隔离资源确保），启动进度轮询/SSE；
//   - 管理面：MonitorHandler 的沙箱列表/详情/统计/释放、指标与异常查询（纯 DB）。
//
// K8s 客户端选择（装配层唯一区分云上/本地的位置）：
//   - 配置了 sandbox.kubeconfig：按路径连指定集群（集群外调试）；
//   - 未配置且 Pod 内有 ServiceAccount：走 InClusterConfig 连真实集群，
//     同时接线资源预检、Helm 部署、后台周期调谐与 TTL 回收；
//   - 两者都不满足（纯本地进程联调）：注入 client-go fake clientset，
//     只在内存/DB 里幂等建 namespace/quota/networkpolicy，不部署 Helm，
//     沙箱状态停在 deploying。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/analytics"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"
	"github.com/quenching/go-common/templates"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/quenching/sandbox-service/internal/handler"
	"github.com/quenching/sandbox-service/internal/repository"
	"github.com/quenching/sandbox-service/internal/service"
)

func main() {
	v := config.MustLoad("configs", "config")
	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "sandbox-service.log")
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

	// 仓储：同一 PGStore 同时满足 service.Store 与 service.MonitorStore。
	store := repository.NewPGStore(pool)

	// K8s 客户端：显式 kubeconfig > 集群内 ServiceAccount > fake（本地降级）。
	kubeconfig := v.GetString("sandbox.kubeconfig")
	var kc *service.K8sClient
	switch {
	case kubeconfig != "":
		kc, err = service.NewK8sClientFromKubeconfig(kubeconfig)
		if err != nil {
			log.Fatalf("按 kubeconfig(%s) 初始化 K8s 客户端失败: %v", kubeconfig, err)
		}
	default:
		kc, err = service.NewK8sClientInCluster()
		if err == nil {
			break
		}
		if !errors.Is(err, rest.ErrNotInCluster) {
			log.Fatalf("集群内初始化 K8s 客户端失败: %v", err)
		}
		log.Printf("[WARN] 未配置 sandbox.kubeconfig 且不在集群内，使用 fake clientset 本地降级：" +
			"只落 DB 记录、不触达真实集群，沙箱/故障上云验证待 k8s 环境就绪")
		kc = service.NewK8sClient(fake.NewSimpleClientset())
	}

	kc.ConfigureFaultReader(v.GetString("fault_execution.platform_namespace"), "fault-engine", v.GetBool("fault_execution.enabled"))

	// 用户面链路：配额 + 调谐器 + 沙箱服务。
	quota := service.NewQuotaService(store)
	rec := service.NewReconciler(kc, store)
	// 活跃度续租信号：终端输入 + training 侧操作上报都刷新租期锚点，
	// 使"班次不硬超时"成立（回收改由无活动租期与绝对上限兜底）。
	activityTracker := service.NewActivityTracker(store, nil, 0)
	// 官方模板注册表（template_slug → chart 路径 / Helm release 名）。
	templateRegistry := templates.Load(v.GetString("templates.root"))
	log.Printf("模板注册表加载完成，可用模板 %d 个", len(templateRegistry.List()))
	// 真实集群：接线资源预检 + Helm 部署；fake 降级：两者传 nil 跳过。
	var checker service.ResourceChecker
	var provision *service.ProvisionService
	if kc.RealCluster() {
		checker = service.NewK8sResourceChecker(kc)
		provision = service.NewProvisionService(
			service.NewHelmCLI(), service.NewReadyWaiter(kc.Clientset()))
	}
	sandboxSvc := service.NewSandboxService(rec, quota, checker, store, provision, templateRegistry)
	// 服务端埋点：沙箱就绪上报 first_start；base_url 为空时客户端禁用。
	sandboxSvc.SetAnalytics(analytics.New(analytics.Config{
		BaseURL: v.GetString("review_service.base_url"),
		Token:   v.GetString("internal.token"),
	}))

	platformNamespace := v.GetString("resources.platform_namespace")
	if platformNamespace == "" {
		platformNamespace = "platform"
	}
	kc.ConfigureTerminalAccess(platformNamespace, v.GetString("sandbox.terminal_cluster_role"))
	resourcePool := service.ResourcePool{Client: kc.Clientset(), PlatformNamespace: platformNamespace, Fake: !kc.RealCluster()}
	var resourceQueue *service.ResourceQueue
	var elastic *service.ElasticController
	if kc.RealCluster() {
		elastic = &service.ElasticController{Client: kc.Clientset(), Store: store, Pool: resourcePool, Registry: templateRegistry}
		resourceQueue = &service.ResourceQueue{Store: store, Snapshot: elastic.Snapshot, Deploy: sandboxSvc.ConvergeAllocation}
		sandboxSvc.EnableResourceQueue(resourceQueue)
	}
	tracker := service.NewProgressTracker(store, kc.Clientset())
	sandboxHandler := handler.NewSandboxHandler(sandboxSvc, tracker)

	// 行为模拟运行时：沙箱 ready 后随生命周期启停（fake 降级只标记 degraded）。
	behaviorRuntime := service.NewBehaviorRuntime(kc)
	rec.SetBehaviorRuntime(behaviorRuntime)
	sandboxSvc.SetBehaviorRuntime(behaviorRuntime, v.GetString("behavior.signal_endpoint"))
	behaviorHandler := handler.NewBehaviorHandler(behaviorRuntime)
	// 行为信号汇聚：runner 上报入口 + 排障查询（沙箱释放时清缓冲）。
	// 配置了 training_service.base_url 时：信号异步转发告警引擎，沙箱就绪触发默认规则注册。
	alertClient := service.NewAlertClient(v.GetString("training_service.base_url"), v.GetString("internal.token"))
	behaviorSignalStore := service.NewBehaviorSignalStore()
	if alertClient != nil {
		behaviorSignalStore.SetForwarder(alertClient)
		sandboxSvc.SetAlertClient(alertClient)
	}
	rec.SetBehaviorSignalStore(behaviorSignalStore)
	behaviorSignalHandler := handler.NewBehaviorSignalHandler(behaviorSignalStore)

	// 管理面链路：监控服务（读 DB；后台采集/TTL 扫描本地不启动）。
	monitorSvc := service.NewMonitorService(store, service.DefaultMonitorConfig())
	monitorHandler := handler.NewMonitorHandler(monitorSvc)

	// 桌面化：服务器目标列表 + 真实终端 exec（K8s exec 桥接）。
	operationStore := repository.NewOperationStore(pool)
	kc.MutationAudit = &service.FileAudit{Store: operationStore, Read: kc.ReadAuditState}
	go operationStore.RunRecovery(ctx)
	go service.RunLegacyOperationOutbox(ctx, repository.NewLegacyOperationRepo(pool), v.GetString("training_service.base_url"), v.GetString("internal.token"))
	go operationStore.RunOutbox(ctx, v.GetString("training_service.base_url"), v.GetString("internal.token"))
	auditRepo := repository.NewTrainingAuditRepo(pool)
	trainingAudit := service.NewTrainingAuditService(auditRepo)
	go service.RunTrainingAuditOutbox(ctx, auditRepo, v.GetString("training_service.base_url"), v.GetString("internal.token"))
	desktopHandler := handler.NewDesktopHandler(kc, activityTracker)
	desktopHandler.SetTrainingAudit(trainingAudit)
	desktopHandler.Owned = repository.OperationOwner(pool)
	// 文件管理器：真实文件系统读写 + 上传下载（对标腾讯云 OrcaTerm）。
	filesHandler := handler.NewFilesHandler(kc)
	filesHandler.SetTrainingAudit(trainingAudit)
	filesHandler.Owned = repository.OperationOwner(pool)

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// 用户面路由：uid 由网关注入的 X-User-Id 头解析（本地裸调需自行带头）。
	managedCommands := service.NewManagedCommandService(operationStore, service.K8sManagedExecutor{Client: kc}, repository.OperationOwner(pool))
	managedCommands.WorkspaceOpen = trainingAudit.AssertWorkspaceOpen
	managedCommands.Root = ctx
	g := r.Group("/api/v1/sandboxes", handler.UserID())
	handler.ManagedCommandsHandler{Service: managedCommands}.Register(g)
	{
		g.POST("", sandboxHandler.Create)
		g.GET("/:id/progress", sandboxHandler.Progress)
		g.GET("/:id/progress/stream", sandboxHandler.ProgressStream)
		// 桌面化：服务器目标列表 + 真实终端 exec（WS，uid 由网关注入）。
		g.GET("/:id/targets", desktopHandler.ListTargets)
		g.GET("/:id/exec", desktopHandler.Exec)
		// 文件管理器：真实文件系统读写 + 上传下载（组内补 :id/files 路由）。
		filesHandler.RegisterFiles(g)
		// 行为模拟：随沙箱自动启停，管理/排障接口（:id/behavior/*）。
		behaviorHandler.RegisterBehavior(g)
		// 行为信号：runner 上报 + 信号查询（:id/behavior/signals）。
		behaviorSignalHandler.RegisterBehaviorSignals(g)
	}

	// 管理面路由自带 /api/admin/v1 前缀（8 条）。
	monitorHandler.RegisterRoutes(r)
	handler.ResourcesHandler{Pool: resourcePool, Elastic: elastic, InternalToken: v.GetString("internal.token"), Controls: service.ResourceControls{Client: kc.Clientset(), PlatformNamespace: platformNamespace, Fake: !kc.RealCluster()}, Store: store, JWTSecret: v.GetString("auth.jwt_secret"), ProfileURL: v.GetString("resources.admin_profile_url")}.Register(r)

	// Agent M3：服务间内部只读接口（targets/files/exec），X-Internal-Token 校验。
	handler.OperationEvidenceHandler{Store: operationStore, Owned: repository.OperationOwner(pool), Ingest: service.CanonicalOperationIngest(operationStore, service.TrainingOperationOwner(v.GetString("training_service.base_url"), v.GetString("internal.token"))), Token: v.GetString("internal.token")}.Register(r)
	handler.RegisterLegacyOperations(r, repository.NewLegacyOperationRepo(pool), service.TrainingOperationOwner(v.GetString("training_service.base_url"), v.GetString("internal.token")), v.GetString("internal.token"))
	handler.RegisterLegacyCheckpoint(r, repository.NewLegacyOperationRepo(pool), service.TrainingOperationOwner(v.GetString("training_service.base_url"), v.GetString("internal.token")), v.GetString("internal.token"))
	handler.OperationAuditHandler{Store: operationStore, Token: v.GetString("internal.token")}.Register(r)
	handler.NewInternalReadonlyHandler(kc, v.GetString("internal.token")).RegisterRoutes(r)
	behaviorCommandRepo := repository.NewBehaviorCommandRepo(pool)
	go service.RunBehaviorOutbox(ctx, behaviorCommandRepo, v.GetString("training_service.base_url"), v.GetString("internal.token"))
	behaviorCommandBase := v.GetString("behavior.command_base_url")
	if behaviorCommandBase == "" {
		behaviorCommandBase = "http://sandbox-service:8089"
	}
	behaviorCommandService := service.NewBehaviorCommandService(behaviorCommandRepo, service.NewTemplateBehaviorPreparer(behaviorRuntime, templateRegistry, store, behaviorCommandRepo, behaviorCommandBase))
	handler.NewBehaviorCommandsHandler(behaviorCommandService, v.GetString("internal.token")).RegisterRoutes(r)
	sandboxPaymentRepo := repository.NewSandboxPaymentRepo(pool)
	sandboxPayments := service.NewSandboxPaymentService(sandboxPaymentRepo, store, service.NewTemplatePaymentAdapter(kc, templateRegistry, store))
	handler.NewSandboxPaymentsHandler(sandboxPayments, behaviorCommandService).RegisterRoutes(r)

	// 内部生命周期接口（activity/complete/release）：training 判定活动与结束，沙箱侧执行回收。
	handler.NewInternalLifecycleHandler(store, rec, activityTracker,
		v.GetString("internal.token")).RegisterRoutes(r)

	// 真实集群后台任务（fake 降级不启动）：
	//  - 周期调谐：以 DB 期望态对齐集群，补建残留期望沙箱、回收孤儿 namespace；
	//  - 租期回收：距最后一次活动超过 ActivityLease（默认 30 分钟）即释放，
	//    并有一条 MaxSandboxLifetime（默认 4 小时）绝对上限兜底防资源永驻。
	if kc.RealCluster() {
		go resourceQueue.Run(ctx)
		go elastic.Run(ctx)
		go runReconcileLoop(ctx, rec, store)
		go service.NewTTLScanner(rec, service.NewSystemClock(), 0).Run(ctx)
	}

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		log.Printf("sandbox-service 启动在 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// runReconcileLoop 周期执行一轮调谐，直到 ctx 取消。
// 启动后立即跑一轮（尽快自愈重启前残留），之后固定 1 分钟一轮；
// 单轮失败只记录日志，不中断循环（下一轮继续）。
func runReconcileLoop(ctx context.Context, rec *service.Reconciler, store *repository.PGStore) {
	const interval = time.Minute
	run := func() {
		runCtx, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		if err := store.WithAllocationLock(runCtx, rec.ReconcileOnce); err != nil {
			log.Printf("[WARN] 沙箱调谐失败（下一轮继续）: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
