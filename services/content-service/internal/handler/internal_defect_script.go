// 服务间剧本详情接口处理器（/internal/v1）。
//
// 响应含答案列 root_cause，路由与脱敏内部接口分离；仅供 training-service
// 复盘聚合端点直连，不经 api-gateway、不对前端暴露。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/quenching/content-service/internal/service"
)

// InternalDefectScriptHandler 剧本详情内部接口处理器。
type InternalDefectScriptHandler struct {
	svc   *service.InternalDefectScriptService
	token string
}

// NewInternalDefectScriptHandler 创建剧本详情内部接口处理器。
func NewInternalDefectScriptHandler(svc *service.InternalDefectScriptService, token string) *InternalDefectScriptHandler {
	return &InternalDefectScriptHandler{svc: svc, token: token}
}

// RegisterRoutes 注册服务间路由：
//
//	POST /internal/v1/defects/script-detail 按 ID 批量取剧本详情（含 root_cause）
func (h *InternalDefectScriptHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/defects", h.guard)
	g.POST("/script-detail", h.scriptDetail)
}

// guard 复用脱敏接口的共享头校验：令牌必需，空值拒绝读取。
func (h *InternalDefectScriptHandler) guard(c *gin.Context) {
	catalogTokenGuard(h.token)(c)
}

// scriptDetail 按 ID 列表批量返回剧本详情。
func (h *InternalDefectScriptHandler) scriptDetail(c *gin.Context) {
	var req batchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": []any{}})
		return
	}
	if len(req.IDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "单次批量查询不能超过 500 条"})
		return
	}

	items, err := h.svc.ScriptDetails(c.Request.Context(), req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}
