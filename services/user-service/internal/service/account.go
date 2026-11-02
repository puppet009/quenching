// 账号识别、密码校验等纯逻辑（无外部依赖，可单测）。
package service

import (
	"regexp"
	"strings"

	"github.com/quenching/user-service/internal/model"
)

// 中国大陆手机号：1 开头，第二位 3-9，共11位。MVP 先支持大陆号。
var phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

// 邮箱：基础格式校验。
var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// 密码约束：8-32 位，至少含字母和数字。
const (
	MinPasswordLen = 8
	MaxPasswordLen = 32
)

// NormalizeAccount 去除空白；邮箱统一小写，手机号保留数字。
func NormalizeAccount(raw string) string {
	s := strings.TrimSpace(raw)
	if emailRe.MatchString(s) {
		return strings.ToLower(s)
	}
	return s
}

// DetectAccountType 识别账号是手机号还是邮箱；都不匹配返回错误。
func DetectAccountType(account string) (model.AccountType, error) {
	s := strings.TrimSpace(account)
	switch {
	case phoneRe.MatchString(s):
		return model.AccountPhone, nil
	case emailRe.MatchString(s):
		return model.AccountEmail, nil
	default:
		return "", ErrInvalidAccount
	}
}

// ValidatePassword 校验密码强度。
func ValidatePassword(pwd string) error {
	l := len(pwd)
	if l < MinPasswordLen || l > MaxPasswordLen {
		return ErrPasswordLength
	}
	hasLetter, hasDigit := false, false
	for _, ch := range pwd {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z'):
			hasLetter = true
		case ch >= '0' && ch <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrPasswordWeak
	}
	return nil
}

// DefaultNickname 未填昵称时兜底：工程师 + 账号后4位。
func DefaultNickname(account string) string {
	s := []rune(account)
	if len(s) <= 4 {
		return "工程师" + account
	}
	return "工程师" + string(s[len(s)-4:])
}
