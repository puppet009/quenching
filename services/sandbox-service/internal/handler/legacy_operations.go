package handler

import (
	"context"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/sandbox-service/internal/service"
	"net/http"
	"strconv"
)

type LegacyOperationRecorder interface {
	Record(context.Context, oe.LegacyBatch) (oe.LegacyReceipt, error)
}

type LegacyCheckpointReader interface {
	Checkpoint(context.Context, int64) (oe.LegacyCheckpoint, error)
}

func RegisterLegacyCheckpoint(r *gin.Engine, repo LegacyCheckpointReader, owner service.OperationOwnerCheck, token string) {
	r.GET("/internal/v1/operations/:id/projection-checkpoint", func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(token)) != 1 {
			c.JSON(401, gin.H{"code": 14001})
			return
		}
		sid, e := strconv.ParseInt(c.Param("id"), 10, 64)
		if e != nil || sid <= 0 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		if owner == nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		known, e := owner(c.Request.Context(), sid, 0)
		if e != nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		if !known {
			c.JSON(404, gin.H{"code": 14004})
			return
		}
		checkpoint, e := repo.Checkpoint(c.Request.Context(), sid)
		if e != nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": checkpoint})
	})
}

func RegisterLegacyOperations(r *gin.Engine, repo LegacyOperationRecorder, owner service.OperationOwnerCheck, token string) {
	r.POST("/internal/v1/operations/report", func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(token)) != 1 {
			c.JSON(401, gin.H{"code": 14001})
			return
		}
		var b oe.LegacyBatch
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
		if c.ShouldBindJSON(&b) != nil || b.SessionID <= 0 || b.UserID <= 0 || len(b.RequestID) > 100 || b.RequestID == "" || len(b.Operations) == 0 || len(b.Operations) > 500 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		if owner == nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		owned, e := owner(c.Request.Context(), b.SessionID, b.UserID)
		if e != nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		if !owned {
			c.JSON(404, gin.H{"code": 14004})
			return
		}
		receipt, e := repo.Record(c.Request.Context(), b)
		if e != nil {
			c.JSON(409, gin.H{"code": 14009})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": receipt})
	})
}
