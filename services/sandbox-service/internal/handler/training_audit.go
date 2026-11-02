package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"strconv"
	"sync"
	"time"
)

type trainingAuditSink interface {
	Append(context.Context, int64, string, any) error
	AssertWorkspaceOpen(context.Context, int64) error
}

func (h *DesktopHandler) SetTrainingAudit(s trainingAuditSink) { h.audit = s }
func (h *FilesHandler) SetTrainingAudit(s trainingAuditSink)   { h.audit = s }

type auditedTerminalWriter struct {
	mu        sync.Mutex
	line      []byte
	oversized bool
	writer    io.Writer
	sink      trainingAuditSink
	ctx       context.Context
	sid       int64
	target    string
}

// Persist complete logical output lines so echoed credentials cannot bypass redaction by framing.
func (w *auditedTerminalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sink != nil {
		for _, b := range p {
			if b == '\n' {
				payload := map[string]any{"direction": "output", "target": w.target}
				if w.oversized {
					payload["redacted"] = "oversized terminal line"
					payload["truncated"] = true
					payload["reason"] = "output_limit_exceeded"
				} else {
					payload["data"] = string(w.line) + "\n"
				}
				if err := w.sink.Append(w.ctx, w.sid, "terminal", payload); err != nil {
					return 0, fmt.Errorf("terminal audit unavailable: %w", err)
				}
				w.line = w.line[:0]
				w.oversized = false
			} else if !w.oversized {
				w.line = append(w.line, b)
				if len(w.line) > 16<<10 {
					w.line = nil
					w.oversized = true
				}
			}
		}
	}
	return w.writer.Write(p)
}
func (h *FilesHandler) auditMutation(c *gin.Context) {
	if h.audit == nil {
		c.Next()
		return
	}
	_, e := parseTarget(c)
	if e != nil {
		c.Next()
		return
	}
	sid, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if e = h.audit.AssertWorkspaceOpen(c.Request.Context(), sid); e != nil {
		c.AbortWithStatusJSON(409, gin.H{"code": 40901, "msg": "实训已结束，工作区已冻结"})
		return
	}
	// Record all file mutations and their HTTP outcome. Large multipart uploads record metadata, not binary content.
	payload := map[string]any{"operation": c.FullPath(), "pod": c.Query("pod"), "container": c.Query("container"), "path": c.Query("path")}
	if c.ContentType() == "application/json" {
		raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		if e == nil {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			for _, key := range []string{"path", "from", "to", "mode"} {
				if v, ok := body[key]; ok {
					payload[key] = v
				}
			}
			c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), c.Request.Body))
		}
	}
	c.Next()
	payload["http_status"] = c.Writer.Status()
	_ = h.audit.Append(c.Request.Context(), func() int64 { sid, _ := strconv.ParseInt(c.Param("id"), 10, 64); return sid }(), "file_action", payload)
}

func (w *auditedTerminalWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sink == nil || len(w.line) == 0 && !w.oversized {
		return nil
	}
	payload := map[string]any{"direction": "output", "target": w.target, "truncated": true, "reason": "unsafe_record_boundary", "redacted": "unterminated terminal output omitted"}
	w.line = nil
	w.oversized = false
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), 5*time.Second)
	defer cancel()
	return w.sink.Append(ctx, w.sid, "terminal", payload)
}
