package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/api-gateway/internal/config"
	"github.com/quenching/api-gateway/internal/middleware"
)

func TestRateLimitByIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rl := middleware.NewRateLimiter(config.RateConfig{
		Enabled:    true,
		RatePerSec: 5,
		Burst:      3, // 桶容量 3，初始满
		KeyByUser:  false,
		KeyByIP:    true,
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// 前 3 次应该通过（满桶）
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("第 %d 次请求应通过，got %d", i+1, w.Code)
		}
	}

	// 第 4 次应该被限流
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("第 4 次请求应被限流(429)，got %d", w.Code)
	}
}

func TestRateLimitDifferentIPs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rl := middleware.NewRateLimiter(config.RateConfig{
		Enabled:    true,
		RatePerSec: 1,
		Burst:      1,
		KeyByUser:  false,
		KeyByIP:    true,
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// IP A 第一次通过
	reqA1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqA1.RemoteAddr = "10.0.0.1:1111"
	wA1 := httptest.NewRecorder()
	r.ServeHTTP(wA1, reqA1)
	if wA1.Code != http.StatusOK {
		t.Errorf("IP A 第一次应通过，got %d", wA1.Code)
	}

	// IP B 第一次也通过（不同 IP 独立计数）
	reqB1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqB1.RemoteAddr = "10.0.0.2:2222"
	wB1 := httptest.NewRecorder()
	r.ServeHTTP(wB1, reqB1)
	if wB1.Code != http.StatusOK {
		t.Errorf("IP B 第一次应通过，got %d", wB1.Code)
	}

	// IP A 第二次被限流
	reqA2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	reqA2.RemoteAddr = "10.0.0.1:1111"
	wA2 := httptest.NewRecorder()
	r.ServeHTTP(wA2, reqA2)
	if wA2.Code != http.StatusTooManyRequests {
		t.Errorf("IP A 第二次应被限流，got %d", wA2.Code)
	}
}

func TestRateLimitWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rl := middleware.NewRateLimiter(config.RateConfig{
		Enabled:      true,
		RatePerSec:   1,
		Burst:        1,
		KeyByUser:    false,
		KeyByIP:      true,
		WhitelistIPs: []string{"127.0.0.1"},
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// 白名单 IP 不限流（发 5 次都通过）
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("白名单 IP 第 %d 次也应通过，got %d", i+1, w.Code)
		}
	}
}

func TestRateLimitDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rl := middleware.NewRateLimiter(config.RateConfig{
		Enabled: false,
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// 关闭限流，发 10 次都通过
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("限流关闭时应全部通过，got %d", w.Code)
		}
	}
}
