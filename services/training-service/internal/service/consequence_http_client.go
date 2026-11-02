// HTTP 版次生故障执行通道（T3-12）。
//
// 把 ConsequenceExecutor 翻译为 fault-engine 运行时 API（内网调用，无鉴权）：
//
//	POST {base}/api/v1/runtime/inject   临时注入一次
//	POST {base}/api/v1/runtime/revoke   按 uid 撤销
//	POST {base}/api/v1/runtime/sessions/:id/stop  会话结束兜底（剧本故障一并撤销）
//
// 两侧不发生编译期依赖（fault-engine 代码均在 internal/ 下），JSON 字段名即契约。
// 客户端复用一个 http.Client（连接池），单次调用 15s 超时；任何错误向上返回，
// 由编排层（applyConsequence / finish 钩子）决定只记日志、不阻断训练主链路。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// consequenceHTTPTimeout 单次运行时调用的超时上限（注入/撤销均含 blade 动作）。
const consequenceHTTPTimeout = 15 * time.Second

// FaultEngineClientConfig HTTP 执行通道配置。
type FaultEngineClientConfig struct {
	// BaseURL fault-engine 运行时根地址，如 http://fault-engine:8088（不带结尾斜杠）。
	BaseURL string
	Token   string
	// DefaultNamespace 请求未显式指定命名空间时的兜底（取 observability.namespace）。
	DefaultNamespace string
	// DefaultPod 目标 Pod 全局兜底（如命名空间内固定的单一业务 Pod）；通常留空。
	DefaultPod string
}

// FaultSessionStopper 故障引擎会话结束通知端口。
// 仅暴露会话级停止能力，避免会话结束钩子依赖完整的 ConsequenceExecutor 契约。
type FaultSessionStopper interface {
	// StopSession 停止会话剧本编排并撤销该会话全部活跃实验（幂等）。
	StopSession(ctx context.Context, sessionID int64) error
}

// FaultEngineHTTPClient 基于 HTTP 的次生故障执行通道，同时实现 ConsequenceExecutor。
// 另提供 StopSession 供会话结束钩子调用（停剧本编排并撤销该会话全部活跃实验）。
type FaultEngineHTTPClient struct {
	baseURL string
	token   string
	defNS   string
	defPod  string
	http    *http.Client
}

// NewFaultEngineHTTPClient 创建执行通道。BaseURL 去尾斜杠以稳定拼接路径。
func NewFaultEngineHTTPClient(cfg FaultEngineClientConfig) *FaultEngineHTTPClient {
	return &FaultEngineHTTPClient{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		token:   cfg.Token,
		defNS:   cfg.DefaultNamespace,
		defPod:  cfg.DefaultPod,
		http:    &http.Client{Timeout: consequenceHTTPTimeout},
	}
}

// 编译期断言：HTTP 客户端满足执行通道接口与会话结束通知端口。
var (
	_ ConsequenceExecutor = (*FaultEngineHTTPClient)(nil)
	_ FaultSessionStopper = (*FaultEngineHTTPClient)(nil)
)

// faultRuntimeEnvelope 与 fault-engine 统一响应范式对齐：code=0 成功。
type faultRuntimeEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		// ExperimentUID blade create 返回的实验句柄（撤销凭据）。
		ExperimentUID string `json:"experiment_uid"`
	} `json:"data"`
}

// injectPayload 临时注入请求体，字段与 fault-engine service.InjectOnceReq 对齐。
type injectPayload struct {
	SessionID        int64  `json:"session_id"`
	Kind             string `json:"kind"`
	Namespace        string `json:"namespace"`
	Pod              string `json:"pod,omitempty"`
	NetworkInterface string `json:"network_interface,omitempty"`
	DelayMillis      int    `json:"delay_millis,omitempty"`
	LossPercent      int    `json:"loss_percent,omitempty"`
	CPUPercent       int    `json:"cpu_percent,omitempty"`
	MemPercent       int    `json:"mem_percent,omitempty"`
	DiskPercent      int    `json:"disk_percent,omitempty"`
}

// revokePayload 撤销请求体，字段与 fault-engine handler.revokeReq 对齐。
type revokePayload struct {
	SessionID int64  `json:"session_id"`
	UID       string `json:"uid"`
}

// Inject 调用故障引擎临时注入一次故障，返回 blade 实验 uid。
func (c *FaultEngineHTTPClient) Inject(ctx context.Context, req ConsequenceInjectRequest) (string, error) {
	if req.SessionID <= 0 {
		return "", fmt.Errorf("会话ID不能为空")
	}
	if req.Kind == "" {
		return "", fmt.Errorf("故障类型不能为空")
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = c.defNS
	}
	pod := req.Pod
	if pod == "" {
		pod = c.defPod
	}

	body := injectPayload{
		SessionID:   req.SessionID,
		Kind:        req.Kind,
		Namespace:   namespace,
		Pod:         pod,
		DelayMillis: req.DelayMillis,
		LossPercent: req.LossPercent,
		CPUPercent:  req.CPUPercent,
		MemPercent:  req.MemPercent,
		DiskPercent: req.DiskPercent,
		// DurationSec 不透传：临时链路由平台掌控恢复（会话结束统一撤销）。
	}

	var resp faultRuntimeEnvelope
	if err := c.post(ctx, "/api/v1/runtime/inject", body, &resp); err != nil {
		return "", err
	}
	if resp.Data.ExperimentUID == "" {
		return "", fmt.Errorf("故障引擎未返回 experiment_uid")
	}
	return resp.Data.ExperimentUID, nil
}

// Revoke 按实验 uid 撤销一次已注入的次生故障。
func (c *FaultEngineHTTPClient) Revoke(ctx context.Context, sessionID int64, experimentUID string) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID不能为空")
	}
	if experimentUID == "" {
		return fmt.Errorf("实验 uid 不能为空")
	}
	return c.post(ctx, "/api/v1/runtime/revoke", revokePayload{
		SessionID: sessionID,
		UID:       experimentUID,
	}, nil)
}

// StopSession 通知故障引擎结束会话：停止剧本编排并补偿撤销该会话全部活跃实验。
// 由 training 侧 finish 钩子在本地次生故障撤销之后调用；幂等，编排不存在也成功。
func (c *FaultEngineHTTPClient) StopSession(ctx context.Context, sessionID int64) error {
	if sessionID <= 0 {
		return fmt.Errorf("会话ID不能为空")
	}
	return c.post(ctx, fmt.Sprintf("/api/v1/runtime/sessions/%d/stop", sessionID), nil, nil)
}

// post 发起 JSON POST 并按统一响应范式校验。out 为 nil 时只校验业务码不解析 data。
func (c *FaultEngineHTTPClient) post(ctx context.Context, path string, payload any, out *faultRuntimeEnvelope) error {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化请求失败: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	httpReq.Header.Set("X-Internal-Token", c.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("调用故障引擎失败: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取故障引擎响应失败: %w", err)
	}

	var envelope faultRuntimeEnvelope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("解析故障引擎响应失败(http=%d): %w", resp.StatusCode, err)
		}
	}
	if resp.StatusCode != http.StatusOK || envelope.Code != 0 {
		// 优先回传引擎给出的业务错误信息，便于编排层日志定位。
		if envelope.Msg != "" {
			return fmt.Errorf("故障引擎返回错误(http=%d code=%d): %s", resp.StatusCode, envelope.Code, envelope.Msg)
		}
		return fmt.Errorf("故障引擎返回非成功状态: http=%d", resp.StatusCode)
	}
	if out != nil {
		*out = envelope
	}
	return nil
}
