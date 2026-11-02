package hub

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// mockConn 测试用的 WebSocket 连接模拟。
type mockConn struct {
	readCh        chan []byte
	writeCh       chan []byte
	closed        bool
	mu            sync.Mutex
	pongHandler   func(string) error
	readDeadline  time.Time
	writeDeadline time.Time
}

func newMockConn() *mockConn {
	return &mockConn{
		readCh:  make(chan []byte, 16),
		writeCh: make(chan []byte, 16),
	}
}

func (m *mockConn) ReadMessage() (int, []byte, error) {
	msg := <-m.readCh
	return websocket.TextMessage, msg, nil
}

func (m *mockConn) WriteMessage(_ int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.writeCh <- data
	return nil
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	close(m.readCh)
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	m.readDeadline = t
	return nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	m.writeDeadline = t
	return nil
}

func (m *mockConn) SetPongHandler(h func(string) error) {
	m.pongHandler = h
}

func TestHubRegisterUnregister(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond) // 等主循环启动

	conn := newMockConn()
	client := &Client{
		ID:     "test-conn-1",
		UserID: 1001,
		Conn:   conn,
		Send:   make(chan []byte, 256),
	}

	h.Register(client)
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	if len(h.clients) != 1 {
		t.Fatalf("期望 1 个用户，实际 %d", len(h.clients))
	}
	if len(h.clients[1001]) != 1 {
		t.Fatalf("期望用户 1001 有 1 个连接，实际 %d", len(h.clients[1001]))
	}
	h.mu.RUnlock()

	h.Unregister(client)
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	if len(h.clients) != 0 {
		t.Fatalf("期望 0 个用户，实际 %d", len(h.clients))
	}
	h.mu.RUnlock()
}

func TestHubMultiConnectionsPerUser(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)

	c1 := &Client{ID: "c1", UserID: 1001, Conn: newMockConn(), Send: make(chan []byte, 256)}
	c2 := &Client{ID: "c2", UserID: 1001, Conn: newMockConn(), Send: make(chan []byte, 256)}
	c3 := &Client{ID: "c3", UserID: 1002, Conn: newMockConn(), Send: make(chan []byte, 256)}

	h.Register(c1)
	h.Register(c2)
	h.Register(c3)
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	if len(h.clients[1001]) != 2 {
		t.Fatalf("用户 1001 应有 2 个连接，实际 %d", len(h.clients[1001]))
	}
	if len(h.clients[1002]) != 1 {
		t.Fatalf("用户 1002 应有 1 个连接，实际 %d", len(h.clients[1002]))
	}
	h.mu.RUnlock()

	h.Unregister(c1)
	time.Sleep(50 * time.Millisecond)

	h.mu.RLock()
	if len(h.clients[1001]) != 1 {
		t.Fatalf("注销 c1 后用户 1001 应有 1 个连接，实际 %d", len(h.clients[1001]))
	}
	if _, ok := h.clients[1001]["c2"]; !ok {
		t.Fatal("应保留 c2 连接")
	}
	h.mu.RUnlock()
}

func TestSendToUser(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)

	conn := newMockConn()
	client := &Client{
		ID:     "u1-c1",
		UserID: 1001,
		Conn:   conn,
		Send:   make(chan []byte, 256),
	}
	h.Register(client)
	go client.WritePump()
	time.Sleep(50 * time.Millisecond)

	h.SendToUser(1001, Event{
		Type: EventScoreUpdate,
		Data: map[string]interface{}{"score": 95},
	})

	select {
	case msg := <-conn.writeCh:
		var evt Event
		if err := json.Unmarshal(msg, &evt); err != nil {
			t.Fatalf("解析消息失败: %v", err)
		}
		if evt.Type != EventScoreUpdate {
			t.Fatalf("期望事件类型 %s，实际 %s", EventScoreUpdate, evt.Type)
		}
		if evt.Timestamp == 0 {
			t.Fatal("事件时间戳不应为 0")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("未收到推送消息")
	}
}

func TestBroadcast(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)

	c1 := &Client{ID: "c1", UserID: 1001, Conn: newMockConn(), Send: make(chan []byte, 256)}
	c2 := &Client{ID: "c2", UserID: 1002, Conn: newMockConn(), Send: make(chan []byte, 256)}

	h.Register(c1)
	h.Register(c2)
	go c1.WritePump()
	go c2.WritePump()
	time.Sleep(50 * time.Millisecond)

	h.Broadcast(Event{Type: EventAlert, Data: "test alert"})

	// 两个连接都应该收到
	for i, client := range []*Client{c1, c2} {
		mc := client.Conn.(*mockConn)
		select {
		case msg := <-mc.writeCh:
			var evt Event
			json.Unmarshal(msg, &evt)
			if evt.Type != EventAlert {
				t.Fatalf("连接 %d 事件类型不匹配: %s", i, evt.Type)
			}
		case <-time.After(1 * time.Second):
			t.Fatalf("连接 %d 未收到广播消息", i)
		}
	}
}

func TestEventHelloData(t *testing.T) {
	h := NewHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)

	conn := newMockConn()
	client := &Client{
		ID:     "hello-test",
		UserID: 999,
		Conn:   conn,
		Send:   make(chan []byte, 256),
	}
	h.Register(client)
	go client.WritePump()

	client.SendHello()

	select {
	case msg := <-conn.writeCh:
		var evt struct {
			Type string `json:"type"`
			Data struct {
				ConnectionID string `json:"connection_id"`
				UserID       int64  `json:"user_id"`
				HeartbeatSec int    `json:"heartbeat_sec"`
			} `json:"data"`
		}
		if err := json.Unmarshal(msg, &evt); err != nil {
			t.Fatalf("解析 hello 事件失败: %v", err)
		}
		if evt.Type != string(EventHello) {
			t.Fatalf("期望 hello 事件，实际 %s", evt.Type)
		}
		if evt.Data.ConnectionID != "hello-test" {
			t.Fatalf("connection_id 不匹配: %s", evt.Data.ConnectionID)
		}
		if evt.Data.UserID != 999 {
			t.Fatalf("user_id 不匹配: %d", evt.Data.UserID)
		}
		if evt.Data.HeartbeatSec != 30 {
			t.Fatalf("heartbeat_sec 应为 30，实际 %d", evt.Data.HeartbeatSec)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("未收到 hello 事件")
	}
}

func TestClientTouch(t *testing.T) {
	client := &Client{
		ID:         "touch-test",
		lastActive: time.Now().Add(-1 * time.Hour),
	}

	oldTime := client.lastActive
	time.Sleep(10 * time.Millisecond)
	client.Touch()

	client.mu.Lock()
	newTime := client.lastActive
	client.mu.Unlock()

	if !newTime.After(oldTime) {
		t.Fatal("Touch 后 lastActive 应该更新")
	}
}
