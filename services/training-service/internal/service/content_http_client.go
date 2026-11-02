// content-service 项目目录只读 HTTP 客户端（架构拆库第二批）。
//
// 把原先共库直接 JOIN projects / project_services 的只读访问，替换为直连
// content-service 的内部接口（/internal/v1）：
//
//	POST /internal/v1/projects/batch            按 ID 批量取项目 id/name
//	GET  /internal/v1/projects/search?q=        按名称模糊反查项目
//	POST /internal/v1/project-services/batch    按 ID 批量取服务 id/name
//
// 两侧不发生编译期依赖（content-service 代码均在 internal/ 下），JSON 字段名即契约。
// 复用单个 http.Client（15s 超时）；任何错误向上返回，由仓储/编排层按各自口径
// 保守降级（项目名留空、关键词退化为仅故障分支）。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// contentHTTPTimeout 单次内部查询的超时上限。
const contentHTTPTimeout = 15 * time.Second

// ContentClientConfig 项目目录只读客户端配置。
type ContentClientConfig struct {
	// BaseURL content-service 根地址，如 http://content-service:8082（不带尾斜杠）。
	BaseURL string
	// Token 内部共享密钥，随 X-Internal-Token 头发送；留空则服务端不校验。
	Token string
}

// ContentProjectView 项目最小投影，字段与 content-service 内部视图 JSON 对齐。
type ContentProjectView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ContentServiceNameView 项目服务最小投影。
type ContentServiceNameView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ContentReader 跨域项目目录只读端口：仓储层依赖此抽象做回填/反查，
// 编排层与仓储均不直接访问 content-service 的库。
type ContentReader interface {
	// BatchProjects 按 ID 批量取项目最小投影（去重由服务端处理）。
	BatchProjects(ctx context.Context, ids []int64) ([]ContentProjectView, error)
	// SearchProjectByName 按名称模糊反查项目 ID/名称。
	SearchProjectByName(ctx context.Context, keyword string) ([]ContentProjectView, error)
	// BatchServiceNames 按 ID 批量取项目服务 id/name。
	BatchServiceNames(ctx context.Context, ids []int64) ([]ContentServiceNameView, error)
}

// ContentHTTPClient 基于 HTTP 的项目目录只读客户端。
type ContentHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewContentHTTPClient 创建只读客户端。BaseURL 去尾斜杠以稳定拼接路径。
func NewContentHTTPClient(cfg ContentClientConfig) *ContentHTTPClient {
	return &ContentHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: contentHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足 ContentReader 端口。
var _ ContentReader = (*ContentHTTPClient)(nil)

// BatchProjects 按 ID 批量返回项目最小投影；空列表不发请求。
func (c *ContentHTTPClient) BatchProjects(
	ctx context.Context, ids []int64,
) ([]ContentProjectView, error) {
	if len(ids) == 0 {
		return []ContentProjectView{}, nil
	}
	var out []ContentProjectView
	if err := c.call(ctx, http.MethodPost, "/internal/v1/projects/batch",
		map[string]any{"ids": ids}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchProjectByName 按名称模糊反查项目 ID/名称。
func (c *ContentHTTPClient) SearchProjectByName(
	ctx context.Context, keyword string,
) ([]ContentProjectView, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	var out []ContentProjectView
	path := "/internal/v1/projects/search?q=" + url.QueryEscape(kw)
	if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BatchServiceNames 按 ID 批量返回项目服务 id/name；空列表不发请求。
func (c *ContentHTTPClient) BatchServiceNames(
	ctx context.Context, ids []int64,
) ([]ContentServiceNameView, error) {
	if len(ids) == 0 {
		return []ContentServiceNameView{}, nil
	}
	var out []ContentServiceNameView
	if err := c.call(ctx, http.MethodPost, "/internal/v1/project-services/batch",
		map[string]any{"ids": ids}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// call 发起 JSON 请求并按统一响应范式校验，data 反序列化到 out。
// 复用 fault_defect_http_client.go 中定义的 apiEnvelope（同一 service 包）。
func (c *ContentHTTPClient) call(
	ctx context.Context, method, path string, payload any, out any,
) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用内容服务失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取内容服务响应失败: %w", err)
	}

	var envelope apiEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("解析内容服务响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		if envelope.Msg != "" {
			return fmt.Errorf("内容服务返回错误(http=%d code=%d): %s",
				resp.StatusCode, envelope.Code, envelope.Msg)
		}
		return fmt.Errorf("内容服务返回非成功状态: http=%d", resp.StatusCode)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析内容服务数据失败: %w", err)
		}
	}
	return nil
}
