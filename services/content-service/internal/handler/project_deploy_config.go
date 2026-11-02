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

type ProjectDeployConfigHandler struct {
	svc *service.ProjectDeployConfigService
}

func NewProjectDeployConfigHandler(svc *service.ProjectDeployConfigService) *ProjectDeployConfigHandler {
	return &ProjectDeployConfigHandler{svc: svc}
}

// Save 保存部署配置（Upsert：存在则更新，不存在则创建）。
func (h *ProjectDeployConfigHandler) Save(c *gin.Context) {
	var req dto.SaveProjectDeployConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	cfg, err := h.svc.Save(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, cfg)
}

// GetByProject 按项目ID获取部署配置。
func (h *ProjectDeployConfigHandler) GetByProject(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	cfg, err := h.svc.GetByProjectID(c.Request.Context(), projectID)
	if err != nil {
		if errors.Is(err, service.ErrDeployConfigNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, cfg)
}

// Update 部分更新部署配置。
func (h *ProjectDeployConfigHandler) Update(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	var req dto.UpdateProjectDeployConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Update(c.Request.Context(), projectID, req); err != nil {
		if errors.Is(err, service.ErrDeployConfigNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}

// Delete 删除部署配置。
func (h *ProjectDeployConfigHandler) Delete(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), projectID); err != nil {
		if errors.Is(err, service.ErrDeployConfigNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}
