package service

import "github.com/quenching/go-common/response"

// 内容域错误码段位：2xxxx。各服务占用独立段位，避免冲突。
var (
	ErrProjectNotFound        = response.New(20004, "项目不存在")
	ErrProjectConflict        = response.New(20005, "项目已存在或状态冲突")
	ErrProjectServiceNotFound = response.New(20104, "项目服务不存在")
	ErrDeployConfigNotFound   = response.New(20204, "项目部署配置不存在")
)
