package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/quenching/go-common/analytics"
	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	"k8s.io/client-go/kubernetes"
)

// SandboxService 沙箱业务服务。
// 封装创建/释放沙箱的完整业务流程：模板解析 → 资源预检 → 配额校验 → 状态流转 → K8s 操作 → Helm 部署。
// 调谐器(Reconciler)负责后台对齐，本服务负责前台发起的用户操作。
type SandboxService struct {
	allocation *ResourceQueue
	rec        *Reconciler
	quota      *QuotaService
	resource   ResourceChecker
	db         Store
	provision  *ProvisionService
	registry   templates.Registry // 官方模板注册表（chart 路径 / release 名来源）
	// behavior 行为模拟运行时；为 nil 时沙箱就绪后不自动启动行为模拟。
	behavior *BehaviorRuntime
	// behaviorSignalEndpoint 行为信号汇聚端点；随 Start 传入，可为空。
	behaviorSignalEndpoint string
	// alertClient training-service 告警客户端；为 nil 时不转发信号也不注册规则。
	alertClient *AlertClient
	// tracker review-service 服务端埋点；为 nil 时不上报。
	tracker *analytics.Client
}

// NewSandboxService 构造沙箱服务。
// resource / provision 可为 nil（如本地开发无集群环境），此时跳过对应步骤；
// registry 为 nil 时使用空注册表。
func NewSandboxService(rec *Reconciler, quota *QuotaService, resource ResourceChecker, db Store, provision *ProvisionService, registry templates.Registry) *SandboxService {
	if registry == nil {
		registry = templates.EmptyRegistry()
	}
	return &SandboxService{
		rec:       rec,
		quota:     quota,
		resource:  resource,
		db:        db,
		provision: provision,
		registry:  registry,
	}
}

// SetAnalytics 注入服务端埋点客户端（nil=不上报）。
func (s *SandboxService) SetAnalytics(c *analytics.Client) {
	s.tracker = c
}

// SetBehaviorRuntime 注入行为模拟运行时与信号汇聚端点。
// 注入后沙箱进入 ready 会异步启动行为模拟；启动失败只记录日志，
// 不影响沙箱本身的创建结果。
func (s *SandboxService) SetBehaviorRuntime(rt *BehaviorRuntime, signalEndpoint string) {
	s.behavior = rt
	s.behaviorSignalEndpoint = signalEndpoint
}

// SetAlertClient 注入 training-service 告警客户端（nil 表示不启用转发/注册）。
func (s *SandboxService) SetAlertClient(c *AlertClient) {
	s.alertClient = c
}

// NewFromK8sClient 用 K8s clientset 便捷构造（注入默认部署器和等待器）。
// provision 为 nil 时不做 Helm 部署（仅创建 namespace 等隔离资源）。
func NewFromK8sClient(rec *Reconciler, quota *QuotaService, resource ResourceChecker, db Store, cs kubernetes.Interface, withHelm bool, registry templates.Registry) *SandboxService {
	var provision *ProvisionService
	if withHelm {
		helm := NewHelmCLI()
		waiter := NewReadyWaiter(cs)
		provision = NewProvisionService(helm, waiter)
	}
	return NewSandboxService(rec, quota, resource, db, provision, registry)
}

