// Package hub WebSocket 连接管理中枢（T1-18 WS连接与鉴权）。
//
// 职责：
//   - 客户端注册/注销
//   - 按用户ID管理连接（一个用户可有多条连接，如多标签页）
//   - 事件广播（单用户 / 全员 / 指定频道）
//   - 心跳检测（超时自动断开）
//   - 断线重连（通过 connection_id 恢复会话订阅）
//
// 为 T1-19 事件订阅分发预留了频道订阅机制。
package hub

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// EventType 事件类型（T1-19 逐步完善）。
type EventType string

const (
	// 系统事件
	EventHello EventType = "hello" // 连接成功握手响应
	EventPing  EventType = "ping"  // 心跳 ping
	EventPong  EventType = "pong"  // 心跳 pong
	EventError EventType = "error" // 错误通知
	EventClose EventType = "close" // 服务端主动关闭

	// 业务事件（T1-19 起陆续启用）
	EventMetrics      EventType = "metrics"       // 实时指标
	EventAlert        EventType = "alert"         // 告警
	EventStatusChange EventType = "status_change" // 状态变更（沙箱/训练）
	EventAIMessage    EventType = "ai_message"    // AI 消息推送
	EventScoreUpdate  EventType = "score_update"  // 评分更新
	EventLogLine      EventType = "log_line"      // 日志行
)

// Event WebSocket 事件帧，前后端统一格式。
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp int64       `json:"ts"`
	Data      interface{} `json:"data,omitempty"`
}

// helloData 连接成功响应数据。
type helloData struct {
	ConnectionID string `json:"connection_id"`
	UserID       int64  `json:"user_id"`
	HeartbeatSec int    `json:"heartbeat_sec"`
}

// Client 一个 WebSocket 连接。
type Client struct {
	ID            string
	UserID        int64
	Conn          WebsocketConn
	Send          chan []byte
	hub           *Hub
	lastActive    time.Time
	mu            sync.Mutex
	subscriptions map[string]bool // 订阅的频道
}

// WebsocketConn WebSocket 连接接口（便于测试 mock）。
type WebsocketConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
}

const (
	// heartbeatInterval 服务端 ping 间隔。
	heartbeatInterval = 30 * time.Second
	// pongWait 等待 pong 的超时时间（2 倍心跳间隔）。
	pongWait = 60 * time.Second
	// writeWait 写操作超时。
	writeWait = 10 * time.Second
	// maxMessageSize 最大消息大小（防止恶意大包）。
	maxMessageSize = 8 * 1024 // 8KB，控制指令足够
)

// Hub 连接管理器。全局单例。
type Hub struct {
	// 按用户ID索引的连接集合（一个用户可能多个连接）
	clients map[int64]map[string]*Client
	// 频道订阅：channel -> set of connection IDs
	channels map[string]map[string]*Client

	mu sync.RWMutex

	// 注册/注销通道
	register   chan *Client
	unregister chan *Client
	// 广播通道
	broadcast chan Event
	// 用户定向推送
	sendToUser chan userMessage
	// 频道订阅/取消订阅
	subscribe   chan channelAction
	unsubscribe chan channelAction
	// 频道定向推送
	sendToChannel chan channelMessage
}

type userMessage struct {
	UserID int64
	Event  Event
}

type channelAction struct {
	ClientID string
	Channel  string
}

type channelMessage struct {
	Channel string
	Event   Event
}

// NewHub 创建 Hub。
func NewHub() *Hub {
	return &Hub{
		clients:       make(map[int64]map[string]*Client),
		channels:      make(map[string]map[string]*Client),
		register:      make(chan *Client, 256),
		unregister:    make(chan *Client, 256),
		broadcast:     make(chan Event, 256),
		sendToUser:    make(chan userMessage, 256),
		subscribe:     make(chan channelAction, 256),
		unsubscribe:   make(chan channelAction, 256),
		sendToChannel: make(chan channelMessage, 256),
	}
}

