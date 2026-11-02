// terminal_ws.go 真实终端 WebSocket 端点（桌面化第一批）。
//
//	GET /api/v1/sandboxes/:id/exec?target=<pod>&container=<容器>
//
// 帧协议与前端 useTerminalWS 对齐：
//
//	client → server: {"type":"data","data":...} / {"type":"resize","cols","rows"} / ping
//	server → client: {"type":"hello",...} / {"type":"data","data":...}
//
// 鉴权复用 UserID() 无法作用于升级请求（WS 不走普通 JSON 中间件链时），因此
// 这里直接信任网关已校验并透传；本地裸连需在网关之后访问。
package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/quenching/go-common/logger"
	"go.uber.org/zap"

	"github.com/quenching/sandbox-service/internal/service"
)

// wsUpgrader 终端 WS 升级器。
var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 5 * time.Second,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

const (
	wsWriteWait = 10 * time.Second
	wsPongWait  = 90 * time.Second
)

// wsClientMessage 浏览器 → 服务端消息。
type wsClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// wsStdout 把 exec stdout 编码成 data 帧并发给浏览器（加锁串行化写）。
type wsStdout struct {
	conn *websocket.Conn
	mu   *sync.Mutex
}

// Write 实现 io.Writer。
func (w *wsStdout) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if err := w.conn.WriteJSON(map[string]any{
		"type": "data",
		"data": string(p),
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Exec 终端 WS 入口。
func (h *DesktopHandler) Exec(c *gin.Context) {
	if h.Owned != nil {
		uid, _ := c.Get(ContextUID)
		u, _ := uid.(int64)
		sid, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		if h.Owned(c.Request.Context(), u, sid) != nil {
			c.AbortWithStatusJSON(404, gin.H{"code": 14004})
			return
		}
	}
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "会话ID非法"})
		return
	}
	target := c.Query("target")
	container := c.Query("container")
	if target == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "缺少目标服务器"})
		return
	}

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // 升级失败 upgrader 已写响应
	}
	defer conn.Close()

	var writeMu sync.Mutex
	send := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteJSON(v)
	}

	// hello：带连接 ID，前端据此判定已连接。
	connectionID := uuid.NewString()
	if err := send(map[string]any{
		"type": "hello",
		"data": map[string]string{"connection_id": connectionID},
	}); err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	stdinR, stdinW := io.Pipe()
	defer stdinR.Close()

	sizes := service.NewResizeQueue()
	defer sizes.Close() // 结束时让尺寸队列退出

	stdout := &wsStdout{conn: conn, mu: &writeMu}

	// exec 在独立 goroutine 跑，主 goroutine 负责读 ws。
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	fail := func(stage string, err error, message string) {
		logger.From(ctx).Warn("终端连接失败", zap.Int64("session_id", sessionID), zap.String("target", target), zap.String("container", container), zap.String("stage", stage), zap.Error(err))
		_ = send(map[string]any{"type": "error", "message": message})
	}

	auditWriter := &auditedTerminalWriter{writer: stdout, sink: h.audit, ctx: ctx, sid: sessionID, target: target}
	defer auditWriter.Close()
	if h.audit != nil {
		if err := h.audit.AssertWorkspaceOpen(ctx, sessionID); err != nil {
			fail("workspace", err, "实训工作区不可用，终端已冻结")
			return
		}
		if err := h.audit.Append(ctx, sessionID, "terminal", map[string]any{"target": target, "truncated": true, "reason": "interactive_untracked"}); err != nil {
			fail("audit_start", err, "终端操作记录暂不可用，请稍后重新连接")
			return
		}
	}
	execErrCh := make(chan error, 1)
	go func() {
		execErrCh <- h.kc.ExecShell(ctx, sessionID, target, container, stdinR, auditWriter, sizes)
	}()
	var workspaceClosed <-chan error
	if h.audit != nil {
		closed := make(chan error, 1)
		workspaceClosed = closed
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			closed <- watchTerminalWorkspace(ctx, h.audit, sessionID, ticker.C)
		}()
	}

	// 读浏览器消息：键盘/尺寸/心跳。
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, payload, rErr := conn.ReadMessage()
			if rErr != nil {
				return
			}
			var msg wsClientMessage
			if uErr := json.Unmarshal(payload, &msg); uErr != nil {
				continue // 非协议帧忽略
			}
			switch msg.Type {
			case "data":
				if h.audit != nil {
					if e := h.audit.AssertWorkspaceOpen(ctx, sessionID); e != nil {
						fail("workspace", e, "实训工作区不可用，终端已冻结")
						return
					}
				}
				if msg.Data != "" {
					// 键盘输入即实训活动：按会话限流后刷新租期锚点。
					h.activity.Touch(ctx, sessionID)
				}
				if h.audit != nil {
					if e := h.audit.Append(ctx, sessionID, "terminal", map[string]any{"direction": "input", "target": target, "bytes": len(msg.Data), "data_redacted": true}); e != nil {
						fail("audit_input", e, "终端操作记录暂不可用，请稍后重新连接")
						return
					}
				}
				_, _ = io.WriteString(stdinW, msg.Data)
			case "resize":
				sizes.Push(msg.Cols, msg.Rows)
			case "ping":
				_ = send(map[string]any{"type": "pong", "timestamp": time.Now().Unix()})
			}
		}
	}()

	select {
	case execErr := <-execErrCh:
		// exec 结束（正常退出或失败）：告知前端后收尾。
		if execErr != nil {
			fail("exec", execErr, execErr.Error())
		} else {
			_ = send(map[string]any{"type": "close"})
		}
	case <-readDone:
		cancel()
		<-execErrCh
	case err := <-workspaceClosed:
		fail("workspace", err, "实训工作区不可用，终端已冻结")
		cancel()
		<-execErrCh
	}

	// 收尾补一次落库：限流窗口内的最后输入也要计入租期。
	h.activity.TouchNow(context.WithoutCancel(ctx), sessionID)
}

// Revoking RBAC cannot terminate an already established exec stream. Continue
// checking lifecycle even when the user is idle, then cancel the stream on end.
func watchTerminalWorkspace(ctx context.Context, audit trainingAuditSink, sid int64, ticks <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticks:
			if err := audit.AssertWorkspaceOpen(ctx, sid); err != nil {
				return err
			}
		}
	}
}
