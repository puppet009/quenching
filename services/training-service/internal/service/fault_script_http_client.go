// fault-engine 剧本详情 HTTP 客户端（架构拆库第五批）。
//
// 与 fault_defect_http_client.go 的脱敏通道严格分离：本客户端调用的端点返回
// 答案列 root_cause，仅供本服务复盘聚合仓储在服务端组装 AI 复盘输入时使用，
// 严禁用于任何用户面响应或转发给前端。
//
//	POST /internal/v1/defects/script-detail 按 ID 批量取剧本详情（含 root_cause）
//
// 复用 fault_engine.base_url 与 internal.token；JSON 字段名即契约。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// faultScriptHTTPTimeout 单次内部查询的超时上限（与脱敏通道一致）。
const faultScriptHTTPTimeout = faultDefectHTTPTimeout

// FaultScriptView 剧本详情投影，字段与 fault-engine
// repository.InternalDefectScriptView 的 JSON 标签一一对应（含 root_cause）。
type FaultScriptView struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Description string `json:"description"`
	RootCause   string `json:"root_cause"`
}

// DefectScriptReader 剧本详情只读端口（含答案列，调用方限于复盘聚合链路）。
type DefectScriptReader interface {
	// ListScriptDetails 按 ID 批量取剧本详情；缺失/已软删 ID 不占结果位。
	ListScriptDetails(ctx context.Context, ids []int64) ([]FaultScriptView, error)
}

// FaultScriptHTTPClient 基于 HTTP 的剧本详情客户端（独立于脱敏客户端）。
type FaultScriptHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewFaultScriptHTTPClient 创建剧本详情客户端。BaseURL 去尾斜杠。
func NewFaultScriptHTTPClient(cfg FaultDefectClientConfig) *FaultScriptHTTPClient {
	return &FaultScriptHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: faultScriptHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足 DefectScriptReader 端口。
var _ DefectScriptReader = (*FaultScriptHTTPClient)(nil)

// ListScriptDetails 按 ID 批量返回剧本详情；空列表不发请求。
func (c *FaultScriptHTTPClient) ListScriptDetails(ctx context.Context, ids []int64) ([]FaultScriptView, error) {
	if len(ids) == 0 {
		return []FaultScriptView{}, nil
	}
	var out []FaultScriptView
	if err := c.callScript(ctx, "/internal/v1/defects/script-detail",
		map[string]any{"ids": ids}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// callScript 发起 POST JSON 请求并按统一响应范式校验，data 反序列化到 out。
func (c *FaultScriptHTTPClient) callScript(
	ctx context.Context, path string, payload any, out any,
) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		httpReq.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用故障引擎失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取故障引擎响应失败: %w", err)
	}

	var envelope apiEnvelope
	if len(body) > 0 {
		if err := json.Unmarshal(body, &envelope); err != nil {
			return fmt.Errorf("解析故障引擎响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		if envelope.Msg != "" {
			return fmt.Errorf("故障引擎返回错误(http=%d code=%d): %s",
				resp.StatusCode, envelope.Code, envelope.Msg)
		}
		return fmt.Errorf("故障引擎返回非成功状态: http=%d", resp.StatusCode)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析故障引擎数据失败: %w", err)
		}
	}
	return nil
}
