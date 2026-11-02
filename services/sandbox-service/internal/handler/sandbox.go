// Package handler 沙箱 HTTP 接口。
//
// 接口列表：
//
//	POST   /api/v1/sandboxes          创建沙箱（启动实训）
//	GET    /api/v1/sandboxes/:id      沙箱详情
//	DELETE /api/v1/sandboxes/:id      释放沙箱
//	GET    /api/v1/sandboxes/:id/progress     启动进度查询（轮询）
//	GET    /api/v1/sandboxes/:id/progress/stream  启动进度推送（SSE）
package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/logger"
	"github.com/quenching/go-common/response"
	"go.uber.org/zap"

	"github.com/quenching/sandbox-service/internal/service"
)

// SandboxHandler 沙箱接口处理器。
type SandboxHandler struct {
	svc     *service.SandboxService
	tracker *service.ProgressTracker
}

// NewSandboxHandler 创建处理器。
func NewSandboxHandler(svc *service.SandboxService, tracker *service.ProgressTracker) *SandboxHandler {
	return &SandboxHandler{svc: svc, tracker: tracker}
}

// CreateRequest 创建沙箱请求。
type CreateRequest struct {
	SessionID    int64  `json:"session_id" binding:"required"`
	ProjectID    int64  `json:"project_id"`    // 文件模板模式可传 0，与 template_slug 至少一个
	TemplateSlug string `json:"template_slug"` // 可选：指定官方模板；为空创建不预置业务系统的裸沙箱
}

// Create 创建沙箱（启动实训）。
// @Summary 创建沙箱
// @Description 创建一个新的沙箱环境，开始实训
// @Tags 沙箱
// @Accept json
// @Produce json
// @Param request body CreateRequest true "创建参数"
// @Success 200 {object} response.Response{data=model.Sandbox}
// @Router /api/v1/sandboxes [post]
func (h *SandboxHandler) Create(c *gin.Context) {
	var req CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	userID := getUID(c)
	sb, err := h.svc.CreateSandbox(c.Request.Context(), userID, req.ProjectID, req.SessionID, req.TemplateSlug)
	if err != nil {
		response.Fail(c, toSandboxError(c, err))
		return
	}

	response.OK(c, sb)
}

// Progress 查询启动进度（轮询接口）。
// @Summary 查询沙箱启动进度
// @Description 查询沙箱启动进度，用于前端轮询
// @Tags 沙箱
// @Produce json
// @Param id path int true "会话ID"
// @Success 200 {object} response.Response{data=service.ProgressStatus}
// @Router /api/v1/sandboxes/{id}/progress [get]
func (h *SandboxHandler) Progress(c *gin.Context) {
	sessionID, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	status, err := h.tracker.Status(c.Request.Context(), sessionID)
	if err != nil {
		response.Fail(c, response.ErrNotFound)
		return
	}

	response.OK(c, status)
}

// ProgressStream SSE 流式推送启动进度。
// @Summary SSE 启动进度推送
// @Description 服务端事件流推送启动进度，每2秒一次，直到就绪或失败
// @Tags 沙箱
// @Produce text/event-stream
// @Param id path int true "会话ID"
// @Router /api/v1/sandboxes/{id}/progress/stream [get]
func (h *SandboxHandler) ProgressStream(c *gin.Context) {
	sessionID, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrBadRequest)
		return
	}

	StreamProgress(c, h.tracker, sessionID)
}

// ---------- 工具函数 ----------

// parseID 从路径参数解析 int64 ID。
func parseID(c *gin.Context) (int64, error) {
	idStr := c.Param("id")
	return strconv.ParseInt(idStr, 10, 64)
}

// getUID 从上下文取用户ID。
// 真实项目中通过中间件注入，这里先简化处理。
func getUID(c *gin.Context) int64 {
	if uid, exists := c.Get("uid"); exists {
		if v, ok := uid.(int64); ok {
			return v
		}
	}
	return 0 // 测试用，实际中间件会保证有值
}

// toSandboxError 将业务错误映射为统一错误响应。
// 未命中哨兵的未知错误视为内部错误，必须记录真实原因（此前直接返回
// ErrInternal 会吞掉错误，导致日志无法排查）。
func toSandboxError(c *gin.Context, err error) *response.Error {
	// 配额超限携带动态数量信息（quotaExceededErr 返回 *response.Error），原样透传。
	var bizErr *response.Error
	if errors.As(err, &bizErr) && bizErr.Code == 30006 {
		return bizErr
	}
	switch err {
	case service.ErrSandboxQuotaExceeded:
		return service.ErrSandboxQuotaExceeded
	case service.ErrSandboxInsufficientResource:
		return service.ErrSandboxInsufficientResource
	case service.ErrSandboxTemplateNotFound:
		return service.ErrSandboxTemplateNotFound
	case service.ErrSandboxProjectRequired:
		return service.ErrSandboxProjectRequired
	default:
		logger.From(c.Request.Context()).Error("创建沙箱失败",
			zap.Error(err),
			zap.Int64("uid", getUID(c)),
		)
		return response.ErrInternal
	}
}
