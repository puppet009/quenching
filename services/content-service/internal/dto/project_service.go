package dto

import "github.com/quenching/content-service/internal/model"

// CreateProjectServiceReq 创建项目服务请求。
type CreateProjectServiceReq struct {
	ProjectID        int64                  `json:"project_id" binding:"required,min=1"`
	Name             string                 `json:"name" binding:"required,min=1,max=100"`
	DisplayName      string                 `json:"display_name" binding:"max=100"`
	ServiceType      string                 `json:"service_type" binding:"required,oneof=app database cache mq gateway"`
	TechStack        string                 `json:"tech_stack" binding:"max=50"`
	Image            string                 `json:"image" binding:"required,max=500"`
	Ports            []interface{}          `json:"ports"`
	EnvConfig        map[string]interface{} `json:"env_config"`
	ResourceDefault  map[string]interface{} `json:"resource_default"`
	ResourceMax      map[string]interface{} `json:"resource_max"`
	Dependencies     []interface{}          `json:"dependencies"`
	MonitoringConfig map[string]interface{} `json:"monitoring_config"`
	LogConfig        map[string]interface{} `json:"log_config"`
	SortOrder        int                    `json:"sort_order"`
}

// UpdateProjectServiceReq 更新项目服务请求。
type UpdateProjectServiceReq struct {
	Name             *string                 `json:"name" binding:"omitempty,min=1,max=100"`
	DisplayName      *string                 `json:"display_name" binding:"omitempty,max=100"`
	ServiceType      *string                 `json:"service_type" binding:"omitempty,oneof=app database cache mq gateway"`
	TechStack        *string                 `json:"tech_stack" binding:"omitempty,max=50"`
	Image            *string                 `json:"image" binding:"omitempty,max=500"`
	Ports            *[]interface{}          `json:"ports"`
	EnvConfig        *map[string]interface{} `json:"env_config"`
	ResourceDefault  *map[string]interface{} `json:"resource_default"`
	ResourceMax      *map[string]interface{} `json:"resource_max"`
	Dependencies     *[]interface{}          `json:"dependencies"`
	MonitoringConfig *map[string]interface{} `json:"monitoring_config"`
	LogConfig        *map[string]interface{} `json:"log_config"`
	SortOrder        *int                    `json:"sort_order"`
}

// ProjectServiceListResp 服务列表响应。
type ProjectServiceListResp struct {
	List []model.ProjectService `json:"list"`
}
