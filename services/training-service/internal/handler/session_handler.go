// 训练会话接口（个人中心 / 人机对照真接口）。
//
// 端点（鉴权由网关完成，用户身份取自 X-User-Id）：
//   - GET    /api/v1/training/sessions             历史分页（难度/模式/结果/关键词过滤）
//   - GET    /api/v1/training/sessions/overview   概览统计 + 双维雷达
//   - GET    /api/v1/training/sessions/:id         会话详情（训练页 header）
//   - GET    /api/v1/training/comparison          人机对照（最近一对双向配对会话）
//   - POST   /api/v1/training/comparison/summary  人机对照 AI 总结 SSE（ai-service 生成）
//   - POST   /api/v1/training/sessions            创建会话（交叉分组前两次 / 其后普通）
//   - POST   /api/v1/training/sessions/:id/start  开始会话
//   - POST   /api/v1/training/sessions/:id/finish  结束会话（completed/aborted/failed）
//   - POST   /api/v1/training/sessions/:id/operations  批量操作上报
package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/logger"
	"github.com/quenching/go-common/response"
	"go.uber.org/zap"

	"github.com/quenching/training-service/internal/service"
)

// SessionHandler 训练会话处理器。
type SessionHandler struct {
	svc *service.SessionService
}

// NewSessionHandler 创建训练会话处理器。
func NewSessionHandler(svc *service.SessionService) *SessionHandler {
	return &SessionHandler{svc: svc}
}

// RegisterRoutes 注册训练会话相关路由。
func (h *SessionHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/training", UserID())
	{
		g.GET("/sessions", h.listSessions)
		g.GET("/sessions/overview", h.overview)
		// gin radix 允许静态段 overview 与参数段 :id 共存。
		g.GET("/sessions/:id", h.getSessionDetail)
		g.GET("/comparison", h.comparison)
		g.POST("/comparison/summary", h.comparisonSummary)

		g.POST("/sessions", h.createSession)
		g.POST("/sessions/:id/start", h.startSession)
		g.POST("/sessions/:id/finish", h.finishSession)
		g.POST("/sessions/:id/operations", h.addOperations)
	}
}

// ——— 历史 ———

func (h *SessionHandler) listSessions(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	f := service.SessionListFilter{
		Status:     strings.TrimSpace(c.Query("status")),
		Difficulty: strings.TrimSpace(c.Query("difficulty")),
		AIMode:     strings.TrimSpace(c.Query("aiMode")),
		Keyword:    strings.TrimSpace(c.Query("keyword")),
	}
	if f.Status != "" && f.Status != "finished" {
		response.FailWithHTTP(c, http.StatusBadRequest, response.ErrParam)
		return
	}
	if raw := c.Query("page"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			f.Page = v
		}
	}
	if raw := c.Query("pageSize"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			f.PageSize = v
		}
	}
	if raw := c.Query("success"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			f.Success = &v
		}
	}

	page, err := h.svc.ListHistory(c.Request.Context(), uid, f)
	if err != nil {
		logger.From(c.Request.Context()).Error("查询训练会话历史失败", zap.Error(err))
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, page)
}

// ——— 概览 ———

func (h *SessionHandler) overview(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	out, err := h.svc.Overview(c.Request.Context(), uid)
	if err != nil {
		logger.From(c.Request.Context()).Error("查询训练概览失败", zap.Error(err))
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, out)
}

// ——— 详情 ———

func (h *SessionHandler) getSessionDetail(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	sessionID, err := parsePathID(c, "id")
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	detail, err := h.svc.Detail(c.Request.Context(), sessionID, uid)
	if err != nil {
		h.failBySessionError(c, err)
		return
	}
	response.OK(c, detail)
}

// ——— 人机对照 ———

func (h *SessionHandler) comparison(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	out, err := h.svc.Comparison(c.Request.Context(), uid)
	if err != nil {
		logger.From(c.Request.Context()).Error("查询人机对照失败", zap.Error(err))
		response.Fail(c, response.ErrInternal)
		return
	}
	if out == nil {
		response.Fail(c, response.ErrNotFound)
		return
	}
	response.OK(c, out)
}

