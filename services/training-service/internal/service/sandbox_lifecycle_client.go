// sandbox_lifecycle_client.go —— 沙箱生命周期回写客户端（班次机制）。
//
// 一次实训 = 一个班次，"什么算活动、什么时候算结束"由 training 判定，
// 资源回收由 sandbox-service 执行，因此这里把判定结果回写给沙箱侧：
//
//	POST /internal/v1/sandboxes/:session_id/activity  刷新活跃度租期（活跃即续租）
//	POST /internal/v1/sandboxes/:session_id/complete  回写结束时刻（进入结束宽限期）
//	POST /internal/v1/sandboxes/:session_id/release   立即回收（弃班）
//
// 三个调用都是尽力而为：沙箱侧不可达时最坏退回按创建时间的兜底回收，
// 绝不阻断实训主链路。BaseURL 为空（未配置）时构造返回 nil，方法对 nil 安全。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sandboxLifecycleTimeout 单次生命周期回写超时（短：不能拖慢操作上报）。
const sandboxLifecycleTimeout = 3 * time.Second

// SandboxLifecycle 沙箱生命周期回写端口。
type SandboxLifecycle interface {
	// TouchActivity 按活跃度续租（观察/分析类操作上报后调用）。
	TouchActivity(ctx context.Context, sessionID int64) error
	// MarkCompleted 回写实训结束时刻；reason 取 completed/aborted/manual。
	MarkCompleted(ctx context.Context, sessionID int64, reason string) error
	// ReleaseNow 立即回收沙箱（弃班判定用，释放稀缺槽位）。
	ReleaseNow(ctx context.Context, sessionID int64, reason string) error
}

// SandboxLifecycleClient 基于 HTTP 的沙箱生命周期回写客户端。
type SandboxLifecycleClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewSandboxLifecycleClient 创建客户端；baseURL 为空返回 nil（未接线，调用方按 nil 降级）。
func NewSandboxLifecycleClient(baseURL, token string) *SandboxLifecycleClient {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil
	}
	return &SandboxLifecycleClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: sandboxLifecycleTimeout},
	}
}

var _ SandboxLifecycle = (*SandboxLifecycleClient)(nil)

// TouchActivity 上报一次活动；请求体为空（沙箱侧以服务端时间为准）。
func (c *SandboxLifecycleClient) TouchActivity(ctx context.Context, sessionID int64) error {
	if c == nil {
		return nil
	}
	return c.post(ctx, "/internal/v1/sandboxes/"+strconv.FormatInt(sessionID, 10)+"/activity", nil)
}

// MarkCompleted 回写结束时刻与原因。
func (c *SandboxLifecycleClient) MarkCompleted(ctx context.Context, sessionID int64, reason string) error {
	if c == nil {
		return nil
	}
	return c.post(ctx, "/internal/v1/sandboxes/"+strconv.FormatInt(sessionID, 10)+"/complete",
		map[string]string{"reason": reason})
}

// ReleaseNow 立即回收沙箱。
func (c *SandboxLifecycleClient) ReleaseNow(ctx context.Context, sessionID int64, reason string) error {
	if c == nil {
		return nil
	}
	return c.post(ctx, "/internal/v1/sandboxes/"+strconv.FormatInt(sessionID, 10)+"/release",
		map[string]string{"reason": reason})
}

func (c *SandboxLifecycleClient) post(ctx context.Context, path string, payload any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
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
		Code int    `json:"code"`
		Msg  string `json:"msg"`
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
	return nil
}

// CleanupBrainSession requires namespace disappearance, not merely a successful DELETE request.
func (c *SandboxLifecycleClient) CleanupBrainSession(ctx context.Context, sid int64) (string, error) {
	if c == nil || c.token == "" {
		return "pending", fmt.Errorf("sandbox cleanup not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/sandboxes/"+strconv.FormatInt(sid, 10)+"/brain-cleanup", nil)
	if err != nil {
		return "pending", err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "pending", err
	}
	defer resp.Body.Close()
	var body struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body); err != nil {
		return "pending", err
	}
	if resp.StatusCode != http.StatusOK || body.Code != 0 {
		return "pending", fmt.Errorf("cleanup request failed: HTTP %d", resp.StatusCode)
	}
	if body.Data.Status != "pending" && body.Data.Status != "succeeded" {
		return "pending", fmt.Errorf("invalid cleanup response")
	}
	return body.Data.Status, nil
}

func (c *SandboxLifecycleClient) SealOperations(ctx context.Context, sid int64) (oe.AuditSeal, error) {
	var seal oe.AuditSeal
	if c == nil || c.token == "" {
		return seal, fmt.Errorf("audit seal unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/internal/v1/sandboxes/"+strconv.FormatInt(sid, 10)+"/audit-seal", nil)
	if err != nil {
		return seal, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return seal, err
	}
	defer resp.Body.Close()
	var body struct {
		Code int          `json:"code"`
		Data oe.AuditSeal `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body); err != nil {
		return seal, err
	}
	if resp.StatusCode != 200 || body.Code != 0 || body.Data.SessionID != sid || body.Data.Producer != "sandbox" {
		return seal, fmt.Errorf("invalid audit seal")
	}
	return body.Data, nil
}

func (c *SandboxLifecycleClient) ReadyForUser(ctx context.Context, sid, uid int64) (bool, error) {
	if c == nil || c.token == "" {
		return false, fmt.Errorf("sandbox readiness unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/internal/v1/sandboxes/%d/readiness?user_id=%d", c.baseURL, sid, uid), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Ready bool `json:"ready"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&envelope) != nil || envelope.Code != 0 {
		return false, fmt.Errorf("sandbox readiness unavailable")
	}
	return envelope.Data.Ready, nil
}
