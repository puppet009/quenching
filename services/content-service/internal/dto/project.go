// Package dto 请求/响应结构体。handler 只接触 dto，不直接暴露 model。
package dto

import "github.com/quenching/content-service/internal/model"

// CreateProjectReq 创建项目请求。
type CreateProjectReq struct {
	Name              string                 `json:"name" binding:"required,min=1,max=100"`
	Domain            string                 `json:"domain" binding:"required"`
	Description       string                 `json:"description" binding:"max=500"`
	TechStack         []string               `json:"tech_stack"`
	ArchitectureImage string                 `json:"architecture_image" binding:"max=500"`
	DeployConfig      map[string]interface{} `json:"deploy_config"`
	Difficulty        string                 `json:"difficulty" binding:"omitempty,oneof=easy medium hard"`
	SortOrder         int                    `json:"sort_order"`
}

// UpdateProjectReq 更新项目请求（指针区分"未传"与"零值"）。
type UpdateProjectReq struct {
	Name              *string                 `json:"name" binding:"omitempty,max=100"`
	Domain            *string                 `json:"domain"`
	Description       *string                 `json:"description" binding:"omitempty,max=500"`
	TechStack         *[]string               `json:"tech_stack"`
	ArchitectureImage *string                 `json:"architecture_image" binding:"omitempty,max=500"`
	DeployConfig      *map[string]interface{} `json:"deploy_config"`
	Difficulty        *string                 `json:"difficulty" binding:"omitempty,oneof=easy medium hard"`
	Status            *string                 `json:"status" binding:"omitempty,oneof=online offline draft"`
	SortOrder         *int                    `json:"sort_order"`
}

// ProjectListResp 分页列表响应。
type ProjectListResp struct {
	Total int64           `json:"total"`
	List  []model.Project `json:"list"`
}
