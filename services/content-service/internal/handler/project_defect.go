// 用户面项目故障只读接口。
//
//	GET /api/v1/projects/:projectId/defects  项目启用中故障概览
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"

	"github.com/quenching/content-service/internal/service"
)

// ProjectDefectHandler 用户面故障只读处理器。
type ProjectDefectHandler struct {
	svc *service.ProjectDefectService
}

// NewProjectDefectHandler 创建只读处理器。
func NewProjectDefectHandler(svc *service.ProjectDefectService) *ProjectDefectHandler {
	return &ProjectDefectHandler{svc: svc}
}

// ListByProject 列出项目下启用中的故障概览。
func (h *ProjectDefectHandler) ListByProject(c *gin.Context) {
	projectID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || projectID <= 0 {
		response.Fail(c, response.ErrParam)
		return
	}

	items, err := h.svc.ListByProject(c.Request.Context(), projectID)
	if err != nil {
		response.Fail(c, response.ErrInternal)
		return
	}
	response.OK(c, items)
}
