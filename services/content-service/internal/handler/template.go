// 官方预置实训模板只读接口：列表数据全部来自模板目录扫描（go-common/templates），
// 不依赖数据库登记。模板的新增/停用通过 templates/<slug>/template.yaml 治理。
package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"

	"github.com/quenching/go-common/response"
	"github.com/quenching/go-common/templates"
)

// TemplateHandler 模板只读接口处理器。
type TemplateHandler struct {
	registry templates.Registry
}

func (h *TemplateHandler) Get(c *gin.Context) {
	t, ok := h.registry.Get(c.Param("slug"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 20004, "msg": "模板不存在或已停用"})
		return
	}
	response.OK(c, t)
}

// NewTemplateHandler 创建模板处理器。
func NewTemplateHandler(registry templates.Registry) *TemplateHandler {
	return &TemplateHandler{registry: registry}
}

// List 返回全部已启用的官方模板（按 sort_order、slug 排序）。
func (h *TemplateHandler) List(c *gin.Context) {
	response.OK(c, h.registry.List())
}

// Media serves only the validated cover of an enabled template. It never maps
// arbitrary request paths onto the template source, config or acceptance files.
func (h *TemplateHandler) Media(c *gin.Context) {
	t, ok := h.registry.Get(c.Param("slug"))
	if !ok || t.Cover == nil || t.Cover.File != c.Param("file") {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Content-Type", t.Cover.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("ETag", t.Cover.ETag)
	c.File(t.Cover.AssetPath)
}
