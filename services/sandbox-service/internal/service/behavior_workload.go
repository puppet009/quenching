// behavior_workload.go 行为模拟工作负载的 K8s 清单生成（行为模拟第一批）。
//
// 行为模拟运行时以沙箱 namespace 内的一个 Deployment 形式存在：
// Pod 内运行 Playwright（官方镜像自带 Node + Chromium），行为脚本与配置
// 由两个 ConfigMap 挂载（第二批创建；这里只声明卷引用，挂载点缺失时
// Pod 启动会失败，因此 Apply 与 ConfigMap 创建在同一流程内成对执行）。
//
// 所有 Apply/Delete/Get 操作幂等，fake clientset 下同样可执行（本地
// 降级时 runtime 层不会真正 Start，但清单生成可独立单测）。
package service

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/quenching/sandbox-service/internal/model"
)

const (
	// BehaviorWorkloadName 行为模拟 Deployment 的固定名称。
	BehaviorWorkloadName = "behavior-runner"
	// behaviorRunnerVolume 行为脚本 ConfigMap 卷名。
	behaviorRunnerVolume = "behavior-runner-script"
	// behaviorFlowVolume 行为流配置 ConfigMap 卷名。
	behaviorFlowVolume = "behavior-flow-config"
	// BehaviorRunnerScriptPath runner 脚本在容器内的挂载目录。
	BehaviorRunnerScriptPath = "/opt/behavior"
	// BehaviorFlowConfigPath 行为流配置在容器内的挂载目录。
	BehaviorFlowConfigPath = "/etc/behavior"
	// DefaultBehaviorImage Playwright 官方镜像（自带 Node 与浏览器依赖）。
	DefaultBehaviorImage = "mcr.microsoft.com/playwright:v1.49.1-jammy"
)

// BehaviorWorkloadSpec 行为模拟工作负载参数。
type BehaviorWorkloadSpec struct {
	CommandURL   string
	RunnerSecret string
	// SessionID 实训会话ID（决定 namespace 与标签）。
	SessionID int64
	// Image 容器镜像；为空使用 DefaultBehaviorImage。
	Image string
	// EntryURL 被测项目前端入口地址；注入 ENTRY_URL 环境变量。
	EntryURL string
	// SignalEndpoint 行为信号上报地址；注入 SIGNAL_ENDPOINT 环境变量。
	// 第一批可留空（此时 runner 只打印信号，第二批接上汇聚端点）。
	SignalEndpoint string
	// ReplicaCount 副本数；<=0 时取 1。
	ReplicaCount int32
}

