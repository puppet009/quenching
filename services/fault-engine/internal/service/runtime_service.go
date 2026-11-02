// Package service 的 runtime_service.go：故障注入运行时服务。
//
// 对上（HTTP handler / training-service）提供四类用例：
//
//	StartScenario  剧本链路：按 defects.amplifier_config 启动一个会话的自动编排（T2-13）
//	StopSession    会话结束：停编排并补偿撤销该会话全部未终结实验（含临时注入）
//	InjectOnce     临时链路：training 踩坑后果传导，立即注入一次（T3-12）
//	RevokeOne      临时链路：按 blade uid 撤销单次注入
//	ListExperiments 查看会话下的注入历史
//
// 设计要点：
//   - 注入一律"先登记后执行"：experiments 先落 created 行，拿到行 ID 再调 blade；
//     成功 MarkRunning，失败 MarkFailed。进程崩溃后可凭 ListActiveBySession 补偿。
//   - 剧本链路的事件（注入/撤销/失败）经 Orchestrator EventHandler 回写仓储；
//     编排器 goroutine 退出后再按终态兜底一次，覆盖 skipped 等无事件回调的分支。
//   - runtime 服务不读 training 库、不关心业务会话语义；目标 Pod 由请求显式携带，
//     缺省回落全局默认命名空间（fault-engine 配置项）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/repository"
	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"
)

// 默认运行时参数。
const (
	defaultStableRequired = 30 * time.Second // 修复判定稳定窗口
)

// autoInjectable 当前可走自动编排/临时注入的故障类型。
// threadpool 为预留类型，blade 命令构造明确不支持，此处前置拦截给出清晰错误。
var autoInjectable = map[model.FaultKind]bool{
	model.KindCPU:      true,
	model.KindMem:      true,
	model.KindDisk:     true,
	model.KindNetDelay: true,
	model.KindNetLoss:  true,
	model.KindProcess:  true,
}

// RuntimeConfig 运行时服务参数。
type RuntimeConfig struct {
	// StableRequired 修复判定稳定窗口，<=0 用默认 30s。
	StableRequired time.Duration
	// PollInterval 指标轮询周期，<=0 用 supervisor 默认 5s。
	PollInterval time.Duration
	// DefaultNamespace 请求未显式给命名空间时的兜底命名空间。
	DefaultNamespace string
}

// RuntimeService 故障注入运行时服务。
type RuntimeService struct {
	resolveTemplate       func(context.Context, string) ([]string, error)
	resolveTemplateTarget func(context.Context, int64, string, string) (string, error)
	inj                   *Injector
	defectRepo            repository.DefectRepository
	expRepo               repository.ExperimentRepository
	registry              *SupervisorRegistry
	fetcher               JudgeMetricFetcher
	cfg                   RuntimeConfig
	rootCtx               context.Context // 编排 goroutine 的父上下文（服务生命周期）
	stopRootCtx           context.CancelFunc
}

// NewRuntimeService 创建运行时服务。rootCtx 随服务生命周期；fetcher 为生产指标源。
func NewRuntimeService(
	rootCtx context.Context,
	exec BladeExecutor,
	defectRepo repository.DefectRepository,
	expRepo repository.ExperimentRepository,
	registry *SupervisorRegistry,
	fetcher JudgeMetricFetcher,
	cfg RuntimeConfig,
) *RuntimeService {
	ctx, cancel := context.WithCancel(rootCtx)
	if cfg.StableRequired <= 0 {
		cfg.StableRequired = defaultStableRequired
	}
	return &RuntimeService{
		inj:         NewInjector(exec),
		defectRepo:  defectRepo,
		expRepo:     expRepo,
		registry:    registry,
		fetcher:     fetcher,
		cfg:         cfg,
		rootCtx:     ctx,
		stopRootCtx: cancel,
	}
}

// Close 释放运行时自身资源（编排器的统一撤销由 registry.StopAll 负责）。
func (s *RuntimeService) Close() {
	s.stopRootCtx()
}

// StartScenarioReq 启动剧本编排请求。
type StartScenarioReq struct {
	TemplateSlug string `json:"template_slug"`
	SessionID    int64  `json:"session_id"` // 训练会话 ID（由 training-service 编排）
	ProjectID    int64  `json:"project_id"` // 剧本所属项目
	Code         string `json:"code"`       // 剧本编码
	Namespace    string `json:"namespace"`  // 目标命名空间，空取全局默认
	Pod          string `json:"pod"`        // 目标 Pod，空取剧本 target.pod
}

