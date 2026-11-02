// Package model 用户领域模型。
package model

import "time"

const (
	StatusActive = "active"
	StatusBanned = "banned"
)

// User 用户账号。
type User struct {
	ID           int64     `json:"id"`
	AccountType  string    `json:"account_type"` // phone / email
	Phone        string    `json:"phone,omitempty"`
	Email        string    `json:"email,omitempty"`
	PasswordHash string    `json:"-"` // 不序列化给前端
	Nickname     string    `json:"nickname"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Bio          string    `json:"bio,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// IsBanned 是否封禁。
func (u *User) IsBanned() bool { return u.Status == StatusBanned }
