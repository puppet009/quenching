// behavior_signals.go 行为信号上报与查询接口（行为模拟第二批）。
//
// runner Pod 在沙箱 namespace 内运行，通过沙箱服务的集群内 Service
// 把信号 POST 到 :id/behavior/signals；管理面/监控部件通过同路径 GET
// 读取最近信号。上报接口不做用户身份要求（runner 不持用户令牌），
// 仅按路径中的会话ID归属，且只接受 runner 定义的真实信号类型。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/sandbox-service/internal/service"
)

// 合法的行为信号类型（白名单，拒绝未知类型写入）。
var validSignalTypes = map[string]bool{
	service.SignalStepFailure:   true,
	service.SignalPageError:     true,
	service.SignalConsoleError:  true,
	service.SignalHTTPError:     true,
	service.SignalRequestFailed: true,
}

// BehaviorSignalHandler 行为信号上报/查询处理器。
type BehaviorSignalHandler struct {
	store *service.BehaviorSignalStore
}

// NewBehaviorSignalHandler 创建行为信号处理器。
func NewBehaviorSignalHandler(store *service.BehaviorSignalStore) *BehaviorSignalHandler {
	return &BehaviorSignalHandler{store: store}
}

// RegisterBehaviorSignals 在沙箱路由组内注册信号上报与查询路由。
func (h *BehaviorSignalHandler) RegisterBehaviorSignals(g *gin.RouterGroup) {
	g.POST("/:id/behavior/signals", h.report)
	g.GET("/:id/behavior/signals", h.list)
}

// report 接收 runner 上报的一条行为信号。
func (h *BehaviorSignalHandler) report(c *gin.Context) {
	sessionID, ok := parseBehaviorSessionID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "message": "会话ID非法"})
		return
	}

	var sig service.BehaviorSignal
	if err := c.ShouldBindJSON(&sig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10002, "message": "信号格式错误"})
		return
	}
	if !validSignalTypes[sig.Type] {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10003, "message": "未知信号类型"})
		return
	}

	// 以路径会话ID为准，防止跨沙箱上报；两个字段都对齐。
	sig.SessionID = sessionID
	sig.SandboxID = sessionID

	h.store.Add(sig)
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// list 返回指定沙箱最近汇聚的行为信号。
func (h *BehaviorSignalHandler) list(c *gin.Context) {
	sessionID, ok := parseBehaviorSessionID(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "message": "会话ID非法"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": h.store.List(sessionID),
	})
}