// Run 启动 Hub 主循环（应在独立 goroutine 中运行）。
func (h *Hub) Run() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.addClient(client)

		case client := <-h.unregister:
			h.removeClient(client)

		case event := <-h.broadcast:
			h.broadcastAll(event)

		case msg := <-h.sendToUser:
			h.sendToUserInternal(msg.UserID, msg.Event)

		case action := <-h.subscribe:
			h.subscribeChannel(action.ClientID, action.Channel)

		case action := <-h.unsubscribe:
			h.unsubscribeChannel(action.ClientID, action.Channel)

		case msg := <-h.sendToChannel:
			h.sendToChannelInternal(msg.Channel, msg.Event)

		case <-ticker.C:
			h.checkHeartbeat()
		}
	}
}

// Register 注册一个新连接。
func (h *Hub) Register(c *Client) {
	c.hub = h
	c.lastActive = time.Now()
	c.subscriptions = make(map[string]bool)
	h.register <- c
}

// Unregister 注销连接。
func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

// Broadcast 广播事件到所有连接。
func (h *Hub) Broadcast(event Event) {
	event.Timestamp = time.Now().UnixMilli()
	h.broadcast <- event
}

// SendToUser 向指定用户的所有连接推送事件。
func (h *Hub) SendToUser(userID int64, event Event) {
	event.Timestamp = time.Now().UnixMilli()
	h.sendToUser <- userMessage{UserID: userID, Event: event}
}

// Subscribe 客户端订阅频道（线程安全，通过 channel 排队到主循环）。
//
// 频道命名规范：
//   - "user:{userId}"        用户个人频道（自动订阅）
//   - "sandbox:{sandboxId}"  沙箱频道（训练相关事件）
//   - "session:{sessionId}"  会话频道
//   - "room:{roomId}"        协作房间频道
func (h *Hub) Subscribe(clientID string, channel string) {
	h.subscribe <- channelAction{ClientID: clientID, Channel: channel}
}

// Unsubscribe 客户端取消订阅频道。
func (h *Hub) Unsubscribe(clientID string, channel string) {
	h.unsubscribe <- channelAction{ClientID: clientID, Channel: channel}
}

// SendToChannel 向指定频道的所有订阅者推送事件。
func (h *Hub) SendToChannel(channel string, event Event) {
	event.Timestamp = time.Now().UnixMilli()
	h.sendToChannel <- channelMessage{Channel: channel, Event: event}
}

// ChannelCount 获取指定频道的订阅人数（用于监控）。
func (h *Hub) ChannelCount(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.channels[channel]; ok {
		return len(clients)
	}
	return 0
}

// ListChannels 列出所有活跃频道（用于监控）。
func (h *Hub) ListChannels() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	channels := make([]string, 0, len(h.channels))
	for ch := range h.channels {
		channels = append(channels, ch)
	}
	return channels
}

// ——— 内部方法（都在主循环 goroutine 中执行，无需加锁）———

func (h *Hub) addClient(c *Client) {
	if _, ok := h.clients[c.UserID]; !ok {
		h.clients[c.UserID] = make(map[string]*Client)
	}
	h.clients[c.UserID][c.ID] = c
	log.Printf("[WS] 用户 %d 连接上线 (conn=%s, 当前连接数=%d)",
		c.UserID, c.ID, h.totalConns())
}

func (h *Hub) removeClient(c *Client) {
	if userClients, ok := h.clients[c.UserID]; ok {
		if _, exists := userClients[c.ID]; exists {
			delete(userClients, c.ID)
			close(c.Send)
			if len(userClients) == 0 {
				delete(h.clients, c.UserID)
			}
			log.Printf("[WS] 用户 %d 连接下线 (conn=%s, 当前连接数=%d)",
				c.UserID, c.ID, h.totalConns())
		}
	}
	// 从所有频道中移除
	for ch, clients := range h.channels {
		delete(clients, c.ID)
		if len(clients) == 0 {
			delete(h.channels, ch)
		}
	}
}

