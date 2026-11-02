package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/sandbox-service/internal/service"
)

// SSEInterval SSE 推送间隔。
const SSEInterval = 2 * time.Second

// StreamProgress SSE 流式推送进度。
// 每 2 秒推送一次当前状态，直到：
//   - 沙箱进入终态（ready / failed）
//   - 客户端断开连接
//   - 超时（5 分钟兜底）
//
// gin 框架下的 SSE 实现：
//   - 设置 Content-Type: text/event-stream
//   - 设置 Cache-Control: no-cache
//   - 设置 Connection: keep-alive
//   - 循环写入 "data: {json}\n\n" 格式
func StreamProgress(c *gin.Context, tracker *service.ProgressTracker, sessionID int64) {
	// 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // 禁用 Nginx 缓冲

	flusher, ok := c.Writer.(gin.ResponseWriter)
	if !ok {
		c.JSON(500, gin.H{"error": "streaming not supported"})
		return
	}

	// 立即推送一次（让前端不用等 2 秒）
	if err := pushStatus(c, flusher, tracker, sessionID); err != nil {
		return
	}

	// 定时推送
	ticker := time.NewTicker(SSEInterval)
	defer ticker.Stop()

	// 5 分钟兜底超时
	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-c.Request.Context().Done():
			// 客户端断开
			return
		case <-timeout:
			// 兜底超时，发一条超时消息然后关闭
			sendEvent(flusher, "timeout", gin.H{"message": "连接超时，请刷新页面"})
			return
		case <-ticker.C:
			if err := pushStatus(c, flusher, tracker, sessionID); err != nil {
				return
			}
			// 如果是终态，发完就关闭
			st, _ := tracker.Status(c.Request.Context(), sessionID)
			if st != nil && (st.Stage == service.StageReady || st.Stage == service.StageFailed) {
				return
			}
		}
	}
}

// pushStatus 查询并推送一次状态。
func pushStatus(c *gin.Context, w gin.ResponseWriter, tracker *service.ProgressTracker, sessionID int64) error {
	status, err := tracker.Status(c.Request.Context(), sessionID)
	if err != nil {
		sendEvent(w, "error", gin.H{"message": err.Error()})
		return err
	}
	sendEvent(w, "progress", status)
	return nil
}

// sendEvent 发送一个 SSE 事件。
// 格式：event: {name}\ndata: {json}\n\n
func sendEvent(w gin.ResponseWriter, event string, data interface{}) {
	jsonBytes, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
	w.Flush()
}

// contextKey 用于 SSE 的 context key（预留）
type contextKey string
