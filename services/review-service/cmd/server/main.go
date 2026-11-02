// review-service 入口。
// 装配：PG 仓储 + RabbitMQ 生产者/消费者 + ai-service gRPC 客户端 +
// 方法论复盘生成器（method-v3 六章，T3-2/T3-3）+ 坑点章节（T3-13）。
//
// HTTP 接口同步触发/查询复盘；生成过程异步：API 投递任务到 review.exchange，
// 同进程 worker 串行消费、调 ai-service 生成六章并幂等落库。
// MVP 合并部署共库：复盘输入/坑点数据跨服务只读 training/fault-engine 的表，
// 拆库后替换 repository 实现为 gRPC 调用即可，装配层之外不动。
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"

	"github.com/quenching/review-service/internal/handler"
	"github.com/quenching/review-service/internal/repository"
	"github.com/quenching/review-service/internal/service"
)

func main() {
	v := config.MustLoad("configs", "config")
	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "review-service.log")
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

	rabbitURL := v.GetString("rabbitmq.url")

	// 同步链路：组合仓储（主表 + 章节）+ 任务生产者 + 复盘服务。
	reviewStore := repository.NewReviewStore(pool)
	producer, err := service.NewReviewTaskProducer(rabbitURL)
	if err != nil {
		log.Fatalf("初始化 RabbitMQ 生产者失败: %v", err)
	}
	defer producer.Close()

	reviewSvc := service.NewReviewService(reviewStore, producer, reviewStore)
	reviewHandler := handler.NewReviewHandler(reviewSvc)

	// 异步链路：ai-service gRPC 客户端 + 方法论生成器（method-v3 六章，
	// templates 传 nil 时生成器内部也会用静态注册表兜底）。
	// T4-16：gRPC 调用超时可配置（默认 310s，略大于模型侧 300s）。
	callTimeout := parseDurationOrFatal(v.GetString("ai.call_timeout"), "ai.call_timeout")
	aiClient, err := service.NewAIClient(service.AIClientConfig{
		Address:       v.GetString("ai.addr"),
		CallTimeout:   callTimeout,
		InternalToken: v.GetString("internal.token"),
	})
	if err != nil {
		log.Fatalf("初始化 ai-service 客户端失败: %v", err)
	}
	defer aiClient.Close()

	// 第五批拆库：复盘输入与坑点数据不再跨库直查，统一直连 training-service
	// 的 /internal/v1 聚合端点（东西向直连，不经网关）。
	// training_service.base_url 未配置时不阻断启动：复盘主输入取数失败由 worker
	// 标记任务失败，坑点章节采集失败只跳过该章（非关键路径）。
	trBaseURL := v.GetString("training_service.base_url")
	internalToken := v.GetString("internal.token")
	snapshots := repository.NewHTTPReviewSnapshotRepo(trBaseURL, internalToken)
	growthJobs := repository.NewGrowthJobRepo(pool)
	reviewSvc.WithReviewEligibility(snapshots).WithGrowthSnapshots(snapshots).WithGrowthProgress(growthJobs)
	growthHandler := handler.NewGrowthReviewHandler(service.NewGrowthReportService(reviewSvc, growthJobs, snapshots))

	pitfallCollector := service.NewPitfallSectionBuilder(
		repository.NewHTTPPitfallRepo(trBaseURL, internalToken))
	changeCollector := service.NewChangeSectionBuilder(
		repository.NewHTTPChangeRepo(trBaseURL, internalToken))

	// T4-16：每个消费 goroutine 一个独立生成器实例——MethodReviewGenerator
	// 内部缓存最近一次六章结果（七返回值签名锁定的变通端口），共享会串章节；
	// gRPC 连接/输入仓储/模板提供器均并发安全，由闭包共享。
	inputRepo := repository.NewHTTPReviewInputRepo(trBaseURL, internalToken)
	templates := service.NewStaticProvider()
	generatorFactory := func() service.ReviewGenerator {
		return service.NewMethodReviewGenerator(inputRepo, aiClient, templates)
	}

	workerCfg := service.WorkerConfig{
		Concurrency: v.GetInt("worker.concurrency"),
		BackoffBase: parseDurationOrFatal(v.GetString("worker.backoff_base"), "worker.backoff_base"),
		TaskTimeout: parseDurationOrFatal(v.GetString("worker.task_timeout"), "worker.task_timeout"),
	}
	reviewWorker, err := service.NewReviewWorker(rabbitURL, workerCfg, reviewStore, generatorFactory, logger.L(), pitfallCollector, changeCollector)
	if err != nil {
		log.Fatalf("初始化复盘 worker 失败: %v", err)
	}
	growthPipeline := service.NewGrowthPipeline(growthJobs, snapshots, aiClient).WithFinalizer(service.NewGrowthFinalizer(snapshots, reviewStore))
	reviewWorker.WithGrowthPipeline(growthPipeline)
	go service.NewGrowthDispatcher(growthJobs, producer).Run(ctx, func(err error) { log.Printf("成长复盘调度暂未完成: %v", err) })
	defer reviewWorker.Close()

	go func() {
		if err := reviewWorker.Start(ctx); err != nil {
			log.Fatalf("复盘 worker 异常退出: %v", err)
		}
	}()

	// 埋点分析（T4-13/T4-14，原 analytics-service 冻结后并入）：
	// 生产落 events 表（000004 迁移），重启不丢数据、支持多实例。
	// 路由直接挂在同一 gin.Engine 上。
	analyticsSvc := service.NewAnalyticsService(repository.NewPGEventRepo(pool))
	analyticsHandler := handler.NewAnalyticsHandler(analyticsSvc)
	internalAnalyticsHandler := handler.NewInternalAnalyticsHandler(analyticsSvc, internalToken)

	// 第四批拆库：training-service 对 reviews 的协作子分读写改走本服务内部端点。
	// generate 一并挂内部路由：弃班等无浏览器在场的终态由训练服务带 user_id 触发。
	internalReviewSvc := service.NewInternalReviewService(repository.NewInternalReviewRepo(pool))
	internalReviewHandler := handler.NewInternalReviewHandler(
		internalReviewSvc, reviewSvc, v.GetString("internal.token"))

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// 业务路由：uid 由网关注入的 X-User-Id 头解析（本地裸调需自行带头）。
	g := r.Group("/api/v1/reviews", handler.UserID())
	{
		g.POST("/generate", reviewHandler.Generate)
		// 静态路由与 /:id 参数路由并存，Gin 静态优先匹配（T4-16 历史列表）。
		g.GET("/history", reviewHandler.ListHistory)
		g.GET("/session/:session_id", reviewHandler.GetBySession)
		g.GET("/:id", reviewHandler.GetByID)
		g.GET("/:id/sections", reviewHandler.GetSections)
		g.GET("/:id/source", growthHandler.Source)
		g.GET("/:id/progress", growthHandler.Progress)
	}

	// 埋点采集 + 漏斗 + 看板 + AI 行为统计（13 个端点，路径保持不变）。
	analyticsHandler.RegisterRoutes(r)

	// 服务间复盘协作数据（东西向直连，不经网关）。
	internalReviewHandler.RegisterRoutes(r)

	// 服务端埋点上报（东西向直连，不经网关）。
	internalAnalyticsHandler.RegisterRoutes(r)

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		log.Printf("review-service 启动在 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// parseDurationOrFatal 解析时长配置项，格式非法时直接终止启动并指出配置键，
// 避免带着零值超时运行（T4-16）。
func parseDurationOrFatal(s, key string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Fatalf("解析 %s 失败（应为 Go duration，如 300s/5m）: %v", key, err)
	}
	return d
}
