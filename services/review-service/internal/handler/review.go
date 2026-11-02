// Package handler 复盘 HTTP 接口。
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"

	"github.com/quenching/review-service/internal/service"
)

// ReviewHandler 复盘接口处理器。
type ReviewHandler struct {
	svc *service.ReviewService
}

// NewReviewHandler 创建。
func NewReviewHandler(svc *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{svc: svc}
}

// GenerateRequest 触发复盘生成请求。
type GenerateRequest struct {
	SessionID int64 `json:"session_id" binding:"required"`
	ProjectID int64 `json:"project_id"`
}

// Generate 触发复盘生成（异步）。
// @Summary 触发复盘生成
// @Description 异步触发复盘生成，立即返回当前状态
// @Tags 复盘
// @Accept json
// @Produce json
// @Param request body GenerateRequest true "生成参数"
// @Success 200 {object} response.Response{data=model.Review}
// @Router /api/v1/reviews/generate [post]
func (h *ReviewHandler) Generate(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	userID := getUID(c)
	rv, err := h.svc.StartGeneration(c.Request.Context(), req.SessionID, userID, req.ProjectID)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, rv)
}

// GetBySession 按会话 ID 查询复盘状态。
// @Summary 查询复盘状态
// @Description 按会话 ID 查询复盘生成状态和结果
// @Tags 复盘
// @Produce json
// @Param session_id path int true "会话ID"
// @Success 200 {object} response.Response{data=model.Review}
// @Router /api/v1/reviews/session/{session_id} [get]
func (h *ReviewHandler) GetBySession(c *gin.Context) {
	sessionIDStr := c.Param("session_id")
	sessionID, err := strconv.ParseInt(sessionIDStr, 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	rv, ownerErr := h.svc.GetBySessionID(c.Request.Context(), sessionID)
	if ownerErr != nil || rv.UserID != getUID(c) {
		response.Fail(c, response.ErrNotFound)
		return
	}

	// T4-16：轮询只取状态字段，不拉大 TEXT（前端生成中按 3s 间隔轮询）。
	st, err := h.svc.GetStatus(c.Request.Context(), sessionID)
	if err != nil {
		if err == service.ErrNotFound {
			response.Fail(c, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, st)
}

// ListHistory 查询当前用户的复盘历史（分页）。
// @Summary 复盘历史列表
// @Description 按创建时间倒序分页查询当前用户的复盘摘要列表
// @Tags 复盘
// @Produce json
// @Param page query int false "页码，从 1 开始，默认 1"
// @Param size query int false "每页条数，默认 20，最大 100"
// @Success 200 {object} response.Response{data=service.HistoryPage}
// @Router /api/v1/reviews/history [get]
func (h *ReviewHandler) ListHistory(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	result, err := h.svc.ListHistory(c.Request.Context(), getUID(c), page, size)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, result)
}

// GetByID 按 ID 查询复盘详情（含章节明细）。
// @Summary 查询复盘详情
// @Description 按 ID 查询完整复盘报告，含所有章节明细
// @Tags 复盘
// @Produce json
// @Param id path int true "复盘ID"
// @Success 200 {object} response.Response{data=service.ReviewDetail}
// @Router /api/v1/reviews/{id} [get]
func (h *ReviewHandler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	detail, err := h.svc.GetDetail(c.Request.Context(), id)
	if err != nil {
		if err == service.ErrNotFound {
			response.Fail(c, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	if detail.UserID != getUID(c) {
		response.Fail(c, response.ErrNotFound)
		return
	}
	response.OK(c, detail)
}

// GetSections 按复盘 ID 查询章节列表。
// @Summary 查询复盘章节
// @Description 按复盘 ID 查询所有章节明细
// @Tags 复盘
// @Produce json
// @Param id path int true "复盘ID"
// @Success 200 {object} response.Response{data=[]model.ReviewSection}
// @Router /api/v1/reviews/{id}/sections [get]
func (h *ReviewHandler) GetSections(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	rv, ownerErr := h.svc.GetByID(c.Request.Context(), id)
	if ownerErr != nil || rv.UserID != getUID(c) {
		response.Fail(c, response.ErrNotFound)
		return
	}
	sections, err := h.svc.GetSections(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, sections)
}

// getUID 从上下文取用户ID。
func getUID(c *gin.Context) int64 {
	if uid, exists := c.Get("uid"); exists {
		if v, ok := uid.(int64); ok {
			return v
		}
	}
	return 0
}
