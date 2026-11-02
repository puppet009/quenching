package handler

import (
	"context"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"github.com/quenching/training-service/internal/model"
	"net/http"
)

type BrainEventIngestor interface {
	Ingest(context.Context, model.BrainEvent, []model.BrainEvidence) error
}
type BrainInternalHandler struct {
	sink  BrainEventIngestor
	token string
}

func NewBrainInternalHandler(s BrainEventIngestor, token string) *BrainInternalHandler {
	return &BrainInternalHandler{s, token}
}
func (h *BrainInternalHandler) RegisterRoutes(r *gin.Engine) {
	r.POST("/internal/v1/brain/events", h.report)
}
func (h *BrainInternalHandler) report(c *gin.Context) {
	if h.token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.token)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "internal authorization required"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	var req struct {
		Event    model.BrainEvent      `json:"event"`
		Evidence []model.BrainEvidence `json:"evidence"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "invalid event"})
		return
	}
	if err := h.sink.Ingest(c.Request.Context(), req.Event, req.Evidence); err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "event rejected"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"accepted": true}})
}
