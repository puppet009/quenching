// Package model 故障注入领域模型。
// 定义支持的故障类型、注入规格、实验记录。
// 与具体注入工具（ChaosBlade）解耦：model 只描述"要制造什么故障"。
package model

import "time"

// FaultKind 故障大类。
type FaultKind string

const (
	KindCPU        FaultKind = "cpu"           // CPU 满载
	KindMem        FaultKind = "memory"        // 内存占用
	KindDisk       FaultKind = "disk"          // 磁盘填充
	KindNetDelay   FaultKind = "network_delay" // 网络延迟
	KindNetLoss    FaultKind = "network_loss"  // 网络丢包
	KindProcess    FaultKind = "process"       // 进程 kill
	KindThreadpool FaultKind = "threadpool"    // 线程池打满（JVM 类，预留）
)

// FaultStatus 实验状态。
type FaultStatus string

const (
	StatusCreated FaultStatus = "created" // 已创建未注入
	StatusRunning FaultStatus = "running" // 注入生效中
	StatusRevoked FaultStatus = "revoked" // 已恢复
	StatusFailed  FaultStatus = "failed"  // 注入/恢复失败
)

// InjectSpec 一次故障注入的规格（与剧本 defects 配置对应，运行时由调度引擎填充）。
type InjectSpec struct {
	ScalingHoldID     string `json:"scaling_hold_id,omitempty"`
	FreezeAutoscaling bool   `json:"freeze_autoscaling,omitempty"`
	TemplateDigest    string `json:"template_digest,omitempty"`
	Scope             string `json:"scope,omitempty"`
	TargetPodUID      string `json:"target_pod_uid,omitempty"`
	ExecNode          string `json:"exec_node,omitempty"`
	// Kind 故障类型。
	Kind FaultKind `json:"kind"`
	// TargetNamespace / TargetPod / TargetContainer 注入目标。
	TargetNamespace string `json:"target_namespace"`
	TargetPod       string `json:"target_pod,omitempty"`
	// 容器内执行时的目标进程/匹配表达式（ChaosBlade 的 --process 等）。
	TargetProcess string `json:"target_process,omitempty"`
	// 网卡（网络类）。
	NetworkInterface string `json:"network_interface,omitempty"`

	// 强度参数（各类型按需取）。
	CPUPercent  int `json:"cpu_percent,omitempty"`  // 0-100，CPU 满载百分比
	MemPercent  int `json:"mem_percent,omitempty"`  // 0-100，内存占用百分比
	DiskPercent int `json:"disk_percent,omitempty"` // 0-100，磁盘填充百分比
	DelayMillis int `json:"delay_millis,omitempty"` // 网络延迟毫秒
	LossPercent int `json:"loss_percent,omitempty"` // 0-100，丢包百分比
	DurationSec int `json:"duration_sec,omitempty"` // 持续时间，0 表示不自动恢复（由平台控制）
}

// 注入来源（experiments.source）。
const (
	// SourceScenario 剧本链路：按 defects.amplifier_config 由编排器自动注入（T2-13）。
	SourceScenario = "scenario"
	// SourceConsequence 临时链路：training-service 踩坑后果传导直注（T3-12）。
	SourceConsequence = "consequence"
)

// Experiment 一次注入实验记录。ExperimentUID 来自 ChaosBlade，恢复时必用。
type Experiment struct {
	ID            int64       `json:"id"`
	SessionID     int64       `json:"session_id"`
	Source        string      `json:"source"` // scenario / consequence
	Kind          FaultKind   `json:"kind"`
	Target        string      `json:"target"` // 描述性目标：namespace/pod
	Spec          InjectSpec  `json:"spec"`
	Status        FaultStatus `json:"status"`
	ExperimentUID string      `json:"experiment_uid"` // blade create 返回的 uid，destroy 时用
	Command       string      `json:"command"`        // 实际执行的 blade 命令（审计用）
	FailReason    string      `json:"fail_reason,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}
