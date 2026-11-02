// Package handler 评分体系 HTTP 接口（T2-6）。
package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// ScoringHandler 评分处理器。
type ScoringHandler struct {
	svc *service.ScoringService
}

// NewScoringHandler 创建评分处理器。
func NewScoringHandler(svc *service.ScoringService) *ScoringHandler {
	return &ScoringHandler{svc: svc}
}

// RegisterRoutes 注册路由。
//
// POST   /api/v1/scoring/calculate     执行评分
// GET    /api/v1/scoring/result        获取评分结果
// GET    /api/v1/scoring/dimensions    获取评分维度配置
// GET    /api/v1/scoring/history       评分历史记录
func (h *ScoringHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/scoring", UserID())
	{
		g.POST("/calculate", h.calculate)
		g.GET("/result", h.getResult)
		g.GET("/dimensions", h.getDimensions)
		g.GET("/history", h.getHistory)
	}
}

// calculate 执行评分。
//
// 请求体：
//
//	{
//	  "session_id": "sess-abc",
//	  "fault_start_time": 1726000000000,
//	  "recovery_time": 1726000600000,
//	  "recovery_score": 85.5,
//	  "method_score": 75.0,
//	  "knowledge_score": 80.0,
//	  "collab_score": 90.0,
//	  "expected_seconds": 600
//	}
//
// 响应示例：
//
//	{
//	  "code": 0,
//	  "msg": "ok",
//	  "data": {
//	    "session_id": "sess-abc",
//	    "user_id": 123,
//	    "total_score": 82.5,
//	    "grade": {
//	      "grade": "good",
//	      "name": "良好",
//	      "min_score": 80,
//	      "max_score": 89.99,
//	      "color": "#1890ff",
//	      "description": "表现不错，基本掌握"
//	    },
//	    "dimensions": [
//	      {
//	        "dimension": "speed",
//	        "name": "排障速度",
//	        "weight": 0.3,
//	        "score": 90,
//	        "weighted": 27,
//	        "feedback": "用时 600 秒，在预期时间内完成"
//	      },
//	      ...
//	    ],
//	    "overall_time": 600,
//	    "recovery_time": 600,
//	    "scored_at": 1726000000000,
//	    "feedback": "表现不错！得分 82.5 分（良好）..."
//	  }
//	}
func (h *ScoringHandler) calculate(c *gin.Context) {
	userID, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	var req struct {
		SessionID       string  `json:"session_id" binding:"required"`
		FaultStartTime  int64   `json:"fault_start_time"`
		RecoveryTime    int64   `json:"recovery_time"`
		RecoveryScore   float64 `json:"recovery_score"`
		MethodScore     float64 `json:"method_score"`
		KnowledgeScore  float64 `json:"knowledge_score"`
		CollabScore     float64 `json:"collab_score"`
		ExpectedSeconds int     `json:"expected_seconds"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "参数错误: " + err.Error()})
		return
	}

	input := model.ScoreInput{
		SessionID:       req.SessionID,
		UserID:          userID,
		FaultStartTime:  req.FaultStartTime,
		RecoveryTime:    req.RecoveryTime,
		RecoveryScore:   req.RecoveryScore,
		MethodScore:     req.MethodScore,
		KnowledgeScore:  req.KnowledgeScore,
		CollabScore:     req.CollabScore,
		ExpectedSeconds: req.ExpectedSeconds,
	}

	result, err := h.svc.Calculate(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50001, "msg": "评分失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// getResult 获取评分结果。
//
// Query 参数：
//   - session_id  会话ID（必填）
func (h *ScoringHandler) getResult(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	sessionID := c.Query("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "缺少 session_id 参数"})
		return
	}

	result, exists := h.svc.GetResult(sessionID)
	if !exists {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"msg":  "ok",
			"data": nil,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// getDimensions 获取评分维度配置。
func (h *ScoringHandler) getDimensions(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	dims := h.svc.GetDimensions()

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"dimensions":   dims,
			"total_weight": 1.0,
		},
	})
}

// getHistory 获取评分历史记录。
//
// Query 参数：
//   - limit   返回条数（可选，默认 10）
//   - user_id 用户ID（可选，默认当前用户）
func (h *ScoringHandler) getHistory(c *gin.Context) {
	userID, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}

	limit := 10
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	// 查询指定用户的历史
	targetUserID := userID
	if v := c.Query("user_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			targetUserID = n
		}
	}

	results := h.svc.ListResults(targetUserID, limit)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"total": len(results),
			"items": results,
		},
	})
}
