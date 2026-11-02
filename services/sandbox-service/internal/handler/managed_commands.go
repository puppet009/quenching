package handler

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/sandbox-service/internal/service"
	"io"
	"net/http"
	"strconv"
)

type ManagedCommandsHandler struct {
	Service *service.ManagedCommandService
}

func (h ManagedCommandsHandler) Register(g *gin.RouterGroup) {
	g.POST("/:id/commands", h.start)
	g.GET("/:id/commands/:operation_id", h.get)
	g.POST("/:id/commands/:operation_id/cancel", h.cancel)
}
func (h ManagedCommandsHandler) scope(c *gin.Context) (int64, int64, bool) {
	sid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	uid, _ := c.Get(ContextUID)
	u, ok := uid.(int64)
	if err != nil || sid <= 0 || !ok || u <= 0 {
		c.JSON(400, gin.H{"code": 10001})
		return 0, 0, false
	}
	return u, sid, true
}
func commandResponse(c *gin.Context, e oe.Evidence, err error, status int) {
	if err != nil {
		n := 400
		if errors.Is(err, oe.ErrNotFound) {
			n = 404
		}
		if errors.Is(err, oe.ErrConflict) || errors.Is(err, oe.ErrClosed) {
			n = 409
		}
		c.JSON(n, gin.H{"code": n * 100, "msg": "command rejected or unavailable"})
		return
	}
	c.JSON(status, gin.H{"code": 0, "data": e})
}
func (h ManagedCommandsHandler) start(c *gin.Context) {
	uid, sid, ok := h.scope(c)
	if !ok {
		return
	}
	var req service.ManagedCommandRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(&struct{}{}) != io.EOF {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	e, err := h.Service.Start(c.Request.Context(), uid, sid, req)
	commandResponse(c, e, err, 202)
}
func (h ManagedCommandsHandler) get(c *gin.Context) {
	uid, sid, ok := h.scope(c)
	if !ok {
		return
	}
	e, err := h.Service.Get(c.Request.Context(), uid, sid, c.Param("operation_id"))
	commandResponse(c, e, err, 200)
}
func (h ManagedCommandsHandler) cancel(c *gin.Context) {
	uid, sid, ok := h.scope(c)
	if !ok {
		return
	}
	e, err := h.Service.Cancel(c.Request.Context(), uid, sid, c.Param("operation_id"))
	commandResponse(c, e, err, 202)
}
