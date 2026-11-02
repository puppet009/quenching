// 服务间复盘协作数据接口处理器（/internal/v1）。
//
// 路由仅供后端服务直连调用，不经 api-gateway、不对前端暴露。配置 token 后
// 校验 X-Internal-Token；本地开发留空则不校验。
package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/service"
)

// InternalReviewHandler 复盘协作数据内部接口处理器。
type InternalReviewHandler struct {
	svc   *service.InternalReviewService
	gen   ReviewGenerator
	token string
}

// ReviewGenerator 复盘生成触发端口（由 *service.ReviewService 实现）。
// 弃班等"没有浏览器在场"的终态由训练服务侧调用，用户态接口那条路径拿不到 user 身份。
type ReviewGenerator interface {
	StartGeneration(ctx context.Context, sessionID, userID, projectID int64) (*model.Review, error)
}

// NewInternalReviewHandler 创建复盘协作数据内部接口处理器。
func NewInternalReviewHandler(
	svc *service.InternalReviewService, gen ReviewGenerator, token string,
) *InternalReviewHandler {
	return &InternalReviewHandler{svc: svc, gen: gen, token: token}
}

// RegisterRoutes 注册服务间复盘协作数据路由。
//
//	GET  /internal/v1/reviews/by-session/:sessionId
//	POST /internal/v1/reviews/collab-scores
//	POST /internal/v1/reviews/existence
//	POST /internal/v1/reviews/generate
func (h *InternalReviewHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/reviews", h.guard)
	{
		g.GET("/by-session/:sessionId", h.getBySession)
		g.POST("/collab-scores", h.submitCollabScores)
		g.POST("/existence", h.listExistence)
		g.POST("/generate", h.generate)
	}
}

// generateReq 触发复盘生成请求。弃班会话无请求者可鉴权，归属用户由训练侧显式传入。
type generateReq struct {
	SessionID int64 `json:"session_id"`
	UserID    int64 `json:"user_id"`
	ProjectID int64 `json:"project_id"`
}

// generate 服务间触发复盘生成（幂等：会话已有复盘记录则直接回现有记录）。
func (h *InternalReviewHandler) generate(c *gin.Context) {
	var req generateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	if req.SessionID <= 0 || req.UserID <= 0 || h.gen == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "session_id/user_id 非法"})
		return
	}
	rv, err := h.gen.StartGeneration(c.Request.Context(), req.SessionID, req.UserID, req.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{"review_id": rv.ID, "generation_status": rv.GenerationStatus},
	})
}

// guard 内部接口共享头校验。
func (h *InternalReviewHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// getBySession 返回会话对应复盘的协作子分最小投影。
func (h *InternalReviewHandler) getBySession(c *gin.Context) {
	sessionID, ok := parseSessionIDParam(c)
	if !ok {
		return
	}
	view, err := h.svc.GetCollabBySessionID(c.Request.Context(), sessionID)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": view})
}

// collabScoresReq 提交协作子分请求。
type collabScoresReq struct {
	SessionID              int64   `json:"session_id"`
	CollabScore            float64 `json:"collab_score"`
	QuestionQualityScore   float64 `json:"question_quality_score"`
	InfoUtilizationScore   float64 `json:"info_utilization_score"`
	JudgmentScore          float64 `json:"judgment_score"`
	ErrorRecognitionRate   float64 `json:"error_recognition_rate"`
	EvidenceValidationRate float64 `json:"evidence_validation_rate"`
	AIPitfallCount         int     `json:"ai_pitfall_count"`
	AIPitfallRecognized    int     `json:"ai_pitfall_recognized"`
}

// submitCollabScores 接收训练服务的协作评估明细。
func (h *InternalReviewHandler) submitCollabScores(c *gin.Context) {
	var req collabScoresReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	updated, err := h.svc.SubmitCollabScores(c.Request.Context(), service.CollabScoresInput{
		SessionID:              req.SessionID,
		CollabScore:            req.CollabScore,
		QuestionQualityScore:   req.QuestionQualityScore,
		InfoUtilizationScore:   req.InfoUtilizationScore,
		JudgmentScore:          req.JudgmentScore,
		ErrorRecognitionRate:   req.ErrorRecognitionRate,
		EvidenceValidationRate: req.EvidenceValidationRate,
		AIPitfallCount:         req.AIPitfallCount,
		AIPitfallRecognized:    req.AIPitfallRecognized,
	})
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{"updated": updated},
	})
}

// existenceReq 批量复盘存在性请求。
type existenceReq struct {
	SessionIDs []int64 `json:"session_ids"`
}

// listExistence 批量返回复盘 id 映射。
func (h *InternalReviewHandler) listExistence(c *gin.Context) {
	var req existenceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	items, err := h.svc.ListExistence(c.Request.Context(), req.SessionIDs)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"items": items}})
}

// parseSessionIDParam 解析路径中的会话 ID。
func parseSessionIDParam(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.Param("sessionId"))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return 0, false
	}
	return id, true
}

// writeServiceError 将领域错误映射为内部接口错误响应。
func (h *InternalReviewHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInternalReviewNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 15001, "msg": err.Error()})
	case errors.Is(err, service.ErrInvalidReviewInput):
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
	}
}
