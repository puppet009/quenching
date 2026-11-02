// fault-engine 故障目录内部 HTTP 客户端（架构拆库第二批）。
//
// 把原先共库直接 SELECT content_defects 表的用户面只读访问，替换为直连 fault-engine
// 的内部接口（/internal/v1）：
//
//	GET /internal/v1/defects/project/:id 项目启用中的故障目录（含展示列）
//
// 两侧不发生编译期依赖（fault-engine 代码均在 internal/ 下），JSON 字段名即契约。
// 复用单个 http.Client（15s 超时）；任何错误向上返回，由 handler 侧按既有口径
// 返回内部查询失败。
//
// 脱敏红线：本文件只反序列化白名单展示列，不得新增 root_cause / fix_config /
// trigger_config 等答案或注入字段的结构定义。
package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// faultDefectHTTPTimeout 单次内部查询的超时上限。
const faultDefectHTTPTimeout = 15 * time.Second

// faultDefectView 故障投影，JSON 字段与 fault-engine InternalDefectView 一一对应。
type faultDefectView struct {
	ID              int64   `json:"id"`
	ProjectID       int64   `json:"project_id"`
	Code            string  `json:"code"`
	Name            string  `json:"name"`
	DefectType      string  `json:"defect_type"`
	Category        string  `json:"category"`
	Difficulty      string  `json:"difficulty"`
	Description     string  `json:"description"`
	TargetServiceID int64   `json:"target_service_id"`
	ScoreWeight     float64 `json:"score_weight"`
	EstimatedFixMin int32   `json:"estimated_fix_time"`
	SortOrder       int32   `json:"sort_order"`
}

// faultEnvelope 内部接口统一响应范式；用 RawMessage 延迟解析 data。
type faultEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// FaultDefectClient 基于 HTTP 的 fault-engine 故障目录只读客户端。
type FaultDefectClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewFaultDefectClient 创建只读客户端。BaseURL 去尾斜杠以稳定拼接路径。
func NewFaultDefectClient(baseURL, token string) *FaultDefectClient {
	return &FaultDefectClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: faultDefectHTTPTimeout},
	}
}

// ListActiveByProject 返回项目下启用中的故障目录投影。
func (c *FaultDefectClient) ListActiveByProject(
	ctx context.Context, projectID int64,
) ([]*faultDefectView, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("项目ID非法")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/internal/v1/defects/project/%d", c.baseURL, projectID), nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	if c.token != "" {
		httpReq.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("调用故障引擎失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取故障引擎响应失败: %w", err)
	}

	var envelope faultEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("解析故障引擎响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		if envelope.Msg != "" {
			return nil, fmt.Errorf("故障引擎返回错误(http=%d code=%d): %s",
				resp.StatusCode, envelope.Code, envelope.Msg)
		}
		return nil, fmt.Errorf("故障引擎返回非成功状态: http=%d", resp.StatusCode)
	}

	out := make([]*faultDefectView, 0)
	if len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, &out); err != nil {
			return nil, fmt.Errorf("解析故障引擎数据失败: %w", err)
		}
	}
	return out, nil
}
