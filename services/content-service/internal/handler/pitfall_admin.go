// Package handler 坑点配置后台管理接口（T3-18）。
//
// RESTful 接口：
//
//	GET    /api/v1/admin/pitfalls           分页列表
//	GET    /api/v1/admin/pitfalls/:id       详情
//	POST   /api/v1/admin/pitfalls           创建
//	PUT    /api/v1/admin/pitfalls/:id       更新
//	PATCH  /api/v1/admin/pitfalls/:id/status  改状态（启用/禁用/草稿）
//	DELETE /api/v1/admin/pitfalls/:id       删除（软删）
package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"

	"github.com/quenching/content-service/internal/model"
	"github.com/quenching/content-service/internal/repository"
	"github.com/quenching/content-service/internal/service"
)

// PitfallAdminHandler 坑点配置后台管理处理器。
type PitfallAdminHandler struct {
	svc *service.PitfallAdminService
}

// NewPitfallAdminHandler 创建坑点配置后台管理处理器。
func NewPitfallAdminHandler(svc *service.PitfallAdminService) *PitfallAdminHandler {
	return &PitfallAdminHandler{svc: svc}
}

// RegisterRoutes 注册路由。
// 前缀: /api/v1/admin/pitfalls
func (h *PitfallAdminHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/admin/pitfalls", UserID())
	{
		g.GET("", h.list)
		g.POST("", h.create)
		g.GET("/:id", h.get)
		g.PUT("/:id", h.update)
		g.PATCH("/:id/status", h.updateStatus)
		g.DELETE("/:id", h.delete)
	}
}

// list 分页查询坑点配置列表。
// Query 参数：
//   - defect_id: 按缺陷筛选
//   - pitfall_type: 按类型筛选（causal_misjudgment/info_omission/overconfidence）
//   - status: 按状态筛选（draft/active/inactive）
//   - page: 页码，默认 1
//   - page_size: 每页条数，默认 20，最大 100
func (h *PitfallAdminHandler) list(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	q := repository.PitfallQuery{
		DefectID:    queryInt64(c, "defect_id"),
		PitfallType: c.Query("pitfall_type"),
		Status:      c.Query("status"),
		Page:        queryInt(c, "page", 1),
		PageSize:    queryInt(c, "page_size", 20),
	}

	page, err := h.svc.List(c.Request.Context(), q)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, page)
}

// get 按 ID 查询坑点配置详情。
func (h *PitfallAdminHandler) get(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	id, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	cfg, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrPitfallNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		if errors.Is(err, service.ErrPitfallInvalid) {
			response.Fail(c, response.ErrParam)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, cfg)
}

// create 创建坑点配置。
// 请求体：model.PitfallConfig 的完整 JSON
func (h *PitfallAdminHandler) create(c *gin.Context) {
	userID, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	var cfg model.PitfallConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	result, err := h.svc.Create(c.Request.Context(), &cfg, userID)
	if err != nil {
		if errors.Is(err, service.ErrPitfallInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 10001,
				"msg":  err.Error(),
			})
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": result,
	})
}

// update 更新坑点配置。
// 请求体：model.PitfallConfig 的完整 JSON
func (h *PitfallAdminHandler) update(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	id, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	var cfg model.PitfallConfig
	if err := c.ShouldBindJSON(&cfg); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	result, err := h.svc.Update(c.Request.Context(), id, &cfg)
	if err != nil {
		if errors.Is(err, service.ErrPitfallNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		if errors.Is(err, service.ErrPitfallInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 10001,
				"msg":  err.Error(),
			})
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, result)
}

// updateStatus 更新配置状态。
// 请求体：{"status": "active|inactive|draft"}
func (h *PitfallAdminHandler) updateStatus(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	id, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	if err := h.svc.UpdateStatus(c.Request.Context(), id, body.Status); err != nil {
		if errors.Is(err, service.ErrPitfallNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		if errors.Is(err, service.ErrPitfallInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{
				"code": 10001,
				"msg":  err.Error(),
			})
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, nil)
}

// delete 软删除坑点配置。
func (h *PitfallAdminHandler) delete(c *gin.Context) {
	_, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	id, err := parseID(c)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrPitfallNotFound) {
			response.Fail(c, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}

	response.OK(c, nil)
}

// ---------- 工具函数 ----------

func parseID(c *gin.Context) (int64, error) {
	idStr := c.Param("id")
	return strconv.ParseInt(idStr, 10, 64)
}

func queryInt(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func queryInt64(c *gin.Context, key string) int64 {
	v := c.Query(key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
