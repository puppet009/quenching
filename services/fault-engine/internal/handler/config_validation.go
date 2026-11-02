package handler

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/quenching/fault-engine/internal/service"
	"net/http"
)

func RegisterConfigValidation(r *gin.Engine, token string) {
	r.POST("/internal/v1/faults/validate-config", internalFaultGuard(token), func(c *gin.Context) {
		var req struct {
			Configuration json.RawMessage `json:"configuration"`
			PodRequired   bool            `json:"pod_required"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		err := service.ValidateCatalogConfiguration(req.Configuration, req.PodRequired)
		if err != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": err.Error()})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"valid": true}})
	})
}
