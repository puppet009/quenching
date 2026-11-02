// fault-engine 故障目录只读 HTTP 客户端（架构拆库第一批）。
//
// 把原先共库直接 SELECT defects 表的只读访问，替换为直连 fault-engine 的
// 内部接口（/internal/v1），与次生故障执行通道 FaultEngineHTTPClient 共用
// fault_engine.base_url，但职责仅限只读：
//
//	POST /internal/v1/defects/batch       按 ID 批量取脱敏故障
//	GET  /internal/v1/defects/project/:id 项目启用中的故障目录
//	GET  /internal/v1/defects/search?q=   按名称模糊反查故障 ID
//
// 两侧不发生编译期依赖（fault-engine 代码均在 internal/ 下），JSON 字段名即契约。
// 复用单个 http.Client（连接池），单次调用 15s 超时；任何错误向上返回，
// 由调用方/编排层按各自口径保守降级（对话跳过上下文、卡顿弃权、配对报错）。
//
// 脱敏红线：fault-engine 内部接口本就只投影白名单列，本文件不得新增任何
// root_cause / fix_config / trigger_config 等答案或注入字段的结构定义。
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

// faultDefectHTTPTimeout 单次内部查询的超时上限。
const faultDefectHTTPTimeout = 15 * time.Second

// FaultDefectClientConfig 故障目录只读客户端配置。
type FaultDefectClientConfig struct {
	// BaseURL fault-engine 根地址，如 http://fault-engine:8088（不带结尾斜杠）。
	BaseURL string
	// Token 内部共享密钥，随 X-Internal-Token 头发送；留空则服务端不校验。
	Token string
}

// FaultDefectView 故障脱敏投影，字段与 fault-engine repository.InternalDefectView
// 的 JSON 标签一一对应；只含本服务编排所需的白名单列。
type FaultDefectView struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"project_id"`
	Code            string `json:"code"`
	Name            string `json:"name"`
	DefectType      string `json:"defect_type"`
	Category        string `json:"category"`
	Difficulty      string `json:"difficulty"`
	Description     string `json:"description"`
	TargetServiceID int64  `json:"target_service_id"`
	IsActive        bool   `json:"is_active"`
}

// FaultDefectName 名称反查结果（id/name）。
type FaultDefectName struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// DefectReader 跨域故障目录只读端口：仓储层依赖此抽象做回填/反查，
// 编排层与仓储均不直接访问 fault-engine 的库。
type DefectReader interface {
	// BatchDefects 按 ID 批量取脱敏故障（去重由服务端处理）。
	BatchDefects(ctx context.Context, ids []int64) ([]FaultDefectView, error)
	// ListProjectDefects 取项目下启用中的故障目录。
	ListProjectDefects(ctx context.Context, projectID int64) ([]FaultDefectView, error)
	// SearchDefectByName 按名称模糊反查故障 ID/名称。
	SearchDefectByName(ctx context.Context, keyword string) ([]FaultDefectName, error)
}

// FaultDefectHTTPClient 基于 HTTP 的故障目录只读客户端。
type FaultDefectHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewFaultDefectHTTPClient 创建只读客户端。BaseURL 去尾斜杠以稳定拼接路径。
func NewFaultDefectHTTPClient(cfg FaultDefectClientConfig) *FaultDefectHTTPClient {
	return &FaultDefectHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		http:    &http.Client{Timeout: faultDefectHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足 DefectReader 端口。
var _ DefectReader = (*FaultDefectHTTPClient)(nil)

// apiEnvelope 内部接口统一响应范式：code=0 成功；用 RawMessage 延迟解析 data。
type apiEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// BatchDefects 按 ID 批量返回脱敏故障；空列表不发请求直接返回空切片。
func (c *FaultDefectHTTPClient) BatchDefects(ctx context.Context, ids []int64) ([]FaultDefectView, error) {
	if len(ids) == 0 {
		return []FaultDefectView{}, nil
	}
	var out []FaultDefectView
	if err := c.call(ctx, http.MethodPost, "/internal/v1/defects/batch",
		map[string]any{"ids": ids}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjectDefects 返回项目下启用中的故障目录。
func (c *FaultDefectHTTPClient) ListProjectDefects(ctx context.Context, projectID int64) ([]FaultDefectView, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("项目ID非法")
	}
	var out []FaultDefectView
	if err := c.call(ctx, http.MethodGet,
		fmt.Sprintf("/internal/v1/defects/project/%d", projectID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchDefectByName 按名称模糊反查故障 ID/名称。
func (c *FaultDefectHTTPClient) SearchDefectByName(ctx context.Context, keyword string) ([]FaultDefectName, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	var out []FaultDefectName
	path := "/internal/v1/defects/search?q=" + url.QueryEscape(kw)
	if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// call 发起 JSON 请求并按统一响应范式校验，data 反序列化到 out。
// payload 为 nil 时不携带请求体（GET）；out 为 nil 时只校验业务码。
func (c *FaultDefectHTTPClient) call(
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
		return fmt.Errorf("调用故障引擎失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取故障引擎响应失败: %w", err)
	}

	var envelope apiEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
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
