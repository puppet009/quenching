// Package eventbus 事件总线客户端（T1-19 事件订阅分发）。
//
// 封装 WS Gateway 的事件发布 HTTP API，提供业务友好的事件发布接口。
// 其他模块通过此客户端发布事件，无需关心 WS Gateway 的细节。
package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EventType 事件类型（与 ws-gateway 保持一致）。
type EventType string

const (
	// 系统事件
	EventHello EventType = "hello"
	EventError EventType = "error"
	EventClose EventType = "close"

	// 业务事件
	EventMetrics      EventType = "metrics"       // 实时指标
	EventAlert        EventType = "alert"         // 告警
	EventStatusChange EventType = "status_change" // 状态变更
	EventAIMessage    EventType = "ai_message"    // AI 消息推送
	EventScoreUpdate  EventType = "score_update"  // 评分更新
	EventLogLine      EventType = "log_line"      // 日志行
)

// TargetType 发布目标类型。
type TargetType string

const (
	TargetChannel   TargetType = "channel"   // 频道
	TargetUser      TargetType = "user"      // 指定用户
	TargetBroadcast TargetType = "broadcast" // 全员广播
)

// ——— 标准事件数据结构 ———

// MetricsEventData 实时指标事件数据。
//
// 用于 T2-19 实时图表组件，定期推送指标快照。
type MetricsEventData struct {
	SandboxID string        `json:"sandbox_id"`
	Timestamp int64         `json:"timestamp"`
	Metrics   []MetricPoint `json:"metrics"`
}

// MetricPoint 单个指标数据点。
type MetricPoint struct {
	Name    string  `json:"name"`    // 指标名：cpu_usage/memory_usage/qps/error_rate/p99_latency
	Value   float64 `json:"value"`   // 指标值
	Unit    string  `json:"unit"`    // 单位
	Service string  `json:"service"` // 服务名（空=整体）
}

// AlertEventData 告警事件数据。
type AlertEventData struct {
	AlertID   string  `json:"alert_id"`   // 告警ID
	SandboxID string  `json:"sandbox_id"` // 沙箱ID
	RuleID    string  `json:"rule_id"`    // 规则ID
	RuleName  string  `json:"rule_name"`  // 规则名称
	Severity  string  `json:"severity"`   // 级别：info/warning/critical
	Metric    string  `json:"metric"`     // 关联指标
	Value     float64 `json:"value"`      // 当前值
	Threshold float64 `json:"threshold"`  // 阈值
	Message   string  `json:"message"`    // 告警消息
	Timestamp int64   `json:"timestamp"`  // 触发时间
	Source    string  `json:"source"`     // 告警来源：backend/frontend
	Step      string  `json:"step"`       // 前端事件：失败的行为步骤名
}

// StatusChangeEventData 状态变更事件数据。
type StatusChangeEventData struct {
	EntityType string `json:"entity_type"` // 实体类型：sandbox/session/training
	EntityID   string `json:"entity_id"`   // 实体ID
	OldStatus  string `json:"old_status"`  // 旧状态
	NewStatus  string `json:"new_status"`  // 新状态
	Reason     string `json:"reason"`      // 变更原因
	Timestamp  int64  `json:"timestamp"`   // 变更时间
}

// AIMessageEventData AI 消息推送事件数据。
type AIMessageEventData struct {
	SessionID   string `json:"session_id"`   // 会话ID
	MessageID   string `json:"message_id"`   // 消息ID
	Role        string `json:"role"`         // 角色：assistant/user/system
	Content     string `json:"content"`      // 内容（流式时是片段）
	IsStreaming bool   `json:"is_streaming"` // 是否流式中
	IsEnd       bool   `json:"is_end"`       // 是否结束
	Timestamp   int64  `json:"timestamp"`    // 时间
}

// ScoreUpdateEventData 评分更新事件数据。
type ScoreUpdateEventData struct {
	SessionID  string  `json:"session_id"`  // 会话ID
	TotalScore float64 `json:"total_score"` // 总分
	Category   string  `json:"category"`    // 评分维度
	Score      float64 `json:"score"`       // 该维度得分
	MaxScore   float64 `json:"max_score"`   // 该维度满分
	Reason     string  `json:"reason"`      // 评分理由
	Timestamp  int64   `json:"timestamp"`   // 更新时间
}

