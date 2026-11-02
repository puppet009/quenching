// Package handler 故障注入日志观察 HTTP 接口（T1-22）。
package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// LogViewerHandler 日志观察处理器。
type LogViewerHandler struct {
	svc *service.LogViewerService
}

// NewLogViewerHandler 创建日志观察处理器。
func NewLogViewerHandler(svc *service.LogViewerService) *LogViewerHandler {
	return &LogViewerHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// GET    /api/v1/logviewer/query     日志查询（带高亮）
// GET    /api/v1/logviewer/context   日志上下文
// GET    /api/v1/logviewer/stats     日志统计
// GET    /api/v1/logviewer/export    日志导出
// POST   /api/v1/logviewer/stream/start  启动实时日志流
// POST   /api/v1/logviewer/stream/stop   停止实时日志流
func (h *LogViewerHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/logviewer", UserID())
	{
		g.GET("/query", h.query)
		g.GET("/context", h.context)
		g.GET("/stats", h.stats)
		g.GET("/export", h.exportLogs)
		g.POST("/stream/start", h.startStream)
		g.POST("/stream/stop", h.stopStream)
	}
}

// query 日志查询。
//
// Query 参数：
//   - sandbox_id   沙箱ID（必填）
//   - service      服务名（可选）
//   - level        日志级别（可选）：DEBUG/INFO/WARN/ERROR/FATAL
//   - keyword      关键词（可选）
//   - start_time   开始时间（毫秒，可选，默认 1 小时前）
//   - end_time     结束时间（毫秒，可选，默认当前）
//   - limit        返回条数（可选，默认 200）
//   - direction    方向（可选）：backward(默认，从新到旧) / forward
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "total": 200,
//	    "has_more": true,
//	    "services": ["user-service", "order-service"],
//	    "levels": ["INFO", "ERROR", "WARN"],
//	    "lines": [
//	      {
//	        "ts": 1726000000000,
//	        "line": "2024-09-12 10:00:00 ERROR OrderService - 创建订单失败...",
//	        "svc": "order-service",
//	        "level": "ERROR",
//	        "pod": "order-7f9d8c7b5-x2k4m",
//	        "is_err": true,
//	        "hl": [{"s": 20, "e": 25, "t": "error"}]
//	      }
//	    ]
//	  }
//	}
func (h *LogViewerHandler) query(c *gin.Context) {
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

	limit := 200
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	direction := c.DefaultQuery("direction", "backward")

	result, err := h.svc.QueryLogs(c.Request.Context(),
		sandboxID,
		c.Query("service"),
		c.Query("level"),
		c.Query("keyword"),
		start, end,
		limit,
		direction,
	)
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

// context 获取日志上下文。
//
// 返回指定时间点前后各 N 条日志，用于点击查看上下文。
//
// Query 参数：
//   - sandbox_id   沙箱ID（必填）
//   - service      服务名（可选）
//   - timestamp    中心日志的时间戳（毫秒，必填）
//   - size         上下文条数（可选，默认 10 条）
func (h *LogViewerHandler) context(c *gin.Context) {
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

	tsStr := c.Query("timestamp")
	if tsStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40002, "msg": "缺少 timestamp 参数"})
		return
	}

	timestamp, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40003, "msg": "timestamp 格式错误"})
		return
	}

	size := 10
	if v := c.Query("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}

	result, err := h.svc.GetLogContext(c.Request.Context(),
		sandboxID,
		c.Query("service"),
		timestamp,
		size,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "获取日志上下文失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// stats 日志统计。
//
// Query 参数：
//   - sandbox_id   沙箱ID（必填）
//   - service      服务名（可选）
//   - start_time   开始时间（毫秒，可选，默认 1 小时前）
//   - end_time     结束时间（毫秒，可选，默认当前）
func (h *LogViewerHandler) stats(c *gin.Context) {
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

	result, err := h.svc.GetStats(c.Request.Context(), sandboxID, c.Query("service"), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "获取日志统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// exportLogs 导出日志。
//
// 导出指定时间范围内的日志为纯文本文件，支持下载。
//
// Query 参数：
//   - sandbox_id   沙箱ID（必填）
//   - service      服务名（可选）
//   - level        日志级别（可选）
//   - keyword      关键词（可选）
//   - start_time   开始时间（毫秒，可选，默认 1 小时前）
//   - end_time     结束时间（毫秒，可选，默认当前）
func (h *LogViewerHandler) exportLogs(c *gin.Context) {
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

	result, err := h.svc.ExportLogs(c.Request.Context(),
		sandboxID,
		c.Query("service"),
		c.Query("level"),
		c.Query("keyword"),
		start, end,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "日志导出失败"})
		return
	}

	// 设置下载响应头
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+result.Filename+"\"")
	c.Header("Content-Length", strconv.Itoa(result.Size))
	// T4-16：命中导出上限时显式告知前端结果已截断。
	if result.Truncated {
		c.Header("X-Log-Export-Truncated", "1")
	}
	c.String(http.StatusOK, result.Content)
}

// startStream 启动实时日志流。
//
// 开始定时推送新日志到 WebSocket 频道。
//
// 请求体：
//
//	{
//	  "sandbox_id": "sandbox-123",
//	  "service": "order-service",
//	  "interval_ms": 3000
//	}
func (h *LogViewerHandler) startStream(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	var req struct {
		SandboxID  string `json:"sandbox_id" binding:"required"`
		Service    string `json:"service"`
		IntervalMs int    `json:"interval_ms"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	interval := 3 * time.Second
	if req.IntervalMs > 0 {
		interval = time.Duration(req.IntervalMs) * time.Millisecond
	}

	// T4-16：流的生命周期已与 HTTP 请求解耦（service 内部以 Background ctx
	// 派生），这里同步启动即可；间隔下限由 service 统一钳制（默认 2s）。
	if err := h.svc.StartStream(req.SandboxID, req.Service, interval); err != nil {
		if errors.Is(err, service.ErrMaxStreamsReached) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code": 42901,
				"msg":  "实时日志流数量已达上限，请稍后再试",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "启动实时日志流失败"})
		return
	}

	// 回传钳制后的实际间隔，前端按此节奏消费频道消息。
	actual := interval
	if minInterval := h.svc.MinInterval(); actual < minInterval {
		actual = minInterval
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"started":     true,
			"interval_ms": actual.Milliseconds(),
		},
	})
}

// stopStream 停止实时日志流。
//
// 请求体：
//
//	{
//	  "sandbox_id": "sandbox-123"
//	}
func (h *LogViewerHandler) stopStream(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	var req struct {
		SandboxID string `json:"sandbox_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	h.svc.StopStream(req.SandboxID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"stopped": true,
		},
	})
}
