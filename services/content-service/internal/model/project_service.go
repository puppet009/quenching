package model

import "time"

// ProjectService 项目包含的服务（微服务），每个服务有自己的镜像、端口、资源配置。
type ProjectService struct {
	ID               int64     `json:"id"`
	ProjectID        int64     `json:"project_id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	ServiceType      string    `json:"service_type"` // app/database/cache/mq/gateway
	TechStack        string    `json:"tech_stack"`
	Image            string    `json:"image"`
	Ports            JSONArray `json:"ports"`             // JSONB 端口映射配置（数组）
	EnvConfig        JSON      `json:"env_config"`        // JSONB 环境变量配置
	ResourceDefault  JSON      `json:"resource_default"`  // JSONB 默认资源配置（cpu/memory）
	ResourceMax      JSON      `json:"resource_max"`      // JSONB 最大资源配置
	Dependencies     JSONArray `json:"dependencies"`      // JSONB 依赖的其他服务ID列表（数组）
	MonitoringConfig JSON      `json:"monitoring_config"` // JSONB 监控指标配置
	LogConfig        JSON      `json:"log_config"`        // JSONB 日志采集配置
	SortOrder        int       `json:"sort_order"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// 服务类型常量。
const (
	ServiceTypeApp      = "app"
	ServiceTypeDatabase = "database"
	ServiceTypeCache    = "cache"
	ServiceTypeMQ       = "mq"
	ServiceTypeGateway  = "gateway"
)
