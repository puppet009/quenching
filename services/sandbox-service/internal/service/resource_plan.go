package service

import (
	"context"
	"fmt"
	tpl "github.com/quenching/go-common/templates"
	"github.com/quenching/sandbox-service/internal/model"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const RunnerCPUMilli int64 = 200
const RunnerMemoryBytes int64 = 512 * 1024 * 1024

func BuildResourcePlan(m tpl.Manifest) (model.ResourcePlan, error) {
	if m.Resources == nil {
		m.Resources = &tpl.ResourcesSpec{CPU: "2", Memory: "4Gi"}
	}
	if e := tpl.ValidateResourceContract(m); e != nil {
		return model.ResourcePlan{}, e
	}
	r := m.Resources
	req := tpl.ResourceAmounts{CPU: r.CPU, Memory: r.Memory}
	if r.Requests != nil {
		req = *r.Requests
	}
	c, b, _ := req.Values()
	lc, lm, _ := (tpl.ResourceAmounts{CPU: r.CPU, Memory: r.Memory}).Values()
	p := model.ResourcePlan{Requests: model.ResourceAmounts{CPUMilli: c, MemoryBytes: b}, Limits: model.ResourceAmounts{CPUMilli: lc, MemoryBytes: lm}, Components: []model.ResourceComponent{}}
	if r.Storage != "" {
		p.StorageBytes, _ = tpl.ParseBytes(r.Storage)
	}
	for _, s := range m.Services {
		if s.Resources != nil {
			c, b, _ := s.Resources.Requests.Values()
			p.Components = append(p.Components, model.ResourceComponent{Name: s.Name, Requests: model.ResourceAmounts{CPUMilli: c, MemoryBytes: b}})
		}
	}
	if len(p.Components) == 0 {
		p.Components = append(p.Components, model.ResourceComponent{Name: "template", Requests: p.Requests})
	}
	if m.BehaviorFlow != nil || m.BehaviorProtocol != nil || m.BehaviorProtocolFile != "" {
		v := model.ResourceAmounts{CPUMilli: RunnerCPUMilli, MemoryBytes: RunnerMemoryBytes}
		p.Requests = p.Requests.Add(v)
		p.Limits = p.Limits.Add(model.ResourceAmounts{CPUMilli: 1000, MemoryBytes: 1024 * 1024 * 1024})
		p.StorageBytes += 32 * 1024 * 1024
		p.Components = append(p.Components, model.ResourceComponent{Name: "behavior-runner", Requests: v})
	}
	return p, nil
}
func BuildPlannedResourceQuota(namespace string, p model.ResourcePlan) *core.ResourceQuota {
	q := BuildResourceQuota(namespace)
	q.Spec.Hard = core.ResourceList{core.ResourceRequestsCPU: *resource.NewMilliQuantity(p.Requests.CPUMilli, resource.DecimalSI), core.ResourceRequestsMemory: *resource.NewQuantity(p.Requests.MemoryBytes, resource.BinarySI), core.ResourceLimitsCPU: *resource.NewMilliQuantity(p.Limits.CPUMilli, resource.DecimalSI), core.ResourceLimitsMemory: *resource.NewQuantity(p.Limits.MemoryBytes, resource.BinarySI)}
	if p.StorageBytes > 0 {
		q.Spec.Hard[core.ResourceRequestsStorage] = *resource.NewQuantity(p.StorageBytes, resource.BinarySI)
	}
	return q
}
func (c *K8sClient) EnsureSandboxResourcesWithPlan(ctx context.Context, sid, uid string, p model.ResourcePlan) (string, error) {
	if p.Requests.CPUMilli <= 0 || p.Requests.MemoryBytes <= 0 || !p.Requests.Fits(p.Limits) {
		return "", fmt.Errorf("invalid resource plan")
	}
	// Create the planned quota before the legacy helper; it never overwrites an existing quota.
	ns, err := c.ensureSandboxNamespace(ctx, sid, uid)
	if err != nil {
		return "", err
	}
	if err := c.setSandboxAccess(ctx, ns.Name, false); err != nil {
		return "", err
	}
	want := BuildPlannedResourceQuota(ns.Name, p)
	quotas := c.clientset.CoreV1().ResourceQuotas(ns.Name)
	q, e := quotas.Get(ctx, want.Name, meta.GetOptions{})
	if isNotFound(e) {
		_, e = quotas.Create(ctx, want, meta.CreateOptions{})
	} else if e == nil {
		q.Spec = want.Spec
		_, e = quotas.Update(ctx, q, meta.UpdateOptions{})
	}
	if e != nil {
		return "", e
	}
	return c.EnsureSandboxResources(ctx, sid, uid)
}
