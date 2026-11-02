package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"
	"github.com/quenching/training-service/internal/service"
	"net/http"
	"strconv"
)

type BrainStatusHandler struct {
	store interface {
		PublicBrainStatus(context.Context, int64, int64) (*service.PublicBrainStatus, error)
	}
}

func NewBrainStatusHandler(s interface {
	PublicBrainStatus(context.Context, int64, int64) (*service.PublicBrainStatus, error)
}) *BrainStatusHandler {
	return &BrainStatusHandler{s}
}
func (h *BrainStatusHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/api/v1/training/sessions/:id/brain", UserID(), h.get)
}
func (h *BrainStatusHandler) get(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	sid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sid <= 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	status, err := h.store.PublicBrainStatus(c.Request.Context(), sid, uid)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	if status == nil {
		c.Status(http.StatusNotFound)
		return
	}
	response.OK(c, status)
}
