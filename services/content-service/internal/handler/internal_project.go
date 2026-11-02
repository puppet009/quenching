// 服务间内部接口处理器（/internal/v1）。
//
// 这些路由仅供同集群其他后端服务直连调用，不经 api-gateway、不对前端暴露。
// 安全上先做共享头简单校验（X-Internal-Token），云上再叠加网络策略/Service Mesh
// 做来源隔离；token 留空时不校验（兼容本地无密钥开发）。
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/quenching/content-service/internal/service"
)

// InternalProjectHandler 项目目录内部接口处理器。
type InternalProjectHandler struct {
	svc   *service.InternalProjectService
	token string
}

// NewInternalProjectHandler 创建内部接口处理器。
// token 为内部共享密钥；留空表示不校验请求头。
func NewInternalProjectHandler(svc *service.InternalProjectService, token string) *InternalProjectHandler {
	return &InternalProjectHandler{svc: svc, token: token}
}

// RegisterRoutes 注册服务间路由。
//
//	POST /internal/v1/projects/batch            按 ID 批量取项目 id/name
//	GET  /internal/v1/projects/search?q=        按名称模糊反查项目
//	POST /internal/v1/project-services/batch    按 ID 批量取服务 id/name/display_name
func (h *InternalProjectHandler) RegisterRoutes(r *gin.Engine) {
	pg := r.Group("/internal/v1/projects", h.guard)
	{
		pg.POST("/batch", h.batchProjects)
		pg.GET("/search", h.searchProjects)
	}
	ps := r.Group("/internal/v1/project-services", h.guard)
	{
		ps.POST("/batch", h.batchServices)
	}
}

// guard 内部接口共享头校验。配置了 token 才校验，留空放行。
func (h *InternalProjectHandler) guard(c *gin.Context) {
	if h.token != "" && c.GetHeader("X-Internal-Token") != h.token {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 14001, "msg": "内部接口未授权"})
		return
	}
	c.Next()
}

// idBatchReq 批量查询请求体。
type idBatchReq struct {
	IDs []int64 `json:"ids"`
}

// bindBatch 解析并校验批量请求体：非空且不超过 500 条。
func bindBatch(c *gin.Context) ([]int64, bool) {
	var req idBatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "请求体格式错误"})
		return nil, false
	}
	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "ID列表不能为空"})
		return nil, false
	}
	if len(req.IDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "单次批量查询不能超过 500 条"})
		return nil, false
	}
	return req.IDs, true
}

// batchProjects 按 ID 批量返回项目最小投影。
func (h *InternalProjectHandler) batchProjects(c *gin.Context) {
	ids, ok := bindBatch(c)
	if !ok {
		return
	}
	items, err := h.svc.BatchProjects(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// searchProjects 按名称模糊反查项目，供调用方对齐本地关键词过滤。
func (h *InternalProjectHandler) searchProjects(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "关键词不能为空"})
		return
	}
	items, err := h.svc.SearchProjectsByName(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}

// batchServices 按 ID 批量返回项目服务最小投影。
func (h *InternalProjectHandler) batchServices(c *gin.Context) {
	ids, ok := bindBatch(c)
	if !ok {
		return
	}
	items, err := h.svc.BatchServices(c.Request.Context(), ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 15001, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": items})
}
