// Package handler fault-engine 管理后台 HTTP 接口。
// 提供故障剧本的 CRUD、启停、排序、校验等管理能力。
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/content-service/internal/repository"
	"github.com/quenching/content-service/internal/service"
)

// DefectHandler 故障剧本管理接口处理器。
type DefectHandler struct {
	svc *service.DefectService
}

// NewDefectHandler 创建剧本管理处理器。
func NewDefectHandler(svc *service.DefectService) *DefectHandler {
	return &DefectHandler{svc: svc}
}

// RegisterRoutes 注册路由（管理后台）。
//
// GET    /api/admin/v1/defects            剧本列表（分页+筛选）
// POST   /api/admin/v1/defects            创建剧本
// GET    /api/admin/v1/defects/:id        剧本详情
// PUT    /api/admin/v1/defects/:id        更新剧本
// DELETE /api/admin/v1/defects/:id        删除剧本
// PATCH  /api/admin/v1/defects/:id/active 启用/禁用
// POST   /api/admin/v1/defects/reorder    调整排序
// POST   /api/admin/v1/defects/:id/validate 校验剧本配置
func (h *DefectHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/admin/v1/defects")
	{
		// 静态路由须在参数路由同级注册；gin 的 radix 树允许 "/reorder" 与 "/:id" 共存。
		g.GET("", h.list)
		g.POST("", h.create)
		g.POST("/reorder", h.reorder)
		g.GET("/:id", h.get)
		g.PUT("/:id", h.update)
		g.DELETE("/:id", h.delete)
		g.PATCH("/:id/active", h.setActive)
		g.POST("/:id/validate", h.validate)
		g.POST("/:id/bindings/:slug/accept", h.acceptBinding)
	}
}

func (h *DefectHandler) acceptBinding(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "故障 ID 非法"})
		return
	}
	var req struct {
		Evidence map[string]interface{} `json:"evidence"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": "验收证据非法"})
		return
	}
	if err := h.svc.AcceptBinding(c.Request.Context(), id, c.Param("slug"), req.Evidence); err != nil {
		c.JSON(400, gin.H{"code": 10001, "msg": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "data": nil})
}

// ——— 列表 ———

// list 分页查询剧本列表。
//
// Query 参数：
//   - project_id  项目ID（必填）
//   - keyword     关键词搜索
//   - category    分类
//   - difficulty  难度
//   - defect_type 类型
//   - is_active   是否启用（true/false，不传=全部）
//   - page        页码
//   - page_size   每页条数
func (h *DefectHandler) list(c *gin.Context) {
	projectID, _ := strconv.ParseInt(c.Query("project_id"), 10, 64)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	q := repository.DefectQuery{
		TemplateSlug: c.Query("template_slug"),
		ProjectID:    projectID,
		Keyword:      c.Query("keyword"),
		Category:     c.Query("category"),
		Difficulty:   c.Query("difficulty"),
		DefectType:   c.Query("defect_type"),
		Page:         page,
		PageSize:     pageSize,
	}

	// is_active 可选参数
	if activeStr := c.Query("is_active"); activeStr != "" {
		active := activeStr == "true"
		q.IsActive = &active
	}

	result, err := h.svc.ListDefects(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": result})
}

// ——— 详情 ———

func (h *DefectHandler) get(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "剧本ID非法"})
		return
	}

	defect, err := h.svc.GetDefect(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": defect})
}

// ——— 创建 ———

func (h *DefectHandler) create(c *gin.Context) {
	var req service.CreateDefectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}

	defect, err := h.svc.CreateDefect(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, repository.ErrDefectCodeDuplicate) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 10003, "msg": "剧本编码已存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": defect})
}

// ——— 更新 ———

func (h *DefectHandler) update(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "剧本ID非法"})
		return
	}

	var req service.UpdateDefectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误: " + err.Error()})
		return
	}

	defect, err := h.svc.UpdateDefect(c.Request.Context(), id, &req)
	if err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		if errors.Is(err, repository.ErrDefectCodeDuplicate) {
			c.JSON(http.StatusBadRequest, gin.H{"code": 10003, "msg": "剧本编码已存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": defect})
}

// ——— 删除 ———

func (h *DefectHandler) delete(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "剧本ID非法"})
		return
	}

	if err := h.svc.DeleteDefect(c.Request.Context(), id); err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// ——— 启用/禁用 ———

type setActiveReq struct {
	IsActive bool `json:"is_active"`
}

func (h *DefectHandler) setActive(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "剧本ID非法"})
		return
	}

	var req setActiveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}

	if err := h.svc.SetActive(c.Request.Context(), id, req.IsActive); err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// ——— 排序调整 ———

type reorderReq struct {
	ProjectID int64 `json:"project_id"`
	ID1       int64 `json:"id1"`
	ID2       int64 `json:"id2"`
}

func (h *DefectHandler) reorder(c *gin.Context) {
	var req reorderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}

	if err := h.svc.Reorder(c.Request.Context(), req.ProjectID, req.ID1, req.ID2); err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
}

// ——— 配置校验 ———

type validateResp struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

func (h *DefectHandler) validate(c *gin.Context) {
	id, err := parsePathID(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "剧本ID非法"})
		return
	}

	valid, errs, err := h.svc.ValidateDefectConfig(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrDefectNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": 10002, "msg": "剧本不存在"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": validateResp{Valid: valid, Errors: errs},
	})
}

// ——— 工具函数 ———

func parsePathID(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(c.Param(name), 10, 64)
}
