package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const elasticAnnotation = "quenching.io/elastic-intent"

// Graceful fault-engine termination revokes its active scenarios. Its automatic
// rolling restart must therefore obey training holds even in the platform namespace.
func (c *ElasticController) FreezeReason(p model.ElasticPolicy, holds []model.ScalingHold) string {
	for _, h := range holds {
		if h.Namespace == p.Namespace {
			return "资源故障期间冻结自动调整"
		}
	}
	if len(holds) > 0 && p.Namespace == c.Pool.PlatformNamespace && p.Name == "fault-engine" {
		return "实训资源故障期间冻结故障引擎，避免重启撤销故障"
	}
	return ""
}

type elasticSamples struct {
	High              int
	Low, Last, Metric time.Time
}
type ElasticController struct {
	Client        kubernetes.Interface
	Store         ElasticStore
	Pool          ResourcePool
	Registry      templates.Registry
	SnapshotBase  func(context.Context) (model.PoolSnapshot, error)
	CheckExternal func(context.Context, string) error
	samples       map[string]elasticSamples // accessed only under the durable control lock
}

func targetTotal(t model.ElasticTarget) model.ResourceAmounts {
	return model.ResourceAmounts{CPUMilli: t.Requests.CPUMilli * int64(t.Replicas), MemoryBytes: t.Requests.MemoryBytes * int64(t.Replicas)}
}
func deploymentTarget(d *apps.Deployment) model.ElasticTarget {
	n := int32(1)
	if d.Spec.Replicas != nil {
		n = *d.Spec.Replicas
	}
	return model.ElasticTarget{Replicas: n, Requests: amounts(d.Spec.Template.Spec.Containers[0].Resources.Requests)}
}
func safeElasticDeployment(d *apps.Deployment) bool {
	return len(d.Spec.Template.Spec.Containers) == 1 && len(d.Spec.Template.Spec.InitContainers) == 0 && len(d.Spec.Template.Spec.Overhead) == 0 && d.Spec.Selector != nil && d.DeletionTimestamp == nil
}
func deploymentSettled(d *apps.Deployment) bool {
	n := int32(1)
	if d.Spec.Replicas != nil {
		n = *d.Spec.Replicas
	}
	return d.Status.ObservedGeneration >= d.Generation && d.Status.Replicas == n && d.Status.UpdatedReplicas == n && d.Status.ReadyReplicas == n && d.Status.AvailableReplicas == n
}
func (c *ElasticController) external(ctx context.Context, ns string) error {
	if c.CheckExternal != nil {
		return c.CheckExternal(ctx, ns)
	}
	h, e := c.Client.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, meta.ListOptions{})
	if e != nil {
		return e
	}
	if len(h.Items) > 0 {
		return errors.New("存在其他 HPA，不能保证故障冻结")
	}
	r := c.Client.CoreV1().RESTClient()
	if r == nil || (reflect.ValueOf(r).Kind() == reflect.Ptr && reflect.ValueOf(r).IsNil()) {
		return errors.New("无法确认其他自动伸缩器")
	}
	// CRD absence is safe. Forbidden, unavailable, or invalid responses fail closed.
	raw, e := r.Get().AbsPath("/apis/autoscaling.k8s.io/v1/namespaces/" + ns + "/verticalpodautoscalers").DoRaw(ctx)
	if apierrors.IsNotFound(e) {
		return nil
	}
	if e != nil {
		return e
	}
	var vpa struct {
		Items *[]json.RawMessage `json:"items"`
	}
	if json.Unmarshal(raw, &vpa) != nil || vpa.Items == nil {
		return errors.New("无法确认 VPA 状态")
	}
	if len(*vpa.Items) > 0 {
		return errors.New("存在其他 VPA，不能保证故障冻结")
	}
	return nil
}
func (c *ElasticController) Snapshot(ctx context.Context) (model.PoolSnapshot, error) {
	read := c.SnapshotBase
	if read == nil {
		read = c.Pool.Snapshot
	}
	snap, e := read(ctx)
	if e != nil {
		return snap, e
	}
	state, e := c.Store.ElasticState(ctx)
	if e != nil {
		return snap, e
	}
	for _, p := range state.Policies {
		if p.Pending != nil && p.SessionID == 0 {
			observed := model.ResourceAmounts{}
			for _, pod := range snap.Pods {
				if p.UID != "" && pod.Namespace == p.Namespace && pod.WorkloadUID == p.UID {
					observed = observed.Add(pod.Requests)
				}
			}
			budget := maxAmounts(targetTotal(p.Pending.Target), targetTotal(p.Pending.Previous))
			delta := positiveAmounts(subtractAmounts(budget, observed))
			snap.Available = subtractAmounts(snap.Available, delta)
			snap.Reserved = snap.Reserved.Add(delta)
			if snap.NamespaceReserved == nil {
				snap.NamespaceReserved = map[string]model.ResourceAmounts{}
			}
			snap.NamespaceReserved[p.Namespace] = snap.NamespaceReserved[p.Namespace].Add(delta)
		}
	}
	return snap, nil
}
func (c *ElasticController) AcquireHold(ctx context.Context, h model.ScalingHold) error {
	return c.scalingHold(ctx, h, true)
}
func (c *ElasticController) CheckHold(ctx context.Context, namespace string) error {
	return c.scalingHold(ctx, model.ScalingHold{Namespace: namespace, ID: "preflight", Reason: "preflight"}, false)
}
func (c *ElasticController) scalingHold(ctx context.Context, h model.ScalingHold, acquire bool) error {
	sid, e := strconv.ParseInt(strings.TrimPrefix(h.Namespace, "sandbox-"), 10, 64)
	if e != nil || sid <= 0 || h.Namespace != model.NamespaceName(sid) || h.ID == "" || len(h.ID) > 100 || h.Reason == "" || len(h.Reason) > 200 {
		return errors.New("invalid scaling hold")
	}
	return c.Store.WithElasticLock(ctx, true, func(ctx context.Context) error {
		sb, e := c.Store.GetBySessionID(ctx, sid)
		if e != nil {
			return e
		}
		if sb == nil || sb.Namespace != h.Namespace || !sb.ResourcesAdmitted || !sb.CompletedAt.IsZero() || (sb.Status != model.StatusReady && sb.Status != model.StatusRunning) {
			return errors.New("故障只能冻结活跃且已就绪的班次")
		}
		ns, e := c.Client.CoreV1().Namespaces().Get(ctx, h.Namespace, meta.GetOptions{})
		if e != nil {
			return e
		}
		if ns.DeletionTimestamp != nil {
			return errors.New("班次正在释放")
		}
		state, e := c.Store.ElasticState(ctx)
		if e != nil {
			return e
		}
		alreadyFrozen := false
		for _, hold := range state.Holds {
			if hold.Namespace == h.Namespace {
				alreadyFrozen = true
			}
		}
		for _, p := range state.Policies {
			if (p.Namespace == h.Namespace || p.Namespace == c.Pool.PlatformNamespace && p.Name == "fault-engine") && p.Pending != nil {
				return errors.New("资源调整尚未收敛，暂不能触发资源故障")
			}
		}
		// Recheck even when retrying an existing hold: new foreign controllers must not mask a fault.
		if e = c.external(ctx, h.Namespace); e != nil {
			return e
		}
		ds, e := c.Client.AppsV1().Deployments(h.Namespace).List(ctx, meta.ListOptions{})
		if e != nil {
			return e
		}
		for i := range ds.Items {
			if alreadyFrozen {
				break
			}
			if !deploymentSettled(&ds.Items[i]) {
				return errors.New("班次有未收敛的部署，暂不能触发资源故障")
			}
		}
		if !acquire {
			return nil
		}
		if e = c.Store.AcquireScalingHold(ctx, h); e != nil {
			return e
		}
		return c.Store.AuditElastic(ctx, model.ElasticAudit{Namespace: h.Namespace, Name: h.ID, Action: "freeze", Detail: h.Reason})
	})
}
func (c *ElasticController) ReleaseHold(ctx context.Context, ns, id string) error {
	if !strings.HasPrefix(ns, "sandbox-") || id == "" {
		return errors.New("invalid scaling hold")
	}
	return c.Store.WithElasticLock(ctx, true, func(ctx context.Context) error {
		state, e := c.Store.ElasticState(ctx)
		if e != nil {
			return e
		}
		// Reset cooldown before deleting: interruption never leaves an unsafe immediate recovery.
		for _, p := range state.Policies {
			if p.Namespace == ns {
				p.LastChanged = time.Now().UTC()
				ResetElasticSamples(&p)
				if e = c.Store.SaveElasticPolicy(ctx, &p, nil); e != nil {
					return e
				}
				delete(c.samples, ns+"/"+p.Name)
			}
		}
		if e = c.Store.ReleaseScalingHold(ctx, ns, id); e != nil {
			return e
		}
		return c.Store.AuditElastic(ctx, model.ElasticAudit{Namespace: ns, Name: id, Action: "unfreeze", Detail: "故障引擎已确认清理"})
	})
}
func (c *ElasticController) bootstrap(ctx context.Context) error {
	ds, e := c.Client.AppsV1().Deployments("").List(ctx, meta.ListOptions{})
	if e != nil {
		return e
	}
	allocations, e := c.Store.ListReservations(ctx)
	if e != nil {
		return e
	}
	byNS := map[string]model.Sandbox{}
	for _, sb := range allocations {
		byNS[sb.Namespace] = sb
	}
	platform := map[string]int32{"api-gateway": 1, "content-service": 2, "user-service": 2, "training-service": 1, "review-service": 1, "fault-engine": 1, "sandbox-service": 1, "ai-service": 1, "web": 2, "admin": 2}
	for i := range ds.Items {
		d := &ds.Items[i]
		if !safeElasticDeployment(d) || !deploymentSettled(d) {
			continue
		}
		p := model.ElasticPolicy{Namespace: d.Namespace, Name: d.Name, UID: string(d.UID), Enabled: true, Current: deploymentTarget(d), Limits: amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits), Reason: "等待完整指标"}
		p.Min = p.Current.Requests
		p.Max = p.Limits
		if p.Current.Replicas < 1 || !p.Min.Fits(p.Max) || p.Min.CPUMilli <= 0 || p.Min.MemoryBytes <= 0 {
			continue
		}
		if d.Namespace == c.Pool.PlatformNamespace {
			p.MaxReplicas = platform[d.Name]
			if p.MaxReplicas == 0 {
				continue
			}
			p.MaxReplicas = max(p.MaxReplicas, p.Current.Replicas)
		} else {
			sb, ok := byNS[d.Namespace]
			if !ok || !sb.CompletedAt.IsZero() || (sb.Status != model.StatusReady && sb.Status != model.StatusRunning) || c.Registry == nil {
				continue
			}
			m, ok := c.Registry.Get(sb.TemplateSlug)
			if !ok || m.ArtifactDigest != sb.TemplateDigest {
				continue
			}
			for _, s := range m.Services {
				workload := s.Workload
				if workload == "" {
					workload = s.Name
				}
				if workload == d.Name && s.Elastic != nil && s.Role != templates.RoleMiddleware {
					p.SessionID = sb.SessionID
					p.Component = s.Name
					p.MaxReplicas = s.Elastic.MaxReplicas
				}
			}
			if p.SessionID == 0 || p.MaxReplicas < 1 {
				continue
			}
		}
		if e = c.Store.InsertElasticPolicy(ctx, p); e != nil {
			return e
		}
	}
	return nil
}

