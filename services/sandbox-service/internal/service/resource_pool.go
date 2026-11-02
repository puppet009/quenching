package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"reflect"
	"sort"
	"strings"
	"time"
)

type ResourcePool struct {
	Client            kubernetes.Interface
	PlatformNamespace string
	Fake              bool
}

func amounts(r core.ResourceList) model.ResourceAmounts {
	c := r[core.ResourceCPU]
	m := r[core.ResourceMemory]
	return model.ResourceAmounts{CPUMilli: c.MilliValue(), MemoryBytes: m.Value()}
}
func maxAmounts(a, b model.ResourceAmounts) model.ResourceAmounts {
	if b.CPUMilli > a.CPUMilli {
		a.CPUMilli = b.CPUMilli
	}
	if b.MemoryBytes > a.MemoryBytes {
		a.MemoryBytes = b.MemoryBytes
	}
	return a
}
func subtractAmounts(a, b model.ResourceAmounts) model.ResourceAmounts {
	return model.ResourceAmounts{CPUMilli: a.CPUMilli - b.CPUMilli, MemoryBytes: a.MemoryBytes - b.MemoryBytes}
}
func positiveAmounts(a model.ResourceAmounts) model.ResourceAmounts {
	return maxAmounts(a, model.ResourceAmounts{})
}

