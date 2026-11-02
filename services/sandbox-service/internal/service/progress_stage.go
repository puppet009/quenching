// progress_stage.go 沙箱启动进度。
//
// 将沙箱启动过程拆分为多个阶段，
// 结合 DB 状态 + K8s 实际资源情况计算进度百分比，
// 支持 SSE 推送和轮询查询两种方式。

package service

import (
	"fmt"
	"time"
)

// Stage 启动阶段。
type Stage string

const (
	StageInitializing Stage = "initializing" // 初始化：创建 namespace / 配额 / 网络策略
	StageDeploying    Stage = "deploying"    // 部署中：Helm Chart 安装
	StageStarting     Stage = "starting"     // 启动中：Pod 启动等待就绪
	StageReady        Stage = "ready"        // 就绪
	StageFailed       Stage = "failed"       // 失败
)

// StageInfo 阶段描述信息。
type StageInfo struct {
	Stage   Stage  `json:"stage"`
	Name    string `json:"name"`    // 中文名称，前端展示
	Percent int    `json:"percent"` // 该阶段完成时的累计进度百分比
}

// 阶段定义 + 进度权重（总 100%）。
var stages = []StageInfo{
	{Stage: StageInitializing, Name: "初始化环境", Percent: 15},
	{Stage: StageDeploying, Name: "部署应用", Percent: 50},
	{Stage: StageStarting, Name: "启动服务", Percent: 95},
	{Stage: StageReady, Name: "就绪", Percent: 100},
}

// ProgressStatus 进度状态（返回给前端的完整信息）。
type ProgressStatus struct {
	SessionID    int64     `json:"session_id"`
	Status       string    `json:"status"`          // 沙箱整体状态（creating/deploying/ready/failed）
	Stage        Stage     `json:"stage"`           // 当前阶段
	StageName    string    `json:"stage_name"`      // 当前阶段中文名
	Percent      int       `json:"percent"`         // 总进度百分比 0-100
	Message      string    `json:"message"`         // 当前阶段描述
	ElapsedSec   int64     `json:"elapsed_sec"`     // 已用时间（秒）
	EstimatedSec int64     `json:"estimated_sec"`   // 预计剩余时间（秒），-1 表示未知
	ReadyCount   int       `json:"ready_count"`     // 已就绪 Deployment 数
	TotalCount   int       `json:"total_count"`     // 总 Deployment 数
	Error        string    `json:"error,omitempty"` // 失败时的错误信息
	StartedAt    time.Time `json:"started_at"`      // 开始时间
}

// stageIndex 找阶段在 stages 中的索引，找不到返回 -1。
func stageIndex(s Stage) int {
	for i, info := range stages {
		if info.Stage == s {
			return i
		}
	}
	return -1
}

// StageInfoOf 获取阶段信息。
func StageInfoOf(s Stage) (StageInfo, bool) {
	for _, info := range stages {
		if info.Stage == s {
			return info, true
		}
	}
	return StageInfo{}, false
}

// AllStages 返回所有阶段列表（前端渲染进度条步骤用）。
func AllStages() []StageInfo {
	result := make([]StageInfo, len(stages))
	copy(result, stages)
	return result
}

// calculatePercent 计算当前进度百分比。
// 当前阶段内的进度 = (前一阶段百分比 + 当前阶段内进度)
// intraProgress 是当前阶段内的进度 0.0 - 1.0
func calculatePercent(s Stage, intraProgress float64) int {
	idx := stageIndex(s)
	if idx < 0 {
		return 0
	}

	// 终态直接返回 100 或 0
	if s == StageReady {
		return 100
	}
	if s == StageFailed {
		return 0
	}

	// 前一阶段的百分比
	prevPercent := 0
	if idx > 0 {
		prevPercent = stages[idx-1].Percent
	}

	// 当前阶段的总百分比范围
	currPercent := stages[idx].Percent
	stageRange := currPercent - prevPercent

	// 计算当前阶段内的进度
	current := float64(prevPercent) + float64(stageRange)*intraProgress

	result := int(current)
	if result < 0 {
		result = 0
	}
	if result > 100 {
		result = 100
	}
	return result
}

// estimateRemaining 估算剩余时间（秒）。
// 简单算法：已用时间 / 进度百分比 * 剩余百分比
// percent 必须大于 0，否则返回 -1（未知）
func estimateRemaining(elapsed time.Duration, percent int) int64 {
	if percent <= 0 || percent >= 100 {
		return -1
	}

	elapsedSec := int64(elapsed.Seconds())
	totalEstimate := int64(float64(elapsedSec) * 100.0 / float64(percent))
	remaining := totalEstimate - elapsedSec
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// stageMessage 返回当前阶段的友好描述。
func stageMessage(s Stage, ready, total int) string {
	switch s {
	case StageInitializing:
		return "正在创建沙箱环境..."
	case StageDeploying:
		return "正在部署应用服务..."
	case StageStarting:
		if total > 0 {
			return fmt.Sprintf("正在启动服务 (%d/%d)", ready, total)
		}
		return "正在启动服务..."
	case StageReady:
		return "环境已就绪"
	case StageFailed:
		return "启动失败，请重试"
	default:
		return "准备中..."
	}
}