// Called under both control and capacity locks. No cluster mutation precedes durable intent.
func (c *ElasticController) BeginResize(ctx context.Context, p *model.ElasticPolicy, d *apps.Deployment, target model.ElasticTarget, snap model.PoolSnapshot) error {
	if p.Pending != nil || !safeElasticDeployment(d) || string(d.UID) != p.UID || deploymentTarget(d) != p.Current || amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits) != p.Limits {
		return ErrResourceConflict
	}
	if target.Replicas < 1 || target.Replicas > p.MaxReplicas || !p.Min.Fits(target.Requests) || !target.Requests.Fits(p.Max) {
		return errors.New("伸缩超出策略上下界")
	}
	state, e := c.Store.ElasticState(ctx)
	if e != nil {
		return e
	}
	if reason := c.FreezeReason(*p, state.Holds); reason != "" {
		return errors.New(reason)
	}
	for _, v := range state.Policies {
		if v.Namespace == p.Namespace && v.Pending != nil {
			return errors.New("同一命名空间已有调整")
		}
	}
	if e = c.external(ctx, p.Namespace); e != nil {
		return e
	}
	delta := subtractAmounts(targetTotal(target), targetTotal(p.Current))
	allocations, e := c.Store.ListReservations(ctx)
	if e != nil {
		return e
	}
	if delta.CPUMilli > 0 || delta.MemoryBytes > 0 {
		ok, reason := CanAdmit(snap, model.ResourcePlan{Requests: positiveAmounts(delta), Components: []model.ResourceComponent{{Name: p.Name, Requests: target.Requests}}}, allocations)
		if !ok {
			return errors.New(reason)
		}
	}
	intent := &model.ElasticIntent{ID: uuid.NewString(), Target: target, Previous: p.Current, Version: d.ResourceVersion, Generation: d.Generation}
	if p.SessionID > 0 {
		sb, e := c.Store.GetBySessionID(ctx, p.SessionID)
		if e != nil {
			return e
		}
		if sb == nil || !sb.CompletedAt.IsZero() || (sb.Status != model.StatusReady && sb.Status != model.StatusRunning) {
			return errors.New("班次已关闭")
		}
		final := sb.ResourcePlan
		final.Components = append([]model.ResourceComponent{}, final.Components...)
		final.Requests = final.Requests.Add(delta)
		limDelta := model.ResourceAmounts{CPUMilli: p.Limits.CPUMilli * int64(target.Replicas-p.Current.Replicas), MemoryBytes: p.Limits.MemoryBytes * int64(target.Replicas-p.Current.Replicas)}
		final.Limits = final.Limits.Add(limDelta)
		found := false
		for i := range final.Components {
			if final.Components[i].Name == p.Component {
				final.Components[i].Requests = target.Requests
				found = true
			}
		}
		if !found {
			return errors.New("模板缺少可伸缩组件预算")
		}
		budget := final
		budget.Requests = maxAmounts(final.Requests, sb.ResourcePlan.Requests)
		budget.Limits = maxAmounts(final.Limits, sb.ResourcePlan.Limits)
		intent.Budget = &budget
		intent.FinalBudget = &final
	}
	p.Pending = intent
	p.Reason = "正在调整资源，保留容量预留"
	if e = c.Store.SaveElasticPolicy(ctx, p, intent.Budget); e != nil {
		p.Pending = nil
		return e
	}
	if e = c.Store.AuditElastic(ctx, model.ElasticAudit{Namespace: p.Namespace, Name: p.Name, Action: "resize", Detail: fmt.Sprintf("%+v -> %+v", intent.Previous, intent.Target)}); e != nil {
		return e
	}
	return c.applyIntent(ctx, p, d)
}
func resourceList(v model.ResourceAmounts) core.ResourceList {
	return core.ResourceList{core.ResourceCPU: *resource.NewMilliQuantity(v.CPUMilli, resource.DecimalSI), core.ResourceMemory: *resource.NewQuantity(v.MemoryBytes, resource.BinarySI)}
}
func (c *ElasticController) applyIntent(ctx context.Context, p *model.ElasticPolicy, d *apps.Deployment) error {
	in := p.Pending
	if in == nil {
		return nil
	}
	if !safeElasticDeployment(d) || string(d.UID) != p.UID {
		return ErrResourceConflict
	}
	if d.Annotations[elasticAnnotation] == in.ID {
		if deploymentTarget(d) != in.Target || amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits) != p.Limits {
			return ErrResourceConflict
		}
		if !deploymentSettled(d) {
			return nil
		}
		// A status alone must not release a reservation while old pods are terminating.
		selector, e := meta.LabelSelectorAsSelector(d.Spec.Selector)
		if e != nil {
			return e
		}
		pods, e := c.Client.CoreV1().Pods(p.Namespace).List(ctx, meta.ListOptions{LabelSelector: selector.String()})
		if e != nil {
			return e
		}
		if len(pods.Items) != int(in.Target.Replicas) {
			return nil
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Status.Phase != core.PodRunning || PodResources(pod, false) != in.Target.Requests {
				return nil
			}
		}
		if in.FinalBudget != nil {
			confirmed, err := c.applyElasticQuota(ctx, p.Namespace, *in.FinalBudget, true)
			if err != nil {
				return err
			}
			if !confirmed {
				p.Reason = "等待最终配额生效，保留容量预留"
				return nil
			}
		}
		p.Current = in.Target
		p.Pending = nil
		p.LastChanged = time.Now().UTC()
		p.Reason = "资源调整完成，冷却中"
		if e = c.Store.SaveElasticPolicy(ctx, p, in.FinalBudget); e != nil {
			p.Pending = in
			return e
		}
		return c.Store.AuditElastic(ctx, model.ElasticAudit{Namespace: p.Namespace, Name: p.Name, Action: "converged", Detail: in.ID})
	}
	if d.Generation != in.Generation || deploymentTarget(d) != in.Previous || amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits) != p.Limits {
		return ErrResourceConflict
	}
	if in.Budget != nil {
		if _, err := c.applyElasticQuota(ctx, p.Namespace, *in.Budget, false); err != nil {
			return err
		}
	}
	copy := d.DeepCopy()
	copy.Spec.Replicas = &in.Target.Replicas
	if copy.Spec.Template.Spec.Containers[0].Resources.Requests == nil {
		copy.Spec.Template.Spec.Containers[0].Resources.Requests = core.ResourceList{}
	}
	for name, value := range resourceList(in.Target.Requests) {
		copy.Spec.Template.Spec.Containers[0].Resources.Requests[name] = value
	}
	copy.Spec.Strategy = apps.DeploymentStrategy{Type: apps.RollingUpdateDeploymentStrategyType, RollingUpdate: &apps.RollingUpdateDeployment{MaxSurge: &intstr.IntOrString{IntVal: 0}, MaxUnavailable: &intstr.IntOrString{IntVal: 1}}}
	if copy.Annotations == nil {
		copy.Annotations = map[string]string{}
	}
	copy.Annotations[elasticAnnotation] = in.ID
	_, e := c.Client.AppsV1().Deployments(p.Namespace).Update(ctx, copy, meta.UpdateOptions{})
	return e
}

