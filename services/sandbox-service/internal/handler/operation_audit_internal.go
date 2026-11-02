package handler

import (
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	oe "github.com/quenching/proto/operationevidence"
	operationaudit "github.com/quenching/sandbox-service/internal/repository/operationaudit"
	"strconv"
	"time"
)

type OperationAuditHandler struct {
	Store *operationaudit.PGStore
	Token string
}

func (h OperationAuditHandler) Register(r *gin.Engine) {
	g := r.Group("/internal/v1/sandboxes", func(c *gin.Context) {
		if h.Token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.Token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001})
			return
		}
		c.Next()
	})
	g.POST("/:session_id/audit-seal", h.seal)
	g.GET("/:session_id/audit-seal", h.seal)
}
func (h OperationAuditHandler) seal(c *gin.Context) {
	sid, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || sid <= 0 {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	seal, err := h.Store.Seal(c.Request.Context(), sid, "sandbox", time.Now().UTC())
	if err != nil {
		c.JSON(503, gin.H{"code": 15003})
		return
	}
	var legacyPending int
	err = h.Store.Pool.QueryRow(c.Request.Context(), `SELECT (SELECT count(*) FROM sandbox_session_operations WHERE session_id=$1 AND NOT projection_delivered)+(SELECT count(*) FROM sandbox_training_audit WHERE session_id=$1 AND NOT delivered)+(SELECT count(*) FROM behavior_commands WHERE session_id=$1 AND status IN ('accepted','running','succeeded','failed','unknown') AND NOT delivered)`, sid).Scan(&legacyPending)
	if err != nil {
		c.JSON(503, gin.H{"code": 15003})
		return
	}
	if legacyPending > 0 {
		seal.Pending += legacyPending
		seal.Acknowledged = 0
		seal = oe.EvaluateSeal(seal, time.Now().UTC())
	}
	c.JSON(200, gin.H{"code": 0, "data": seal})
}
