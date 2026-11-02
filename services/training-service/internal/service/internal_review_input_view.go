// 服务间内部接口专用的复盘聚合投影与只读端口（架构拆库第五批）。
//
// 本服务既有分层约定：跨域 DTO 与端口定义在 service 包，repository 单向
// 依赖 service。review-service 生成复盘时需要的会话难度、操作时间线、本次
// 会话剧本（含剧本预设根因）与 AI 坑点原始数据，统一由 training-service
// 在本服务内聚合后经 /internal/v1 输出，review-service 不再直查本服务任何表。
//
// 剧本详情（root_cause）由 PG 仓储在服务端调 fault-engine 的剧本详情端点
// 回填，不来自调用方；这些路由不挂网关、不对前端暴露。
package service

import (
	"context"
	"encoding/json"
)

// ReviewTimelineItem 时间线操作一项（对应 session_operations 一行）。
type ReviewTimelineItem struct {
	OffsetSec int    `json:"t"`
	Type      string `json:"type"`
	Category  string `json:"category,omitempty"`
	Target    string `json:"target,omitempty"`
	Result    string `json:"result,omitempty"`
}

// ReviewSessionScript 本次会话的故障剧本（session_defects 状态 + 剧本详情）。
type ReviewSessionScript struct {
	DefectID       int64  `json:"defect_id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Difficulty     string `json:"difficulty,omitempty"`
	Description    string `json:"description,omitempty"`
	RootCause      string `json:"root_cause,omitempty"`
	Status         string `json:"status"`
	TimeToIdentify int    `json:"time_to_identify_sec,omitempty"`
	TimeToFix      int    `json:"time_to_fix_sec,omitempty"`
}

// SessionReviewInput 复盘主输入聚合（review-input 端点响应）。
type SessionReviewInput struct {
	Brain      json.RawMessage       `json:"brain,omitempty"`
	SessionID  int64                 `json:"session_id"`
	FaultType  string                `json:"fault_type"`
	Difficulty string                `json:"difficulty,omitempty"`
	Timeline   []ReviewTimelineItem  `json:"timeline"`
	Scripts    []ReviewSessionScript `json:"scripts"`
}

// ReviewPitfallRawItem 会话中出现的 AI 坑点原始数据（pitfalls 端点响应单元）。
// 仅做数据采集，识别与否/识别率统计由 review-service 侧完成。
type ReviewPitfallRawItem struct {
	MessageID       int64  `json:"message_id"`
	PitfallConfigID int64  `json:"pitfall_config_id"`
	PitfallType     string `json:"pitfall_type"`
	UserResponse    string `json:"user_response"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	ErrorContent    string `json:"error_content"`
	CorrectApproach string `json:"correct_approach"`
	Severity        string `json:"severity"`
	AppearOffsetSec int    `json:"appear_offset_sec"`
}

// ReviewChangeItem 助手一次文件修改记录（changes 端点响应单元，M5）。
type ReviewChangeItem struct {
	ID            int64  `json:"id"`
	Pod           string `json:"pod"`
	Container     string `json:"container,omitempty"`
	Path          string `json:"path"`
	BeforeExisted bool   `json:"before_existed"`
	BeforeContent string `json:"before_content,omitempty"`
	AfterContent  string `json:"after_content"`
	OffsetSec     int    `json:"t"` // 相对会话开始的秒数
}

// ReviewInputStore 复盘聚合服务间只读查询端口（由 repository 层 PG 实现）。
type ReviewInputStore interface {
	// GetReviewInput 聚合会话难度 + 时间线 + 剧本（含 root_cause）。
	// 会话不存在返回 (nil,nil)，由 service 层转 NotFound。
	GetReviewInput(ctx context.Context, sessionID int64) (*SessionReviewInput, error)
	// ListPitfalls 返回会话中全部 AI 坑点原始数据，按出现时间升序；无坑点返回空切片。
	ListPitfalls(ctx context.Context, sessionID int64) ([]ReviewPitfallRawItem, error)
	// ListChanges 返回助手全部文件修改记录，按修改时间升序；无记录返回空切片。
	ListChanges(ctx context.Context, sessionID int64) ([]ReviewChangeItem, error)
}