// StartScenario 加载剧本 -> 登记实验 -> 装配编排器并启动驱动循环。
func (s *RuntimeService) StartScenario(ctx context.Context, req StartScenarioReq) (*model.Experiment, error) {
	if req.SessionID <= 0 {
		return nil, errors.New("会话ID不能为空")
	}
	if req.ProjectID <= 0 && req.TemplateSlug == "" {
		return nil, errors.New("项目ID不能为空")
	}
	if req.Code == "" {
		return nil, errors.New("剧本编码不能为空")
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = s.cfg.DefaultNamespace
	}
	if req.TemplateSlug != "" && req.Namespace == "" {
		namespace = fmt.Sprintf("sandbox-%d", req.SessionID)
	}
	if namespace == "" {
		return nil, errors.New("目标命名空间不能为空")
	}

	// 1. 加载并解析剧本。
	defect, err := s.defectRepo.GetByCode(ctx, req.ProjectID, req.Code)
	if err != nil {
		return nil, err
	}
	if !defect.IsActive {
		return nil, errors.New("故障已禁用")
	}
	if req.TemplateSlug != "" && len(defect.TemplateBindings) == 0 {
		return nil, errors.New("故障未绑定此模板")
	}
	config := defect.AmplifierConfig
	if len(defect.TemplateBindings) > 0 {
		found := false
		for _, binding := range defect.TemplateBindings {
			if binding.TemplateSlug == req.TemplateSlug {
				if binding.Status != "verified" {
					return nil, errors.New("模板故障尚未验收")
				}
				if err := checkAccepted(defect, binding); err != nil {
					return nil, err
				}
				if namespace != fmt.Sprintf("sandbox-%d", req.SessionID) {
					return nil, errors.New("模板故障必须指定本班次沙箱和真实 Pod")
				}
				if s.resolveTemplate == nil {
					return nil, errors.New("模板查询服务未配置")
				}
				targets, err := s.resolveTemplate(ctx, req.TemplateSlug)
				if err != nil {
					return nil, err
				}
				validTarget := false
				for _, target := range targets {
					if target == binding.TargetWorkload {
						validTarget = true
					}
				}
				if !validTarget {
					return nil, errors.New("模板不再提供绑定的工作负载")
				}
				if s.resolveTemplateTarget == nil {
					return nil, errors.New("沙箱目标解析未配置")
				}
				pod, err := s.resolveTemplateTarget(ctx, req.SessionID, req.TemplateSlug, binding.TargetWorkload)
				if err != nil {
					return nil, err
				}
				if req.Pod != "" && req.Pod != pod {
					return nil, errors.New("请求 Pod 与模板绑定目标不符")
				}
				req.Pod = pod
				if len(binding.Overrides) > 0 {
					config = binding.Overrides
				}
				found = true
			}
		}
		if !found {
			return nil, errors.New("故障未绑定此模板")
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("剧本配置序列化失败: %w", err)
	}
	scn, err := ParseScenario(raw)
	if err != nil {
		return nil, err
	}
	if len(defect.TemplateBindings) > 0 {
		scn.Spec.Scope = "pod"
	}
	if !autoInjectable[scn.Kind] {
		return nil, fmt.Errorf("该剧本类型暂不支持自动注入: %s", scn.Kind)
	}
	// 请求显式指定 Pod 时覆盖剧本配置（同场实训沙箱名动态生成的场景）。
	if req.Pod != "" {
		scn.Target.Pod = req.Pod
	}

	// 2. 先登记实验行（created），事件回调按行 ID 回写。
	scn.Spec = PrepareScalingSpec(scn.Spec, defect.DefectType == "resource")
	// Kind belongs to the scenario, not necessarily its embedded spec.
	if resourceFault(model.InjectSpec{Kind: scn.Kind}) {
		scn.Spec = PrepareScalingSpec(scn.Spec, true)
	}
	registered := &model.Experiment{
		SessionID: req.SessionID,
		Source:    model.SourceScenario,
		Kind:      scn.Kind,
		Target:    joinTarget(namespace, scn.Target.Pod),
		Spec:      scenarioSpec(scn, namespace),
		Status:    model.StatusCreated,
	}
	if err := s.expRepo.Create(ctx, registered); err != nil {
		return nil, err
	}
	expID := registered.ID

	// 3. 装配编排器，事件回写仓储。
	orch := NewOrchestrator(OrchestratorConfig{
		SessionID:      req.SessionID,
		Namespace:      namespace,
		Scenario:       scn,
		Injector:       s.inj,
		StableRequired: s.cfg.StableRequired,
		Events: EventHandler{
			OnInjected: func(cb context.Context, exp *model.Experiment) {
				exp.SessionID = req.SessionID
				if err := s.expRepo.MarkRunning(cb, expID, exp.ExperimentUID, exp.Command); err != nil {
					logger.L().Error("回写注入成功状态失败",
						zap.Int64("session_id", req.SessionID), zap.Int64("exp_id", expID), zap.Error(err))
				}
			},
			OnRevoked: func(cb context.Context, sessionID int64) {
				if err := s.expRepo.MarkRevoked(cb, expID); err != nil {
					logger.L().Error("回写撤销状态失败",
						zap.Int64("session_id", sessionID), zap.Int64("exp_id", expID), zap.Error(err))
				}
			},
			OnFailed: func(cb context.Context, sessionID int64, cause error) {
				if err := s.expRepo.MarkFailed(cb, expID, cause.Error()); err != nil {
					logger.L().Error("回写失败状态失败",
						zap.Int64("session_id", sessionID), zap.Int64("exp_id", expID), zap.Error(err))
				}
			},
		},
	})

	// 4. 装配驱动循环并注册（重复会话直接拒绝，防止双重注入）。
	sup := NewFaultSupervisor(SupervisorConfig{
		Orchestrator: orch,
		Fetcher:      s.fetcher,
		Interval:     s.cfg.PollInterval,
		Namespace:    namespace,
		Metrics:      collectMetricNames(scn),
	})
	if err := s.registry.Start(s.rootCtx, req.SessionID, sup); err != nil {
		// 编排没启动：登记行标记失败，避免悬挂 created。
		_ = s.expRepo.MarkFailed(ctx, expID, err.Error())
		return nil, err
	}

	// 5. goroutine 退出兜底：按终态补齐仓储（skipped 无事件回调等）。
	go s.finalizeWhenDone(req.SessionID, expID, sup)

	return registered, nil
}

// finalizeWhenDone 编排器结束后按终态兜底回写一次。
func (s *RuntimeService) finalizeWhenDone(sessionID, expID int64, sup *FaultSupervisor) {
	<-sup.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch sup.Phase() {
	case PhaseRevoked, PhaseSkipped:
		// revoked 回调可能已写过（幂等）；skipped 从未注入，直接终态化。
		if err := s.expRepo.MarkRevoked(ctx, expID); err != nil {
			logger.L().Warn("编排终态兜底回写撤销失败",
				zap.Int64("session_id", sessionID), zap.Int64("exp_id", expID), zap.Error(err))
		}
	case PhaseFailed:
		// OnFailed 已写原因，兜底检查无需重复。
	default:
		// resolved 后紧跟 revoke，理论上不会停留；不做处理。
	}
}

// InjectOnceReq 临时注入请求（T3-12，由 training-service 踩坑后果传导调用）。
type InjectOnceReq struct {
	SessionID int64  `json:"session_id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"` // 空取全局默认
	Pod       string `json:"pod"`
	Process   string `json:"process"`
	// 网络类
	NetworkInterface string `json:"network_interface"`
	DelayMillis      int    `json:"delay_millis"`
	LossPercent      int    `json:"loss_percent"`
	// 强度
	CPUPercent  int `json:"cpu_percent"`
	MemPercent  int `json:"mem_percent"`
	DiskPercent int `json:"disk_percent"`
}

// InjectOnce 立即注入一次故障并登记。返回带 blade uid 的实验记录。
func (s *RuntimeService) InjectOnce(ctx context.Context, req InjectOnceReq) (*model.Experiment, error) {
	if req.SessionID <= 0 {
		return nil, errors.New("会话ID不能为空")
	}
	kind := model.FaultKind(req.Kind)
	if !autoInjectable[kind] {
		return nil, fmt.Errorf("该故障类型暂不支持注入: %s", kind)
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = s.cfg.DefaultNamespace
	}
	if namespace == "" {
		return nil, errors.New("目标命名空间不能为空")
	}

	spec := model.InjectSpec{
		Kind:             kind,
		TargetNamespace:  namespace,
		TargetPod:        req.Pod,
		TargetProcess:    req.Process,
		NetworkInterface: req.NetworkInterface,
		CPUPercent:       req.CPUPercent,
		MemPercent:       req.MemPercent,
		DiskPercent:      req.DiskPercent,
		DelayMillis:      req.DelayMillis,
		LossPercent:      req.LossPercent,
	}

	// 登记先于注入：崩溃后可凭 created/running 记录补偿。
	spec = PrepareScalingSpec(spec, false)
	exp := &model.Experiment{
		SessionID: req.SessionID,
		Source:    model.SourceConsequence,
		Kind:      kind,
		Target:    joinTarget(namespace, req.Pod),
		Spec:      spec,
		Status:    model.StatusCreated,
	}
	if err := s.expRepo.Create(ctx, exp); err != nil {
		return nil, err
	}

	injected, err := s.inj.Inject(ctx, spec)
	if err != nil {
		_ = s.expRepo.MarkFailed(ctx, exp.ID, err.Error())
		return nil, err
	}
	if err := s.expRepo.MarkRunning(ctx, exp.ID, injected.ExperimentUID, injected.Command); err != nil {
		// blade 已生效但落库失败：返回 uid 供调用方撤销，同时打错误日志。
		logger.L().Error("注入成功但落 running 失败",
			zap.Int64("session_id", req.SessionID), zap.Int64("exp_id", exp.ID), zap.Error(err))
	}
	exp.Status = model.StatusRunning
	exp.ExperimentUID = injected.ExperimentUID
	exp.Command = injected.Command
	return exp, nil
}

// RevokeOne 按 blade uid 撤销单次注入（带会话归属校验）。
func (s *RuntimeService) RevokeOne(ctx context.Context, sessionID int64, uid string) error {
	if sessionID <= 0 {
		return errors.New("会话ID不能为空")
	}
	if uid == "" {
		return errors.New("实验 uid 不能为空")
	}
	exp, err := s.expRepo.GetByUID(ctx, sessionID, uid)
	if err != nil {
		return err
	}
	if err := s.inj.Revoke(ctx, exp.Spec, uid); err != nil {
		return err
	}
	return s.expRepo.MarkRevoked(ctx, exp.ID)
}

// StopSession 结束会话：停止剧本编排并补偿撤销该会话全部未终结实验。
// 幂等：编排不存在（仅临时注入/重复结束）不报错；重复撤销安全。
func (s *RuntimeService) StopSession(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return errors.New("会话ID不能为空")
	}

	// 触发编排器 Abort（异步撤销剧本故障）。
	phase, managed, stopErr := s.registry.StopSessionAndWait(ctx, sessionID)
	if stopErr != nil {
		return stopErr
	}

	// 补偿：列出全部未终结实验（含临时注入与登记后未及回写的行）。
	active, err := s.expRepo.ListActiveBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("查询会话活跃实验失败: %w", err)
	}
	var pending []error
	for _, exp := range active {
		if exp.ExperimentUID == "" {
			if exp.Source == model.SourceScenario && managed && (phase == PhaseArmed || phase == PhaseSkipped || phase == PhaseRevoked) {
				if e := s.expRepo.MarkRevoked(ctx, exp.ID); e != nil {
					pending = append(pending, e)
				}
				continue
			}
			pending = append(pending, fmt.Errorf("experiment %d has unknown execution outcome", exp.ID))
			continue
		}
		if err := s.inj.Revoke(ctx, exp.Spec, exp.ExperimentUID); err != nil {
			pending = append(pending, fmt.Errorf("revoke experiment %d: %w", exp.ID, err))
			continue
		}
		if err := s.expRepo.MarkRevoked(ctx, exp.ID); err != nil {
			pending = append(pending, err)
		}
	}
	return errors.Join(pending...)
}