// Admission uses status.hard. Keep the durable budget until the quota controller
// has observed the final hard limits; an API update acknowledgement alone is insufficient.
func (c *ElasticController) applyElasticQuota(ctx context.Context, namespace string, plan model.ResourcePlan, confirm bool) (bool, error) {
	want := BuildPlannedResourceQuota(namespace, plan)
	quotas := c.Client.CoreV1().ResourceQuotas(namespace)
	q, e := quotas.Get(ctx, want.Name, meta.GetOptions{})
	if e != nil {
		return false, e
	}
	if !sameQuotaHard(q.Spec.Hard, want.Spec.Hard) {
		q.Spec = want.Spec
		q, e = quotas.Update(ctx, q, meta.UpdateOptions{})
		if e != nil {
			return false, e
		}
	}
	return !confirm || sameQuotaHard(q.Status.Hard, want.Spec.Hard), nil
}
func sameQuotaHard(a, b core.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for key, want := range b {
		have, ok := a[key]
		if !ok || have.Cmp(want) != 0 {
			return false
		}
	}
	return true
}
func (c *ElasticController) Tick(ctx context.Context) error {
	return c.Store.WithElasticLock(ctx, false, func(ctx context.Context) error {
		return c.Store.WithAllocationLock(ctx, func(ctx context.Context) error {
			if e := c.bootstrap(ctx); e != nil {
				return e
			}
			state, e := c.Store.ElasticState(ctx)
			if e != nil {
				return e
			}
			snap, e := c.Snapshot(ctx)
			if e != nil {
				return e
			}
			if c.samples == nil {
				c.samples = map[string]elasticSamples{}
			}
			// Holds can exist for a template without any elastic workload.
			for _, h := range state.Holds {
				ns, err := c.Client.CoreV1().Namespaces().Get(ctx, h.Namespace, meta.GetOptions{})
				_ = ns
				if apierrors.IsNotFound(err) {
					if err = c.Store.DeleteElasticNamespace(ctx, h.Namespace); err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
			}
			for _, p := range state.Policies {
				freezeReason := c.FreezeReason(p, state.Holds)
				p.Frozen = freezeReason != ""
				key := p.Namespace + "/" + p.Name
				if p.SessionID > 0 {
					ns, e := c.Client.CoreV1().Namespaces().Get(ctx, p.Namespace, meta.GetOptions{})
					if apierrors.IsNotFound(e) {
						if e = c.Store.DeleteElasticNamespace(ctx, p.Namespace); e != nil {
							return e
						}
						delete(c.samples, key)
						continue
					}
					if e != nil {
						return e
					}
					sb, e := c.Store.GetBySessionID(ctx, p.SessionID)
					if e != nil {
						return e
					}
					if sb == nil || !sb.CompletedAt.IsZero() || ns.DeletionTimestamp != nil || (sb.Status != model.StatusReady && sb.Status != model.StatusRunning) {
						delete(c.samples, key)
						continue
					}
				}
				d, e := c.Client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Name, meta.GetOptions{})
				if e != nil {
					p.Reason = "工作负载不可读"
					if !apierrors.IsNotFound(e) {
						return e
					}
				} else if p.Pending != nil {
					if e = c.external(ctx, p.Namespace); e == nil {
						e = c.applyIntent(ctx, &p, d)
					}
					if e != nil {
						p.Reason = "调整待确认：" + e.Error()
					}
					delete(c.samples, key)
				} else if !p.Enabled {
					p.Reason = "已关闭自动伸缩"
					delete(c.samples, key)
				} else if freezeReason != "" {
					p.Reason = freezeReason
					delete(c.samples, key)
				} else if !safeElasticDeployment(d) || string(d.UID) != p.UID || deploymentTarget(d) != p.Current || amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits) != p.Limits {
					p.Reason = "配置已由其他操作修改，请关闭策略并核对"
					p.Enabled = false
					delete(c.samples, key)
				} else if !deploymentSettled(d) {
					p.Reason = "等待部署就绪"
					delete(c.samples, key)
				} else {
					usage, at, ok, e := c.observation(ctx, d, snap)
					if e != nil {
						return e
					}
					samples := c.samples[key]
					if !ok {
						p.Reason = "等待完整且新鲜的指标"
						delete(c.samples, key)
					} else if at.After(samples.Metric) {
						p.HighSamples, p.LowSince, p.LastSample = samples.High, samples.Low, samples.Last
						target := DecideElastic(&p, usage, time.Now().UTC())
						c.samples[key] = elasticSamples{High: p.HighSamples, Low: p.LowSince, Last: p.LastSample, Metric: at}
						if target != nil {
							if e = c.BeginResize(ctx, &p, d, *target, snap); e != nil {
								p.Reason = e.Error()
							}
							delete(c.samples, key)
						} else {
							p.Reason = "观察负载与冷却窗口"
						}
					}
				}
				if e = c.Store.SaveElasticPolicy(ctx, &p, nil); e != nil {
					return e
				}
				// Re-read after each durable intent; later policies cannot consume the same capacity.
				snap, e = c.Snapshot(ctx)
				if e != nil {
					return e
				}
			}
			return nil
		})
	})
}
func (c *ElasticController) observation(ctx context.Context, d *apps.Deployment, snap model.PoolSnapshot) (model.ResourceAmounts, time.Time, bool, error) {
	selector, e := meta.LabelSelectorAsSelector(d.Spec.Selector)
	if e != nil {
		return model.ResourceAmounts{}, time.Time{}, false, e
	}
	pods, e := c.Client.CoreV1().Pods(d.Namespace).List(ctx, meta.ListOptions{LabelSelector: selector.String()})
	if e != nil {
		return model.ResourceAmounts{}, time.Time{}, false, e
	}
	rs, e := c.Client.AppsV1().ReplicaSets(d.Namespace).List(ctx, meta.ListOptions{LabelSelector: selector.String()})
	if e != nil {
		return model.ResourceAmounts{}, time.Time{}, false, e
	}
	owners := map[string]bool{}
	for _, r := range rs.Items {
		for _, o := range r.OwnerReferences {
			if o.UID == d.UID && o.Controller != nil && *o.Controller {
				owners[string(r.UID)] = true
			}
		}
	}
	n := int32(1)
	if d.Spec.Replicas != nil {
		n = *d.Spec.Replicas
	}
	if n < 1 || len(pods.Items) != int(n) {
		return model.ResourceAmounts{}, time.Time{}, false, nil
	}
	metrics := map[string]*model.ResourceUsage{}
	for _, p := range snap.Pods {
		if p.Namespace == d.Namespace {
			metrics[p.Name] = p.Usage
		}
	}
	total := model.ResourceAmounts{}
	at := time.Time{}
	for _, pod := range pods.Items {
		owned := false
		for _, o := range pod.OwnerReferences {
			if owners[string(o.UID)] && o.Controller != nil && *o.Controller {
				owned = true
			}
		}
		if !owned || pod.DeletionTimestamp != nil || pod.Status.Phase != core.PodRunning || PodResources(pod, false) != deploymentTarget(d).Requests {
			return total, at, false, nil
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == core.PodReady && condition.Status == core.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			return total, at, false, nil
		}
		v, ok := FreshUsage(metrics[pod.Name], time.Now())
		if !ok {
			return total, at, false, nil
		}
		total = total.Add(v)
		timestamp, _ := time.Parse(time.RFC3339Nano, metrics[pod.Name].Timestamp)
		if at.IsZero() || timestamp.Before(at) {
			at = timestamp
		}
	}
	return model.ResourceAmounts{CPUMilli: total.CPUMilli / int64(n), MemoryBytes: total.MemoryBytes / int64(n)}, at, true, nil
}
func (c *ElasticController) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		run, cancel := context.WithTimeout(ctx, 25*time.Second)
		if e := c.Tick(run); e != nil && ctx.Err() == nil {
			log.Printf("elastic resources: %v", e)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