// ApplyBehaviorWorkload 创建/更新行为模拟 Deployment（幂等）。
// 返回 Deployment 的 UID，便于上层记录与排障。
func (c *K8sClient) ApplyBehaviorWorkload(ctx context.Context, spec BehaviorWorkloadSpec) (types.UID, error) {
	if spec.SessionID <= 0 {
		return "", fmt.Errorf("会话ID非法")
	}
	if spec.Image == "" {
		spec.Image = DefaultBehaviorImage
	}
	if spec.ReplicaCount <= 0 {
		spec.ReplicaCount = 1
	}
	namespace := model.NamespaceName(spec.SessionID)
	if spec.CommandURL != "" {
		if spec.ReplicaCount != 1 {
			return "", fmt.Errorf("durable runner requires one replica")
		}
		pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "behavior-state", Namespace: namespace, Labels: map[string]string{"quenching.io/session": itoa(spec.SessionID)}}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: apiresource.MustParse("32Mi")}}}}
		if _, err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return "", err
		}
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       BehaviorWorkloadName,
		"app.kubernetes.io/managed-by": "sandbox-service",
		"quenching.io/session":         itoa(spec.SessionID),
		"quenching.io/component":       "behavior-runner",
	}

	// 入口命令：
	//   1. 等待行为脚本挂载就绪；
	//   2. 在可写目录安装 playwright-core（官方镜像只内置浏览器二进制，
	//      不保留 playwright npm 包）；浏览器复用 /ms-playwright 无需下载；
	//   3. 通过 NODE_PATH 引入本地安装的 playwright-core 后启动 runner。
	// npm 默认随镜像内置的 Node 22 提供；安装失败 set -e 让 Pod 快速失败，
	// 由 Deployment/状态接口暴露问题，绝不退化为"静默不跑"。
	command := []string{
		"/bin/sh", "-c",
		`set -e
while [ ! -f /opt/behavior/runner.js ]; do echo "waiting for behavior runner script..."; sleep 2; done
mkdir -p /tmp/pw-runtime
cd /tmp/pw-runtime
if [ ! -d node_modules/playwright-core ]; then
  echo "installing playwright-core runtime..."
  npm install --no-audit --no-fund playwright-core@1.49.1
fi
export NODE_PATH=/tmp/pw-runtime/node_modules
exec node /opt/behavior/runner.js`,
	}

	env := []corev1.EnvVar{
		{Name: "ENTRY_URL", Value: spec.EntryURL},
		{Name: "SIGNAL_ENDPOINT", Value: spec.SignalEndpoint},
		{Name: "SESSION_ID", Value: itoa(spec.SessionID)},
		{Name: "FLOW_CONFIG_PATH", Value: BehaviorFlowConfigPath + "/flow.json"},
	}

	if spec.CommandURL != "" {
		env = append(env, corev1.EnvVar{Name: "BEHAVIOR_COMMAND_URL", Value: spec.CommandURL}, corev1.EnvVar{Name: "BEHAVIOR_RUNNER_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: spec.RunnerSecret}, Key: "token"}}})
	}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BehaviorWorkloadName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": BehaviorWorkloadName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					PriorityClassName: "quenching-training",
					Containers: []corev1.Container{
						{
							Name:    "runner",
							Image:   spec.Image,
							Command: command,
							Env:     env,
							// Playwright 浏览器 + 持续循环行为，资源请求纳入配额。
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    apiresource.MustParse("200m"),
									corev1.ResourceMemory: apiresource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    apiresource.MustParse("1"),
									corev1.ResourceMemory: apiresource.MustParse("1Gi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      behaviorRunnerVolume,
									MountPath: BehaviorRunnerScriptPath,
									ReadOnly:  true,
								},
								{
									Name:      behaviorFlowVolume,
									MountPath: BehaviorFlowConfigPath,
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: behaviorRunnerVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: BehaviorRunnerConfigMapName(spec.SessionID),
									},
								},
							},
						},
						{
							Name: behaviorFlowVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: BehaviorFlowConfigMapName(spec.SessionID),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if spec.CommandURL != "" {
		dep.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		dep.Spec.Template.Spec.Containers[0].Env = append(dep.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "BEHAVIOR_STATE_PATH", Value: "/state/session.json"})
		dep.Spec.Template.Spec.Containers[0].VolumeMounts = append(dep.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "behavior-state", MountPath: "/state"})
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{Name: "behavior-state", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "behavior-state"}}})
	}
	apps := c.clientset.AppsV1().Deployments(namespace)
	existing, err := apps.Get(ctx, BehaviorWorkloadName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return "", fmt.Errorf("查询行为模拟工作负载: %w", err)
		}
		created, createErr := apps.Create(ctx, dep, metav1.CreateOptions{})
		if createErr != nil {
			return "", fmt.Errorf("创建行为模拟工作负载: %w", createErr)
		}
		return created.UID, nil
	}

	// 已存在：保留对象的 ClusterIP 类运行时字段，只更新规格。
	dep.ResourceVersion = existing.ResourceVersion
	updated, err := apps.Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		return "", fmt.Errorf("更新行为模拟工作负载: %w", err)
	}
	return updated.UID, nil
}

// DeleteBehaviorWorkload 删除行为模拟 Deployment（幂等；不存在视为成功）。
func (c *K8sClient) DeleteBehaviorWorkload(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}
	namespace := model.NamespaceName(sessionID)
	err := c.clientset.AppsV1().Deployments(namespace).
		Delete(ctx, BehaviorWorkloadName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("删除行为模拟工作负载: %w", err)
	}
	return nil
}

// GetBehaviorWorkload 查询行为模拟 Deployment；不存在时返回 (nil, nil)。
func (c *K8sClient) GetBehaviorWorkload(ctx context.Context, sessionID int64) (*appsv1.Deployment, error) {
	if sessionID <= 0 {
		return nil, fmt.Errorf("会话ID非法")
	}
	namespace := model.NamespaceName(sessionID)
	dep, err := c.clientset.AppsV1().Deployments(namespace).
		Get(ctx, BehaviorWorkloadName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询行为模拟工作负载: %w", err)
	}
	return dep, nil
}

// BehaviorRunnerConfigMapName runner 脚本 ConfigMap 名。
func BehaviorRunnerConfigMapName(sessionID int64) string {
	return fmt.Sprintf("behavior-runner-script-%d", sessionID)
}

// BehaviorFlowConfigMapName 行为流配置 ConfigMap 名。
func BehaviorFlowConfigMapName(sessionID int64) string {
	return fmt.Sprintf("behavior-flow-config-%d", sessionID)
}
