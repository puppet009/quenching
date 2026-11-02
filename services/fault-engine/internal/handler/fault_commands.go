package handler

import (
	"crypto/subtle"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/fault-engine/internal/model"
	"github.com/quenching/fault-engine/internal/service"
	"net/http"
	"strconv"
	"strings"
)

func internalFaultGuard(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(c.GetHeader("X-Internal-Token"))) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "internal authorization required", "command_rejected": true})
			return
		}
		c.Next()
	}
}

type FaultCommandHandler struct {
	svc   *service.FaultCommandService
	token string
}

func NewFaultCommandHandler(s *service.FaultCommandService, t string) *FaultCommandHandler {
	return &FaultCommandHandler{s, t}
}
func (h *FaultCommandHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/faults", internalFaultGuard(h.token))
	g.POST("/commands", func(c *gin.Context) {
		var cmd model.FaultCommand
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if e := c.ShouldBindJSON(&cmd); e != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "invalid command", "command_rejected": true})
			return
		}
		res, e := h.svc.Submit(c.Request.Context(), cmd)
		h.reply(c, res, e)
	})
	g.GET("/commands/:command_id", func(c *gin.Context) {
		res, e := h.svc.Get(c.Request.Context(), c.Param("command_id"))
		h.reply(c, res, e)
	})
	g.GET("/trainable/:template_slug/combinations", func(c *gin.Context) {
		res, e := h.svc.ListAcceptedCombinations(c.Request.Context(), c.Param("template_slug"))
		h.reply(c, res, e)
	})
	g.GET("/trainable/:template_slug", func(c *gin.Context) {
		res, e := h.svc.ListTrainable(c.Request.Context(), c.Param("template_slug"))
		h.reply(c, res, e)
	})
	admin := r.Group("/internal/v1/faults/combinations", internalFaultGuard(h.token))
	admin.GET("/preview", func(c *gin.Context) {
		ids := []int64{}
		for _, part := range strings.Split(c.Query("defect_ids"), ",") {
			id, e := strconv.ParseInt(part, 10, 64)
			if e != nil {
				h.reply(c, nil, e)
				return
			}
			ids = append(ids, id)
		}
		res, e := h.svc.PreviewCombination(c.Request.Context(), c.Query("template_slug"), ids)
		h.reply(c, res, e)
	})

	admin.POST("/validate", func(c *gin.Context) {
		var combo model.FaultCombination
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if e := c.ShouldBindJSON(&combo); e != nil {
			h.reply(c, nil, e)
			return
		}
		e := h.svc.ValidateCombination(c.Request.Context(), combo)
		h.reply(c, combo, e)
	})
}
func (h *FaultCommandHandler) reply(c *gin.Context, data any, e error) {
	if e != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": e.Error(), "command_rejected": errors.Is(e, service.ErrFaultCommandRejected)})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": data})
}
