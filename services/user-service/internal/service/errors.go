package service

import "errors"

// 认证相关领域错误（handler 层映射为统一错误码）。
var (
	ErrInvalidAccount  = errors.New("账号格式不正确，请输入手机号或邮箱")
	ErrPasswordLength  = errors.New("密码长度需为 8-32 位")
	ErrPasswordWeak    = errors.New("密码需同时包含字母和数字")
	ErrAccountExists   = errors.New("该账号已注册")
	ErrAccountNotFound = errors.New("账号或密码错误")
	ErrPasswordWrong   = errors.New("账号或密码错误")
	ErrUserBanned      = errors.New("账号已被封禁")

	// —— 短信验证码 ——
	ErrSMSCodeInvalid = errors.New("验证码错误或已过期")
	ErrSMSCodeUsed    = errors.New("验证码已使用")
	ErrSMSSendFailed  = errors.New("验证码发送失败，请稍后重试")

	// —— 登录限流 ——
	ErrLoginRateLimited = errors.New("登录失败次数过多，请稍后再试")

	// —— 密码重置 ——
	ErrResetTokenInvalid = errors.New("重置链接无效或已过期")
	ErrResetTokenUsed    = errors.New("重置链接已使用")

	// —— Token ——
	ErrRefreshTokenInvalid = errors.New("刷新令牌无效或已过期")
)
