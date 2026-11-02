// Package model 领域模型：核心实体定义。
// 内容域实体：项目、故障剧本、AI 坑点配置、标签、难度/复盘模板。
// 这里以 Project 为 CRUD 参考实现示例。
package model

import "time"

// Project 训练项目（电商、微服务等模板项目的登记信息）。
type Project struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	Domain            string    `json:"domain"`             // 业务领域：ecommerce/im/...
	Description       string    `json:"description"`        // 项目简介
	TechStack         []string  `json:"tech_stack"`         // 技术栈，PG text[]
	ArchitectureImage string    `json:"architecture_image"` // 架构图 URL
	DeployConfig      JSON      `json:"deploy_config"`      // 部署配置，JSONB
	Difficulty        string    `json:"difficulty"`         // 难度：easy/medium/hard
	Status            string    `json:"status"`             // online/offline/draft
	SortOrder         int       `json:"sort_order"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// JSON 用于映射 PG JSONB（对象型），repository 层负责序列化。
type JSON = map[string]interface{}

// JSONArray 用于映射 PG JSONB（数组型）。
type JSONArray = []interface{}

// 状态常量。
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusDraft   = "draft"
)
