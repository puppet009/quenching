// Package handler 人机对照数据统计 HTTP 接口（T3-26）。
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// ABStatsHandler 对照统计接口处理器。
type ABStatsHandler struct {
	svc *service.ABStatsService
}

// NewABStatsHandler 创建对照统计处理器。
func NewABStatsHandler(svc *service.ABStatsService) *ABStatsHandler {
	return &ABStatsHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// GET /api/v1/ab-stats/summary       对照统计汇总
// GET /api/v1/ab-stats/by-project    按项目维度对照统计
func (h *ABStatsHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/ab-stats", UserID())
	{
		g.GET("/summary", h.summary)
		g.GET("/by-project", h.byProject)
	}
}

// summary 对照统计汇总（四项核心差异 + 样本量提示）。
//
// Query 参数：
//   - project_id   项目ID（可选）
//   - difficulty   难度（可选，easy/normal/hard）
//   - start_date   开始日期 YYYY-MM-DD（可选）
//   - end_date     结束日期 YYYY-MM-DD（可选）
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "pair_count": 42,
//	    "speed_diff_pct": -15.5,
//	    "accuracy_diff_pct": 8.2,
//	    "dangerous_op_diff_pct": -25.0,
//	    "verify_diff_pct": 35.0,
//	    "sample_info": {
//	      "pair_count": 42,
//	      "user_count": 42,
//	      "enough_sample": true,
//	      "tip": "样本量充足，对照结果具有统计意义",
//	      "confidence_level": "高"
//	    }
//	  }
//	}
func (h *ABStatsHandler) summary(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	filter := parseABFilter(c)

	summary, err := h.svc.GetSummary(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": summary,
	})
}

// byProject 按项目维度汇总对照统计。
//
// Query 参数同 summary。
func (h *ABStatsHandler) byProject(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	filter := parseABFilter(c)

	result, err := h.svc.ListByProject(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "统计失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// ——— 工具函数 ———

func parseABFilter(c *gin.Context) model.ABFilter {
	f := model.ABFilter{}

	if v := c.Query("project_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.ProjectID = id
		}
	}

	if v := c.Query("difficulty"); v != "" {
		f.Difficulty = v
	}

	if v := c.Query("start_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.StartDate = t
		}
	}

	if v := c.Query("end_date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			f.EndDate = t.AddDate(0, 0, 1) // 包含当天
		}
	}

	return f
}
