// 告警事件内部接口处理器（第三批）。
//
// 路由仅供 sandbox-service 直连调用，不经 api-gateway、不对前端暴露：
//
//	POST /api/v1/internal/alerts/events   行为引擎前端信号转发
//	POST /api/v1/internal/alerts/register 沙箱就绪时幂等注册默认规则
//
// 安全上做共享头简单校验（X-Internal-API-Key，与 eventbus HTTPPublisher
// 同一密钥），云上再叠加网络策略做来源隔离；token 留空时不校验。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// InternalAlertEventsHandler 告警事件内部接口处理器。
type InternalAlertEventsHandler struct {
	manager *service.AlertManager
	token   string
}

// NewInternalAlertEventsHandler 创建告警事件内部接口处理器。
func NewInternalAlertEventsHandler(manager *service.AlertManager, token string) *InternalAlertEventsHandler {
	return &InternalAlertEventsHandler{manager: manager, token: token}
}

// RegisterRoutes 注册服务间路由（挂在裸 engine 上，不经网关）。
func (h *InternalAlertEventsHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/internal/alerts", h.guard)
	g.POST("/events", h.reportEvent)
	g.POST("/register", h.registerDefaults)
}

// guard 内部接口共享头校验：配置了 token 才校验，留空放行。
func (h *InternalAlertEventsHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-API-Key") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// alertEventRequest 前端信号请求体，字段与 sandbox-service BehaviorSignal 对齐。
type alertEventRequest struct {
	SandboxID int64  `json:"sandbox_id"`
	SessionID int64  `json:"session_id"`
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Timestamp string `json:"timestamp"`
	Step      string `json:"step"`
	Action    string `json:"action"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Status    int    `json:"status"`
}

// reportEvent 接收行为引擎前端信号并驱动告警引擎。
func (h *InternalAlertEventsHandler) reportEvent(c *gin.Context) {
	var req alertEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	if req.SandboxID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "sandbox_id 非法"})
		return
	}
	if !isValidFrontendSignal(req.Type) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10002, "msg": "不支持的信号类型"})
		return
	}

	eventAt := time.Time{}
	if req.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
			eventAt = t
		}
	}

	h.manager.ReportEvent(c.Request.Context(), &service.FrontendEvent{
		SandboxID: strconv.FormatInt(req.SandboxID, 10),
		Type:      req.Type,
		Severity:  req.Severity,
		Step:      req.Step,
		Action:    req.Action,
		Message:   req.Message,
		URL:       req.URL,
		Status:    req.Status,
		EventAt:   eventAt,
	})

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// registerRequest 默认规则注册请求体。
type registerRequest struct {
	SandboxID int64 `json:"sandbox_id"`
}

// registerDefaults 为沙箱幂等注册默认告警规则。
func (h *InternalAlertEventsHandler) registerDefaults(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	if req.SandboxID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "sandbox_id 非法"})
		return
	}

	added := service.RegisterDefaults(h.manager, strconv.FormatInt(req.SandboxID, 10))
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{"registered": added},
	})
}

// isValidFrontendSignal 校验五类前端信号。
func isValidFrontendSignal(t string) bool {
	switch t {
	case service.FrontendSignalStepFailure,
		service.FrontendSignalPageError,
		service.FrontendSignalConsoleError,
		service.FrontendSignalHTTPError,
		service.FrontendSignalRequestFailed:
		return true
	default:
		return false
	}
}
