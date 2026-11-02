package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/go-common/templates"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *DefectService) SetTemplateResolver(resolve func(context.Context, string) ([]string, error)) {
	s.resolveTemplate = resolve
}

func RegistryTemplateResolver(registry templates.Registry) func(context.Context, string) ([]string, error) {
	return func(_ context.Context, slug string) ([]string, error) {
		if registry == nil {
			return nil, fmt.Errorf("模板注册表未配置")
		}
		manifest, ok := registry.Get(slug)
		if !ok {
			return nil, fmt.Errorf("模板不存在或已停用")
		}
		out := make([]string, 0, len(manifest.Services))
		for _, component := range manifest.Services {
			out = append(out, component.Workload)
		}
		return out, nil
	}
}

func TemplateResolver(baseURL string) func(context.Context, string) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	return func(ctx context.Context, slug string) ([]string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/api/v1/templates/"+url.PathEscape(slug), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var result struct {
			Code int `json:"code"`
			Data *struct {
				Services []struct {
					Workload string `json:"workload"`
				} `json:"services"`
			} `json:"data"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.Code != 0 || result.Data == nil {
			return nil, fmt.Errorf("模板不存在或内容服务不可用")
		}
		workloads := make([]string, 0, len(result.Data.Services))
		for _, svc := range result.Data.Services {
			workloads = append(workloads, svc.Workload)
		}
		return workloads, nil
	}
}

func validateFields(d *model.Defect) error {
	if d.ProjectID < 0 || (d.ProjectID == 0 && len(d.TemplateBindings) == 0) {
		return fmt.Errorf("模板故障必须保留至少一个绑定")
	}
	if !map[string]bool{"code": true, "config": true, "architecture": true, "resource": true}[d.DefectType] {
		return fmt.Errorf("故障类型非法")
	}
	if !map[string]bool{"easy": true, "medium": true, "hard": true}[d.Difficulty] {
		return fmt.Errorf("难度非法")
	}
	if d.Name == "" || len(d.Name) > 600 || len(d.Code) > 100 || d.ScoreWeight <= 0 || d.ScoreWeight > 100 || d.EstimatedFixTime < 0 {
		return fmt.Errorf("名称、编码、权重或预计时间非法")
	}
	return nil
}

func (s *DefectService) validateBindings(ctx context.Context, d *model.Defect) error {
	seen := map[string]bool{}
	for _, b := range d.TemplateBindings {
		if b.TemplateSlug == "" || seen[b.TemplateSlug] {
			return fmt.Errorf("模板不能为空或重复绑定")
		}
		seen[b.TemplateSlug] = true
		if s.resolveTemplate == nil {
			return fmt.Errorf("模板查询服务未配置")
		}
		targets, err := s.resolveTemplate(ctx, b.TemplateSlug)
		if err != nil {
			return err
		}
		valid := false
		for _, target := range targets {
			if b.TargetWorkload == target {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("模板 %s 不存在目标 %s", b.TemplateSlug, b.TargetWorkload)
		}
		if b.Status != "draft" && b.Status != "pending_validation" && b.Status != "verified" {
			return fmt.Errorf("验收状态非法")
		}
		if b.Status == "verified" {
			if err := s.checkAccepted(ctx, d, b); err != nil {
				return err
			}
		}
		if d.IsActive && b.Status != "verified" {
			return fmt.Errorf("故障尚未通过服务器验收，不能启用")
		}
	}
	return nil
}

func (s *DefectService) checkAccepted(ctx context.Context, d *model.Defect, b model.TemplateBinding) error {
	if b.Executor != "pod_chaosblade" {
		return fmt.Errorf("该故障适配器尚未实现，不能标记可训练")
	}
	source, ok := d.Provenance["url"].(string)
	u, err := url.Parse(source)
	if !ok || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("缺少真实来源 URL")
	}
	kind, _ := d.Provenance["kind"].(string)
	if kind != "incident" && kind != "issue" {
		return fmt.Errorf("正式验收需要具体事故或真实 Issue 来源")
	}
	for _, key := range []string{"observed", "repaired", "revoked", "cleanup", "validated_at"} {
		v, ok := b.Evidence[key].(string)
		if !ok || strings.TrimSpace(v) == "" {
			return fmt.Errorf("缺少验收证据 %s", key)
		}
	}
	cfg := d.AmplifierConfig
	if len(b.Overrides) > 0 {
		cfg = b.Overrides
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if s.validateExecution == nil {
		return fmt.Errorf("执行器校验未配置")
	}
	return s.validateExecution(ctx, raw, true)
}

// AcceptBinding is separate from editing: edits always invalidate old evidence.
func (s *DefectService) AcceptBinding(ctx context.Context, id int64, slug string, evidence model.JSONMap) error {
	d, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	copyDefect := *d
	copyDefect.TemplateBindings = append([]model.TemplateBinding(nil), d.TemplateBindings...)
	found := false
	for i := range copyDefect.TemplateBindings {
		if copyDefect.TemplateBindings[i].TemplateSlug == slug {
			copyDefect.TemplateBindings[i].Status = "verified"
			copyDefect.TemplateBindings[i].Evidence = evidence
			found = true
		}
	}
	if !found {
		return fmt.Errorf("未绑定该模板")
	}
	if err := s.validateBindings(ctx, &copyDefect); err != nil {
		return err
	}
	return s.repo.Update(ctx, &copyDefect)
}

func TemplateRevisionResolver(baseURL string) func(context.Context, string) (string, error) {
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, slug string) (string, error) {
		req, e := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(baseURL, "/")+"/api/v1/templates/"+url.PathEscape(slug), nil)
		if e != nil {
			return "", e
		}
		res, e := client.Do(req)
		if e != nil {
			return "", e
		}
		defer res.Body.Close()
		var reply struct {
			Code int `json:"code"`
			Data struct {
				Digest string `json:"artifact_digest"`
			} `json:"data"`
		}
		if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&reply) != nil || reply.Code != 0 || len(reply.Data.Digest) != 64 {
			return "", fmt.Errorf("template revision unavailable")
		}
		return reply.Data.Digest, nil
	}
}
