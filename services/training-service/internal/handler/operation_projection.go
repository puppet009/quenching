package handler

import (
	"context"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	oe "github.com/quenching/proto/operationevidence"
	"net/http"
	"strconv"
)

type OperationProjectionPort interface {
	Owner(context.Context, int64, int64) (bool, error)
	IngestLegacy(context.Context, oe.LegacyProjection) error
}

func RegisterOperationProjection(r *gin.Engine, p OperationProjectionPort, token string) {
	guard := func(c *gin.Context) {
		if token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001})
			return
		}
		c.Next()
	}
	r.GET("/internal/v1/sessions/:id/operation-owner", guard, func(c *gin.Context) {
		sid, e := strconv.ParseInt(c.Param("id"), 10, 64)
		uid, x := strconv.ParseInt(c.DefaultQuery("user_id", "0"), 10, 64)
		if e != nil || x != nil || sid <= 0 || uid < 0 {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		owned, e := p.Owner(c.Request.Context(), sid, uid)
		if e != nil {
			c.JSON(503, gin.H{"code": 15003})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"owned": owned, "session_id": sid, "user_id": uid}})
	})
	r.POST("/internal/v1/operation-projections/legacy", guard, func(c *gin.Context) {
		var b oe.LegacyProjection
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
		if c.ShouldBindJSON(&b) != nil {
			c.JSON(400, gin.H{"code": 10001})
			return
		}
		if e := p.IngestLegacy(c.Request.Context(), b); e != nil {
			c.JSON(409, gin.H{"code": 14009})
			return
		}
		ids := []int64{}
		for _, v := range b.Rows {
			ids = append(ids, v.ID)
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"ids": ids}})
	})
}
