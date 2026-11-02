// behavior_configmaps.go 行为模拟 ConfigMap 的创建/删除（行为模拟第二批）。
//
// runner Pod 挂载两个 ConfigMap：
//   - behavior-runner-script-<id>：runner.js（内嵌脚本，全版本一致）；
//   - behavior-flow-config-<id>  ：flow.json（按模板 behavior_flow 生成）。
//
// 两者与 Deployment 在同一启动流程内成对创建：脚本 ConfigMap 全环境一致
// 可提前创建，flow ConfigMap 依赖模板解析结果。全部操作幂等，随 namespace
// 删除自动回收，Stop 时显式删除作为兜底。
package service

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/quenching/sandbox-service/internal/model"
)

// ApplyBehaviorConfigMaps 创建/更新 runner 脚本与 flow 两个 ConfigMap（幂等）。
// 必须先于（或同时于）ApplyBehaviorWorkload 调用，避免 Pod 因脚本缺失空转。
func (c *K8sClient) ApplyBehaviorConfigMaps(ctx context.Context, sessionID int64, flow *BehaviorFlowConfig) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}
	if flow == nil {
		return fmt.Errorf("行为流配置不能为空")
	}

	flowJSON, err := json.Marshal(flow)
	if err != nil {
		return fmt.Errorf("序列化行为流配置: %w", err)
	}

	namespace := model.NamespaceName(sessionID)
	if err := c.applyConfigMap(ctx, namespace, BehaviorRunnerConfigMapName(sessionID), map[string]string{
		BehaviorRunnerScriptFile: runnerScript,
		"command_runner.js":      commandRunnerScript,
		"protocol_runner.js":     protocolRunnerScript,
	}); err != nil {
		return err
	}
	if err := c.applyConfigMap(ctx, namespace, BehaviorFlowConfigMapName(sessionID), map[string]string{
		"flow.json": string(flowJSON),
	}); err != nil {
		return err
	}
	return nil
}

// DeleteBehaviorConfigMaps 删除两个行为模拟 ConfigMap（幂等；不存在视为成功）。
func (c *K8sClient) DeleteBehaviorConfigMaps(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID非法")
	}
	namespace := model.NamespaceName(sessionID)
	cms := c.clientset.CoreV1().ConfigMaps(namespace)
	for _, name := range []string{
		BehaviorRunnerConfigMapName(sessionID),
		BehaviorFlowConfigMapName(sessionID),
	} {
		err := cms.Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除行为模拟 ConfigMap %s: %w", name, err)
		}
	}
	return nil
}

// applyConfigMap 创建或更新单个 ConfigMap（幂等）。
func (c *K8sClient) applyConfigMap(ctx context.Context, namespace, name string, data map[string]string) error {
	cms := c.clientset.CoreV1().ConfigMaps(namespace)
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "sandbox-service",
				"quenching.io/component":       "behavior-runner",
			},
		},
		Data: data,
	}

	existing, err := cms.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("查询 ConfigMap %s: %w", name, err)
		}
		if _, err := cms.Create(ctx, desired, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("创建 ConfigMap %s: %w", name, err)
		}
		return nil
	}

	desired.ResourceVersion = existing.ResourceVersion
	if _, err := cms.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 ConfigMap %s: %w", name, err)
	}
	return nil
}
