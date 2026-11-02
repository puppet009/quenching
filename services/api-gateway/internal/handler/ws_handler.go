// Package handler WebSocket 连接处理器。
//
// 端点: GET /ws
// 鉴权方式（二选一）:
//  1. Query 参数: ?token=<access_token>
//  2. Authorization header: Bearer <access_token>
//
// 心跳: 服务端每 30s 发 ping，客户端 60s 内须回 pong，否则断开
// 断线重连: 客户端保存 connection_id，重连时通过 ?reconnect=<connection_id> 参数，
//
//	服务端将尝试恢复该连接的订阅状态（T1-19 完善）。
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	commonauth "github.com/quenching/go-common/auth"

	"github.com/quenching/api-gateway/internal/pkg/hub"
)

// WSHandler WebSocket 处理器。
type WSHandler struct {
	hub      *hub.Hub
	jwtMgr   *commonauth.Manager
	upgrader websocket.Upgrader
}

// NewWSHandler 创建 WS 处理器。
// allowOrigins 允许的 Origin 列表，空或 ["*"] 表示全部允许。
func NewWSHandler(h *hub.Hub, jwtMgr *commonauth.Manager, allowOrigins []string) *WSHandler {
	allowAll := len(allowOrigins) == 0 || (len(allowOrigins) == 1 && allowOrigins[0] == "*")

	return &WSHandler{
		hub:    h,
		jwtMgr: jwtMgr,
		upgrader: websocket.Upgrader{
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
			HandshakeTimeout: 5 * time.Second,
			CheckOrigin: func(r *http.Request) bool {
				if allowAll {
					return true
				}
				origin := r.Header.Get("Origin")
				for _, o := range allowOrigins {
					if strings.EqualFold(origin, o) {
						return true
					}
				}
				return false
			},
		},
	}
}

// Handle WS 握手处理函数。
func (h *WSHandler) Handle(c *gin.Context) {
	// 1) 鉴权：从 query 参数或 header 中提取 token
	token := c.Query("token")
	if token == "" {
		authHeader := c.GetHeader("Authorization")
		if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		}
	}
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 20001,
			"msg":  "未登录或缺少令牌",
		})
		return
	}

	claims, err := h.jwtMgr.ParseAccess(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 20002,
			"msg":  "登录已失效，请重新登录",
		})
		return
	}

	// 2) 升级 WebSocket 连接
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade 失败时 upgrader 已写错误响应，无需再写
		return
	}

	// 3) 生成连接 ID 并注册
	connID := genConnID()
	client := &hub.Client{
		ID:     connID,
		UserID: claims.UID,
		Conn:   &gorillaAdapter{Conn: conn},
		Send:   make(chan []byte, 256),
	}

	h.hub.Register(client)

	// 4) 发送 hello 事件
	client.SendHello()

	// 5) 启动读写循环（WritePump 在前，确保发送缓冲被消费）
	go client.WritePump()
	client.ReadPump()
}

// genConnID 生成 16 字节随机连接 ID。
func genConnID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// gorillaAdapter 将 gorilla/websocket.Conn 适配为 hub.WebsocketConn 接口。
type gorillaAdapter struct {
	Conn *websocket.Conn
}

func (g *gorillaAdapter) ReadMessage() (int, []byte, error) {
	return g.Conn.ReadMessage()
}

func (g *gorillaAdapter) WriteMessage(t int, data []byte) error {
	return g.Conn.WriteMessage(t, data)
}

func (g *gorillaAdapter) Close() error {
	return g.Conn.Close()
}

func (g *gorillaAdapter) SetReadDeadline(t time.Time) error {
	return g.Conn.SetReadDeadline(t)
}

func (g *gorillaAdapter) SetWriteDeadline(t time.Time) error {
	return g.Conn.SetWriteDeadline(t)
}

func (g *gorillaAdapter) SetPongHandler(h func(string) error) {
	g.Conn.SetPongHandler(h)
}
