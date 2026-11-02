package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

var ErrResourceConflict = errors.New("资源状态已变化，请刷新后重试")

type ResourceControls struct {
	Client            kubernetes.Interface
	PlatformNamespace string
	Fake              bool
}

func (c ResourceControls) node(ctx context.Context, name, version, uid string) (*core.Node, error) {
	if c.Fake || c.Client == nil {
		return nil, errors.New("真实集群未连接")
	}
	if name == "" || version == "" || uid == "" {
		return nil, errors.New("缺少节点版本")
	}
	n, e := c.Client.CoreV1().Nodes().Get(ctx, name, meta.GetOptions{})
	if e != nil {
		return nil, e
	}
	if n.ResourceVersion != version || string(n.UID) != uid {
		return nil, ErrResourceConflict
	}
	return n, nil
}
func (c ResourceControls) SetNodeScheduling(ctx context.Context, name, version, uid string, enabled bool) error {
	n, e := c.node(ctx, name, version, uid)
	if e != nil {
		return e
	}
	n.Spec.Unschedulable = !enabled
	_, e = c.Client.CoreV1().Nodes().Update(ctx, n, meta.UpdateOptions{})
	return e
}
func (c ResourceControls) RemoveNode(ctx context.Context, name, version, uid string) error {
	n, e := c.node(ctx, name, version, uid)
	if e != nil {
		return e
	}
	if nodeReady(n) || !n.Spec.Unschedulable {
		return errors.New("先停止调度、迁移任务并断开节点后再移除")
	}
	for _, key := range []string{"node-role.kubernetes.io/control-plane", "node-role.kubernetes.io/master"} {
		if _, ok := n.Labels[key]; ok {
			return errors.New("控制平面节点需由集群运维流程移除")
		}
	}
	pods, e := c.Client.CoreV1().Pods("").List(ctx, meta.ListOptions{FieldSelector: "spec.nodeName=" + name})
	if e != nil {
		return e
	}
	for _, p := range pods.Items {
		if p.Spec.NodeName == name {
			return errors.New("节点仍有关联Pod，请先完成迁移和清理")
		}
	}
	volumes, e := c.Client.CoreV1().PersistentVolumes().List(ctx, meta.ListOptions{})
	if e != nil {
		return e
	}
	for _, pv := range volumes.Items {
		if pv.Spec.Local != nil || pv.Spec.HostPath != nil {
			if pv.Spec.NodeAffinity == nil || pv.Spec.NodeAffinity.Required == nil {
				return errors.New("存在位置不明确的本地存储，请先核对")
			}
			matches, err := localAffinityMatches(pv.Spec.NodeAffinity.Required, n)
			if err != nil {
				return errors.New("本地卷节点亲和性无法确认，请先核对")
			}
			if matches {
				return errors.New("节点存在本地持久卷，请先迁移数据")
			}
		}
	}
	id := types.UID(uid)
	return c.Client.CoreV1().Nodes().Delete(ctx, name, meta.DeleteOptions{Preconditions: &meta.Preconditions{UID: &id, ResourceVersion: &version}})
}
func (c ResourceControls) UpdatePlatformWorkload(ctx context.Context, ns, kind, name, container, version string, req, lim model.ResourceAmounts) error {
	if c.Fake || c.Client == nil {
		return errors.New("真实集群未连接")
	}
	if ns != c.PlatformNamespace || ns == "" {
		return errors.New("只允许调整平台工作负载")
	}
	if version == "" || container == "" || req.CPUMilli <= 0 || req.MemoryBytes <= 0 || !req.Fits(lim) || lim.CPUMilli > 1000000 || lim.MemoryBytes > 1099511627776 {
		return errors.New("资源申请和上限无效")
	}
	apply := func(cs []core.Container) error {
		for i := range cs {
			if cs[i].Name == container {
				cs[i].Resources.Requests = mergeCPUAndMemory(cs[i].Resources.Requests, core.ResourceList{core.ResourceCPU: *resource.NewMilliQuantity(req.CPUMilli, resource.DecimalSI), core.ResourceMemory: *resource.NewQuantity(req.MemoryBytes, resource.BinarySI)})
				cs[i].Resources.Limits = mergeCPUAndMemory(cs[i].Resources.Limits, core.ResourceList{core.ResourceCPU: *resource.NewMilliQuantity(lim.CPUMilli, resource.DecimalSI), core.ResourceMemory: *resource.NewQuantity(lim.MemoryBytes, resource.BinarySI)})
				return nil
			}
		}
		return fmt.Errorf("容器不存在")
	}
	switch kind {
	case "Deployment":
		v, e := c.Client.AppsV1().Deployments(ns).Get(ctx, name, meta.GetOptions{})
		if e != nil {
			return e
		}
		if v.ResourceVersion != version {
			return ErrResourceConflict
		}
		if e = apply(v.Spec.Template.Spec.Containers); e != nil {
			return e
		}
		_, e = c.Client.AppsV1().Deployments(ns).Update(ctx, v, meta.UpdateOptions{})
		return e
	case "StatefulSet":
		v, e := c.Client.AppsV1().StatefulSets(ns).Get(ctx, name, meta.GetOptions{})
		if e != nil {
			return e
		}
		if v.ResourceVersion != version {
			return ErrResourceConflict
		}
		if e = apply(v.Spec.Template.Spec.Containers); e != nil {
			return e
		}
		_, e = c.Client.AppsV1().StatefulSets(ns).Update(ctx, v, meta.UpdateOptions{})
		return e
	}
	return errors.New("不支持此工作负载类型")
}

func mergeCPUAndMemory(old, changes core.ResourceList) core.ResourceList {
	if old == nil {
		old = core.ResourceList{}
	}
	for key, value := range changes {
		old[key] = value
	}
	return old
}

// Terms are OR; expressions and fields inside a term are AND, matching K8s semantics.
func localAffinityMatches(selector *core.NodeSelector, n *core.Node) (bool, error) {
	any := false
	for _, term := range selector.NodeSelectorTerms {
		if len(term.MatchExpressions)+len(term.MatchFields) == 0 {
			continue
		}
		match := true
		for _, r := range term.MatchExpressions {
			var op selection.Operator
			switch r.Operator {
			case core.NodeSelectorOpIn:
				op = selection.In
			case core.NodeSelectorOpNotIn:
				op = selection.NotIn
			case core.NodeSelectorOpExists:
				op = selection.Exists
			case core.NodeSelectorOpDoesNotExist:
				op = selection.DoesNotExist
			case core.NodeSelectorOpGt:
				op = selection.GreaterThan
			case core.NodeSelectorOpLt:
				op = selection.LessThan
			default:
				return false, errors.New("unknown selector operator")
			}
			req, err := labels.NewRequirement(r.Key, op, r.Values)
			if err != nil {
				return false, err
			}
			match = match && labels.NewSelector().Add(*req).Matches(labels.Set(n.Labels))
		}
		for _, r := range term.MatchFields {
			if r.Key != "metadata.name" || len(r.Values) != 1 {
				return false, errors.New("unsupported node field selector")
			}
			switch r.Operator {
			case core.NodeSelectorOpIn:
				match = match && n.Name == r.Values[0]
			case core.NodeSelectorOpNotIn:
				match = match && n.Name != r.Values[0]
			default:
				return false, errors.New("invalid field selector operator")
			}
		}
		any = any || match
	}
	return any, nil
}
