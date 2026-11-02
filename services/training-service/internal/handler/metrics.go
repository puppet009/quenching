// Package handler 核心指标查询 HTTP 接口（T1-15）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/pkg/metrics"
)

// MetricsHandler 指标查询接口处理器。
type MetricsHandler struct {
	svc *metrics.Service
}

// NewMetricsHandler 创建指标查询处理器。
func NewMetricsHandler(svc *metrics.Service) *MetricsHandler {
	return &MetricsHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// GET /api/v1/metrics/definitions     列出所有核心指标定义
// GET /api/v1/metrics/query           区间查询指标数据（时间序列）
// GET /api/v1/metrics/current         获取指标当前值
// GET /api/v1/metrics/snapshot        获取所有指标快照
// GET /api/v1/metrics/health          服务健康状态
func (h *MetricsHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/metrics", UserID())
	{
		g.GET("/definitions", h.definitions)
		g.GET("/query", h.query)
		g.GET("/current", h.current)
		g.GET("/snapshot", h.snapshot)
		g.GET("/health", h.health)
	}
}

// definitions 列出所有核心指标定义。
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": [
//	    {"name":"cpu_usage","display_name":"CPU 使用率","unit":"%","category":"system",...},
//	    {"name":"memory_usage","display_name":"内存使用率","unit":"%","category":"system",...},
//	    ...
//	  ]
//	}
func (h *MetricsHandler) definitions(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	defs := metrics.ListMetrics()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": defs,
	})
}

// query 区间查询指标数据（时间序列）。
//
// Query 参数：
//   - metric       指标名称（必填）：cpu_usage/memory_usage/qps/error_rate/p99_latency
//   - service      服务名（可选）：如 user-service、order-service，空=全部聚合
//   - instance     实例名（可选）
//   - start_time   开始时间（可选，毫秒时间戳），默认 15 分钟前
//   - end_time     结束时间（可选，毫秒时间戳），默认当前
//   - step         采样步长（可选，秒），默认 15
//   - aggregation  聚合方式（可选）：avg/max/min/last，默认 avg
func (h *MetricsHandler) query(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	metricName := c.Query("metric")
	if metricName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 metric 参数"})
		return
	}

	req := metrics.QueryRequest{
		Metric:      metrics.MetricName(metricName),
		Service:     c.Query("service"),
		Instance:    c.Query("instance"),
		Aggregation: metrics.Aggregation(c.DefaultQuery("aggregation", "avg")),
	}

	// 解析时间范围
	if v := c.Query("start_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.Start = time.UnixMilli(ms)
		}
	}
	if v := c.Query("end_time"); v != "" {
		if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.End = time.UnixMilli(ms)
		}
	}

	// 步长
	if v := c.Query("step"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			req.Step = s
		}
	}

	result, err := h.svc.QueryMetric(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "指标查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// current 获取指标当前值。
//
// Query 参数：
//   - metric       指标名称（必填）
//   - service      服务名（可选）
//   - instance     实例名（可选）
//   - aggregation  聚合方式（可选），默认 avg
func (h *MetricsHandler) current(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	metricName := c.Query("metric")
	if metricName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 metric 参数"})
		return
	}

	req := metrics.InstantQueryRequest{
		Metric:      metrics.MetricName(metricName),
		Service:     c.Query("service"),
		Instance:    c.Query("instance"),
		Aggregation: metrics.Aggregation(c.DefaultQuery("aggregation", "avg")),
	}

	result, err := h.svc.GetCurrentValue(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "指标查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// snapshot 获取所有核心指标的当前快照。
//
// 一次性返回 5 大核心指标的当前值，用于概览页面。
//
// Query 参数：
//   - service  服务名（可选），空=所有服务聚合
func (h *MetricsHandler) snapshot(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	service := c.Query("service")

	result, err := h.svc.GetAllMetrics(c.Request.Context(), service)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "指标查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// health 服务健康状态检查。
//
// Query 参数：
//   - service  服务名（必填）
func (h *MetricsHandler) health(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	service := c.Query("service")
	if service == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 service 参数"})
		return
	}

	result, err := h.svc.CheckServiceHealth(c.Request.Context(), service)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "健康检查失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}
