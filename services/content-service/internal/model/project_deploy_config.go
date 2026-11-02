package model

import "time"

// ProjectDeployConfig 项目在K8s中的部署配置，沙箱创建时读取此配置生成部署清单。
type ProjectDeployConfig struct {
	ID                   int64     `json:"id"`
	ProjectID            int64     `json:"project_id"`
	K8sNamespaceTemplate string    `json:"k8s_namespace_template"`
	HelmChart            string    `json:"helm_chart"`
	HelmValues           JSON      `json:"helm_values"`         // JSONB Helm Values配置
	IngressConfig        JSON      `json:"ingress_config"`      // JSONB Ingress配置
	StorageConfig        JSON      `json:"storage_config"`      // JSONB 存储配置（PVC等）
	NetworkPolicy        JSON      `json:"network_policy"`      // JSONB 网络策略配置
	InitScript           string    `json:"init_script"`         // 初始化脚本（建表、导入数据等）
	HealthCheck          JSON      `json:"health_check"`        // JSONB 健康检查配置
	AccessLayerConfig    JSON      `json:"access_layer_config"` // JSONB 接入层配置
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}
