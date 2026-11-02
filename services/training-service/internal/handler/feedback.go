package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/quenching/training-service/internal/service"
	"strconv"
)

type FeedbackHandler struct{ store service.FeedbackStore }

func NewFeedbackHandler(store service.FeedbackStore) *FeedbackHandler {
	return &FeedbackHandler{store: store}
}
func (h *FeedbackHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1", UserID())
	g.POST("/feedback/nps", func(c *gin.Context) { h.save(c, true) })
	g.POST("/feedback/value", func(c *gin.Context) { h.save(c, false) })
	g.GET("/admin/feedback/nps", h.list)
}
func (h *FeedbackHandler) save(c *gin.Context, nps bool) {
	uid, ok := MustUserID(c)
	if !ok {
		c.JSON(401, gin.H{"code": 20001, "msg": "请先登录"})
		return
	}
	var in service.FeedbackInput
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "参数非法"})
		return
	}
	if err := service.ValidateFeedback(&in, nps); err != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": err.Error()})
		return
	}
	id, err := h.store.Save(c.Request.Context(), uid, in)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(404, gin.H{"code": 20004, "msg": "训练不存在或不属于当前用户"})
		return
	}
	if err != nil {
		c.JSON(500, gin.H{"code": 20005, "msg": "反馈保存失败"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"success": true, "id": id}})
}
func (h *FeedbackHandler) list(c *gin.Context) {
	if _, ok := MustUserID(c); !ok {
		c.JSON(401, gin.H{"code": 20001, "msg": "请先登录"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	min, err := strconv.Atoi(c.DefaultQuery("score_min", "0"))
	if err != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "分数筛选非法"})
		return
	}
	max, err := strconv.Atoi(c.DefaultQuery("score_max", "10"))
	if err != nil || min < 0 || max > 10 || min > max {
		c.JSON(400, gin.H{"code": 10001, "msg": "分数筛选非法"})
		return
	}
	var comment *bool
	if v := c.Query("has_comment"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "评论筛选非法"})
			return
		}
		comment = &b
	}
	data, err := h.store.List(c.Request.Context(), page, size, min, max, comment)
	if err != nil {
		c.JSON(500, gin.H{"code": 20005, "msg": "反馈查询失败"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": data})
}
