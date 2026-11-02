// alert_forwarder.go 向 training-service 转发前端信号 / 注册默认规则（第三批）。
//
// training-service 是告警引擎的唯一归属方：sandbox-service 只负责把 runner
// 真实上报的浏览器侧事件异步转交，以及在沙箱就绪时触发默认规则注册。
//
// BaseURL 留空时整体不启用：不转发、不注册（本地 fake 降级环境），
// 绝不因此生成任何模拟信号。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AlertClient training-service 告警内部接口客户端。
type AlertClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAlertClient 创建告警客户端。baseURL 为空返回 nil（调用方据此跳过装配）。
func NewAlertClient(baseURL, token string) *AlertClient {
	if baseURL == "" {
		return nil
	}
	return &AlertClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// Enabled 是否配置了 training-service 地址。
func (c *AlertClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// Forward 转发一条行为信号到 /api/v1/internal/alerts/events。
// 失败只返回错误由调用方记日志，不重试、不阻塞汇聚存储。
func (c *AlertClient) Forward(ctx context.Context, sig BehaviorSignal) error {
	body, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("序列化信号失败: %w", err)
	}
	return c.post(ctx, "/api/v1/internal/alerts/events", body)
}

// Register 触发沙箱默认告警规则幂等注册。
func (c *AlertClient) Register(ctx context.Context, sandboxID int64) error {
	body, err := json.Marshal(map[string]int64{"sandbox_id": sandboxID})
	if err != nil {
		return fmt.Errorf("序列化注册请求失败: %w", err)
	}
	return c.post(ctx, "/api/v1/internal/alerts/register", body)
}

// post 发送内部 POST 请求，统一携带 X-Internal-API-Key。
func (c *AlertClient) post(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Internal-API-Key", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 training-service 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("training-service 返回非预期状态码 %d", resp.StatusCode)
	}
	return nil
}
