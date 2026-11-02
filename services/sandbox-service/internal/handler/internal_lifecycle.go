// internal_lifecycle.go 服务间沙箱生命周期接口。
//
// training-service 掌握"什么算活动、什么时候算结束"，sandbox-service 掌握资源，
// 因此这两个判断通过内部接口回传给沙箱侧，不经网关、不对前端暴露（X-Internal-Token）。
//
// 端点（统一前缀 /internal/v1/sandboxes/:session_id）：
//   - POST /activity  刷新活跃度租期（观察类操作上报时调用；终端输入由 WS 侧自行刷新）
//   - POST /complete  回写实训结束时刻，沙箱进入结束宽限期（默认 5 分钟）后由回收扫描释放
//   - POST /release   立即回收（弃班判定用：namespace 当场删除，释放槽位）
//
// body 可选 {"reason":"completed|abandoned|manual"}；全部幂等。
package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

// lifecycleStore 生命周期接口需要的存储能力。
type lifecycleStore interface {
	service.ActivityPort
	GetBySessionID(ctx context.Context, sessionID int64) (*model.Sandbox, error)
}

// InternalLifecycleHandler 沙箱生命周期内部接口处理器。
type InternalLifecycleHandler struct {
	store    lifecycleStore
	rec      *service.Reconciler
	activity *service.ActivityTracker
	token    string
}

// NewInternalLifecycleHandler 创建处理器。
func NewInternalLifecycleHandler(
	store lifecycleStore, rec *service.Reconciler, activity *service.ActivityTracker, token string,
) *InternalLifecycleHandler {
	return &InternalLifecycleHandler{store: store, rec: rec, activity: activity, token: token}
}

// RegisterRoutes 注册内部生命周期路由（与只读接口共用 /internal/v1/sandboxes 前缀）。
func (h *InternalLifecycleHandler) RegisterRoutes(r gin.IRouter) {
	g := r.Group("/internal/v1/sandboxes", h.checkToken)
	g.GET("/:session_id/readiness", h.readiness)
	g.POST("/:session_id/activity", h.activityTouch)
	g.POST("/:session_id/complete", h.complete)
	g.POST("/:session_id/release", h.release)
	g.POST("/:session_id/brain-cleanup", h.brainCleanup)
}

func (h *InternalLifecycleHandler) activityTouch(c *gin.Context) {
	sessionID, ok := sessionIDParam(c)
	if !ok {
		return
	}
	if err := h.store.TouchActivity(c.Request.Context(), sessionID, time.Now()); err != nil {
		h.fail(c, err)
		return
	}
	lifecycleOK(c)
}

func (h *InternalLifecycleHandler) complete(c *gin.Context) {
	sessionID, ok := sessionIDParam(c)
	if !ok {
		return
	}
	reason := bodyReason(c)
	if err := h.store.MarkCompleted(c.Request.Context(), sessionID, time.Now(), reason); err != nil {
		h.fail(c, err)
		return
	}
	sb, err := h.store.GetBySessionID(c.Request.Context(), sessionID)
	if err != nil {
		h.fail(c, err)
		return
	}
	if sb == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 30004, "msg": "沙箱不存在"})
		return
	}
	if err := h.rec.K8sClient().CloseSandboxAccess(c.Request.Context(), sb.Namespace); err != nil {
		h.fail(c, err)
		return
	}
	lifecycleOK(c)
}

// release 立即回收：先回写结束时刻（便于运行时长与原因追溯），再删 namespace。
func (h *InternalLifecycleHandler) release(c *gin.Context) {
	sessionID, ok := sessionIDParam(c)
	if !ok {
		return
	}
	reason := bodyReason(c)
	sb, err := h.store.GetBySessionID(c.Request.Context(), sessionID)
	if err != nil {
		h.fail(c, err)
		return
	}
	if sb == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 30004, "msg": "沙箱不存在"})
		return
	}
	if sb.Status == model.StatusReleased {
		lifecycleOK(c) // 幂等：已释放视为成功
		return
	}
	if err := h.store.MarkCompleted(c.Request.Context(), sessionID, time.Now(), reason); err != nil {
		h.fail(c, err)
		return
	}
	if err := h.rec.ReleaseSandbox(c.Request.Context(), sb); err != nil {
		h.fail(c, err)
		return
	}
	h.activity.Forget(sessionID)
	lifecycleOK(c)
}

func (h *InternalLifecycleHandler) checkToken(c *gin.Context) {
	if h.token == "" {
		return
	}
	if c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 40101, "msg": "内部令牌无效"})
	}
}

func (h *InternalLifecycleHandler) fail(c *gin.Context, err error) {
	c.JSON(http.StatusBadGateway, gin.H{"code": 50201, "msg": err.Error()})
}

func sessionIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("session_id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 40001, "msg": "session_id 非法"})
		return 0, false
	}
	return id, true
}

func lifecycleOK(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

func bodyReason(c *gin.Context) string {
	var body struct {
		Reason string `json:"reason"`
	}
	// 缺省或解析失败都按"用户主动离开"记，不影响主流程。
	_ = c.ShouldBindJSON(&body)
	if body.Reason == "" {
		return service.ReasonUserExit
	}
	return body.Reason
}

func (h *InternalLifecycleHandler) brainCleanup(c *gin.Context) {
	if h.token == "" {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	sid, ok := sessionIDParam(c)
	if !ok {
		return
	}
	sb, err := h.store.GetBySessionID(c.Request.Context(), sid)
	if err != nil {
		h.fail(c, err)
		return
	}
	if sb == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 30004, "msg": "sandbox missing"})
		return
	}
	if err = h.store.MarkCompleted(c.Request.Context(), sid, time.Now(), "completed"); err != nil {
		h.fail(c, err)
		return
	}
	status, err := h.rec.CleanupBrainSession(c.Request.Context(), sb)
	if err != nil {
		h.fail(c, err)
		return
	}
	if h.activity != nil {
		h.activity.Forget(sid)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"status": status}})
}

// Readiness is fail closed and returns only the status needed by the training timer.
func (h *InternalLifecycleHandler) readiness(c *gin.Context) {
	if h.token == "" {
		c.AbortWithStatus(503)
		return
	}
	sid, ok := sessionIDParam(c)
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(c.Query("user_id"), 10, 64)
	if err != nil || uid <= 0 {
		c.AbortWithStatus(400)
		return
	}
	sb, err := h.store.GetBySessionID(c.Request.Context(), sid)
	if err != nil {
		h.fail(c, err)
		return
	}
	ready := sb != nil && sb.UserID == uid && (sb.Status == model.StatusReady || sb.Status == model.StatusRunning) && sb.CompletedAt.IsZero() && !sb.IsExpired(time.Now())
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"ready": ready}})
}