// CreateSandbox 创建沙箱的完整流程。
//  1. 集群资源预检 —— 资源不足直接友好报错
//  2. 配额校验（用户并发数等）—— 超限直接拒绝
//  3. 创建 DB 记录（状态 creating）
//  4. 确保 K8s 隔离资源存在（ns + quota + networkpolicy）
//  5. 状态推进到 deploying
//  6. 部署 Helm Chart（可选，provision 非 nil 时执行）
//  7. 等待就绪 → 状态推进到 ready
//  8. 失败回滚 → 标记 failed
//
// 成功返回沙箱记录（状态为 ready 或 deploying，取决于是否启用 provision）。
//
// templateSlug 指定要部署的官方模板：仅当 provision 非 nil 且 slug 非空时部署 Chart；
// slug 为空表示创建不预置业务系统的裸沙箱（隔离资源就绪后直接 ready）。
// 指定了 slug 但注册表中不存在（或模板已停用）直接返回 ErrSandboxTemplateNotFound。
func (s *SandboxService) CreateSandbox(ctx context.Context, userID, projectID, sessionID int64, templateSlug string) (*model.Sandbox, error) {
	// 0) 模板解析：指定 slug 时必须能在注册表中找到（已停用模板同样视为不存在）。
	manifest, hasTemplate := templates.Manifest{}, false
	if templateSlug != "" {
		manifest, hasTemplate = s.registry.Get(templateSlug)
		if !hasTemplate {
			return nil, ErrSandboxTemplateNotFound
		}
	}
	if projectID <= 0 && templateSlug == "" {
		return nil, ErrSandboxProjectRequired
	}

	if s.allocation != nil && hasTemplate {
		return s.enqueue(ctx, userID, projectID, sessionID, manifest)
	}
	// 1) 集群资源预检 —— 资源不足直接拒绝
	if s.resource != nil {
		ok, err := s.resource.HasEnoughResource(ctx)
		if err != nil {
			// 资源检查失败不阻塞创建（可能是 API 临时波动）
			// 真正的调度由 kube-scheduler 兜底
			_ = err
		} else if !ok {
			return nil, ErrSandboxInsufficientResource
		}
	}

	// 2) 用户配额校验 —— 超限直接拦截
	if err := s.quota.CheckCreateQuota(ctx, userID); err != nil {
		return nil, err
	}

	// 3) 构造沙箱记录
	sb := &model.Sandbox{
		SessionID: sessionID,
		UserID:    userID,
		ProjectID: projectID,
		Status:    model.StatusCreating,
		Namespace: model.NamespaceName(sessionID),
	}

	// 4) 落库
	if err := s.db.Create(ctx, sb); err != nil {
		return nil, fmt.Errorf("创建沙箱记录失败: %w", err)
	}

	// 5) 确保 K8s 隔离资源（ns + quota + networkpolicy）
	if _, err := s.rec.EnsureSandbox(ctx, sb); err != nil {
		_ = s.db.MarkStatus(ctx, sessionID, model.StatusFailed)
		return nil, fmt.Errorf("创建沙箱资源失败: %w", err)
	}

	// 6) 状态推进到 deploying
	if err := s.db.MarkStatus(ctx, sessionID, model.StatusDeploying); err != nil {
		return nil, fmt.Errorf("更新沙箱状态失败: %w", err)
	}
	sb.Status = model.StatusDeploying

	// 7) 部署 Helm Chart + 等待就绪（可选）
	switch {
	case s.provision == nil:
		// 本地降级：不部署，停留在 deploying（与无集群环境的本地联调一致）。
	case !hasTemplate:
		// 裸沙箱：无模板，隔离资源已就绪，直接进入 ready。
		if err := s.markReady(ctx, sb, nil); err != nil {
			return nil, err
		}
	default:
		if err := s.deployChart(ctx, sb, manifest); err != nil {
			// 部署失败 → 标记 failed（回滚在 provision 内部已做）
			_ = s.db.MarkStatus(ctx, sessionID, model.StatusFailed)
			return nil, err
		}
		// 部署成功 → 标记 ready
		if err := s.markReady(ctx, sb, &manifest); err != nil {
			return nil, err
		}
	}

	return sb, nil
}

// markReady 把沙箱状态推进到 ready，并在装配了行为模拟运行时异步启动。
// manifest 为 nil（裸沙箱）或未声明 behavior_flow 时不启动行为模拟。
// 行为模拟启动脱离请求上下文：HTTP 请求返回后仍继续执行，且失败只记日志。
func (s *SandboxService) markReady(ctx context.Context, sb *model.Sandbox, manifest *templates.Manifest) error {
	if err := s.db.MarkStatus(ctx, sb.SessionID, model.StatusReady); err != nil {
		return fmt.Errorf("更新沙箱状态为 ready 失败: %w", err)
	}
	sb.Status = model.StatusReady
	// 沙箱首次就绪即漏斗第二步；重复就绪由 review-service 的唯一事件去重。
	s.tracker.Track(ctx, analytics.Event{
		EventType: "first_start",
		UserID:    sb.UserID,
		SessionID: sb.SessionID,
		ProjectID: sb.ProjectID,
		Platform:  "server",
	})
	s.startBehavior(sb, manifest)
	s.registerDefaultAlerts(sb)
	return nil
}

