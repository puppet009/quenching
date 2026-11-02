// 短信验证码服务。
//
// 支持两种模式：
//   - mock 模式：开发/测试环境使用，直接返回生成的验证码，不真实发送
//   - real 模式：生产环境使用，对接真实短信服务商（当前预留接口）
//
// 验证码使用场景：注册、登录、找回密码、换绑手机等。
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

// 验证码用途类型。
const (
	SMSPurposeRegister    = "register"     // 注册
	SMSPurposeLogin       = "login"        // 登录
	SMSPurposeResetPwd    = "reset_pwd"    // 重置密码
	SMSPurposeChangePhone = "change_phone" // 换绑手机
)

// smsCodeRecord 验证码记录。
type smsCodeRecord struct {
	Code      string
	Purpose   string
	ExpiresAt time.Time
	Used      bool
}

// SMSService 短信服务接口。
type SMSService interface {
	// SendCode 发送验证码到指定手机号。
	// 返回验证码（仅 mock 模式返回真实值，real 模式返回空串）。
	SendCode(ctx context.Context, phone, purpose string) (string, error)

	// VerifyCode 校验验证码是否正确且未过期。
	VerifyCode(ctx context.Context, phone, purpose, code string) (bool, error)

	// IsMock 是否为 mock 模式。
	IsMock() bool
}

// MockSMSService mock 模式短信服务（内存存储验证码）。
// 开发/测试环境使用，不真实发送短信。
type MockSMSService struct {
	mu      sync.RWMutex
	codes   map[string]*smsCodeRecord // key: phone:purpose
	ttl     time.Duration             // 验证码有效期
	codeLen int                       // 验证码长度
}

// NewMockSMSService 创建 mock 短信服务。
func NewMockSMSService() *MockSMSService {
	return &MockSMSService{
		codes:   make(map[string]*smsCodeRecord),
		ttl:     5 * time.Minute,
		codeLen: 6,
	}
}

// NewMockSMSServiceWithTTL 创建自定义有效期的 mock 服务（测试用）。
func NewMockSMSServiceWithTTL(ttl time.Duration) *MockSMSService {
	return &MockSMSService{
		codes:   make(map[string]*smsCodeRecord),
		ttl:     ttl,
		codeLen: 6,
	}
}

func (s *MockSMSService) IsMock() bool { return true }

// SendCode 生成并保存验证码，返回明文（mock 模式直接返回，便于前端调试）。
func (s *MockSMSService) SendCode(_ context.Context, phone, purpose string) (string, error) {
	if phone == "" {
		return "", fmt.Errorf("手机号不能为空")
	}
	if purpose == "" {
		return "", fmt.Errorf("验证码用途不能为空")
	}

	code := generateCode(s.codeLen)

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.key(phone, purpose)
	s.codes[key] = &smsCodeRecord{
		Code:      code,
		Purpose:   purpose,
		ExpiresAt: time.Now().Add(s.ttl),
		Used:      false,
	}

	// mock 模式直接返回验证码，方便开发调试
	return code, nil
}

// VerifyCode 校验验证码。
func (s *MockSMSService) VerifyCode(_ context.Context, phone, purpose, code string) (bool, error) {
	if phone == "" || purpose == "" || code == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.key(phone, purpose)
	record, ok := s.codes[key]
	if !ok {
		return false, nil
	}

	// 已使用
	if record.Used {
		return false, nil
	}

	// 已过期
	if time.Now().After(record.ExpiresAt) {
		delete(s.codes, key)
		return false, nil
	}

	// 验证码不匹配
	if record.Code != code {
		return false, nil
	}

	// 验证成功，标记为已使用（一次性）
	record.Used = true
	return true, nil
}

// ——— 内部工具 ———

func (s *MockSMSService) key(phone, purpose string) string {
	return phone + ":" + purpose
}

// generateCode 生成指定长度的数字验证码。
func generateCode(length int) string {
	if length <= 0 {
		length = 6
	}
	code := make([]byte, length)
	for i := 0; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		code[i] = byte('0' + n.Int64())
	}
	return string(code)
}

// RealSMSService 真实短信服务（预留，生产环境对接短信网关）。
// 当前为占位实现，返回 "not implemented" 错误。
// 未来接入阿里云/腾讯云短信时，只需实现这个结构体。
type RealSMSService struct {
	AccessKeyID     string
	AccessKeySecret string
	SignName        string // 短信签名
	TemplateCode    string // 短信模板编码
}

// NewRealSMSService 创建真实短信服务。
func NewRealSMSService(accessKeyID, accessKeySecret, signName, templateCode string) *RealSMSService {
	return &RealSMSService{
		AccessKeyID:     accessKeyID,
		AccessKeySecret: accessKeySecret,
		SignName:        signName,
		TemplateCode:    templateCode,
	}
}

func (s *RealSMSService) IsMock() bool { return false }

func (s *RealSMSService) SendCode(_ context.Context, phone, purpose string) (string, error) {
	// TODO: 对接真实短信网关
	// 当前返回错误，确保生产环境不会误用
	return "", fmt.Errorf("真实短信服务尚未实现，请配置短信网关")
}

func (s *RealSMSService) VerifyCode(_ context.Context, phone, purpose, code string) (bool, error) {
	// 真实模式下验证码校验也走同一个存储逻辑
	// 这里需要配合 Redis/DB 存储，当前暂未实现
	return false, fmt.Errorf("真实短信服务尚未实现")
}
