// Package service 注册登录业务编排。
// 纯业务规则依赖 UserStore 接口与 go-common 的 JWT Manager，不直接碰 PG/JWT 细节，便于单测。
package service

import (
	"context"

	"github.com/quenching/go-common/analytics"
	commonauth "github.com/quenching/go-common/auth"
	"github.com/quenching/user-service/internal/model"
)

// TokenPair 返回给前端的令牌对（JSON 字段与前端约定一致）。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // access token 有效秒数
}

// AuthService 用户认证服务。
//
// 能力：
//   - 账号密码注册/登录
//   - 短信验证码登录/注册
//   - Token 自动续期（refresh token）
//   - 登录限流 + 异常告警
//   - 找回密码（邮箱重置）
type AuthService struct {
	store                   UserStore
	jwt                     *commonauth.Manager
	sms                     SMSService            // 短信服务（mock 或 real）
	limiter                 *LoginLimiter         // 登录限流
	resetSvc                *ResetPasswordService // 密码重置
	tracker                 *analytics.Client     // 服务端埋点（nil=不上报）
	registrationSMSRequired bool                  // 手机号注册是否要求验证码；MVP 显式关闭
}

// AuthServiceOption 服务选项。
type AuthServiceOption func(*AuthService)

// WithSMS 设置短信服务。
func WithSMS(s SMSService) AuthServiceOption {
	return func(svc *AuthService) { svc.sms = s }
}

// WithRegistrationSMSRequired 设置手机号注册校验策略。
// 默认要求验证码；尚未接入短信的 MVP 入口必须显式关闭。此选项不影响短信登录。
func WithRegistrationSMSRequired(required bool) AuthServiceOption {
	return func(svc *AuthService) { svc.registrationSMSRequired = required }
}

// WithLoginLimiter 设置登录限流器。
func WithLoginLimiter(l *LoginLimiter) AuthServiceOption {
	return func(svc *AuthService) { svc.limiter = l }
}

// WithResetService 设置密码重置服务。
func WithResetService(r *ResetPasswordService) AuthServiceOption {
	return func(svc *AuthService) { svc.resetSvc = r }
}

// WithAnalytics 设置服务端埋点客户端（nil 表示不上报）。
func WithAnalytics(c *analytics.Client) AuthServiceOption {
	return func(svc *AuthService) { svc.tracker = c }
}

// NewAuthService 创建认证服务。
func NewAuthService(store UserStore, jwt *commonauth.Manager, opts ...AuthServiceOption) *AuthService {
	svc := &AuthService{store: store, jwt: jwt, registrationSMSRequired: true}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func (s *AuthService) issueToken(uid int64) (*TokenPair, error) {
	access, refresh, err := s.jwt.IssuePair(uid)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(commonauth.AccessTTL.Seconds()),
	}, nil
}

// ——— 注册 ———

// RegisterInput 注册请求。
type RegisterInput struct {
	Account  string // 手机号或邮箱
	Password string
	Nickname string // 可空，空则自动生成
	SMSCode  string // 短信验证码（启用手机号注册验证时需要）
}

// AuthResult 注册/登录成功结果（用户信息 + token）。
type AuthResult struct {
	User  *model.User `json:"user"`
	Token *TokenPair  `json:"token"`
}

// Register 注册并直接登录（注册即登录，直接发 token）。
// 仅启用手机号注册验证时要求短信验证码；邮箱注册不做验证码验证。
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	account := NormalizeAccount(in.Account)
	typ, err := DetectAccountType(account)
	if err != nil {
		return nil, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}

	// 手机号注册验证独立于 provider 注入；关闭验证的 MVP 仍执行下方账号查重与密码哈希。
	if typ == model.AccountPhone && s.registrationSMSRequired {
		if in.SMSCode == "" {
			return nil, ErrSMSCodeInvalid
		}
		if s.sms == nil {
			return nil, ErrSMSSendFailed
		}
		ok, err := s.sms.VerifyCode(ctx, account, SMSPurposeRegister, in.SMSCode)
		if err != nil {
			return nil, ErrSMSSendFailed
		}
		if !ok {
			return nil, ErrSMSCodeInvalid
		}
	}

	exists, err := s.store.ExistsByAccount(ctx, typ, account)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrAccountExists
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	u := &model.User{
		AccountType:  string(typ),
		PasswordHash: hash,
		Nickname:     in.Nickname,
		Status:       model.StatusActive,
	}
	if typ == model.AccountPhone {
		u.Phone = account
	} else {
		u.Email = account
	}
	if u.Nickname == "" {
		u.Nickname = DefaultNickname(account)
	}

	id, err := s.store.Create(ctx, u)
	if err != nil {
		return nil, err
	}
	u.ID = id

	token, err := s.issueToken(id)
	if err != nil {
		return nil, err
	}

	// 注册成功即上报漏斗第一步；异步、失败静默，不影响注册结果。
	s.tracker.Track(ctx, analytics.Event{
		EventType: "register",
		UserID:    id,
		Platform:  "server",
	})

	return &AuthResult{User: u, Token: token}, nil
}

// ——— 登录（密码） ———

