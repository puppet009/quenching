package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *RuntimeService) SetTemplateTargetResolver(resolve func(context.Context, int64, string, string) (string, error)) {
	s.resolveTemplateTarget = resolve
}
func TemplateTargetResolver(base, token string) func(context.Context, int64, string, string) (string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	return func(ctx context.Context, sid int64, slug, workload string) (string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/internal/v1/sandboxes/%d/targets", strings.TrimRight(base, "/"), sid), nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("X-Internal-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		var result struct {
			Code int `json:"code"`
			Data []struct {
				Name         string `json:"name"`
				Workload     string `json:"workload"`
				TemplateSlug string `json:"template_slug"`
				Ready        bool   `json:"ready"`
			} `json:"data"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil || result.Code != 0 {
			return "", fmt.Errorf("沙箱目标不可用")
		}
		matched := ""
		for _, t := range result.Data {
			if t.TemplateSlug == slug && t.Workload == workload && t.Ready {
				if matched != "" {
					return "", fmt.Errorf("多个就绪目标，需明确适配选择规则")
				}
				matched = t.Name
			}
		}
		if matched == "" {
			return "", fmt.Errorf("当前沙箱不存在模板绑定的就绪目标")
		}
		return matched, nil
	}
}

func (s *RuntimeService) SetTemplateResolver(resolve func(context.Context, string) ([]string, error)) {
	s.resolveTemplate = resolve
}
