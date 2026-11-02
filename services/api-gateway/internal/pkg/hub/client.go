package hub

import (
	"encoding/json"
	"log"
	"time"
)

const (
	// TextMessage WebSocket 文本消息类型（与 gorilla/websocket 一致）。
	TextMessage = 1
	// CloseMessage 关闭帧。
	CloseMessage = 8
)

// ReadPump 读取客户端消息（在独立 goroutine 中运行）。
// 负责：pong 响应、客户端指令（订阅/心跳）、错误处理。
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		c.Touch()
		return nil
	})

	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			log.Printf("[WS] 连接 %s 读错误: %v", c.ID, err)
			return
		}
		c.Touch()

		// 解析客户端消息
		var event Event
		if err := json.Unmarshal(msg, &event); err != nil {
			// 格式错误，忽略
			continue
		}

		switch event.Type {
		case EventPong:
			// pong 已在 pong handler 中处理，这里只是双保险
			c.Touch()

		case EventPing:
			// 客户端主动 ping，服务端回 pong
			pongEvt := Event{
				Type:      EventPong,
				Timestamp: time.Now().UnixMilli(),
			}
			data, _ := json.Marshal(pongEvt)
			select {
			case c.Send <- data:
			default:
				return
			}

		default:
			// 其他客户端上行事件暂不处理（T1-19 扩展订阅）
		}
	}
}

// WritePump 向客户端写入消息（在独立 goroutine 中运行）。
func (c *Client) WritePump() {
	defer c.Conn.Close()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				// channel 被关闭，发送关闭帧
				c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = c.Conn.WriteMessage(CloseMessage, []byte{})
				return
			}

			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(TextMessage, message); err != nil {
				log.Printf("[WS] 连接 %s 写错误: %v", c.ID, err)
				return
			}
		}
	}
}

// SendHello 发送连接成功 hello 事件。
func (c *Client) SendHello() {
	evt := Event{
		Type:      EventHello,
		Timestamp: time.Now().UnixMilli(),
		Data: helloData{
			ConnectionID: c.ID,
			UserID:       c.UserID,
			HeartbeatSec: 30,
		},
	}
	data, _ := json.Marshal(evt)
	c.Send <- data
}
