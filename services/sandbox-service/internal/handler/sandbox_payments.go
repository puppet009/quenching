package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"net/http"
)

type SandboxPaymentsHandler struct {
	payments *service.SandboxPaymentService
	commands *service.BehaviorCommandService
}

func NewSandboxPaymentsHandler(p *service.SandboxPaymentService, c *service.BehaviorCommandService) *SandboxPaymentsHandler {
	return &SandboxPaymentsHandler{p, c}
}
func (h *SandboxPaymentsHandler) RegisterRoutes(r *gin.Engine) {
	group := r.Group("/internal/v1/behavior-runner/:session_id/payments", func(c *gin.Context) {
		if h.commands.AuthenticateRunner(c.Request.Context(), behaviorCommandSession(c), c.GetHeader("X-Runner-Token")) != nil {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001})
			return
		}
		c.Next()
	})
	group.POST("", h.pay)
	group.GET("/:command_id", h.get)
}
func (h *SandboxPaymentsHandler) pay(c *gin.Context) {
	var req struct {
		CommandID string             `json:"command_id"`
		Scenario  string             `json:"scenario"`
		Order     model.SandboxOrder `json:"order"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 65536)
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	if req.Scenario == "" {
		req.Scenario = "success"
	}
	result, err := h.payments.PayScenario(c.Request.Context(), behaviorCommandSession(c), req.CommandID, req.Order, req.Scenario)
	if err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "sandbox payment rejected or unavailable"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": result})
}
func (h *SandboxPaymentsHandler) get(c *gin.Context) {
	result, err := h.payments.Get(c.Request.Context(), behaviorCommandSession(c), c.Param("command_id"))
	if err != nil {
		c.JSON(404, gin.H{"code": 14004})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": result})
}
