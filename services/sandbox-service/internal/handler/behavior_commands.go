package handler

import (
	"crypto/subtle"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"net/http"
	"strconv"
)

type BehaviorCommandsHandler struct {
	service *service.BehaviorCommandService
	token   string
}

func NewBehaviorCommandsHandler(s *service.BehaviorCommandService, token string) *BehaviorCommandsHandler {
	return &BehaviorCommandsHandler{s, token}
}
func (h *BehaviorCommandsHandler) RegisterRoutes(r *gin.Engine) {
	internal := r.Group("/internal/v1/sandboxes/:session_id/behavior/commands", func(c *gin.Context) {
		if h.token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001, "msg": "internal authorization required", "command_rejected": true})
			return
		}
		c.Next()
	})
	internal.POST("", h.submit)
	internal.GET("/:command_id", h.get)
	runner := r.Group("/internal/v1/behavior-runner/:session_id", func(c *gin.Context) {
		sid, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
		if err != nil || sid <= 0 || h.service.AuthenticateRunner(c.Request.Context(), sid, c.GetHeader("X-Runner-Token")) != nil {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001, "msg": "runner authorization required"})
			return
		}
		c.Next()
	})
	runner.POST("/claim", h.claim)
	runner.POST("/results", h.result)
}
func behaviorCommandSession(c *gin.Context) int64 {
	sid, _ := strconv.ParseInt(c.Param("session_id"), 10, 64)
	return sid
}
func (h *BehaviorCommandsHandler) submit(c *gin.Context) {
	var cmd model.BehaviorCommand
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if c.ShouldBindJSON(&cmd) != nil || service.ValidateBehaviorCommand(behaviorCommandSession(c), cmd) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "invalid behavior command", "command_rejected": true})
		return
	}
	result, err := h.service.Submit(c.Request.Context(), cmd)
	if err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "behavior command unavailable or rejected", "command_rejected": errors.Is(err, service.ErrBehaviorCommandRejected)})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": result})
}
func (h *BehaviorCommandsHandler) get(c *gin.Context) {
	result, err := h.service.Get(c.Request.Context(), behaviorCommandSession(c), c.Param("command_id"))
	if err != nil {
		c.JSON(404, gin.H{"code": 14004, "msg": "command not found"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": result})
}
func (h *BehaviorCommandsHandler) claim(c *gin.Context) {
	result, err := h.service.Claim(c.Request.Context(), behaviorCommandSession(c))
	if err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "runner claim unavailable"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": result})
}
func (h *BehaviorCommandsHandler) result(c *gin.Context) {
	var result model.BehaviorResult
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024)
	if c.ShouldBindJSON(&result) != nil {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	if err := h.service.Finish(c.Request.Context(), behaviorCommandSession(c), result); err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "result rejected"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"accepted": true}})
}
