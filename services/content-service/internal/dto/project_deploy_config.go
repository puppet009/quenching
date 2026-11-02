package dto

import "github.com/quenching/content-service/internal/model"

// SaveProjectDeployConfigReq 保存项目部署配置请求（Upsert语义）。
type SaveProjectDeployConfigReq struct {
	ProjectID            int64                  `json:"project_id" binding:"required,min=1"`
	K8sNamespaceTemplate string                 `json:"k8s_namespace_template" binding:"max=100"`
	HelmChart            string                 `json:"helm_chart" binding:"max=500"`
	HelmValues           map[string]interface{} `json:"helm_values"`
	IngressConfig        map[string]interface{} `json:"ingress_config"`
	StorageConfig        map[string]interface{} `json:"storage_config"`
	NetworkPolicy        map[string]interface{} `json:"network_policy"`
	InitScript           string                 `json:"init_script"`
	HealthCheck          map[string]interface{} `json:"health_check"`
	AccessLayerConfig    map[string]interface{} `json:"access_layer_config"`
}

// UpdateProjectDeployConfigReq 部分更新部署配置请求。
type UpdateProjectDeployConfigReq struct {
	K8sNamespaceTemplate *string                 `json:"k8s_namespace_template" binding:"omitempty,max=100"`
	HelmChart            *string                 `json:"helm_chart" binding:"omitempty,max=500"`
	HelmValues           *map[string]interface{} `json:"helm_values"`
	IngressConfig        *map[string]interface{} `json:"ingress_config"`
	StorageConfig        *map[string]interface{} `json:"storage_config"`
	NetworkPolicy        *map[string]interface{} `json:"network_policy"`
	InitScript           *string                 `json:"init_script"`
	HealthCheck          *map[string]interface{} `json:"health_check"`
	AccessLayerConfig    *map[string]interface{} `json:"access_layer_config"`
}

// ProjectDeployConfigResp 部署配置响应（直接用 model）。
type ProjectDeployConfigResp = model.ProjectDeployConfig
