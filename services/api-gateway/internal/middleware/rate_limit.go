package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
)

// 网关层错误码。
const (
	codeRateLimit = 20003
)

// RateLimiter 令牌桶限流器（内存版）。
// MVP 阶段单机限流即可；后续接入 Redis 做分布式限流。
type RateLimiter struct {
	cfg       config.RateConfig
	buckets   map[string]*tokenBucket
	mu        sync.RWMutex
	whitelist map[string]bool
}

// tokenBucket 令牌桶。
type tokenBucket struct {
	tokens   float64   // 当前令牌数
	capacity float64   // 桶容量
	rate     float64   // 每秒填充速率
	lastTime time.Time // 上次填充时间
	mu       sync.Mutex
}

// NewRateLimiter 创建限流器。
func NewRateLimiter(cfg config.RateConfig) *RateLimiter {
	whitelist := make(map[string]bool)
	for _, ip := range cfg.WhitelistIPs {
		whitelist[ip] = true
	}
	return &RateLimiter{
		cfg:       cfg,
		buckets:   make(map[string]*tokenBucket),
		whitelist: whitelist,
	}
}

// Middleware 返回限流中间件。
// 限流优先级：用户ID > IP
// 白名单IP直接放行。
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.cfg.Enabled {
			c.Next()
			return
		}

		// 白名单IP放行
		clientIP := c.ClientIP()
		if rl.whitelist[clientIP] {
			c.Next()
			return
		}

		// 确定限流 key
		key := rl.limitKey(c)
		if key == "" {
			c.Next()
			return
		}

		// 取桶
		bucket := rl.getBucket(key)

		// 尝试取令牌
		if !bucket.allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": codeRateLimit,
				"msg":  "请求过于频繁，请稍后再试",
			})
			return
		}

		c.Next()
	}
}

// limitKey 生成限流 key。
// 已登录用户用 userID，未登录用 IP。
func (rl *RateLimiter) limitKey(c *gin.Context) string {
	// 优先按用户限流
	if rl.cfg.KeyByUser {
		if uid, ok := UserID(c); ok && uid > 0 {
			return "u:" + itoa(uid)
		}
	}
	// 兜底按 IP
	if rl.cfg.KeyByIP {
		return "ip:" + c.ClientIP()
	}
	return ""
}

// getBucket 获取或创建令牌桶。
func (rl *RateLimiter) getBucket(key string) *tokenBucket {
	rl.mu.RLock()
	b, ok := rl.buckets[key]
	rl.mu.RUnlock()
	if ok {
		return b
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()
	// double check
	if b, ok := rl.buckets[key]; ok {
		return b
	}
	b = &tokenBucket{
		tokens:   float64(rl.cfg.Burst), // 初始满桶
		capacity: float64(rl.cfg.Burst),
		rate:     float64(rl.cfg.RatePerSec),
		lastTime: time.Now(),
	}
	rl.buckets[key] = b
	return b
}

// allow 尝试获取一个令牌，成功返回 true。
func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastTime).Seconds()

	// 填充令牌
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.lastTime = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

// itoa 简单的 int64 转字符串（避免 strconv 导入也无所谓，但保持简洁）。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
