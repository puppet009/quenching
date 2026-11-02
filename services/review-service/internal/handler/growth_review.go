package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/review-service/internal/service"
	"net/http"
	"strconv"
)

type GrowthReviewHandler struct{ svc *service.GrowthReportService }

func NewGrowthReviewHandler(svc *service.GrowthReportService) *GrowthReviewHandler {
	return &GrowthReviewHandler{svc: svc}
}
func growthReply(c *gin.Context, data any, err error) {
	if err == nil {
		c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": data})
		return
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrNotFound) {
		status = 404
	}
	if errors.Is(err, service.ErrGrowthSourceRange) {
		status = 400
	}
	c.JSON(status, gin.H{"code": status, "msg": "复盘来源暂不可读取"})
}
func growthID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		growthReply(c, nil, service.ErrGrowthSourceRange)
		return 0, false
	}
	return id, true
}
func (h *GrowthReviewHandler) Source(c *gin.Context) {
	id, ok := growthID(c)
	if !ok {
		return
	}
	offset, e1 := strconv.Atoi(c.DefaultQuery("offset_bytes", "0"))
	limit, e2 := strconv.Atoi(c.DefaultQuery("limit_bytes", "8192"))
	if e1 != nil || e2 != nil || offset < 0 || offset > 2147483647 || limit < 1 || limit > 8192 {
		growthReply(c, nil, service.ErrGrowthSourceRange)
		return
	}
	data, err := h.svc.ReadSource(c.Request.Context(), getUID(c), id, c.Query("reference"), offset, limit)
	growthReply(c, data, err)
}
func (h *GrowthReviewHandler) Progress(c *gin.Context) {
	id, ok := growthID(c)
	if !ok {
		return
	}
	data, err := h.svc.Progress(c.Request.Context(), getUID(c), id)
	growthReply(c, data, err)
}