// Login 账号密码登录（带限流）。
func (s *AuthService) Login(ctx context.Context, account, password, clientIP string) (*AuthResult, error) {
	account = NormalizeAccount(account)
	typ, err := DetectAccountType(account)
	if err != nil {
		return nil, err
	}

	// 限流检查
	if s.limiter != nil {
		allowed, _, err := s.limiter.CheckLoginAllowed(ctx, clientIP, account)
		if err != nil || !allowed {
			return nil, ErrLoginRateLimited
		}
	}

	u, err := s.store.FindByAccount(ctx, typ, account)
	if err != nil {
		return nil, err
	}
	if u == nil || !CheckPassword(u.PasswordHash, password) {
		// 记录失败
		if s.limiter != nil {
			s.limiter.RecordLoginFailure(ctx, clientIP, account)
		}
		return nil, ErrAccountNotFound
	}
	if u.IsBanned() {
		return nil, ErrUserBanned
	}

	// 登录成功，清除限流记录
	if s.limiter != nil {
		s.limiter.RecordLoginSuccess(ctx, clientIP, account)
	}

	token, err := s.issueToken(u.ID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{User: u, Token: token}, nil
}

// ——— 登录（短信验证码） ———

// LoginBySMS 短信验证码登录。
// 如果账号不存在则自动注册（免密登录体验）。
func (s *AuthService) LoginBySMS(ctx context.Context, phone, code, clientIP string) (*AuthResult, error) {
	phone = NormalizeAccount(phone)

	// 限流检查
	if s.limiter != nil {
		allowed, _, err := s.limiter.CheckLoginAllowed(ctx, clientIP, phone)
		if err != nil || !allowed {
			return nil, ErrLoginRateLimited
		}
	}

	// 校验验证码
	if s.sms == nil {
		return nil, ErrSMSSendFailed
	}
	ok, err := s.sms.VerifyCode(ctx, phone, SMSPurposeLogin, code)
	if err != nil {
		return nil, ErrSMSSendFailed
	}
	if !ok {
		if s.limiter != nil {
			s.limiter.RecordLoginFailure(ctx, clientIP, phone)
		}
		return nil, ErrSMSCodeInvalid
	}

	// 查找用户
	u, err := s.store.FindByAccount(ctx, model.AccountPhone, phone)
	if err != nil {
		return nil, err
	}

	// 不存在则自动注册（免密注册）
	if u == nil {
		u = &model.User{
			AccountType:  string(model.AccountPhone),
			PasswordHash: "", // 短信登录的账号可以没有密码，后续可设置
			Phone:        phone,
			Nickname:     DefaultNickname(phone),
			Status:       model.StatusActive,
		}
		id, err := s.store.Create(ctx, u)
		if err != nil {
			return nil, err
		}
		u.ID = id
	}

	if u.IsBanned() {
		return nil, ErrUserBanned
	}

	// 登录成功
	if s.limiter != nil {
		s.limiter.RecordLoginSuccess(ctx, clientIP, phone)
	}

	token, err := s.issueToken(u.ID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{User: u, Token: token}, nil
}

// ——— 发送短信验证码 ———

// SendSMSCode 发送短信验证码。
func (s *AuthService) SendSMSCode(ctx context.Context, phone, purpose string) (string, error) {
	phone = NormalizeAccount(phone)
	if _, err := DetectAccountType(phone); err != nil {
		return "", err
	}

	if s.sms == nil {
		return "", ErrSMSSendFailed
	}

	code, err := s.sms.SendCode(ctx, phone, purpose)
	if err != nil {
		return "", ErrSMSSendFailed
	}
	return code, nil // mock 模式返回验证码，real 模式返回空串
}

// ——— Token 刷新 ———

// RefreshToken 用 refresh token 换新的 token 对。
// 实现 token 自动续期：access token 过期后，前端用 refresh token 静默换取新 token。
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	if refreshToken == "" {
		return nil, ErrRefreshTokenInvalid
	}

	// 解析 refresh token 获取 userID
	claims, err := s.jwt.ParseRefresh(refreshToken)
	if err != nil {
		return nil, ErrRefreshTokenInvalid
	}

	userID := claims.UID
	if userID <= 0 {
		return nil, ErrRefreshTokenInvalid
	}

	// 确认用户存在且未被封禁
	u, err := s.store.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.IsBanned() {
		return nil, ErrRefreshTokenInvalid
	}

	// 签发新的 token 对
	return s.issueToken(userID)
}

// ——— 密码重置 ———

// RequestPasswordReset 请求密码重置（发送邮件）。
// 无论邮箱是否注册都返回成功，避免泄露信息。
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	email = NormalizeAccount(email)
	typ, err := DetectAccountType(email)
	if err != nil {
		return err
	}
	if typ != model.AccountEmail {
		return ErrInvalidAccount
	}

	if s.resetSvc == nil {
		return nil // 未配置重置服务时静默返回
	}

	// 调用重置服务发送邮件
	return s.resetSvc.RequestReset(ctx, email)
}

// VerifyResetToken 验证重置 token（仅验证不消费）。
func (s *AuthService) VerifyResetToken(ctx context.Context, email, token string) (bool, error) {
	if s.resetSvc == nil {
		return false, nil
	}
	return s.resetSvc.VerifyToken(ctx, email, token)
}

// ResetPassword 重置密码。
func (s *AuthService) ResetPassword(ctx context.Context, email, token, newPassword string) error {
	email = NormalizeAccount(email)

	// 校验新密码强度
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	if s.resetSvc == nil {
		return ErrResetTokenInvalid
	}

	// 验证并消费 token
	ok, err := s.resetSvc.ResetPassword(ctx, email, token)
	if err != nil {
		return ErrResetTokenInvalid
	}
	if !ok {
		return ErrResetTokenInvalid
	}

	// 查找用户
	u, err := s.store.FindByAccount(ctx, model.AccountEmail, email)
	if err != nil {
		return err
	}
	if u == nil {
		return ErrResetTokenInvalid // 不暴露邮箱是否注册
	}

	// 更新密码
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.store.UpdatePassword(ctx, u.ID, hash)
}
