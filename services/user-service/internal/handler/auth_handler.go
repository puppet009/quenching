// Package handler 用户认证 HTTP 接口。
//
// 接口清单：
//
//	POST /api/v1/auth/register          注册（账号密码）
//	POST /api/v1/auth/login             登录（账号密码）
//	POST /api/v1/auth/login/sms         登录（短信验证码）
//	POST /api/v1/auth/sms/send          发送短信验证码
//	POST /api/v1/auth/token/refresh     刷新 token
//	POST /api/v1/auth/password/reset-request  请求重置密码（发邮件）
//	POST /api/v1/auth/password/reset    重置密码
//	GET  /api/v1/auth/password/reset/verify  验证重置 token
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/quenching/go-common/response"
	"github.com/quenching/user-service/internal/service"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

func (h *AuthHandler) RegisterRoutes(r *gin.Engine) {
	g := r.Group("/api/v1/auth")
	{
		// 基础
		g.POST("/register", h.register)
		g.POST("/login", h.login)

		// 短信
		g.POST("/login/sms", h.loginBySMS)
		g.POST("/sms/send", h.sendSMSCode)

		// Token 续期
		g.POST("/token/refresh", h.refreshToken)

		// 找回密码
		g.POST("/password/reset-request", h.requestReset)
		g.GET("/password/reset/verify", h.verifyResetToken)
		g.POST("/password/reset", h.resetPassword)
	}
}

// ——— 注册 ———

type registerReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	SMSCode  string `json:"sms_code"` // 启用手机号注册验证时必填，MVP 无需传入
}

func (h *AuthHandler) register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	res, err := h.svc.Register(c.Request.Context(), service.RegisterInput{
		Account:  req.Account,
		Password: req.Password,
		Nickname: req.Nickname,
		SMSCode:  req.SMSCode,
	})
	if err != nil {
		writeAuthError(c, err)
		return
	}
	response.OK(c, res)
}

// ——— 密码登录 ———

type loginReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

func (h *AuthHandler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	clientIP := getClientIP(c)
	res, err := h.svc.Login(c.Request.Context(), req.Account, req.Password, clientIP)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	response.OK(c, res)
}

// ——— 短信登录 ———

type loginSMSReq struct {
	Phone   string `json:"phone"`
	SMSCode string `json:"sms_code"`
}

func (h *AuthHandler) loginBySMS(c *gin.Context) {
	var req loginSMSReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	clientIP := getClientIP(c)
	res, err := h.svc.LoginBySMS(c.Request.Context(), req.Phone, req.SMSCode, clientIP)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	response.OK(c, res)
}

// ——— 发送短信验证码 ———

type sendSMSReq struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"` // register/login/reset_pwd/change_phone
}

func (h *AuthHandler) sendSMSCode(c *gin.Context) {
	var req sendSMSReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	if req.Purpose == "" {
		req.Purpose = "login"
	}

	code, err := h.svc.SendSMSCode(c.Request.Context(), req.Phone, req.Purpose)
	if err != nil {
		writeAuthError(c, err)
		return
	}

	// mock 模式下返回验证码（便于开发调试），生产环境不返回
	data := gin.H{"sent": true}
	if code != "" {
		data["mock_code"] = code
	}
	response.OK(c, data)
}

// ——— Token 刷新 ———

type refreshTokenReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) refreshToken(c *gin.Context) {
	var req refreshTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		// 也支持从 header 读取
		req.RefreshToken = c.GetHeader("X-Refresh-Token")
		if req.RefreshToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "缺少刷新令牌"})
			return
		}
	}

	token, err := h.svc.RefreshToken(c.Request.Context(), req.RefreshToken)
	if err != nil {
		writeAuthError(c, err)
		return
	}
	response.OK(c, token)
}

// ——— 请求重置密码 ———

type requestResetReq struct {
	Email string `json:"email"`
}

func (h *AuthHandler) requestReset(c *gin.Context) {
	var req requestResetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}
	// 无论邮箱是否注册都返回成功，避免泄露信息
	_ = h.svc.RequestPasswordReset(c.Request.Context(), req.Email)
	response.OK(c, gin.H{"sent": true})
}

// ——— 验证重置 token ———

func (h *AuthHandler) verifyResetToken(c *gin.Context) {
	email := c.Query("email")
	token := c.Query("token")

	valid, err := h.svc.VerifyResetToken(c.Request.Context(), email, token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
		return
	}
	response.OK(c, gin.H{"valid": valid})
}

// ——— 重置密码 ———

type resetPasswordReq struct {
	Email       string `json:"email"`
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) resetPassword(c *gin.Context) {
	var req resetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": "参数错误"})
		return
	}

	if err := h.svc.ResetPassword(c.Request.Context(), req.Email, req.Token, req.NewPassword); err != nil {
		writeAuthError(c, err)
		return
	}
	response.OK(c, gin.H{"reset": true})
}

// ——— 工具函数 ———

// writeAuthError 领域错误 -> HTTP状态与错误码。
func writeAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrAccountExists):
		c.JSON(http.StatusConflict, gin.H{"code": 20005, "msg": err.Error()})
	case errors.Is(err, service.ErrUserBanned):
		c.JSON(http.StatusForbidden, gin.H{"code": 20003, "msg": err.Error()})
	case errors.Is(err, service.ErrAccountNotFound), errors.Is(err, service.ErrPasswordWrong):
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20002, "msg": err.Error()})
	case errors.Is(err, service.ErrSMSCodeInvalid), errors.Is(err, service.ErrSMSCodeUsed):
		c.JSON(http.StatusBadRequest, gin.H{"code": 20006, "msg": err.Error()})
	case errors.Is(err, service.ErrSMSSendFailed):
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 20007, "msg": err.Error()})
	case errors.Is(err, service.ErrLoginRateLimited):
		c.JSON(http.StatusTooManyRequests, gin.H{"code": 20008, "msg": err.Error()})
	case errors.Is(err, service.ErrRefreshTokenInvalid):
		c.JSON(http.StatusUnauthorized, gin.H{"code": 20009, "msg": err.Error()})
	case errors.Is(err, service.ErrResetTokenInvalid), errors.Is(err, service.ErrResetTokenUsed):
		c.JSON(http.StatusBadRequest, gin.H{"code": 20010, "msg": err.Error()})
	default:
		// 参数/密码强度类
		c.JSON(http.StatusBadRequest, gin.H{"code": 10001, "msg": err.Error()})
	}
}

// getClientIP 获取客户端 IP。
func getClientIP(c *gin.Context) string {
	// 优先取 X-Forwarded-For（经过反向代理时）
	if ip := c.GetHeader("X-Forwarded-For"); ip != "" {
		return ip
	}
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return ip
	}
	return c.ClientIP()
}
