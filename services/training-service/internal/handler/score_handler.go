// 评分查询接口（T3-24 协作指数汇总）。
//
// 提供训练会话的评分查询：双维分、协作明细、雷达数据、历史对比等。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/go-common/response"
	"github.com/quenching/training-service/internal/service"
)

// ScoreHandler 评分查询处理器。
type ScoreHandler struct {
	svc *service.ScoreService
}

// NewScoreHandler 创建评分处理器。
func NewScoreHandler(svc *service.ScoreService) *ScoreHandler {
	return &ScoreHandler{svc: svc}
}

// RegisterRoutes 注册评分相关路由。
func (h *ScoreHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/training", UserID())
	{
		g.GET("/sessions/:id/score", h.getSessionScore)
	}
}

func (h *ScoreHandler) getSessionScore(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	sessionID, err := parsePathID(c, "id")
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	result, err := h.svc.GetSessionScore(c.Request.Context(), sessionID, uid)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	if result == nil {
		response.Fail(c, response.ErrNotFound)
		return
	}

	response.OK(c, result)
}