// ListExperiments 查询会话下全部注入记录。
func (s *RuntimeService) ListExperiments(ctx context.Context, sessionID int64) ([]*model.Experiment, error) {
	if sessionID <= 0 {
		return nil, errors.New("会话ID不能为空")
	}
	return s.expRepo.ListBySession(ctx, sessionID)
}

// ——— 辅助函数 ———

// collectMetricNames 汇总编排需要拉取的逻辑指标：恢复阈值 + 触发条件指标，去重保序。
func collectMetricNames(scn *Scenario) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, r := range scn.Recovery {
		add(r.Metric)
	}
	if scn.Trigger.Metric != nil {
		add(scn.Trigger.Metric.Metric)
	}
	return out
}

// scenarioSpec 从剧本构造登记用的注入规格（与 Orchestrator.buildSpec 保持一致）。
func scenarioSpec(scn *Scenario, namespace string) model.InjectSpec {
	spec := scn.Spec
	spec.Kind = scn.Kind
	spec.TargetNamespace = namespace
	spec.TargetPod = scn.Target.Pod
	spec.TargetProcess = scn.Target.Process
	if spec.NetworkInterface == "" {
		spec.NetworkInterface = scn.Target.NetworkInterface
	}
	return spec
}

// joinTarget 组装描述性目标 namespace/pod。
func joinTarget(namespace, pod string) string {
	if pod == "" {
		return namespace
	}
	return namespace + "/" + pod
}
