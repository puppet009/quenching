// Package handler 当前用户个人信息接口。
//
//	GET /api/v1/users/me   登录用户账号信息 + 能力档案（训练统计由 training
//	                       服务的 /training/sessions/overview 单独提供）
//	PUT /api/v1/users/me   更新基本资料（昵称/头像/简介）
//	POST /api/v1/users/me/risk-notice-ack  确认 AI 风险告知（幂等）
package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"
	"github.com/quenching/user-service/internal/model"
	"github.com/quenching/user-service/internal/service"
)

type ProfileHandler struct {
	svc *service.ProfileService
}

func NewProfileHandler(svc *service.ProfileService) *ProfileHandler {
	return &ProfileHandler{svc: svc}
}

func (h *ProfileHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/users", UserID())
	{
		g.GET("/me", h.getMe)
		g.PUT("/me", h.updateMe)
		g.POST("/me/risk-notice-ack", h.ackRiskNotice)
	}
}

// profileMeResponse 个人中心用户信息。username 取登录账号（手机/邮箱），
// 训练累计统计不在本服务聚合，由前端再请求 training 概览接口。
type profileMeResponse struct {
	ID             int64           `json:"id"`
	Username       string          `json:"username"`
	AccountType    string          `json:"account_type"`
	Nickname       string          `json:"nickname"`
	AvatarURL      string          `json:"avatar_url"`
	Bio            string          `json:"bio"`
	Email          string          `json:"email"`
	Phone          string          `json:"phone"`
	CreatedAt      string          `json:"created_at"`
	CollabLevel    string          `json:"collab_level"`
	CollabGroup    string          `json:"collab_group"`
	AvgScore       *float64        `json:"avg_score"`
	AvgCollabScore *float64        `json:"avg_collab_score"`
	SkillRadar     json.RawMessage `json:"skill_radar"`
	CollabRadar    json.RawMessage `json:"collab_radar"`
}

func (h *ProfileHandler) getMe(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	u, p, err := h.svc.GetMyProfile(c.Request.Context(), uid)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNotFound):
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
		case errors.Is(err, service.ErrUserBanned):
			response.FailWithHTTP(c, http.StatusForbidden, response.ErrForbidden)
		default:
			log.Printf("getMe 查询用户档案失败 uid=%d err=%v", uid, err)
			response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		}
		return
	}

	response.OK(c, buildProfileMeResponse(u, p))
}

// updateProfileRequest 基本资料更新入参。三个字段均为可选指针：
// 省略时沿用旧值；显式传空串视为清空。
type updateProfileRequest struct {
	Nickname  *string `json:"nickname"`
	AvatarURL *string `json:"avatar_url"`
	Bio       *string `json:"bio"`
}

// updateMe 更新昵称/头像/简介；成功后重新读取并返回完整资料，保证前端拿到最新值。
func (h *ProfileHandler) updateMe(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithHTTP(c, http.StatusBadRequest, response.ErrParam)
		return
	}

	// 先取现有资料：省略的字段沿用旧值，同时校验封禁状态
	u, _, err := h.svc.GetMyProfile(c.Request.Context(), uid)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNotFound):
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
		case errors.Is(err, service.ErrUserBanned):
			response.FailWithHTTP(c, http.StatusForbidden, response.ErrForbidden)
		default:
			log.Printf("updateMe 查询用户档案失败 uid=%d err=%v", uid, err)
			response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		}
		return
	}

	nickname, avatarURL, bio := u.Nickname, u.AvatarURL, u.Bio
	if req.Nickname != nil {
		nickname = strings.TrimSpace(*req.Nickname)
		if len([]rune(nickname)) > 50 {
			response.FailWithHTTP(c, http.StatusBadRequest, response.New(10001, "昵称不能超过50个字符"))
			return
		}
	}
	if req.AvatarURL != nil {
		avatarURL = strings.TrimSpace(*req.AvatarURL)
		if len(avatarURL) > 255 {
			response.FailWithHTTP(c, http.StatusBadRequest, response.New(10001, "头像地址过长"))
			return
		}
	}
	if req.Bio != nil {
		bio = strings.TrimSpace(*req.Bio)
		if len([]rune(bio)) > 200 {
			response.FailWithHTTP(c, http.StatusBadRequest, response.New(10001, "个人简介不能超过200个字符"))
			return
		}
	}

	if err := h.svc.UpdateProfile(c.Request.Context(), uid, nickname, avatarURL, bio); err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNotFound):
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
		case errors.Is(err, service.ErrUserBanned):
			response.FailWithHTTP(c, http.StatusForbidden, response.ErrForbidden)
		default:
			log.Printf("updateMe 更新用户档案失败 uid=%d err=%v", uid, err)
			response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		}
		return
	}

	// 重新读取最新资料返回
	u2, p2, err := h.svc.GetMyProfile(c.Request.Context(), uid)
	if err != nil {
		log.Printf("updateMe 更新后重读档案失败 uid=%d err=%v", uid, err)
		response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		return
	}
	response.OK(c, buildProfileMeResponse(u2, p2))
}

// ackRiskNotice 确认 AI 风险告知；幂等成功返回，无业务数据。
func (h *ProfileHandler) ackRiskNotice(c *gin.Context) {
	uid, ok := MustUserID(c)
	if !ok {
		response.FailWithHTTP(c, http.StatusUnauthorized, response.ErrUnauthorized)
		return
	}

	if err := h.svc.AckRiskNotice(c.Request.Context(), uid); err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNotFound):
			response.FailWithHTTP(c, http.StatusNotFound, response.ErrNotFound)
		case errors.Is(err, service.ErrUserBanned):
			response.FailWithHTTP(c, http.StatusForbidden, response.ErrForbidden)
		default:
			response.FailWithHTTP(c, http.StatusInternalServerError, response.ErrInternal)
		}
		return
	}

	response.OK(c, gin.H{"acknowledged": true})
}

func buildProfileMeResponse(u *model.User, p *model.UserProfile) profileMeResponse {
	res := profileMeResponse{
		ID:          u.ID,
		Username:    u.Phone,
		AccountType: u.AccountType,
		Nickname:    u.Nickname,
		AvatarURL:   u.AvatarURL,
		Bio:         u.Bio,
		Email:       u.Email,
		Phone:       u.Phone,
		CreatedAt:   u.CreatedAt.Format("2006-01-02"),
		SkillRadar:  []byte("null"),
		CollabRadar: []byte("null"),
	}
	if res.Username == "" {
		res.Username = u.Email
	}
	if p != nil {
		res.CollabLevel = p.CollabLevel
		res.CollabGroup = p.CollabGroup
		res.AvgScore = p.AvgScore
		res.AvgCollabScore = p.AvgCollabScore
		if len(p.SkillRadar) > 0 {
			res.SkillRadar = p.SkillRadar
		}
		if len(p.CollabRadar) > 0 {
			res.CollabRadar = p.CollabRadar
		}
	}
	return res
}
