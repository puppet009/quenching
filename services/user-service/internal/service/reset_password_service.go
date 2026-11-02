// 密码重置（邮箱找回）。
//
// 流程：
//  1. 用户输入邮箱 → 发送重置链接邮件（含 token）
//  2. 用户点击链接 → 前端验证 token 有效性
//  3. 用户输入新密码 → 提交重置
//
// Token 设计：
//   - 一次性使用
//   - 有过期时间（默认 30 分钟）
//   - 与邮箱绑定，防止误用
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ResetTokenStore 重置 token 存储接口。
type ResetTokenStore interface {
	// Save 保存重置 token。
	Save(ctx context.Context, email, token string, expiresAt time.Time) error
	// Verify 验证 token 是否有效（邮箱匹配 + 未过期 + 未使用）。
	// 验证成功后标记为已使用。
	Verify(ctx context.Context, email, token string) (bool, error)
	// Consume 消费 token（验证成功后标记已使用）。
	Consume(ctx context.Context, email, token string) error
}

// InMemoryResetTokenStore 内存版 token 存储（开发/测试用）。
type InMemoryResetTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*resetToken // key: email:token
}

type resetToken struct {
	Email     string
	Token     string
	ExpiresAt time.Time
	Used      bool
}

// NewInMemoryResetTokenStore 创建内存存储。
func NewInMemoryResetTokenStore() *InMemoryResetTokenStore {
	return &InMemoryResetTokenStore{
		tokens: make(map[string]*resetToken),
	}
}

func (s *InMemoryResetTokenStore) Save(_ context.Context, email, token string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(email, token)
	s.tokens[key] = &resetToken{
		Email:     email,
		Token:     token,
		ExpiresAt: expiresAt,
		Used:      false,
	}
	return nil
}

func (s *InMemoryResetTokenStore) Verify(_ context.Context, email, token string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := s.key(email, token)
	t, ok := s.tokens[key]
	if !ok {
		return false, nil
	}
	if t.Used {
		return false, nil
	}
	if time.Now().After(t.ExpiresAt) {
		return false, nil
	}
	if t.Email != email {
		return false, nil
	}
	return true, nil
}

func (s *InMemoryResetTokenStore) Consume(_ context.Context, email, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(email, token)
	t, ok := s.tokens[key]
	if !ok {
		return fmt.Errorf("重置token不存在")
	}
	if t.Used {
		return fmt.Errorf("重置token已使用")
	}
	t.Used = true
	return nil
}

func (s *InMemoryResetTokenStore) key(email, token string) string {
	return email + ":" + token
}

// ResetEmailSender 邮件发送接口。
// 生产环境对接真实邮件服务（SMTP/阿里云邮件推送等）。
// 开发环境用 mock 实现。
type ResetEmailSender interface {
	SendResetEmail(ctx context.Context, email, resetLink string) error
}

// MockResetEmailSender mock 邮件发送器（开发/测试用）。
// 不真实发邮件，记录最后一次发送内容便于断言。
type MockResetEmailSender struct {
	mu         sync.Mutex
	lastEmail  string
	lastLink   string
	sendCount  int
	shouldFail bool // 测试用：模拟发送失败
}

// NewMockResetEmailSender 创建 mock 邮件发送器。
func NewMockResetEmailSender() *MockResetEmailSender {
	return &MockResetEmailSender{}
}

func (m *MockResetEmailSender) SendResetEmail(_ context.Context, email, resetLink string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return fmt.Errorf("mock 邮件发送失败")
	}

	m.lastEmail = email
	m.lastLink = resetLink
	m.sendCount++
	return nil
}

// LastSent 获取最后一次发送的邮箱和链接（测试用）。
func (m *MockResetEmailSender) LastSent() (email, link string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastEmail, m.lastLink, m.sendCount
}

// SetShouldFail 设置是否模拟发送失败（测试用）。
func (m *MockResetEmailSender) SetShouldFail(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = fail
}

// ResetPasswordService 密码重置服务。
type ResetPasswordService struct {
	store    ResetTokenStore
	sender   ResetEmailSender
	ttl      time.Duration // token 有效期
	tokenLen int           // token 长度（字节）
}

// NewResetPasswordService 创建密码重置服务。
func NewResetPasswordService(store ResetTokenStore, sender ResetEmailSender) *ResetPasswordService {
	return &ResetPasswordService{
		store:    store,
		sender:   sender,
		ttl:      30 * time.Minute,
		tokenLen: 32,
	}
}

// NewResetPasswordServiceWithTTL 自定义有效期的重置服务（测试用）。
func NewResetPasswordServiceWithTTL(store ResetTokenStore, sender ResetEmailSender, ttl time.Duration) *ResetPasswordService {
	svc := NewResetPasswordService(store, sender)
	svc.ttl = ttl
	return svc
}

// RequestReset 请求密码重置：生成 token + 发送邮件。
// 无论邮箱是否注册，都返回成功（避免暴露哪些邮箱已注册）。
func (s *ResetPasswordService) RequestReset(ctx context.Context, email string) error {
	if email == "" {
		return fmt.Errorf("邮箱不能为空")
	}

	token, err := generateResetToken(s.tokenLen)
	if err != nil {
		return fmt.Errorf("生成重置token失败: %w", err)
	}

	expiresAt := time.Now().Add(s.ttl)

	// 保存 token
	if err := s.store.Save(ctx, email, token, expiresAt); err != nil {
		return fmt.Errorf("保存重置token失败: %w", err)
	}

	// 构造重置链接（前端路由，实际域名由部署配置决定）
	resetLink := fmt.Sprintf("/reset-password?email=%s&token=%s", email, token)

	// 发送邮件
	if err := s.sender.SendResetEmail(ctx, email, resetLink); err != nil {
		// 邮件发送失败不返回给前端（避免泄露邮箱是否注册）
		// 但内部应记录日志
		return nil
	}

	return nil
}

// VerifyToken 验证重置 token 是否有效（仅验证，不消费）。
// 用于用户点击链接后，前端先校验 token 是否有效再显示重置表单。
func (s *ResetPasswordService) VerifyToken(ctx context.Context, email, token string) (bool, error) {
	if email == "" || token == "" {
		return false, nil
	}
	return s.store.Verify(ctx, email, token)
}

// ResetPassword 执行密码重置：验证 token + 消费 token。
// 调用方在验证成功后负责更新用户密码。
func (s *ResetPasswordService) ResetPassword(ctx context.Context, email, token string) (bool, error) {
	if email == "" || token == "" {
		return false, fmt.Errorf("参数不完整")
	}

	// 先验证
	valid, err := s.store.Verify(ctx, email, token)
	if err != nil {
		return false, err
	}
	if !valid {
		return false, nil
	}

	// 消费 token（标记已使用）
	if err := s.store.Consume(ctx, email, token); err != nil {
		return false, err
	}

	return true, nil
}

// ——— 工具函数 ———

// generateResetToken 生成安全的随机 token。
func generateResetToken(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
