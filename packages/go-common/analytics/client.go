// Package analytics 服务端埋点上报客户端。
//
// 各服务在关键业务节点（注册、沙箱就绪、训练完成）调用 Track 上报事件。
// 设计约束：
//   - best-effort：异步发送、失败静默，绝不影响主业务链路；
//   - 东西向直连 review-service 的 /internal/v1 端点，不经 api-gateway；
//   - BaseURL 为空时客户端禁用，所有方法变为 no-op（本地未起 review-service 也能跑通）。
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// internalPath 服务端埋点上报路径（与 review-service 内部路由一致）。
const internalPath = "/internal/v1/analytics/events"

// defaultTimeout 单次上报超时。
const defaultTimeout = 2 * time.Second

// Config 客户端配置。
type Config struct {
	BaseURL string        // review-service 地址，如 http://review-service:8087；空=禁用
	Token   string        // 内部接口共享密钥 X-Internal-Token
	Timeout time.Duration // 单次上报超时，0 时用 2s
}

// Event 埋点事件。
//
// EventType 取值与 review-service internal/model 的事件常量保持一致
// （register / first_start / first_complete / training_complete ...）。
type Event struct {
	EventType  string                 `json:"event_type"`
	UserID     int64                  `json:"user_id"`
	SessionID  int64                  `json:"session_id,omitempty"`
	ProjectID  int64                  `json:"project_id,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	OccurredAt time.Time              `json:"occurred_at,omitempty"`
	Platform   string                 `json:"platform,omitempty"`
}

// Client 埋点上报客户端（nil 方法安全）。
type Client struct {
	url     string
	token   string
	timeout time.Duration
	http    *http.Client
}

// New 创建客户端；BaseURL 为空时返回 nil，调用方无需额外判空。
func New(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		url:     base + internalPath,
		token:   cfg.Token,
		timeout: timeout,
		http:    &http.Client{Timeout: timeout},
	}
}

// Enabled 客户端是否可用。
func (c *Client) Enabled() bool { return c != nil }

// Track 异步上报一个事件，失败静默。
// 不阻塞业务调用方；ctx 仅用于取追踪信息，已被取消也不影响发送。
func (c *Client) Track(ctx context.Context, ev Event) {
	if c == nil || ev.EventType == "" {
		return
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}

	sendCtx := context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(sendCtx, c.timeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if c.token != "" {
			req.Header.Set("X-Internal-Token", c.token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()
}
