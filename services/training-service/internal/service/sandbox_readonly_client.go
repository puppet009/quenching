// sandbox_readonly_client.go 调 sandbox-service 内部接口的客户端（Agent M3/M4）。
//
// training-service 在编排助手工具时，经此客户端在某会话沙箱内做只读观察，或在
// 用户审批通过后执行写文件：
//   - Targets：列出业务服务器（Pod）
//   - ListFiles / ReadFile：目录与文件内容
//   - Exec：执行白名单只读诊断命令（ps/ss/netstat …）
//   - WriteFile（M4）：写文件，仅在用户批准后由工具执行器调用
//
// 走 sandbox-service 的 /internal/v1/sandboxes/:id/* 端点，携带 X-Internal-Token。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// sandboxInternalTimeout 单次内部调用超时。
const sandboxInternalTimeout = 15 * time.Second

// ErrSandboxFileNotFound 读文件时目标文件不存在（由 sandbox-service 读失败映射）。
// 供写前快照区分"新建文件"与"覆盖已有文件"。
var ErrSandboxFileNotFound = errors.New("sandbox 文件不存在")

// SandboxReadonlyClient 基于 HTTP 的 sandbox-service 只读客户端。
type SandboxReadonlyClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// 编译期断言。
var _ SandboxReadonly = (*SandboxReadonlyClient)(nil)

// NewSandboxReadonlyClient 创建客户端，BaseURL 去尾斜杠。
func NewSandboxReadonlyClient(baseURL, token string) *SandboxReadonlyClient {
	return &SandboxReadonlyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: sandboxInternalTimeout},
	}
}

// SandboxTarget 业务服务器（与 sandbox-service ServerTarget 对齐，只取工具需要的字段）。
type SandboxTarget struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Phase       string `json:"phase"`
	Ready       bool   `json:"ready"`
}

// SandboxFileEntry 目录条目（精简投影）。
type SandboxFileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
}

// ExecSpec 只读命令执行参数。
type ExecSpec struct {
	Pod       string
	Container string
	Command   string
	Args      []string
}

// SandboxReadonly 客户端端口（便于单测 fake）。
type SandboxReadonly interface {
	Targets(ctx context.Context, sessionID int64) ([]SandboxTarget, error)
	ListFiles(ctx context.Context, sessionID int64, pod, container, path string) ([]SandboxFileEntry, error)
	ReadFile(ctx context.Context, sessionID int64, pod, container, path string) (string, error)
	Exec(ctx context.Context, sessionID int64, spec ExecSpec) (string, error)
	// WriteFile 写文件（M4）：调用方必须已取得用户审批。
	WriteFile(ctx context.Context, sessionID int64, pod, container, path, content string) error
}

func (c *SandboxReadonlyClient) prefix(sessionID int64) string {
	return fmt.Sprintf("/internal/v1/sandboxes/%d", sessionID)
}

// Targets 列出业务服务器。
func (c *SandboxReadonlyClient) Targets(ctx context.Context, sessionID int64) ([]SandboxTarget, error) {
	var out []SandboxTarget
	if err := c.call(ctx, http.MethodGet, c.prefix(sessionID)+"/targets", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListFiles 列目录。
func (c *SandboxReadonlyClient) ListFiles(
	ctx context.Context, sessionID int64, pod, container, path string,
) ([]SandboxFileEntry, error) {
	q := buildFileQuery(pod, container, path)
	var entries []SandboxFileEntry
	if err := c.call(ctx, http.MethodGet, c.prefix(sessionID)+"/files"+q, nil, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// ReadFile 读文件。
func (c *SandboxReadonlyClient) ReadFile(
	ctx context.Context, sessionID int64, pod, container, path string,
) (string, error) {
	q := buildFileQuery(pod, container, path)
	var data struct {
		Content string `json:"content"`
	}
	if err := c.call(ctx, http.MethodGet, c.prefix(sessionID)+"/files/read"+q, nil, &data); err != nil {
		// cat 对不存在文件报 "No such file or directory"，映射为哨兵便于快照区分。
		if strings.Contains(err.Error(), "No such file or directory") {
			return "", ErrSandboxFileNotFound
		}
		return "", err
	}
	return data.Content, nil
}

// Exec 执行只读诊断命令。
func (c *SandboxReadonlyClient) Exec(ctx context.Context, sessionID int64, spec ExecSpec) (string, error) {
	body := map[string]any{
		"pod":       spec.Pod,
		"container": spec.Container,
		"command":   spec.Command,
		"args":      spec.Args,
	}
	var data struct {
		Output string `json:"output"`
	}
	if err := c.call(ctx, http.MethodPost, c.prefix(sessionID)+"/exec", body, &data); err != nil {
		return "", err
	}
	return data.Output, nil
}

// WriteFile 写文件（M4）：仅在用户审批通过后调用。
func (c *SandboxReadonlyClient) WriteFile(
	ctx context.Context, sessionID int64, pod, container, path, content string,
) error {
	body := map[string]any{
		"pod":       pod,
		"container": container,
		"path":      path,
		"content":   content,
	}
	return c.call(ctx, http.MethodPost, c.prefix(sessionID)+"/files/write", body, nil)
}

// buildFileQuery 拼文件接口 query（path 必填，pod/container 可选）。
func buildFileQuery(pod, container, path string) string {
	v := url.Values{}
	v.Set("path", path)
	if pod != "" {
		v.Set("pod", pod)
	}
	if container != "" {
		v.Set("container", container)
	}
	return "?" + v.Encode()
}

// call 发起内部 JSON 请求，按统一响应 {code,msg,data} 校验并把 data 解到 out。
// payload 为 nil 时不带 body；out 为 nil 时只校验业务码。
func (c *SandboxReadonlyClient) call(
	ctx context.Context, method, path string, payload any, out any,
) error {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		reader = strings.NewReader(string(raw))
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("调用 sandbox-service 失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("解析响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		if envelope.Msg != "" {
			return fmt.Errorf("sandbox-service 返回错误(http=%d code=%d): %s",
				resp.StatusCode, envelope.Code, envelope.Msg)
		}
		return fmt.Errorf("sandbox-service 非成功状态: http=%d", resp.StatusCode)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("解析 data 失败: %w", err)
		}
	}
	return nil
}
