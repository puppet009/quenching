// 服务间内部接口处理器（/internal/v1）。
//
// 这些路由仅供同集群其他后端服务直连调用，不经 api-gateway、不对前端暴露。
// 安全上先做共享头简单校验（X-Internal-Token），云上再叠加网络策略/Service Mesh
// 做来源隔离；token 留空时拒绝读取（兼容本地无密钥开发）。
package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/content-service/internal/service"
)

// InternalDefectHandler 故障剧本内部接口处理器。
type InternalDefectHandler struct {
	svc   *service.InternalDefectService
	token string
}

// NewInternalDefectHandler 创建内部接口处理器。
// token 为内部共享密钥；留空拒绝读取（本地开发 / 依赖网络层隔离时使用）。
func NewInternalDefectHandler(svc *service.InternalDefectService, token string) *InternalDefectHandler {
	return &InternalDefectHandler{svc: svc, token: token}
}

// RegisterRoutes 注册服务间路由。
//
//	POST /internal/v1/defects/batch       按 ID 批量取脱敏故障
//	GET  /internal/v1/defects/project/:id 项目启用中的故障目录
//	GET  /internal/v1/defects/search?q=   按名称模糊反查故障 ID
func (h *InternalDefectHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/internal/v1/defects", h.guard)
	{
		g.POST("/batch", h.batch)
		g.GET("/project/:id", h.projectDefects)
		g.GET("/search", h.search)
	}
}

// guard 内部接口共享头校验。令牌必需，空值拒绝读取。
func (h *InternalDefectHandler) guard(c *gin.Context) {
	catalogTokenGuard(h.token)(c)
}

// batchReq 批量查询请求体。
type batchReq struct {
	IDs []int64 `json:"ids"`
}

// batch 按 ID 列表批量返回脱敏故障投影。
func (h *InternalDefectHandler) batch(c *gin.Context) {
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

	items, err := h.svc.BatchDefects(c.Request.Context(), req.IDs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// projectDefects 返回项目下启用中的故障目录。
func (h *InternalDefectHandler) projectDefects(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || projectID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "项目ID非法"})
		return
	}

	items, err := h.svc.ProjectDefects(c.Request.Context(), projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// search 按名称模糊反查故障 ID/名称，供调用方对齐本地关键词过滤。
func (h *InternalDefectHandler) search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "关键词不能为空"})
		return
	}

	items, err := h.svc.SearchDefectByName(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}
