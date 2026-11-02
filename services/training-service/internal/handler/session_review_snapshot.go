package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/training-service/internal/service"
	"io"
	"net/http"
	"strconv"
)

type SessionReviewSnapshotHandler struct {
	svc   *service.SessionReviewSnapshotService
	token string
}

func NewSessionReviewSnapshotHandler(s *service.SessionReviewSnapshotService, token string) *SessionReviewSnapshotHandler {
	return &SessionReviewSnapshotHandler{s, token}
}
func (h *SessionReviewSnapshotHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/sessions", func(c *gin.Context) {
		if h.token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.token)) != 1 {
			c.AbortWithStatusJSON(401, gin.H{"code": 14001, "msg": "内部接口未授权"})
			return
		}
		c.Next()
	})
	g.GET("/:id/review-eligibility", h.eligibility)
	g.POST("/:id/review-snapshot", h.freeze)
	g.GET("/:id/review-snapshot", h.manifest)
	g.GET("/:id/review-snapshot/sources", h.sources)
	g.GET("/:id/review-snapshot/source", h.source)
}
func snapshotInt(c *gin.Context, key string, def int64, min int64) (int64, bool) {
	raw := c.Query(key)
	value := def
	if raw != "" {
		parsed, e := strconv.ParseInt(raw, 10, 64)
		if e != nil {
			snapshotError(c, service.ErrReviewSourceRange)
			return 0, false
		}
		value = parsed
	}
	if value < min {
		snapshotError(c, service.ErrReviewSourceRange)
		return 0, false
	}
	return value, true
}
func snapshotIdentity(c *gin.Context) (int64, int64, bool) {
	sid, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || sid <= 0 {
		snapshotError(c, service.ErrReviewSourceRange)
		return 0, 0, false
	}
	u, ok := snapshotInt(c, "user_id", 0, 1)
	return u, sid, ok
}
func snapshotError(c *gin.Context, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrReviewSourceNotFound) {
		code = 404
	} else if errors.Is(err, service.ErrReviewSourceRange) {
		code = 400
	} else if errors.Is(err, service.ErrReviewSessionActive) {
		code = 409
	}
	msg := "复盘输入暂不可用"
	if code != 503 {
		msg = err.Error()
	}
	c.JSON(code, gin.H{"code": code, "msg": msg})
}
func snapshotReply(c *gin.Context, v any, err error) {
	if err != nil {
		snapshotError(c, err)
		return
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok", "data": v})
}
func (h *SessionReviewSnapshotHandler) eligibility(c *gin.Context) {
	u, s, ok := snapshotIdentity(c)
	if !ok {
		return
	}
	p, ok := snapshotInt(c, "project_id", 0, 0)
	if !ok {
		return
	}
	v, e := h.svc.Eligibility(c.Request.Context(), u, s, p)
	snapshotReply(c, v, e)
}
func (h *SessionReviewSnapshotHandler) freeze(c *gin.Context) {
	sid, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || sid <= 0 {
		snapshotError(c, service.ErrReviewSourceRange)
		return
	}
	var body struct {
		UserID    int64 `json:"user_id"`
		ProjectID int64 `json:"project_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.UserID <= 0 || body.ProjectID < 0 || decoder.Decode(&struct{}{}) != io.EOF {
		snapshotError(c, service.ErrReviewSourceRange)
		return
	}
	v, e := h.svc.Freeze(c.Request.Context(), body.UserID, sid, body.ProjectID)
	snapshotReply(c, v, e)
}
func (h *SessionReviewSnapshotHandler) manifest(c *gin.Context) {
	u, s, ok := snapshotIdentity(c)
	if !ok {
		return
	}
	v, e := h.svc.Manifest(c.Request.Context(), u, s)
	snapshotReply(c, v, e)
}
func (h *SessionReviewSnapshotHandler) sources(c *gin.Context) {
	u, s, ok := snapshotIdentity(c)
	if !ok {
		return
	}
	version, ok := snapshotInt(c, "version", 0, 1)
	if !ok {
		return
	}
	ordinal, ok := snapshotInt(c, "ordinal", 0, 0)
	if !ok {
		return
	}
	offset, ok := snapshotInt(c, "offset_bytes", 0, 0)
	if !ok {
		return
	}
	budget, ok := snapshotInt(c, "budget", 48*1024, 512)
	if !ok {
		return
	}
	if budget > 48*1024 || offset > 2147483647 {
		snapshotError(c, service.ErrReviewSourceRange)
		return
	}
	v, e := h.svc.Sources(c.Request.Context(), u, s, version, rc.Cursor{Ordinal: ordinal, OffsetBytes: int(offset)}, int(budget))
	snapshotReply(c, v, e)
}
func (h *SessionReviewSnapshotHandler) source(c *gin.Context) {
	u, s, ok := snapshotIdentity(c)
	if !ok {
		return
	}
	version, ok := snapshotInt(c, "version", 0, 1)
	if !ok {
		return
	}
	offset, ok := snapshotInt(c, "offset_bytes", 0, 0)
	if !ok {
		return
	}
	limit, ok := snapshotInt(c, "limit_bytes", 8192, 1)
	if !ok {
		return
	}
	ref := c.Query("reference")
	if ref == "" || len(ref) > 128 || limit > 8192 || offset > 2147483647 {
		snapshotError(c, service.ErrReviewSourceRange)
		return
	}
	v, e := h.svc.Read(c.Request.Context(), u, s, version, ref, int(offset), int(limit))
	snapshotReply(c, v, e)
}
