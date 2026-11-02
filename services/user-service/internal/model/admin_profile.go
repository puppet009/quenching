package model

// AdminProfile 管理员身份信息：用户基础信息 + 角色名列表 + 合并后的权限列表。
// 供管理后台登录后校验角色（apps/admin 的 getAdminProfile）。
type AdminProfile struct {
	ID          int64    `json:"id"`
	AccountType string   `json:"account_type"` // phone / email
	Nickname    string   `json:"nickname"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	Email       string   `json:"email,omitempty"`
	Phone       string   `json:"phone,omitempty"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}
