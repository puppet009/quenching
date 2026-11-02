package service

import (
	"context"
	"errors"
	"github.com/quenching/sandbox-service/internal/model"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

func (c *ElasticController) SetEnabled(ctx context.Context, ns, name string, version int64, enabled bool) error {
	return c.Store.WithElasticLock(ctx, true, func(ctx context.Context) error {
		state, e := c.Store.ElasticState(ctx)
		if e != nil {
			return e
		}
		for _, p := range state.Policies {
			if p.Namespace == ns && p.Name == name {
				if p.Version != version {
					return ErrResourceConflict
				}
				if p.Pending != nil {
					return errors.New("等待当前资源调整完成后再修改策略")
				}
				if enabled {
					d, e := c.Client.AppsV1().Deployments(ns).Get(ctx, name, meta.GetOptions{})
					if e != nil {
						return e
					}
					if !safeElasticDeployment(d) || string(d.UID) != p.UID || deploymentTarget(d) != p.Current || amounts(d.Spec.Template.Spec.Containers[0].Resources.Limits) != p.Limits {
						return errors.New("工作负载配置已改变，请同步部署配置后重新验收策略")
					}
					if e = c.external(ctx, ns); e != nil {
						return e
					}
				}
				p.Enabled = enabled
				p.LastChanged = time.Now().UTC()
				delete(c.samples, ns+"/"+name)
				if e = c.Store.SaveElasticPolicy(ctx, &p, nil); e != nil {
					return e
				}
				return c.Store.AuditElastic(ctx, model.ElasticAudit{Namespace: ns, Name: name, Action: "policy", Detail: map[bool]string{true: "管理员启用", false: "管理员关闭"}[enabled]})
			}
		}
		return errors.New("策略不存在")
	})
}
func (c *ElasticController) ManualWorkloadUpdate(ctx context.Context, ns, name string, update func(context.Context) error) error {
	return c.Store.WithElasticLock(ctx, true, func(ctx context.Context) error {
		state, e := c.Store.ElasticState(ctx)
		if e != nil {
			return e
		}
		for _, p := range state.Policies {
			if p.Namespace == ns && p.Name == name {
				if p.Pending != nil {
					return errors.New("自动调整尚未完成，请等待收敛")
				}
				p.Enabled = false
				p.Reason = "管理员手工修改资源，自动策略已关闭"
				delete(c.samples, ns+"/"+name)
				if e = c.Store.SaveElasticPolicy(ctx, &p, nil); e != nil {
					return e
				}
			}
		}
		return update(ctx)
	})
}
