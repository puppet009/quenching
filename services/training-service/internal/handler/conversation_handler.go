// Package handler training-service 的 HTTP 接口。
// AI 对话采用 SSE 流式：前端用 fetch + ReadableStream 读取（EventSource 不支持
// POST/自定义鉴权头），每个事件一行 data: <json>。
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

type ConversationHandler struct {
	svc *service.ChatService
}

func NewConversationHandler(svc *service.ChatService) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

func (h *ConversationHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/ai", UserID())
	{
		g.POST("/conversations", h.create)
		g.GET("/sessions/:session_id/conversation", h.restoreSession)
		g.GET("/sessions/:session_id/conversation/messages", h.sessionHistory)
		g.POST("/conversations/:id/messages/stream", h.stream)
		g.POST("/conversations/:id/approvals", h.approve)
	}
}

type createReq struct {
	SessionID        *int64 `json:"session_id"`
	ConversationType string `json:"conversation_type"`
}

func (h *ConversationHandler) create(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	convType := req.ConversationType
	if convType == "" {
		convType = model.ConvTrainingAssistant
	}
	id, err := h.svc.StartConversation(c.Request.Context(), uid, req.SessionID, convType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": gin.H{"conversation_id": id}})
}

type sendReq struct {
	Message string `json:"message"`
	// HintLevel 可选：请求一次渐进式提示（T3-5）。不传/传 0 为普通应答；
	// 合法取值 1/2/3（方向引导/缩小范围/接近答案），配额与逐级解锁由编排层校验。
	HintLevel *int `json:"hint_level,omitempty"`
}

// approvalReq 修改类工具审批请求。
type approvalReq struct {
	ApprovalID string `json:"approval_id"`
	Approved   bool   `json:"approved"`
}

// approve 投递用户对某修改类工具的“同意/拒绝”决定。
// 对话归属与 send 路径一致由 UserID 中间件保证；approval_id 非法/已失效返回 40001。
func (h *ConversationHandler) approve(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}
	conversationID, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "对话ID非法"})
		return
	}
	var req approvalReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ApprovalID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "approval_id 不能为空"})
		return
	}

	// 校验对话归属：不允许替他人对话投递审批。
	if !h.svc.OwnsConversation(c.Request.Context(), conversationID, uid) {
		c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "对话不存在或无权访问"})
		return
	}

	if !h.svc.ResolveApproval(req.ApprovalID, req.Approved) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "审批不存在、已处理或已超时"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok",
		"data": gin.H{"approval_id": req.ApprovalID, "approved": req.Approved}})
}

func (h *ConversationHandler) stream(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20001, "msg": "缺少用户身份"})
		return
	}
	conversationID, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "对话ID非法"})
		return
	}
	var req sendReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "消息不能为空"})
		return
	}
	// hint_level 仅接受 1/2/3；0 或缺省按普通应答处理。配额耗尽/越级等语义错误
	// 由编排层在 SSE 错误帧返回（错误信息可直接展示给学员）。
	var opts []service.ReplyOption
	if req.HintLevel != nil {
		level := *req.HintLevel
		if level < 0 || level > 3 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "提示级别非法，仅支持 1-3 级"})
			return
		}
		if level > 0 {
			opts = append(opts, service.WithHintLevel(level))
		}
	}

	// SSE 响应头。X-Accel-Buffering 关闭 Nginx 缓冲，保证逐块下发。
	hdr := c.Writer.Header()
	hdr.Set("Content-Type", "text/event-stream; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	events := h.svc.StreamReply(c.Request.Context(), uid, conversationID, req.Message, opts...)
	for ev := range events {
		writeSSE(c, ev)
		// done/error 为终结帧；其后通道关闭。客户端断连时 ctx 取消，
		// 编排层与 gRPC 流会级联取消并关闭通道，range 自然退出。
		if ev.Type == service.StreamDone || ev.Type == service.StreamError {
			return
		}
	}
}

// writeSSE 输出一帧 data: <json> 并立即 flush。
func writeSSE(c *gin.Context, ev service.StreamEvent) {
	payload, _ := json.Marshal(ev)
	_, _ = c.Writer.Write([]byte("data: "))
	_, _ = c.Writer.Write(payload)
	_, _ = c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}

func parsePathID(c *gin.Context, key string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid ID")
	}
	return id, nil
}

func (h *ConversationHandler) restoreSession(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		return
	}
	sid, e := parsePathID(c, "session_id")
	if e != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "班次ID非法"})
		return
	}
	p, e := h.svc.RestoreSessionConversation(c.Request.Context(), uid, sid)
	if e != nil {
		sessionConversationError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": p})
}
func (h *ConversationHandler) sessionHistory(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		return
	}
	sid, e := parsePathID(c, "session_id")
	if e != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "班次ID非法"})
		return
	}
	var before int64
	limit := 50
	if raw := c.Query("before_id"); raw != "" {
		before, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || before < 0 {
			c.JSON(400, gin.H{"code": 10001, "msg": "历史游标非法"})
			return
		}
	}
	if raw := c.Query("limit"); raw != "" {
		limit, e = strconv.Atoi(raw)
		if e != nil || limit < 1 || limit > 100 {
			c.JSON(400, gin.H{"code": 10001, "msg": "limit须为1至100"})
			return
		}
	}
	p, e := h.svc.SessionConversationHistory(c.Request.Context(), uid, sid, before, limit)
	if e != nil {
		sessionConversationError(c, e)
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": p})
}
func sessionConversationError(c *gin.Context, e error) {
	if service.IsSessionConversationMissing(e) {
		c.JSON(404, gin.H{"code": 10004, "msg": "班次不存在或无权访问"})
		return
	}
	c.JSON(503, gin.H{"code": 50001, "msg": "恢复对话暂不可用，请重试"})
}