// registerDefaultAlerts 异步向 training-service 注册沙箱默认告警规则。
// 未配置 training_service.base_url 时直接跳过；失败只记日志，不影响沙箱就绪。
func (s *SandboxService) registerDefaultAlerts(sb *model.Sandbox) {
	if !s.alertClient.Enabled() {
		return
	}
	go func(sessionID int64) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.alertClient.Register(ctx, sessionID); err != nil {
			log.Printf("[alert] 会话 %d 默认告警规则注册失败（不影响沙箱）: %v", sessionID, err)
		}
	}(sb.SessionID)
}

// startBehavior 异步启动行为模拟。
// 模板未声明 behavior_flow 时直接跳过：不启动 runner，也不产生任何信号；
// fake 降级环境由运行时层统一处理为 degraded，同样不产生假信号。
func (s *SandboxService) startBehavior(sb *model.Sandbox, manifest *templates.Manifest) {
	if s.behavior == nil || manifest == nil || manifest.BehaviorFlow == nil {
		return
	}
	flow, err := BuildBehaviorFlowConfig(sb.Namespace, manifest.BehaviorFlow, manifest.Services)
	if err != nil {
		log.Printf("[behavior] 会话 %d 行为流配置构建失败（不影响沙箱）: %v", sb.SessionID, err)
		return
	}
	go func(sessionID int64, flow *BehaviorFlowConfig) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.behavior.Start(bgCtx, BehaviorStartConfig{
			SessionID:      sessionID,
			EntryURL:       flow.EntryURL,
			SignalEndpoint: s.behaviorSignalEndpoint,
			Flow:           flow,
		}); err != nil {
			log.Printf("[behavior] 会话 %d 行为模拟启动失败（不影响沙箱）: %v", sessionID, err)
		}
	}(sb.SessionID, flow)
}

// BuildBehaviorFlowConfig 把模板声明的 behavior_flow 转换为 runner 运行配置。
// 入口地址按 entry_service 解析为集群内 DNS：
//
//	http://<service>.<namespace>.svc:<port><entry_path>
//
// 模板在注册表加载时已完成校验与缺省填充（validateBehaviorFlow），
// 这里再次防御性处理异常，避免脏清单导致行为模拟静默不启动。
func BuildBehaviorFlowConfig(namespace string, flow *templates.BehaviorFlowSpec, services []templates.ServiceSpec) (*BehaviorFlowConfig, error) {
	if flow == nil {
		return nil, fmt.Errorf("行为流声明为空")
	}

	var svc *templates.ServiceSpec
	for i := range services {
		if services[i].Name == flow.EntryService {
			svc = &services[i]
			break
		}
	}
	if svc == nil {
		return nil, fmt.Errorf("入口服务 %q 未在 services 中声明", flow.EntryService)
	}
	if svc.Port <= 0 {
		return nil, fmt.Errorf("入口服务 %q 未声明有效端口", svc.Name)
	}

	entryPath := flow.EntryPath
	if entryPath == "" {
		entryPath = "/"
	}
	if entryPath[0] != '/' {
		entryPath = "/" + entryPath
	}
	entryURL := fmt.Sprintf("http://%s.%s.svc:%d%s", svc.Name, namespace, svc.Port, entryPath)

	intervalSec := flow.IntervalSeconds
	if intervalSec < 0 {
		intervalSec = 0
	}
	timeoutSec := flow.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	name := flow.Name
	if name == "" {
		name = "default"
	}

	steps := make([]templates.BehaviorStep, len(flow.Steps))
	copy(steps, flow.Steps)

	return &BehaviorFlowConfig{
		Name:       name,
		EntryURL:   entryURL,
		IntervalMS: intervalSec * 1000,
		TimeoutMS:  timeoutSec * 1000,
		Steps:      steps,
	}, nil
}

// deployChart 部署指定模板的 Helm Chart 并等待就绪。
// Helm release 名与模板 slug 一致（slug 已在注册表加载时通过合法性校验）；
// 失败时 provision 内部会自动回滚（卸载 Chart）。
func (s *SandboxService) deployChart(ctx context.Context, sb *model.Sandbox, manifest templates.Manifest) error {
	opts := ProvisionOpts{
		Namespace:   sb.Namespace,
		ReleaseName: manifest.Slug,
		ChartPath:   s.registry.ChartPath(manifest),
		Values:      map[string]string{"global.templateDigest": manifest.ArtifactDigest, "global.priorityClassName": "quenching-training"},
	}

	if err := s.provision.Provision(ctx, opts); err != nil {
		return fmt.Errorf("沙箱环境部署失败: %w", err)
	}
	return nil
}