// ——— 人机对照 AI 总结（SSE）———

// comparisonSummary 调 ai-service 流式产出四段式对照点评，帧格式与 AI 助教一致。
// 配对不存在 / 端口未装配等错误发生在写流之前，按统一错误码返回，前端据此降级
// 使用规则文案；流开始后出错则以 error 终结帧下发（writeSSE 复用 conversation_handler）。
func (h *SessionHandler) comparisonSummary(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	events, err := h.svc.StreamComparisonSummary(c.Request.Context(), uid)
	if err != nil {
		h.failBySessionError(c, err)
		return
	}

	// SSE 响应头。X-Accel-Buffering 关闭 Nginx 缓冲，保证逐块下发。
	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	for ev := range events {
		writeSSE(c, ev)
		// done/error 为终结帧；其后通道关闭。客户端断连时 ctx 取消，
		// gRPC 流级联取消并关闭通道，range 自然退出。
		if ev.Type == service.StreamDone || ev.Type == service.StreamError {
			return
		}
	}
}

// ——— 创建 ———

func (h *SessionHandler) createSession(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	var req service.CreateSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if req.ProjectID <= 0 && req.TemplateSlug == "" {
		response.Fail(c, response.ErrParam)
		return
	}

	out, err := h.svc.Create(c.Request.Context(), uid, req)
	if err != nil {
		h.failCreateSession(c, err)
		return
	}
	response.OK(c, out)
}

// ——— 开始 ———

func (h *SessionHandler) startSession(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	sessionID, err := parsePathID(c, "id")
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	out, err := h.svc.Start(c.Request.Context(), sessionID, uid)
	if err != nil {
		h.failBySessionError(c, err)
		return
	}
	response.OK(c, out)
}

// ——— 结束 ———

func (h *SessionHandler) finishSession(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	sessionID, err := parsePathID(c, "id")
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	var req service.FinishSessionRequest
	// 允许空 body：使用 ShouldBindJSON 时 EOF 视为缺省 outcome。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Fail(c, response.ErrParam)
		return
	}

	out, err := h.svc.Finish(c.Request.Context(), sessionID, uid, req)
	if err != nil {
		h.failBySessionError(c, err)
		return
	}
	response.OK(c, out)
}

// ——— 操作上报 ———

func (h *SessionHandler) addOperations(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	sessionID, err := parsePathID(c, "id")
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	var req service.OperationsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	out, err := h.svc.AddOperations(c.Request.Context(), sessionID, uid, req)
	if err != nil {
		h.failBySessionError(c, err)
		return
	}
	response.OK(c, out)
}

// failCreateSession 把创建会话的服务层错误映射为统一错误码。
// 客户端可纠的输入问题返回参数错误；其余视为内部错误，同时必须记录
// 真实原因（此前直接返回 ErrInternal 会把错误吞掉，导致日志无法排查）。
func (h *SessionHandler) failCreateSession(c *gin.Context, err error) {
	if errors.Is(err, service.ErrSessionProjectRequired) {
		response.Fail(c, response.ErrParam)
		return
	}
	uid, _ := MustUserID(c)
	logger.From(c.Request.Context()).Error("创建训练会话失败",
		zap.Error(err),
		zap.Int64("uid", uid),
	)
	response.Fail(c, response.ErrInternal)
}

// failBySessionError 把服务层哨兵错误映射为统一错误码。
func (h *SessionHandler) failBySessionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSessionNotFound):
		response.Fail(c, response.ErrNotFound)
	case errors.Is(err, service.ErrSessionNotStartable),
		errors.Is(err, service.ErrSessionNotFinishable):
		response.Fail(c, response.ErrConflict)
	case errors.Is(err, service.ErrInvalidOutcome):
		response.Fail(c, response.ErrParam)
	default:
		logger.From(c.Request.Context()).Error("训练会话请求处理失败", zap.Error(err))
		response.Fail(c, response.ErrInternal)
	}
}
