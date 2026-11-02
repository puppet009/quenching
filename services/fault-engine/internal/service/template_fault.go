package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

func checkAccepted(d *model.Defect, b model.TemplateBinding) error {
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
	scenario, err := ParseScenario(raw)
	if err != nil {
		return err
	}
	if !autoInjectable[scenario.Kind] {
		return fmt.Errorf("当前执行器不支持此类故障")
	}
	if scenario.Spec.Scope != "pod" {
		return fmt.Errorf("模板故障缺少 Pod 隔离配置")
	}
	spec := scenario.Spec
	spec.Kind = scenario.Kind
	spec.TargetNamespace = "sandbox-1"
	spec.TargetPod = "validation-target"
	if _, err := BuildPodBladeArgs(spec); err != nil {
		return err
	}
	return nil
}

// AcceptBinding is separate from editing: edits always invalidate old evidence.

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
