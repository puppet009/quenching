// 服务间内部查询专用的最小投影与只读端口。
//
// 微服务拆分后，training 等编排服务不再直接 JOIN projects / project_services，
// 统一经 content-service 的 /internal/v1 接口取数。投影只暴露名称补全所需的
// 最小列，deploy_config / env_config 等业务配置一律不流出。
package repository

import "context"

// InternalProjectView 项目最小投影（内部接口响应单元）。
type InternalProjectView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// InternalProjectServiceView 项目服务最小投影（内部接口响应单元）。
type InternalProjectServiceView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

// InternalProjectReader 项目目录服务间只读查询端口。
type InternalProjectReader interface {
	// ListProjectsByIDs 按 ID 列表批量返回项目最小投影，避免调用方 N+1；
	// 缺失 ID 不占结果位，调用方按返回 ID 自行对齐。
	ListProjectsByIDs(ctx context.Context, ids []int64) ([]*InternalProjectView, error)
	// SearchProjectsByName 按名称模糊反查项目，供调用方对齐本地关键词条件。
	SearchProjectsByName(ctx context.Context, keyword string) ([]*InternalProjectView, error)
	// ListServicesByIDs 按 ID 列表批量返回项目服务最小投影。
	ListServicesByIDs(ctx context.Context, ids []int64) ([]*InternalProjectServiceView, error)
}
