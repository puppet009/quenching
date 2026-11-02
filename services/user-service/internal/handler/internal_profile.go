// 服务间用户协作档案接口处理器（/internal/v1）。
//
// 路由仅供后端服务直连调用，不经 api-gateway、不对前端暴露。配置 token 后
// 校验 X-Internal-Token；本地开发留空则不校验。
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/user-service/internal/service"
)

// InternalProfileHandler 用户协作档案内部接口处理器。
type InternalProfileHandler struct {
	svc   *service.InternalProfileService
	token string
}

// NewInternalProfileHandler 创建用户协作档案内部接口处理器。
func NewInternalProfileHandler(svc *service.InternalProfileService, token string) *InternalProfileHandler {
	return &InternalProfileHandler{svc: svc, token: token}
}

// RegisterRoutes 注册服务间用户协作档案路由。
//
//	GET  /internal/v1/profiles/:userId
//	POST /internal/v1/profiles/collab-group/assign-if-absent
//	POST /internal/v1/profiles/collab/rollup
func (h *InternalProfileHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/profiles", h.guard)
	{
		g.GET("/:userId", h.getProfile)
		g.POST("/collab-group/assign-if-absent", h.assignGroupIfAbsent)
		g.POST("/collab/rollup", h.submitCollabRollup)
	}
}

// guard 内部接口共享头校验。
func (h *InternalProfileHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// getProfile 返回用户协作档案最小投影。
func (h *InternalProfileHandler) getProfile(c *gin.Context) {
	userID, ok := parseUserIDParam(c)
	if !ok {
		return
	}
	profile, err := h.svc.GetCollabProfile(c.Request.Context(), userID)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": profile})
}

// assignGroupReq 条件分配协作分组请求。
type assignGroupReq struct {
	UserID int64  `json:"user_id"`
	Group  string `json:"group"`
}

// assignGroupResponse 返回最终分组及是否本次写入。
type assignGroupResponse struct {
	UserID  int64  `json:"user_id"`
	Group   string `json:"group"`
	Applied bool   `json:"applied"`
}

// assignGroupIfAbsent 条件分配用户协作分组。
func (h *InternalProfileHandler) assignGroupIfAbsent(c *gin.Context) {
	var req assignGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	finalGroup, applied, err := h.svc.AssignCollabGroupIfAbsent(c.Request.Context(), req.UserID, req.Group)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": assignGroupResponse{UserID: req.UserID, Group: finalGroup, Applied: applied},
	})
}

// collabRollupReq 提交本次协作评估并请求并入历史滚动平均。
type collabRollupReq struct {
	UserID int64             `json:"user_id"`
	Radar  service.RadarData `json:"radar"`
	Score  float64           `json:"score"`
	Level  string            `json:"level"`
}

// submitCollabRollup 接收训练服务的协作评估结果。
func (h *InternalProfileHandler) submitCollabRollup(c *gin.Context) {
	var req collabRollupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	err := h.svc.SubmitCollabRollup(c.Request.Context(), req.UserID, service.CollabRollupInput{
		Radar: req.Radar,
		Score: req.Score,
		Level: req.Level,
	})
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"updated": true}})
}

// parseUserIDParam 解析路径中的用户 ID。
func parseUserIDParam(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.Param("userId"))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "用户ID非法"})
		return 0, false
	}
	return id, true
}

// writeServiceError 将领域错误映射为内部接口错误响应。
func (h *InternalProfileHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInternalProfileNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": 15001, "msg": err.Error()})
	case errors.Is(err, service.ErrInvalidCollabInput):
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
	}
}
