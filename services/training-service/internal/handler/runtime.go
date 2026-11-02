// Package handler 的 runtime.go：训练会话结束钩子（T3-12 恢复路径）。
//
// 由云端沙箱释放流程在实训会话结束（完成/中止/超时）时回调：
//
//	POST /api/v1/runtime/sessions/:id/finish
//
// 做两件事，均尽力而为、接口幂等（重复回调安全）：
//  1. 撤销本服务登记的全部"踩坑次生故障"（ChatService.RevokeSessionConsequences，
//     按 ai_consequence_injections 中 injected 记录逐条 blade destroy）；
//  2. 通知 fault-engine 停止该会话的剧本自动编排并补偿撤销剧本故障
//     （FaultSessionStopper，HTTP /sessions/:id/stop）。
//
// 两路撤销针对不同来源的实验（consequence / scenario），互不重复；
// 任一路失败都继续尝试另一路，并把首个错误返回给调用方记录/重试。
//
// 撤销全部成功后，若装配了评分器（最小闭环，T3-24），尽力而为触发会话结束评分：
// 仅"正常完成"计分，评分使用独立超时上下文，任何结果/失败只记日志，
// 绝不改变撤销接口的响应码与响应体（云端只据 500 重试，幂等守卫防重复评分）。
// 走内部网络，不挂用户鉴权中间件。
package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/service"
)

// consequenceRevoker 会话级次生故障撤销能力（由 service.ChatService 实现）。
type consequenceRevoker interface {
	RevokeSessionConsequences(ctx context.Context, sessionID int64) error
}

// faultSessionStopper 故障引擎会话结束通知能力（由 service.FaultEngineHTTPClient 实现）。
type faultSessionStopper interface {
	StopSession(ctx context.Context, sessionID int64) error
}

// sessionScorer 会话结束评分能力（由 *service.ScoreService 实现，最小闭环 T3-24）。
type sessionScorer interface {
	FinishSessionScore(ctx context.Context, sessionID int64, outcome service.FinishOutcome) (*service.FinishSessionResult, error)
}

// finishRequest 云端 finish 回调的可选请求体。
// 当前云端契约为无 body：缺省/无法解析时 outcome 一律按 Unspecified 处理，由服务端据会话记录判定。
type finishRequest struct {
	Outcome service.FinishOutcome `json:"outcome"` // completed | aborted | timeout
}

// RuntimeHandler 训练侧会话结束钩子处理器。
type RuntimeHandler struct {
	revoker consequenceRevoker
	stopper faultSessionStopper // 可为 nil（fault-engine 未配置时仅撤次生故障）
	scorer  sessionScorer       // 可为 nil（评分未装配时保持纯撤销旧行为）
}

// NewRuntimeHandler 创建会话结束钩子处理器。
// revoker 必传（对话编排服务）；stopper 为 fault-engine HTTP 客户端，未配置时传 nil。
func NewRuntimeHandler(revoker consequenceRevoker, stopper faultSessionStopper) *RuntimeHandler {
	return &RuntimeHandler{revoker: revoker, stopper: stopper}
}

// SetSessionScorer 装配会话结束评分器（可选注入，保持两参构造与既有调用不变）。返回 h 便于链式装配。
func (h *RuntimeHandler) SetSessionScorer(scorer sessionScorer) *RuntimeHandler {
	h.scorer = scorer
	return h
}

// RegisterRoutes 注册运行时钩子路由。
func (h *RuntimeHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/runtime")
	{
		g.POST("/sessions/:id/finish", h.finish)
	}
}

// finish 撤销会话相关的全部故障注入。
func (h *RuntimeHandler) finish(c *gin.Context) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return
	}

	// 可选 body 声明结束类型；无 body（当前云端契约）或解析失败均按 Unspecified 处理。
	var outcome service.FinishOutcome
	if c.Request.Body != nil {
		var req finishRequest
		if jerr := json.NewDecoder(c.Request.Body).Decode(&req); jerr == nil {
			outcome = req.Outcome
		}
	}

	// 撤销涉及 blade 动作，给足时间且不绑定可能已断开的请求上下文。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var firstErr error
	// 1. 撤销踩坑次生故障（consequence 来源）。
	if err := h.revoker.RevokeSessionConsequences(ctx, sessionID); err != nil {
		firstErr = err
	}
	// 2. 通知故障引擎停剧本编排并撤销剧本故障（scenario 来源）。
	if h.stopper != nil {
		if err := h.stopper.StopSession(ctx, sessionID); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		// 撤销未完全成功：返回 500，供云上端记录并重试（接口幂等）。
		// 此时不评分——云端重试会再次走到这里，撤销成功后才触发评分。
		c.JSON(http.StatusInternalServerError, gin.H{"code": 50000, "msg": "会话故障撤销未完全成功: " + firstErr.Error()})
		return
	}

	// 3. 撤销全部成功后尽力评分（最小闭环）：独立超时、只记日志，绝不影响撤销响应。
	h.scoreOnFinish(sessionID, outcome)

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// scoreOnFinish 在撤销成功后尽力触发会话结束评分。
// scorer 未装配则直接跳过；评分使用独立于撤销链路的 30s 超时上下文，
// 任何失败仅记录日志（云端据 500 重试撤销，评分幂等守卫保证不重复计分）。
func (h *RuntimeHandler) scoreOnFinish(sessionID int64, outcome service.FinishOutcome) {
	if h.scorer == nil {
		return
	}
	scoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := h.scorer.FinishSessionScore(scoreCtx, sessionID, outcome)
	if err != nil {
		log.Printf("runtime finish: 会话 %d 结束评分失败（不影响撤销结果）: %v", sessionID, err)
		return
	}
	if result == nil {
		return
	}
	if result.TechOnly {
		log.Printf("runtime finish: 会话 %d 技术分已落库、协作评估降级: %s", sessionID, result.Reason)
		return
	}
	if !result.Scored {
		log.Printf("runtime finish: 会话 %d 未计分: %s", sessionID, result.Reason)
		return
	}
	log.Printf("runtime finish: 会话 %d 结束评分完成: %s", sessionID, result.Reason)
}
