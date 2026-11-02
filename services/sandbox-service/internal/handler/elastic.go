package handler

import (
	"context"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"time"
)

func (h ResourcesHandler) registerScalingHolds(r *gin.Engine) {
	g := r.Group("/api/internal/v1/resources/scaling-holds", func(c *gin.Context) {
		if h.InternalToken == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.InternalToken)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 20001, "msg": "internal authentication required"})
			return
		}
		if h.Elastic == nil {
			c.AbortWithStatusJSON(503, gin.H{"code": 30011, "msg": "真实集群弹性控制器未连接"})
			return
		}
		c.Next()
	})
	g.POST("/check", func(c *gin.Context) {
		var hld model.ScalingHold
		if c.ShouldBindJSON(&hld) != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "冻结参数无效"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 9*time.Second)
		defer cancel()
		if e := h.Elastic.CheckHold(ctx, hld.Namespace); e != nil {
			resourceError(c, e)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"checked": true}})
	})
	g.POST("/acquire", func(c *gin.Context) {
		var hld model.ScalingHold
		if c.ShouldBindJSON(&hld) != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "冻结参数无效"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 9*time.Second)
		defer cancel()
		if e := h.Elastic.AcquireHold(ctx, hld); e != nil {
			resourceError(c, e)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"held": true}})
	})
	g.POST("/release", func(c *gin.Context) {
		var hld model.ScalingHold
		if c.ShouldBindJSON(&hld) != nil {
			c.JSON(400, gin.H{"code": 10001, "msg": "冻结参数无效"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 9*time.Second)
		defer cancel()
		if e := h.Elastic.ReleaseHold(ctx, hld.Namespace, hld.ID); e != nil {
			resourceError(c, e)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"released": true}})
	})
}
func (h ResourcesHandler) elasticPolicy(c *gin.Context) {
	if h.Elastic == nil {
		c.JSON(503, gin.H{"code": 30011, "msg": "弹性控制器未连接"})
		return
	}
	var in struct {
		Version int64 `json:"version"`
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Enabled == nil || in.Version <= 0 {
		c.JSON(400, gin.H{"code": 10001, "msg": "策略版本或开关无效"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if e := h.Elastic.SetEnabled(ctx, c.Param("namespace"), c.Param("name"), in.Version, *in.Enabled); e != nil {
		resourceError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"updated": true}})
}
