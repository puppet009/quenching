// 管理员身份接口：供管理后台登录后校验角色。
//
//	GET /api/v1/admin/profile   返回 roles + permissions（需网关 JWT 鉴权并注入 X-User-Id）
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"
	"github.com/quenching/user-service/internal/service"
)

type AdminProfileHandler struct {
	svc *service.AdminProfileService
}

func NewAdminProfileHandler(svc *service.AdminProfileService) *AdminProfileHandler {
	return &AdminProfileHandler{svc: svc}
}

func (h *AdminProfileHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/admin", UserID())
	{
		g.GET("/profile", h.getProfile)
	}
}

func (h *AdminProfileHandler) getProfile(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	p, err := h.svc.GetByUserID(c.Request.Context(), uid)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAdminProfileNotFound):
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
		case errors.Is(err, service.ErrUserBanned):
			response.FailWithHTTP(c, http.StatusForbidden, response.ErrForbidden)
		default:
			response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		}
		return
	}

	response.OK(c, p)
}
