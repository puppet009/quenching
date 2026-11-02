// Package handler 事件发布 HTTP 接口（T1-19 事件订阅分发）。
//
// 供内部服务调用，向指定频道/用户发布事件。
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/api-gateway/internal/pkg/hub"
)

// PublishRequest 事件发布请求。
type PublishRequest struct {
	// 目标类型：channel / user / broadcast
	TargetType string `json:"target_type" binding:"required,oneof=channel user broadcast"`
	// 目标值：频道名 / 用户ID / 空（broadcast）
	Target string `json:"target"`
	// 事件类型
	EventType string `json:"event_type" binding:"required"`
	// 事件数据（任意 JSON）
	Data interface{} `json:"data"`
}

// EventHandler 事件发布处理器。
type EventHandler struct {
	hub *hub.Hub
	// 内部服务调用的 API Key（简单鉴权）
	apiKey string
}

// NewEventHandler 创建事件发布处理器。
func NewEventHandler(h *hub.Hub, apiKey string) *EventHandler {
	return &EventHandler{
		hub:    h,
		apiKey: apiKey,
	}
}

// Handle 处理 /api/v1/events 前缀下的内部事件请求。
//
// 该前缀与网关通配代理 /api/v1/*path 重叠，gin 不允许同位置再注册静态子路由，
// 故由 main 的通配处理器在识别到该前缀时分流到本方法，而非自行 RegisterRoutes。
//
// POST /api/v1/events/publish  发布事件
// GET  /api/v1/events/stats    事件统计（活跃频道数等）
func (h *EventHandler) Handle(c *gin.Context) {
	h.authenticate(c)
	if c.IsAborted() {
		return
	}

	switch strings.TrimPrefix(c.Request.URL.Path, "/api/v1/events") {
	case "/publish":
		if c.Request.Method != http.MethodPost {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"code": 405, "msg": "方法不允许"})
			return
		}
		h.publish(c)
	case "/stats":
		if c.Request.Method != http.MethodGet {
			c.JSON(http.StatusMethodNotAllowed, gin.H{"code": 405, "msg": "方法不允许"})
			return
		}
		h.stats(c)
	default:
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "事件接口不存在"})
	}
}

// authenticate 内部服务鉴权。
//
// 通过 X-Internal-API-Key header 验证，防止外部直接调用。
// apiKey 为空时不校验（仅限本地开发；云上必须配置 WS_INTERNAL_API_KEY）。
func (h *EventHandler) authenticate(c *gin.Context) {
	if h.apiKey == "" {
		return
	}
	if c.GetHeader("X-Internal-API-Key") != h.apiKey {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 40101,
			"msg":  "无效的内部 API Key",
		})
		c.Abort()
	}
}

// publish 发布事件。
//
// 请求体：
//
//	{
//	  "target_type": "channel",     // channel / user / broadcast
//	  "target": "sandbox:abc123",   // 频道名或用户ID
//	  "event_type": "metrics",      // 事件类型
//	  "data": { ... }               // 事件数据
//	}
func (h *EventHandler) publish(c *gin.Context) {
	var req PublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40001,
			"msg":  "参数错误: " + err.Error(),
		})
		return
	}

	event := hub.Event{
		Type: hub.EventType(req.EventType),
		Data: req.Data,
	}

	switch req.TargetType {
	case "channel":
		if req.Target == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 40002,
				"msg":  "缺少 target（频道名）",
			})
			return
		}
		h.hub.SendToChannel(req.Target, event)

	case "user":
		if req.Target == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 40002,
				"msg":  "缺少 target（用户ID）",
			})
			return
		}
		// 解析用户ID
		userID := parseInt64(req.Target)
		if userID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 40003,
				"msg":  "用户ID格式错误",
			})
			return
		}
		h.hub.SendToUser(userID, event)

	case "broadcast":
		h.hub.Broadcast(event)

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 40004,
			"msg":  "不支持的 target_type",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"published": true,
		},
	})
}

// stats 获取事件统计信息。
//
// 返回当前活跃频道数、连接数等监控信息。
func (h *EventHandler) stats(c *gin.Context) {
	channels := h.hub.ListChannels()

	channelStats := make(map[string]int, len(channels))
	for _, ch := range channels {
		channelStats[ch] = h.hub.ChannelCount(ch)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"channel_count": len(channels),
			"channel_stats": channelStats,
		},
	})
}

// parseInt64 解析 int64，失败返回 0。
func parseInt64(s string) int64 {
	n := int64(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