// LogLineEventData 日志行事件数据。
//
// 用于实时日志流推送（T2-22 日志面板）。
type LogLineEventData struct {
	SandboxID string            `json:"sandbox_id"` // 沙箱ID
	Timestamp int64             `json:"timestamp"`  // 日志时间（毫秒）
	Line      string            `json:"line"`       // 日志内容
	Service   string            `json:"service"`    // 服务名
	Level     string            `json:"level"`      // 日志级别
	Pod       string            `json:"pod"`        // Pod名
	Labels    map[string]string `json:"labels"`     // 额外标签
}

// ——— 事件总线客户端 ———

// Publisher 事件发布器接口。
type Publisher interface {
	// Publish 发布事件到指定目标。
	Publish(ctx context.Context, targetType TargetType, target string, eventType EventType, data interface{}) error

	// PublishToChannel 发布到频道。
	PublishToChannel(ctx context.Context, channel string, eventType EventType, data interface{}) error

	// PublishToUser 发布给指定用户。
	PublishToUser(ctx context.Context, userID int64, eventType EventType, data interface{}) error

	// Broadcast 广播给所有连接。
	Broadcast(ctx context.Context, eventType EventType, data interface{}) error
}

// HTTPPublisher 基于 HTTP 的事件发布器。
//
// 通过调用 WS Gateway 的 /api/v1/events/publish 接口发布事件。
type HTTPPublisher struct {
	gatewayURL string
	apiKey     string
	httpClient *http.Client
}

// NewHTTPPublisher 创建 HTTP 事件发布器。
//
// gatewayURL: WS Gateway 地址，如 "http://ws-gateway:8080"
// apiKey: 内部服务调用的 API Key
func NewHTTPPublisher(gatewayURL, apiKey string) *HTTPPublisher {
	return &HTTPPublisher{
		gatewayURL: gatewayURL,
		apiKey:     apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// publishRequest 发布请求体。
type publishRequest struct {
	TargetType string      `json:"target_type"`
	Target     string      `json:"target"`
	EventType  string      `json:"event_type"`
	Data       interface{} `json:"data"`
}

// publishResponse 发布响应。
type publishResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// Publish 发布事件。
func (p *HTTPPublisher) Publish(ctx context.Context, targetType TargetType, target string, eventType EventType, data interface{}) error {
	reqBody := publishRequest{
		TargetType: string(targetType),
		Target:     target,
		EventType:  string(eventType),
		Data:       data,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("序列化事件失败: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/events/publish", p.gatewayURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("X-Internal-API-Key", p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("调用 WS Gateway 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("WS Gateway 返回非 200: %d", resp.StatusCode)
	}

	var result publishResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != 0 {
		return fmt.Errorf("事件发布失败: %s", result.Msg)
	}

	return nil
}

// PublishToChannel 发布到频道。
func (p *HTTPPublisher) PublishToChannel(ctx context.Context, channel string, eventType EventType, data interface{}) error {
	return p.Publish(ctx, TargetChannel, channel, eventType, data)
}

// PublishToUser 发布给指定用户。
func (p *HTTPPublisher) PublishToUser(ctx context.Context, userID int64, eventType EventType, data interface{}) error {
	return p.Publish(ctx, TargetUser, fmt.Sprintf("%d", userID), eventType, data)
}

// Broadcast 广播给所有连接。
func (p *HTTPPublisher) Broadcast(ctx context.Context, eventType EventType, data interface{}) error {
	return p.Publish(ctx, TargetBroadcast, "", eventType, data)
}

// ——— 频道命名工具 ———

// SandboxChannel 沙箱频道名。
func SandboxChannel(sandboxID string) string {
	return fmt.Sprintf("sandbox:%s", sandboxID)
}

// UserChannel 用户频道名。
func UserChannel(userID int64) string {
	return fmt.Sprintf("user:%d", userID)
}

// SessionChannel 会话频道名。
func SessionChannel(sessionID string) string {
	return fmt.Sprintf("session:%s", sessionID)
}

// RoomChannel 协作房间频道名。
func RoomChannel(roomID string) string {
	return fmt.Sprintf("room:%s", roomID)
}
