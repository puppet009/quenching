package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/quenching/content-service/internal/dto"
	"github.com/quenching/content-service/internal/service"
	"github.com/quenching/go-common/response"
)

type ProjectServiceHandler struct {
	svc *service.ProjectServiceService
}

func NewProjectServiceHandler(svc *service.ProjectServiceService) *ProjectServiceHandler {
	return &ProjectServiceHandler{svc: svc}
}

// Create 创建项目服务。
func (h *ProjectServiceHandler) Create(c *gin.Context) {
	var req dto.CreateProjectServiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	svc, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, svc)
}

// Get 获取服务详情。
func (h *ProjectServiceHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	svc, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrProjectServiceNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, svc)
}

// ListByProject 按项目ID列出服务。
func (h *ProjectServiceHandler) ListByProject(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	serviceType := c.Query("service_type")
	resp, err := h.svc.ListByProjectID(c.Request.Context(), projectID, serviceType)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, resp)
}

// Update 更新服务配置。
func (h *ProjectServiceHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	var req dto.UpdateProjectServiceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Update(c.Request.Context(), id, req); err != nil {
		if errors.Is(err, service.ErrProjectServiceNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}

// Delete 删除服务。
func (h *ProjectServiceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrProjectServiceNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}
