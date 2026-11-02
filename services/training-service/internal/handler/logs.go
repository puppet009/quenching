// Package handler 日志查询 HTTP 接口（T1-17）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/pkg/logquery"
)

// LogHandler 日志查询接口处理器。
type LogHandler struct {
	svc *logquery.Service
}

// NewLogHandler 创建日志查询处理器。
func NewLogHandler(svc *logquery.Service) *LogHandler {
	return &LogHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// GET /api/v1/logs/query          分页查询日志
// GET /api/v1/logs/latest         获取最新 N 条日志
// GET /api/v1/logs/stats          日志统计信息
// GET /api/v1/logs/tail           实时日志（轮询式 tail）
// GET /api/v1/logs/services       可用服务列表
func (h *LogHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/logs", UserID())
	{
		g.GET("/query", h.query)
		g.GET("/latest", h.latest)
		g.GET("/stats", h.stats)
		g.GET("/tail", h.tail)
		g.GET("/services", h.services)
	}
}

// query 分页查询日志。
//
// Query 参数：
//   - sandbox_id   沙箱 ID（必填）
//   - service      服务名（可选）
//   - level        日志级别（可选）：DEBUG/INFO/WARN/ERROR/FATAL
//   - keyword      关键词（可选）
//   - start_time   开始时间（毫秒时间戳，可选，默认 1 小时前）
//   - end_time     结束时间（毫秒时间戳，可选，默认当前）
//   - limit        返回条数（可选，默认 200，硬顶由配置 log_max_limit 控制）
//   - direction    方向（可选）：backward(默认) / forward
//   - cursor       翻页游标（可选）：取上一页响应中的 next_cursor 原样回传
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "total": 200,
//	    "has_more": true,
//	    "next_cursor": "b2xkLWN1cnNvcg",
//	    "services": ["user-service", "order-service"],
//	    "entries": [
//	      {"timestamp": 1726000000000, "line": "ERROR ...", "labels": {"service":"order-service", "level":"ERROR", "pod":"..."}},
//	      ...
//	    ]
//	  }
//	}
func (h *LogHandler) query(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	req := logquery.LogQueryRequest{
		SandboxID: sandboxID,
		Service:   c.Query("service"),
		Level:     logquery.LogLevel(c.Query("level")),
		Keyword:   c.Query("keyword"),
		Direction: logquery.QueryDirection(c.DefaultQuery("direction", "backward")),
		// 不透明游标原样透传；服务层解码失败时按首页处理。
		Cursor: c.Query("cursor"),
	}

	// 解析时间
	if v := c.Query("start_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.StartTime = time.UnixMilli(ms)
		}
	}
	if v := c.Query("end_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.EndTime = time.UnixMilli(ms)
		}
	}

	// limit
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			req.Limit = n
		}
	}

	result, err := h.svc.QueryLogs(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// latest 获取最新 N 条日志。
//
// Query 参数：
//   - sandbox_id  沙箱 ID（必填）
//   - service     服务名（可选）
//   - limit       返回条数（可选，默认 100）
func (h *LogHandler) latest(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	result, err := h.svc.GetLatestLogs(c.Request.Context(), sandboxID, c.Query("service"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// stats 日志统计信息。
//
// Query 参数：
//   - sandbox_id  沙箱 ID（必填）
//   - start_time  开始时间（可选，默认 1 小时前）
//   - end_time    结束时间（可选，默认当前）
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "total_count": 5280,
//	    "error_count": 12,
//	    "warn_count": 85,
//	    "info_count": 5183,
//	    "by_service": {"user-service": 2100, "order-service": 1800, "product-service": 1380},
//	    "time_range": "14:00:00 ~ 15:00:00"
//	  }
//	}
func (h *LogHandler) stats(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	var start, end time.Time
	if v := c.Query("start_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = time.UnixMilli(ms)
		}
	}
	if v := c.Query("end_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			end = time.UnixMilli(ms)
		}
	}

	result, err := h.svc.GetLogStats(c.Request.Context(), sandboxID, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// tail 实时日志（轮询式 tail）。
//
// 返回 since 时间之后的新日志，从旧到新排列，便于前端追加。
//
// Query 参数：
//   - sandbox_id  沙箱 ID（必填）
//   - service     服务名（可选）
//   - since       上次最后一条日志的时间戳（毫秒，必填）
//   - limit       最大返回条数（可选，默认 200）
func (h *LogHandler) tail(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	sinceStr := c.Query("since")
	if sinceStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 since 参数"})
		return
	}

	sinceMs, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "msg": "since 参数格式错误"})
		return
	}

	limit := 200
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	result, err := h.svc.TailLogs(c.Request.Context(), sandboxID, c.Query("service"), time.UnixMilli(sinceMs), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// services 获取可用的服务列表。
//
// T4-16：走 Loki /series 标签查询，返回时间窗口内有日志输出的全部服务，
// 替代旧实现"拉 50 条最新日志再提 service 名"（窗口外无日志的服务会漏）。
//
// Query 参数：
//   - sandbox_id  沙箱 ID（必填）
//   - start_time  开始时间（毫秒时间戳，可选，默认 1 小时前）
//   - end_time    结束时间（毫秒时间戳，可选，默认当前）
func (h *LogHandler) services(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sandboxID := c.Query("sandbox_id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 sandbox_id 参数"})
		return
	}

	var start, end time.Time
	if v := c.Query("start_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = time.UnixMilli(ms)
		}
	}
	if v := c.Query("end_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			end = time.UnixMilli(ms)
		}
	}

	services, err := h.svc.ListServices(c.Request.Context(), sandboxID, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": services,
	})
}
