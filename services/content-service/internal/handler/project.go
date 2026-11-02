// Package handler HTTP 接口层：接收请求、参数校验（binding tag）、调 service、返回统一响应。
// handler 不写业务逻辑，不直接碰 repository/数据库。
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

type ProjectHandler struct {
	svc *service.ProjectService
}

func NewProjectHandler(svc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{svc: svc}
}

func (h *ProjectHandler) Create(c *gin.Context) {
	var req dto.CreateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	p, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, p)
}

func (h *ProjectHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	p, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, p)
}

func (h *ProjectHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	domain := c.Query("domain")
	status := c.Query("status")
	difficulty := c.Query("difficulty")
	keyword := c.Query("keyword")
	resp, err := h.svc.List(c.Request.Context(), domain, status, difficulty, keyword, page, size)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, resp)
}

func (h *ProjectHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	var req dto.UpdateProjectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Update(c.Request.Context(), id, req); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}

func (h *ProjectHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Fail(c, response.ErrParam)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
			return
		}
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, nil)
}