// Native sidecars run with later init containers and with app containers.
func PodResources(p core.Pod, limits bool) model.ResourceAmounts {
	get := func(c core.Container) model.ResourceAmounts {
		if limits {
			return amounts(c.Resources.Limits)
		}
		return amounts(c.Resources.Requests)
	}
	app := model.ResourceAmounts{}
	for _, c := range p.Spec.Containers {
		app = app.Add(get(c))
	}
	side := model.ResourceAmounts{}
	peak := model.ResourceAmounts{}
	for _, c := range p.Spec.InitContainers {
		v := get(c)
		if c.RestartPolicy != nil && *c.RestartPolicy == core.ContainerRestartPolicyAlways {
			side = side.Add(v)
			peak = maxAmounts(peak, side)
		} else {
			peak = maxAmounts(peak, side.Add(v))
		}
	}
	return maxAmounts(app.Add(side), peak).Add(amounts(p.Spec.Overhead))
}
func (p ResourcePool) group(ns string) string {
	if ns == p.PlatformNamespace {
		return "platform"
	}
	if strings.HasPrefix(ns, "sandbox-") {
		return "training"
	}
	return "infrastructure"
}
func (p ResourcePool) Snapshot(ctx context.Context) (model.PoolSnapshot, error) {
	s := model.PoolSnapshot{ObservedAt: time.Now().UTC(), Nodes: []model.ResourceNode{}, Pods: []model.ResourcePod{}, Workloads: []model.ResourceWorkload{}, PVCs: []model.ResourcePVC{}, Groups: map[string]model.ResourceGroup{"platform": {}, "training": {}, "infrastructure": {}}, NamespaceRequests: map[string]model.ResourceAmounts{}, Warnings: []string{}, Allocations: []model.Sandbox{}}
	s.NamespaceReserved = map[string]model.ResourceAmounts{}
	if p.Fake || p.Client == nil {
		return s, fmt.Errorf("真实集群资源池未连接")
	}
	nodes, e := p.Client.CoreV1().Nodes().List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	pods, e := p.Client.CoreV1().Pods("").List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	indices := map[string]int{}
	for _, n := range nodes.Items {
		v := model.ResourceNode{Name: n.Name, UID: string(n.UID), Version: n.ResourceVersion, Ready: nodeReady(&n), Unschedulable: n.Spec.Unschedulable, Allocatable: amounts(n.Status.Allocatable), Capacity: amounts(n.Status.Capacity)}
		v.Eligible = v.Ready && !v.Unschedulable
		if !v.Ready {
			v.Reason = "节点未就绪"
		} else if v.Unschedulable {
			v.Reason = "已停止调度"
		}
		for _, t := range n.Spec.Taints {
			if t.Effect == core.TaintEffectNoSchedule || t.Effect == core.TaintEffectNoExecute {
				v.Eligible = false
				v.Reason = "节点存在调度限制"
			}
		}
		indices[n.Name] = len(s.Nodes)
		s.Nodes = append(s.Nodes, v)
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == core.PodSucceeded || pod.Status.Phase == core.PodFailed {
			continue
		}
		req, lim := PodResources(pod, false), PodResources(pod, true)
		group := p.group(pod.Namespace)
		g := s.Groups[group]
		g.Requested = g.Requested.Add(req)
		g.Limits = g.Limits.Add(lim)
		g.PodCount++
		s.Groups[group] = g
		s.NamespaceRequests[pod.Namespace] = s.NamespaceRequests[pod.Namespace].Add(req)
		containerNames := []string{}
		for _, c := range pod.Spec.Containers {
			containerNames = append(containerNames, c.Name)
		}
		for _, c := range pod.Spec.InitContainers {
			if c.RestartPolicy != nil && *c.RestartPolicy == core.ContainerRestartPolicyAlways {
				containerNames = append(containerNames, c.Name)
			}
		}
		s.Pods = append(s.Pods, model.ResourcePod{ContainerNames: containerNames, Namespace: pod.Namespace, Name: pod.Name, Node: pod.Spec.NodeName, Phase: string(pod.Status.Phase), Group: group, Requests: req, Limits: lim})
		if i, ok := indices[pod.Spec.NodeName]; ok {
			s.Nodes[i].Requested = s.Nodes[i].Requested.Add(req)
			s.Nodes[i].Limits = s.Nodes[i].Limits.Add(lim)
			s.Nodes[i].PodCount++
		} else {
			s.Pending = s.Pending.Add(req)
		}
	}
	for _, n := range s.Nodes {
		if n.Eligible {
			s.Available = s.Available.Add(positiveAmounts(subtractAmounts(n.Allocatable, n.Requested)))
		}
	}
	s.Available = subtractAmounts(s.Available, s.Pending)
	pvcs, e := p.Client.CoreV1().PersistentVolumeClaims("").List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	for _, v := range pvcs.Items {
		b := v.Spec.Resources.Requests[core.ResourceStorage]
		class := ""
		if v.Spec.StorageClassName != nil {
			class = *v.Spec.StorageClassName
		}
		s.PVCs = append(s.PVCs, model.ResourcePVC{Namespace: v.Namespace, Name: v.Name, StorageClass: class, Phase: string(v.Status.Phase), Bytes: b.Value()})
		key := p.group(v.Namespace)
		g := s.Groups[key]
		g.StorageBytes += b.Value()
		s.Groups[key] = g
	}
	deps, e := p.Client.AppsV1().Deployments("").List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	for _, v := range deps.Items {
		s.Workloads = append(s.Workloads, p.workload(v.Namespace, v.Name, "Deployment", v.ResourceVersion, v.Spec.Replicas, v.Spec.Template.Spec.Containers))
	}
	// Attribute observed Pod requests by owner UID, never by matching names/labels.
	replicas, e := p.Client.AppsV1().ReplicaSets("").List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	owners := map[string]string{}
	for _, d := range deps.Items {
		if d.UID != "" {
			owners[string(d.UID)] = string(d.UID)
		}
	}
	for _, r := range replicas.Items {
		for _, o := range r.OwnerReferences {
			if o.Controller != nil && *o.Controller && o.Kind == "Deployment" && owners[string(o.UID)] != "" {
				owners[string(r.UID)] = string(o.UID)
			}
		}
	}
	podOwners := map[string]string{}
	for _, pod := range pods.Items {
		for _, o := range pod.OwnerReferences {
			if o.Controller != nil && *o.Controller {
				podOwners[pod.Namespace+"/"+pod.Name] = owners[string(o.UID)]
			}
		}
	}
	for i := range s.Pods {
		s.Pods[i].WorkloadUID = podOwners[s.Pods[i].Namespace+"/"+s.Pods[i].Name]
	}
	states, e := p.Client.AppsV1().StatefulSets("").List(ctx, meta.ListOptions{})
	if e != nil {
		return s, e
	}
	for _, v := range states.Items {
		s.Workloads = append(s.Workloads, p.workload(v.Namespace, v.Name, "StatefulSet", v.ResourceVersion, v.Spec.Replicas, v.Spec.Template.Spec.Containers))
	}
	p.addMetrics(ctx, &s)
	sort.Slice(s.Nodes, func(i, j int) bool { return s.Nodes[i].Name < s.Nodes[j].Name })
	return s, nil
}
func (p ResourcePool) workload(ns, name, kind, version string, replicas *int32, cs []core.Container) model.ResourceWorkload {
	v := model.ResourceWorkload{Namespace: ns, Name: name, Kind: kind, Version: version, Replicas: 1, Group: p.group(ns), Containers: []model.ResourceContainer{}}
	if replicas != nil {
		v.Replicas = *replicas
	}
	for _, c := range cs {
		v.Containers = append(v.Containers, model.ResourceContainer{Name: c.Name, Requests: amounts(c.Resources.Requests), Limits: amounts(c.Resources.Limits)})
	}
	return v
}

