package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	. "github.com/quenching/proto/operationevidence"
	"io"
	"net/http"
	"strconv"
)

type OperationEvidenceHandler struct {
	Store  Store
	Owned  func(context.Context, int64, int64) error
	Ingest func(context.Context, Delivery) error
	Token  string
}

func (h OperationEvidenceHandler) Register(r *gin.Engine) {
	r.POST("/internal/v1/operation-evidence", h.ingest)
	g := r.Group("/api/v1/sessions/:id/operations")
	g.GET("", h.list)
	g.GET("/source", h.read)
}
func (h OperationEvidenceHandler) ingest(c *gin.Context) {
	if h.Token == "" || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-Internal-Token")), []byte(h.Token)) != 1 {
		c.JSON(401, gin.H{"code": 14001})
		return
	}
	var d Delivery
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<20)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || dec.Decode(&struct{}{}) != io.EOF || h.Ingest == nil {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	if err := h.Ingest(c.Request.Context(), d); err != nil {
		c.JSON(409, gin.H{"code": 14009, "msg": "audit rejected"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": gin.H{"event_id": d.Evidence.EventID}})
}
func (h OperationEvidenceHandler) scope(c *gin.Context) (int64, bool) {
	uid, err := strconv.ParseInt(c.GetHeader("X-User-Id"), 10, 64)
	sid, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || uid <= 0 {
		c.JSON(401, gin.H{"code": 20001})
		return 0, false
	}
	if e != nil || sid <= 0 || h.Owned == nil || h.Owned(c.Request.Context(), uid, sid) != nil {
		c.JSON(404, gin.H{"code": 14004})
		return 0, false
	}
	return sid, true
}
func (h OperationEvidenceHandler) list(c *gin.Context) {
	sid, ok := h.scope(c)
	if !ok {
		return
	}
	before, err := strconv.ParseInt(c.DefaultQuery("before_sequence", "0"), 10, 64)
	limit, e := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil || e != nil || before < 0 || limit < 1 || limit > 50 {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	p, err := h.Store.List(c.Request.Context(), sid, before, limit)
	if err != nil {
		c.JSON(503, gin.H{"code": 15003})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": p})
}
func (h OperationEvidenceHandler) read(c *gin.Context) {
	sid, ok := h.scope(c)
	if !ok {
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset_bytes", "0"))
	limit, e := strconv.Atoi(c.DefaultQuery("limit_bytes", "8192"))
	if err != nil || e != nil || offset < 0 || limit < 1 || limit > 8192 {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	t, err := h.Store.Read(c.Request.Context(), sid, c.Query("ref"))
	if errors.Is(err, ErrNotFound) {
		c.JSON(404, gin.H{"code": 14004})
		return
	}
	if err != nil {
		c.JSON(503, gin.H{"code": 15003})
		return
	}
	fragment, err := ReadFragment(t, offset, limit)
	if err != nil {
		c.JSON(400, gin.H{"code": 10001})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": fragment})
}
