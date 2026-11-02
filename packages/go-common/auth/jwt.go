// Package auth 提供平台统一的 JWT 签发与校验。
// user-service 负责签发，api-gateway 用同一实现做鉴权中间件，避免签发/校验两处定义漂移。
// 算法固定 HS256；access 与 refresh 用 subject 区分，refresh 不得用于接口访问。
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	SubjectAccess  = "access"
	SubjectRefresh = "refresh"

	AccessTTL  = 2 * time.Hour
	RefreshTTL = 30 * 24 * time.Hour
)

// Claims JWT 载荷，uid 为用户ID。
type Claims struct {
	UID int64 `json:"uid"`
	jwt.RegisteredClaims
}

// ErrInvalidToken 所有 token 非法情况统一返回，不区分原因，避免被探测。
var ErrInvalidToken = errors.New("invalid token")

// Manager JWT 签发与校验（HS256）。
type Manager struct {
	secret []byte
}

func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

// issue 生成指定类型与有效期的 token。
func (m *Manager) issue(uid int64, ttl time.Duration, subject string) (string, error) {
	now := time.Now()
	claims := Claims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// IssueAccess 签发访问令牌。
func (m *Manager) IssueAccess(uid int64) (string, error) {
	return m.issue(uid, AccessTTL, SubjectAccess)
}

// IssueRefresh 签发刷新令牌。
func (m *Manager) IssueRefresh(uid int64) (string, error) {
	return m.issue(uid, RefreshTTL, SubjectRefresh)
}

// IssuePair 签发 access+refresh 令牌对。
func (m *Manager) IssuePair(uid int64) (access, refresh string, err error) {
	if access, err = m.IssueAccess(uid); err != nil {
		return "", "", err
	}
	if refresh, err = m.IssueRefresh(uid); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// Parse 校验签名、过期时间与算法，返回 Claims（不限制 subject）。
func (m *Manager) Parse(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ParseAccess 仅接受 access 令牌；refresh 令牌不得访问业务接口。
func (m *Manager) ParseAccess(tokenStr string) (*Claims, error) {
	claims, err := m.Parse(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Subject != SubjectAccess {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// ParseRefresh 仅接受 refresh 令牌；仅 user-service 换发令牌对时使用。
func (m *Manager) ParseRefresh(tokenStr string) (*Claims, error) {
	claims, err := m.Parse(tokenStr)
	if err != nil {
		return nil, err
	}
	if claims.Subject != SubjectRefresh {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
