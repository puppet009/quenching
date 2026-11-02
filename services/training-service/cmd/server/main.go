// training-service 入口（T2-27 起承接 AI 对话编排）。
// 装配：PG 仓储 + ai-service gRPC 流式客户端 + 对话编排 + SSE 接口。
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/adminstats"
	"github.com/quenching/go-common/analytics"
	"github.com/quenching/go-common/config"
	"github.com/quenching/go-common/database"
	"github.com/quenching/go-common/logger"

	"github.com/quenching/training-service/internal/handler"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/pkg/eventbus"
	"github.com/quenching/training-service/internal/pkg/logquery"
	"github.com/quenching/training-service/internal/pkg/metrics"
	"github.com/quenching/training-service/internal/repository"
	"github.com/quenching/training-service/internal/service"
)

func main() {
	v := config.MustLoad("configs", "config")
	gin.SetMode(v.GetString("server.mode"))
	// log.dir 为空时仅 stdout（本地开发）；云端挂载 PVC 后日志同步落盘轮转。
	logger.InitWithFile(v.GetString("log.level"), v.GetString("log.dir"), "training-service.log")

	pool, err := database.NewPG(context.Background(), database.PGConfig{
		DSN:      v.GetString("db.dsn"),
		MaxConns: 20,
	})
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	aiClient, err := service.NewAIClient(service.AIClientConfig{Address: v.GetString("ai.addr"), InternalToken: v.GetString("internal.token")})
	if err != nil {
		log.Fatalf("初始化 ai-service 客户端失败: %v", err)
	}
	defer aiClient.Close()

	chatSvc := service.NewChatService(
		repository.NewConversationRepo(pool),
		service.NewAIChatStreamer(aiClient),
		v.GetString("ai.model"),
		v.GetString("ai.prompt_version"),
	)
	memoryStore := repository.NewSessionMemoryRepo(pool)
	chatSvc.WithSessionMemory(memoryStore)
	chatSvc.EnableSessionMemoryContext(service.NewSessionMemoryService(memoryStore, aiClient))
	operationStore := repository.NewOperationStore(pool)
	toolJournal := repository.NewToolJournal(pool)
	chatSvc.EnableSessionHistoryTools(service.NewSessionHistoryTools(memoryStore).WithOperations(operationStore))
	chatSvc.EnableToolAudit(&service.ToolAudit{Store: toolJournal})
	// T3-9：装配规则版坑点引擎（nil 用默认 math/rand 概率源）。
	chatSvc.EnablePitfall(repository.NewPitfallRuntime(pool, repository.NewContentPitfallReader(v.GetString("content_service.base_url"), v.GetString("internal.token"))), nil)
	// T3-12：装配踩坑后果传导登记仓储 + fault-engine 运行时执行通道。
	// base_url 留空时执行通道传 nil：次生故障只登记不注入、会话结束钩子仅做本地清理，
	// 与 Prometheus/Loki 客户端一致的懒连接风格。
	var consequenceExec service.ConsequenceExecutor
	var faultStopper service.FaultSessionStopper
	// 拆库第一批：defects 跨域只读统一走 fault-engine 内部端点（/internal/v1/defects）。
	// base_url 留空时传 nil：各仓储均有 nil 守卫，名称回填保守留空、卡顿检测弃权，不阻断主链路。
	var defectReader service.DefectReader
	// 第五批：剧本详情（含 root_cause）只读端口，仅复盘聚合仓储使用。
	var defectScriptReader service.DefectScriptReader
	if feBaseURL := v.GetString("fault_engine.base_url"); feBaseURL != "" {
		feClient := service.NewFaultEngineHTTPClient(service.FaultEngineClientConfig{
			Token:            v.GetString("internal.token"),
			BaseURL:          feBaseURL,
			DefaultNamespace: v.GetString("fault_engine.default_namespace"),
			DefaultPod:       v.GetString("fault_engine.default_pod"),
		})
		consequenceExec = feClient
		faultStopper = feClient
		defectReader = service.NewFaultDefectHTTPClient(service.FaultDefectClientConfig{
			BaseURL: feBaseURL,
			Token:   v.GetString("internal.token"),
		})
		// 第五批：含 root_cause 的剧本详情通道，仅供复盘聚合端点服务端回填。
		defectScriptReader = service.NewFaultScriptHTTPClient(service.FaultDefectClientConfig{
			BaseURL: feBaseURL,
			Token:   v.GetString("internal.token"),
		})
	} else {
		log.Printf("警告: fault_engine.base_url 未配置，次生故障注入与剧本编排通道不可用")
	}

	// 拆库第二批：projects / project_services 跨域只读统一走 content-service
	// 内部端点（/internal/v1/projects 等）。同样懒连接：base_url 留空传 nil，
	// 仓储 nil 守卫下项目名回填留空、卡顿服务名解析弃权，不阻断主链路。
	var contentReader service.ContentReader
	if csBaseURL := v.GetString("content_service.base_url"); csBaseURL != "" {
		contentReader = service.NewContentHTTPClient(service.ContentClientConfig{
			BaseURL: csBaseURL,
			Token:   v.GetString("internal.token"),
		})
	} else {
		log.Printf("警告: content_service.base_url 未配置，项目/服务名跨域回填不可用")
	}

	// 拆库第三批：user_profiles 协作档案读写统一走 user-service 内部端点。
	// base_url 留空时传 nil：评分/雷达详情可降级；交叉分组缺少持久化端口，
	// 开训编排会返回错误，避免静默跳过分组。
	var userProfiles service.UserProfileStore
	if usBaseURL := v.GetString("user_service.base_url"); usBaseURL != "" {
		userProfiles = service.NewUserProfileHTTPClient(service.UserProfileClientConfig{
			BaseURL: usBaseURL,
			Token:   v.GetString("internal.token"),
		})
	} else {
		log.Printf("警告: user_service.base_url 未配置，协作档案回填与交叉分组不可用")
	}
	// 第四批：reviews 协作子分读写改走 review-service 内部 HTTP（懒连接，留空时静默降级）。
	var reviews service.ReviewStore
	// reviewTrigger 与 reviews 是同一实例，只是单独暴露"触发复盘生成"端口：
	// 弃班班次没有浏览器在场，只能由服务端带着会话归属人身份去触发。
	var reviewTrigger service.ReviewTriggerWriter
	if rvBaseURL := v.GetString("review_service.base_url"); rvBaseURL != "" {
		rc := service.NewReviewHTTPClient(service.ReviewClientConfig{
			BaseURL: rvBaseURL,
			Token:   v.GetString("internal.token"),
		})
		reviews = rc
		reviewTrigger = rc
	} else {
		log.Printf("警告: review_service.base_url 未配置，复盘子分回填与回写不可用")
	}
	// 第五批：复盘聚合内部端点（review-service 取复盘主输入与坑点原始数据）。
	reviewInputSvc := service.NewInternalReviewInputService(
		repository.NewPGInternalReviewInputRepo(pool, defectScriptReader).WithPitfalls(repository.NewContentPitfallReader(v.GetString("content_service.base_url"), v.GetString("internal.token"))))
	reviewInputHandler := handler.NewInternalReviewInputHandler(
		reviewInputSvc, v.GetString("internal.token"))
	chatSvc.EnableConsequence(repository.NewConsequenceRepo(pool), consequenceExec)
	// T3-4：装配脱敏故障上下文供给（本地 session_defects + fault-engine 内部端点回填，
	// 端点 SELECT 白名单不含 root_cause 等答案列），助教应答可结合当前故障现象引导思考。
	chatSvc.EnableFaultContext(repository.NewFaultContextRepo(pool, defectReader))
	// T3-5：装配渐进式提示配额供给（按会话从 ai_messages.is_hint 实时计数，
	// 难度取自 training_sessions，medium 历史值归一为 normal）。
	// 同一实例同时供给结束评分的提示次数（技术雷达信息检索维）。
	hintQuotaRepo := repository.NewHintQuotaRepo(pool)
	chatSvc.EnableHints(hintQuotaRepo)

	// T3-24：协作指数汇总服务（事件采集 + ai-service 远程评估 + user-service 回写）。
	projectionReady := repository.LegacyProjectionReadiness(v.GetString("sandbox_service.base_url"), v.GetString("internal.token"))
	scoreSvc := service.NewScoreService(
		repository.NewScoreRepo(pool, userProfiles, reviews).WithOperationReadiness(projectionReady), aiClient, nil,
	)
	scoreSvc.SetHintUsageReader(hintQuotaRepo)
	// 结束评分最小闭环：难度参考时长允许配置覆盖（秒）；缺省/配 0 的难度回落代码默认。
	if expected := v.GetStringMapString("scoring.expected_duration_sec"); len(expected) > 0 {
		m := make(map[string]int, len(expected))
		for diff, raw := range expected {
			if secs, perr := strconv.Atoi(raw); perr == nil && secs > 0 {
				m[diff] = secs
			} else {
				log.Printf("警告: scoring.expected_duration_sec.%s=%q 非法，已忽略", diff, raw)
			}
		}
		if len(m) > 0 {
			scoreSvc.SetExpectedSecByDifficulty(m)
		}
	}

	// 个人中心 / 人机对照：交叉分组编排 + 会话生命周期服务。
	// 用户分组和故障目录走内部 HTTP 端口，会话配对仍使用本服务仓储；
	// 随机源用普通 math/rand；评分端口接 scoreSvc（结束后正常完成才计分，失败只降级）。
	var groupStore service.CollabGroupStore
	if userProfiles != nil {
		groupStore = userProfiles.(service.CollabGroupStore)
	}
	crossPlanner := service.NewCrossGroupPlanner(
		groupStore,
		repository.NewSessionPairRepo(pool),
		repository.NewDefectCatalogRepo(pool, defectReader),
		service.NewPlannerRNG(),
	)
	sessionRepo := repository.NewSessionRepo(pool, defectReader, contentReader, userProfiles, reviews).WithOperationWriter(repository.NewSandboxOperationWriter(v.GetString("sandbox_service.base_url"), v.GetString("internal.token")))
	sessionSvc := service.NewSessionService(
		sessionRepo,
		crossPlanner,
		scoreSvc,
	)
	// 人机对照 AI 总结：ai-service 流式生成四段式点评，setter 注入保持构造函数稳定。
	// 复用同一 gRPC 客户端；ai-service 故障时前端降级使用规则文案。
	sessionSvc.SetComparisonSummaryStreamer(service.NewAIComparisonSummaryStreamer(aiClient))
	// 服务端埋点：训练完成上报 first_complete（review-service 侧同步派生 retrain）。
	// base_url 为空时客户端禁用，实训主链路不受影响。
	if rvBase := v.GetString("review_service.base_url"); rvBase != "" {
		sessionSvc.SetAnalytics(analytics.New(analytics.Config{
			BaseURL: rvBase,
			Token:   v.GetString("internal.token"),
		}))
	}
	// 班次活跃度回写沙箱侧：操作上报按 30s 节流续租，结束/弃班时置结束时刻或立即回收。
	// 未配置 base_url 时保持未接线，实训主链路不受影响。
	if sbBaseURL := v.GetString("sandbox_service.base_url"); sbBaseURL != "" {
		lifecycle := service.NewSandboxLifecycleClient(sbBaseURL, v.GetString("internal.token"))
		sessionSvc.SetSandboxLifecycle(lifecycle)
		sessionSvc.SetSandboxReadiness(lifecycle.ReadyForUser)
	} else {
		log.Printf("提示: sandbox_service.base_url 未配置，沙箱活跃度续租与结束回收不回写")
	}

	// T3-18：坑点配置后台管理服务。

	// ——— 云原生可观测装配（T1-15/17/20/22/23/29）———
	// Prometheus/Loki/ws-gateway 客户端均为懒连接：本地中间件缺失时不阻断启动，
	// 仅在相关接口被调用时返回查询错误；事件发布器缺失时推送静默降级。
	promURL := v.GetString("observability.prometheus_url")
	if promURL == "" {
		log.Printf("警告: observability.prometheus_url 未配置，指标相关接口将不可用")
	}
	lokiURL := v.GetString("observability.loki_url")
	if lokiURL == "" {
		log.Printf("警告: observability.loki_url 未配置，日志相关接口将不可用")
	}
	metricsSvc := metrics.NewService(
		metrics.NewHTTPPromClient(promURL),
		v.GetString("observability.namespace"),
		v.GetString("observability.release_name"),
	)
	lokiClient := logquery.NewHTTPLokiClient(lokiURL).
		WithOrgID(v.GetString("observability.loki_org_id"))
	logQuerySvc := logquery.NewService(lokiClient)
	// T4-16：单次查询返回条数硬顶可配，防止前端一次拉取过多拖垮 Loki。
	logQuerySvc.SetMaxLimit(v.GetInt("observability.log_max_limit"))

	// ws-gateway 事件发布：云上配置地址后启用，本地传 nil（三处消费方均有 nil 守卫）。
	var eventPublisher eventbus.Publisher
	if gwURL := v.GetString("observability.ws_gateway_url"); gwURL != "" {
		eventPublisher = eventbus.NewHTTPPublisher(gwURL, v.GetString("observability.ws_gateway_api_key"))
	} else {
		log.Printf("警告: observability.ws_gateway_url 未配置，实时推送将静默降级")
	}
	channelPub := &channelPublisher{inner: eventPublisher}

	// 告警管理器：复合评估器（指标阈值/趋势 + 日志关键词），本地 publisher 传 nil。
	alertMgr := service.NewAlertManager(
		service.NewAlertCompositeEvaluator(&alertMetricsAdapter{svc: metricsSvc}, &alertLogsAdapter{svc: logQuerySvc}),
		eventPublisher,
	)

	// Agent 观察类工具（M2）：日志 / 指标快照 / 指标趋势 / 活跃告警，全部只读自动执行。
	// M3：若配置了 sandbox_service.base_url，再注册服务拓扑/文件浏览/只读诊断工具。
	var sandboxReadonly service.SandboxReadonly
	if sbBaseURL := v.GetString("sandbox_service.base_url"); sbBaseURL != "" {
		sandboxReadonly = service.NewSandboxReadonlyClient(sbBaseURL, v.GetString("internal.token"))
	} else {
		log.Printf("提示: sandbox_service.base_url 未配置，服务/文件/诊断类观察工具不启用")
	}
	// M5：助手 write_file 修改登记仓储（写操作成功后落库前后快照）。
	changeRecorder := repository.NewPGChangeRecordRepo(pool)
	chatSvc.EnableTools(RegisterAgentTools(
		logQuerySvc, metricsSvc, alertMgr, sandboxReadonly, changeRecorder,
	))

	// 健康卡片 / 日志观察服务。
	healthCardSvc := service.NewHealthCardService(
		&healthMetricsAdapter{svc: metricsSvc},
		&healthAlertsAdapter{mgr: alertMgr},
		channelPub,
	)
	logViewerSvc := service.NewLogViewerService(&viewerLogsAdapter{svc: logQuerySvc}, channelPub)
	// T4-16：实时流并发上限、轮询间隔下限、导出条数上限均可配。
	// 配置以整数毫秒给出（viper GetDuration 会把裸整数当纳秒，故显式换算）。
	logViewerSvc.SetMaxStreams(v.GetInt("logviewer.max_streams"))
	logViewerSvc.SetMinInterval(time.Duration(v.GetInt("logviewer.min_interval_ms")) * time.Millisecond)
	logViewerSvc.SetExportLimit(v.GetInt("logviewer.export_limit"))

	// ——— 零/仅 DB 依赖的本地服务 ———
	difficultySvc := service.NewDifficultyService()
	scoringSvc := service.NewScoringService()
	abStatsSvc := service.NewABStatsService(repository.NewABStatsRepo(pool))

	// 恢复验证服务：注册 4 个内置检查器（探活检查器待云上沙箱网络可达后再装配）。
	// 待核验的业务服务名随模板而定，由 recovery.services 配置（为空时指标/健康检查器自动弃权）。
	recoverySvc := service.NewRecoveryService()
	recoveryServices := v.GetStringSlice("recovery.services")
	recoverySvc.RegisterChecker(service.NewMetricsChecker(&recoveryMetricsAdapter{svc: metricsSvc}, recoveryServices))
	recoverySvc.RegisterChecker(service.NewAlertChecker(&recoveryAlertsAdapter{mgr: alertMgr}))
	recoverySvc.RegisterChecker(service.NewLogChecker(&recoveryLogsAdapter{svc: logQuerySvc}))
	recoverySvc.RegisterChecker(service.NewHealthChecker(&recoveryMetricsAdapter{svc: metricsSvc}, recoveryServices))

	// ——— T3-6：训练会话卡顿检测（纯规则、内存态，不推送不频控）———
	// 时长项一律以整数毫秒配置后显式换算（viper GetDuration 会把裸整数当纳秒）；
	// 缺省/配 0 时由 model.StallConfig.Normalize 兜底为内置默认值。
	stallDetector := service.NewStallDetector(model.StallConfig{
		IdleTimeout:        time.Duration(v.GetInt("stall.idle_timeout_ms")) * time.Millisecond,
		AbandonTimeout:     time.Duration(v.GetInt("stall.abandon_timeout_ms")) * time.Millisecond,
		ProgressWindow:     time.Duration(v.GetInt("stall.progress_window_ms")) * time.Millisecond,
		MinActivities:      v.GetInt("stall.min_activities"),
		MinSegmentPoints:   v.GetInt("stall.min_segment_points"),
		MinErrorRateDrop:   v.GetFloat64("stall.min_error_rate_drop"),
		MinP99Drop:         v.GetFloat64("stall.min_p99_drop"),
		MinQPSRelativeGain: v.GetFloat64("stall.min_qps_relative_gain"),
		MinQPSAbsoluteGain: v.GetFloat64("stall.min_qps_absolute_gain"),
	})
	stallMonitor := service.NewStallMonitor(
		stallDetector,
		repository.NewStallRepo(pool, metricsSvc, defectReader, contentReader),
		time.Duration(v.GetInt("stall.scan_interval_ms"))*time.Millisecond,
	)
	// ——— T3-7：卡顿主动线索推送 ———
	// 注册为 OnStall 钩子：normal→stalled 翻转时异步推送一条提问式线索，
	// 同卡顿周期同原因以落库事实去重；ws-gateway 未配置时仅落库不发事件。
	cluePusher := service.NewCluePusher(
		repository.NewClueRepo(pool),
		eventPublisher,
		model.ClueConfig{
			Enabled:        v.GetBool("clue.enabled"),
			PersistTimeout: time.Duration(v.GetInt("clue.persist_timeout_ms")) * time.Millisecond,
			PublishTimeout: time.Duration(v.GetInt("clue.publish_timeout_ms")) * time.Millisecond,
		},
	)
	stallMonitor.OnStall(cluePusher.OnStall)
	// R3 弃班：连续空闲超过 stall.abandon_timeout_ms（默认 20 分钟）→ 班次按未完成结束、
	// 立即回收沙箱（全局只有 2 个并发槽位）、以会话归属人身份触发复盘生成。
	// 每会话成功回调一次，失败下一轮重试（见 StallMonitor.checkAbandon）。
	stallMonitor.OnAbandon((&abandonAdapter{
		sessions: sessionSvc,
		reviews:  reviewTrigger,
	}).OnAbandon)

	r := gin.New()
	r.Use(logger.Recovery(), logger.Trace(), logger.Access())
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	// AI 对话 / 协作评分 / 坑点后台
	handler.RegisterOperationProjection(r, repository.NewOperationProjectionRepo(pool), v.GetString("internal.token"))
	handler.OperationEvidenceHandler{Store: operationStore, Owned: repository.OperationOwner(pool), Ingest: operationStore.Ingest, Token: v.GetString("internal.token")}.Register(r)
	handler.NewConversationHandler(chatSvc).RegisterRoutes(r)
	handler.NewScoreHandler(scoreSvc).RegisterRoutes(r)
	handler.RegisterContentCompatibility(r, v.GetString("content_service.base_url"))
	handler.NewFeedbackHandler(repository.NewFeedbackRepo(pool)).RegisterRoutes(r)
	r.GET("/api/admin/v1/stats", adminstats.Handler(v.GetString("internal.token"), func(ctx context.Context) (map[string]interface{}, error) { return repository.ReadAdminStats(ctx, pool) }))
	// 个人中心 / 人机对照：会话历史、概览、对照、创建/开始/结束/操作上报。
	handler.NewSessionHandler(sessionSvc).RegisterRoutes(r)
	// 本地计算服务
	handler.NewDifficultyHandler(difficultySvc).RegisterRoutes(r)
	handler.NewScoringHandler(scoringSvc).RegisterRoutes(r)
	handler.NewABStatsHandler(abStatsSvc).RegisterRoutes(r)
	handler.NewRecoveryHandler(recoverySvc).RegisterRoutes(r)
	// 云原生可观测服务
	handler.NewMetricsHandler(metricsSvc).RegisterRoutes(r)
	handler.NewLogHandler(logQuerySvc).RegisterRoutes(r)
	handler.NewLogViewerHandler(logViewerSvc).RegisterRoutes(r)
	handler.NewAlertHandler(alertMgr).RegisterRoutes(r)
	handler.NewHealthCardHandler(healthCardSvc).RegisterRoutes(r)
	// T3-6：卡顿状态只读查询（单会话 / 全量列表）。
	handler.NewStallHandler(stallMonitor).RegisterRoutes(r)
	// T3-12：会话结束钩子（云端沙箱释放流程回调）：撤次生故障 + 停剧本编排。
	// T3-24 最小闭环：撤销全部成功后尽力触发结束评分（仅正常完成计分，失败只记日志）。
	handler.NewRuntimeHandler(chatSvc, faultStopper).
		SetSessionScorer(scoreSvc).
		RegisterRoutes(r)
	// 第五批：服务间复盘聚合路由 /internal/v1/sessions（不经网关，不对前端暴露）。
	reviewInputHandler.RegisterRoutes(r)
	handler.NewSessionReviewSnapshotHandler(service.NewSessionReviewSnapshotService(repository.NewSessionReviewSnapshotRepo(pool, repository.NewPGInternalReviewInputRepo(pool, defectScriptReader).WithPitfalls(repository.NewContentPitfallReader(v.GetString("content_service.base_url"), v.GetString("internal.token"))))), v.GetString("internal.token")).RegisterRoutes(r)
	// 第三批：sandbox-service 前端信号转发 / 默认规则注册（X-Internal-API-Key）。
	handler.NewInternalAlertEventsHandler(alertMgr, v.GetString("internal.token")).RegisterRoutes(r)
	brainRepo := repository.NewBrainRepo(pool)
	reviewInputSvc.SetBrainInput(brainRepo)
	handler.NewBrainStatusHandler(brainRepo).RegisterRoutes(r)
	brainEvidence := service.NewBrainEvidenceService(brainRepo)
	brainDomains := service.NewBrainDomainClients(v.GetString("sandbox_service.base_url"), v.GetString("fault_engine.base_url"), v.GetString("internal.token"), brainRepo)
	brainTools := service.NewBrainToolExecutor(brainRepo)
	abilitySvc := service.NewAbilityAssessmentService(brainRepo, aiClient)
	brainTools.Register("update_ability_assessment", func(ctx context.Context, c model.BrainCommand, _ *service.BrainToolParameters) (model.BrainToolResult, error) {
		assessment, e := abilitySvc.Assess(ctx, c.SessionID)
		if e != nil {
			return model.BrainToolResult{}, e
		}
		raw, e := json.Marshal(assessment)
		return model.BrainToolResult{CommandID: c.CommandID, Status: "succeeded", ResultJSON: string(raw)}, e
	})
	chatSvc.EnableBrainContext(service.NewStudentBrainContextService(brainRepo), brainRepo)
	service.RegisterBrainRuntimeTools(brainTools, brainRepo, brainRepo, brainEvidence, brainDomains)
	brainDispatcher := service.NewBrainDispatcher(brainRepo, brainTools, brainDomains)
	brainDirector := service.NewBrainDirector(brainRepo, aiClient, brainDispatcher, service.BuildObservation(brainRepo))
	brainDispatcher.SetDirector(brainDirector)
	sandboxLifecycle := service.NewSandboxLifecycleClient(v.GetString("sandbox_service.base_url"), v.GetString("internal.token"))
	operationSeals := &service.OperationSealService{Local: toolJournal, Remote: sandboxLifecycle, ProjectionReady: projectionReady}
	brainCleanup := service.NewBrainCleanupCoordinator(brainRepo, faultStopper, sandboxLifecycle, reviewTrigger).WithOperationSeal(operationSeals).WithSessionScorer(scoreSvc)
	brainDirector.SetCloser(brainCleanup)
	sessionSvc.SetBrainLifecycle(brainRepo)
	handler.NewBrainInternalHandler(service.NewBrainEventIngress(brainRepo, brainEvidence), v.GetString("internal.token")).RegisterRoutes(r)

	srv := &http.Server{Addr: v.GetString("server.addr"), Handler: r}
	go func() {
		log.Printf("training-service 启动在 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 启动告警评估循环（30s 周期）；ctx 取消或 Stop() 均可退出。
	go toolJournal.RunOutbox(ctx, v.GetString("sandbox_service.base_url"), v.GetString("internal.token"))
	go scoreSvc.RunPendingScores(ctx)
	if reviews != nil && reviewTrigger != nil && sandboxLifecycle != nil {
		go service.NewLegacyReviewDispatcher(sessionRepo, reviews, reviewTrigger, sandboxLifecycle, operationSeals).WithSessionScorer(scoreSvc).Run(ctx, func(err error) {
			log.Printf("legacy session closure: %v", err)
		})
	}
	go service.ToolRecovery{Store: toolJournal, Orphans: repository.OrphanedToolOperations(pool)}.Run(ctx)
	go alertMgr.Start(ctx)
	go brainDispatcher.Start(ctx)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ids, err := brainRepo.ClaimClosingSessions(ctx)
				if err != nil {
					log.Printf("brain close scan: %v", err)
					continue
				}
				for _, sid := range ids {
					closeCtx, cancelClose := context.WithTimeout(ctx, 40*time.Second)
					state, e := brainRepo.CleanupState(closeCtx, sid)
					if e == nil {
						_, e = sessionSvc.Finish(closeCtx, sid, state.UserID, service.FinishSessionRequest{Outcome: "completed"})
					}
					if e == nil {
						e = brainDirector.Close(closeCtx, sid)
					}
					if e != nil {
						log.Printf("brain close %d: %v", sid, e)
					}
					if reviews != nil {
						report, re := reviews.GetReviewBySession(closeCtx, sid)
						if re == nil && report != nil {
							_ = brainRepo.RecordReportStatus(closeCtx, sid, report.GenerationStatus)
						}
					}
					cancelClose()
					releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
					_ = brainRepo.ReleaseCleanup(releaseCtx, sid)
					releaseCancel()
				}
			}
		}
	}()
	// T3-6：启动卡顿状态扫描循环（默认 30s 周期，启动即先评估一轮）。
	go stallMonitor.Start(ctx)

	<-ctx.Done()
	log.Printf("收到退出信号，开始优雅关停")
	alertMgr.Stop()
	stallMonitor.Stop()
	// T4-16：回收全部实时日志流（ctx 脱离请求生命周期，必须显式关停）。
	logViewerSvc.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