type resourceMetricsList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Timestamp  string            `json:"timestamp"`
		Usage      map[string]string `json:"usage"`
		Containers []struct {
			Name  string            `json:"name"`
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

func metricAmount(m map[string]string) model.ResourceAmounts {
	c, _ := resource.ParseQuantity(m["cpu"])
	b, _ := resource.ParseQuantity(m["memory"])
	return model.ResourceAmounts{CPUMilli: c.MilliValue(), MemoryBytes: b.Value()}
}
func (p ResourcePool) addMetrics(ctx context.Context, s *model.PoolSnapshot) {
	r := p.Client.CoreV1().RESTClient()
	if r == nil || (reflect.ValueOf(r).Kind() == reflect.Ptr && reflect.ValueOf(r).IsNil()) {
		s.Warnings = append(s.Warnings, "实际使用量不可用：metrics-server 未连接")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, kind := range []string{"nodes", "pods"} {
		raw, e := r.Get().AbsPath("/apis/metrics.k8s.io/v1beta1/" + kind).DoRaw(ctx)
		var list resourceMetricsList
		if e != nil || json.Unmarshal(raw, &list) != nil {
			s.Warnings = append(s.Warnings, "部分实际使用量不可用")
			continue
		}
		s.MetricsAvailable = true
		for _, m := range list.Items {
			v, valid := parseMetricAmount(m.Usage)
			if kind == "nodes" {
				if !valid {
					continue
				}
				for i := range s.Nodes {
					if s.Nodes[i].Name == m.Metadata.Name {
						s.Nodes[i].Usage = &model.ResourceUsage{CPU: v, Timestamp: m.Timestamp}
					}
				}
			} else {
				v = model.ResourceAmounts{}
				valid = len(m.Containers) > 0
				names := map[string]bool{}
				for _, c := range m.Containers {
					a, ok := parseMetricAmount(c.Usage)
					if !ok || c.Name == "" || names[c.Name] {
						valid = false
					}
					names[c.Name] = true
					v = v.Add(a)
				}
				for i := range s.Pods {
					if s.Pods[i].Namespace == m.Metadata.Namespace && s.Pods[i].Name == m.Metadata.Name {
						complete := valid && len(names) == len(s.Pods[i].ContainerNames)
						for _, name := range s.Pods[i].ContainerNames {
							complete = complete && names[name]
						}
						if !complete {
							continue
						}
						s.Pods[i].Usage = &model.ResourceUsage{CPU: v, Timestamp: m.Timestamp}
					}
				}
			}
		}
	}
}
func CanAdmit(s model.PoolSnapshot, p model.ResourcePlan, allocated []model.Sandbox) (bool, string) {
	free := s.Available
	for _, a := range allocated {
		if a.ResourcesAdmitted && a.Status != model.StatusReleased {
			missing := positiveAmounts(subtractAmounts(a.ResourcePlan.Requests, s.NamespaceRequests[a.Namespace]))
			free = subtractAmounts(free, missing)
		}
	}
	if !p.Requests.Fits(free) {
		return false, "等待可用CPU或内存（含平台占用及班次预留）"
	}
	for _, c := range p.Components {
		fit := false
		for _, n := range s.Nodes {
			if n.Eligible && c.Requests.Fits(positiveAmounts(subtractAmounts(n.Allocatable, n.Requested))) {
				fit = true
				break
			}
		}
		if !fit {
			return false, "等待可容纳单个组件的节点"
		}
	}
	return true, ""
}