func (h *Hub) broadcastAll(event Event) {
	data := marshalEvent(event)
	for _, userClients := range h.clients {
		for _, c := range userClients {
			select {
			case c.Send <- data:
			default:
				// 发送缓冲区满，强制断开（防止慢连接堆积）
				h.removeClient(c)
			}
		}
	}
}

func (h *Hub) sendToUserInternal(userID int64, event Event) {
	data := marshalEvent(event)
	userClients, ok := h.clients[userID]
	if !ok {
		return
	}
	for _, c := range userClients {
		select {
		case c.Send <- data:
		default:
			h.removeClient(c)
		}
	}
}

// subscribeChannel 订阅频道（内部方法，主循环中调用）。
func (h *Hub) subscribeChannel(clientID, channel string) {
	// 查找 client
	var target *Client
	for _, userClients := range h.clients {
		if c, ok := userClients[clientID]; ok {
			target = c
			break
		}
	}
	if target == nil {
		return
	}

	// 添加到频道
	if _, ok := h.channels[channel]; !ok {
		h.channels[channel] = make(map[string]*Client)
	}
	h.channels[channel][clientID] = target

	// 更新 client 的订阅记录
	target.mu.Lock()
	target.subscriptions[channel] = true
	target.mu.Unlock()

	log.Printf("[WS] 连接 %s 订阅频道 %s (频道订阅数=%d)",
		clientID, channel, len(h.channels[channel]))
}

// unsubscribeChannel 取消订阅频道（内部方法，主循环中调用）。
func (h *Hub) unsubscribeChannel(clientID, channel string) {
	if clients, ok := h.channels[channel]; ok {
		if c, exists := clients[clientID]; exists {
			delete(clients, clientID)
			// 更新 client 的订阅记录
			c.mu.Lock()
			delete(c.subscriptions, channel)
			c.mu.Unlock()

			if len(clients) == 0 {
				delete(h.channels, channel)
			}
			log.Printf("[WS] 连接 %s 取消订阅频道 %s (频道订阅数=%d)",
				clientID, channel, len(clients))
		}
	}
}

// sendToChannelInternal 向频道推送事件（内部方法，主循环中调用）。
func (h *Hub) sendToChannelInternal(channel string, event Event) {
	data := marshalEvent(event)
	clients, ok := h.channels[channel]
	if !ok {
		return
	}
	for _, c := range clients {
		select {
		case c.Send <- data:
		default:
			h.removeClient(c)
		}
	}
}

func (h *Hub) checkHeartbeat() {
	now := time.Now()
	deadline := now.Add(-pongWait)
	for uid, userClients := range h.clients {
		for cid, c := range userClients {
			c.mu.Lock()
			active := c.lastActive
			c.mu.Unlock()

			if active.Before(deadline) {
				log.Printf("[WS] 用户 %d 连接 %s 心跳超时，断开", uid, cid)
				h.removeClient(c)
				continue
			}
			// 发送 ping
			pingData, _ := json.Marshal(Event{
				Type:      EventPing,
				Timestamp: now.UnixMilli(),
			})
			select {
			case c.Send <- pingData:
			default:
				h.removeClient(c)
			}
		}
	}
}

func (h *Hub) totalConns() int {
	n := 0
	for _, userClients := range h.clients {
		n += len(userClients)
	}
	return n
}

func marshalEvent(e Event) []byte {
	data, err := json.Marshal(e)
	if err != nil {
		return []byte(`{"type":"error","ts":0,"data":"marshal failed"}`)
	}
	return data
}

// Touch 更新连接最后活跃时间（收到 pong 或消息时调用）。
func (c *Client) Touch() {
	c.mu.Lock()
	c.lastActive = time.Now()
	c.mu.Unlock()
}
