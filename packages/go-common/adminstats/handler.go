package adminstats

import (
	"context"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
)

// These endpoints are called directly between services, never routed to browsers.
func Handler(token string, read func(context.Context) (map[string]interface{}, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(c.GetHeader("X-Internal-Token"))) != 1 {
			c.JSON(401, gin.H{"code": 20001, "msg": "内部令牌无效"})
			return
		}
		data, err := read(c.Request.Context())
		if err != nil {
			c.JSON(503, gin.H{"code": 20005, "msg": "统计暂不可用"})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": data})
	}
}
