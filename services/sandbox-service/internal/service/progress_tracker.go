package service

import (
	"context"
	"fmt"
	"time"

	"github.com/quenching/sandbox-service/internal/model"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ProgressStore 进度查询需要的沙箱存储接口。
// 最小化依赖，只取我们需要的方法。
type ProgressStore interface {
	GetBySessionID(ctx context.Context, sessionID int64) (*model.Sandbox, error)
}

// DeploymentLister 列 Deployment 的接口（便于测试 mock）。
type DeploymentLister interface {
	ListDeployments(ctx context.Context, namespace string) ([]appsv1.Deployment, error)
}

// k8sDeploymentLister 用真实 client-go 实现。
type k8sDeploymentLister struct {
	cs kubernetes.Interface
}

func (k *k8sDeploymentLister) ListDeployments(ctx context.Context, namespace string) ([]appsv1.Deployment, error) {
	list, err := k.cs.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// ProgressTracker 进度追踪器。
// 每次调用 Status() 实时计算一次当前进度。
type ProgressTracker struct {
	store        ProgressStore
	deployLister DeploymentLister
}

// NewProgressTracker 创建进度追踪器。
func NewProgressTracker(store ProgressStore, cs kubernetes.Interface) *ProgressTracker {
	return &ProgressTracker{
		store:        store,
		deployLister: &k8sDeploymentLister{cs: cs},
	}
}

// NewProgressTrackerWithLister 用自定义 DeploymentLister 创建（测试用）。
func NewProgressTrackerWithLister(store ProgressStore, lister DeploymentLister) *ProgressTracker {
	return &ProgressTracker{
		store:        store,
		deployLister: lister,
	}
}

// Status 查询当前进度状态。
func (t *ProgressTracker) Status(ctx context.Context, sessionID int64) (*ProgressStatus, error) {
	sb, err := t.store.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询沙箱失败: %w", err)
	}
	if sb == nil {
		return nil, fmt.Errorf("沙箱不存在")
	}

	// 确定当前阶段
	stage := statusToStage(sb.Status)
	info, ok := StageInfoOf(stage)
	if !ok {
		info = StageInfo{Stage: stage, Name: string(stage), Percent: 0}
	}

	if sb.Status == model.StatusQueued {
		reason := sb.AllocationReason
		if reason == "" {
			reason = "等待平台分配资源"
		}
		return &ProgressStatus{SessionID: sessionID, Status: sb.Status, Stage: Stage("queued"), StageName: "等待资源", Percent: 0, Message: reason, StartedAt: sb.CreatedAt}, nil
	}
	// 计算阶段内进度
	intraProgress := 0.5 // 默认假设当前阶段进行了一半
	readyCount := 0
	totalCount := 0

	// starting 阶段查 K8s 得到更精确的进度
	if stage == StageStarting && sb.Namespace != "" && t.deployLister != nil {
		deps, err := t.deployLister.ListDeployments(ctx, sb.Namespace)
		if err == nil {
			totalCount = len(deps)
			for _, dep := range deps {
				replicas := int32(1)
				if dep.Spec.Replicas != nil {
					replicas = *dep.Spec.Replicas
				}
				if dep.Status.ReadyReplicas >= replicas && replicas > 0 {
					readyCount++
				}
			}
			if totalCount > 0 {
				intraProgress = float64(readyCount) / float64(totalCount)
			}
		}
	}

	// 终态直接用终态进度
	if stage == StageReady {
		intraProgress = 1.0
	}
	if stage == StageFailed {
		intraProgress = 0.0
	}

	percent := calculatePercent(stage, intraProgress)

	// 计算已用时间和预计剩余
	startedAt := sb.CreatedAt
	elapsed := time.Since(startedAt)
	estimated := estimateRemaining(elapsed, percent)

	result := &ProgressStatus{
		SessionID:    sb.SessionID,
		Status:       string(sb.Status),
		Stage:        stage,
		StageName:    info.Name,
		Percent:      percent,
		Message:      stageMessage(stage, readyCount, totalCount),
		ElapsedSec:   int64(elapsed.Seconds()),
		EstimatedSec: estimated,
		ReadyCount:   readyCount,
		TotalCount:   totalCount,
		StartedAt:    startedAt,
	}

	// 失败时附带错误信息
	if stage == StageFailed {
		result.Error = "沙箱启动失败，请点击重试"
	}

	return result, nil
}

// statusToStage 将沙箱整体状态映射到启动阶段。
func statusToStage(status string) Stage {
	switch status {
	case model.StatusCreating:
		return StageInitializing
	case model.StatusDeploying:
		return StageDeploying
	case model.StatusReady:
		return StageReady
	case model.StatusFailed:
		return StageFailed
	default:
		// releasing / stopped 等其他状态也归为 initializing 不合适
		// 这里简单处理：非启动中状态按各自显示
		return Stage(status)
	}
}
