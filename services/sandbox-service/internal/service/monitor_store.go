package service

import (
	"context"

	"github.com/quenching/sandbox-service/internal/model"
)

// MonitorStore 沙箱监控仓储接口（管理后台用）。
// 扩展自基础 Store，增加列表查询、详情、异常记录等能力。
// 生产环境用 PG 实现，单测用内存实现。
type MonitorStore interface {
	// 基础接口（沿用 Store 的子集）
	Store

	// ——— 列表 & 详情 ———

	// List 分页查询沙箱列表（支持多维度筛选）。
	List(ctx context.Context, q model.SandboxQuery) (*model.SandboxPage, error)

	// GetBySessionID 按 sessionID 获取沙箱。
	GetBySessionID(ctx context.Context, sessionID int64) (*model.Sandbox, error)

	// GetByID 按 ID 获取沙箱。
	GetByID(ctx context.Context, id int64) (*model.Sandbox, error)

	// Count 统计沙箱数量（按状态分组）。
	CountByStatus(ctx context.Context) (map[string]int, error)

	// CountActive 统计活跃沙箱总数。
	CountActive(ctx context.Context) (int, error)

	// ——— 资源监控 ———

	// SaveMetrics 保存资源指标快照。
	SaveMetrics(ctx context.Context, metrics *model.ResourceMetrics) error

	// GetLatestMetrics 获取沙箱最新资源指标。
	GetLatestMetrics(ctx context.Context, sessionID int64) (*model.ResourceMetrics, error)

	// ListMetrics 获取沙箱最近 N 条指标（用于趋势图）。
	ListMetrics(ctx context.Context, sessionID int64, limit int) ([]model.ResourceMetrics, error)

	// ——— 异常告警 ———

	// RecordAnomaly 记录一条异常。
	RecordAnomaly(ctx context.Context, anomaly *model.SandboxAnomaly) error

	// ListAnomalies 分页查询异常列表。
	ListAnomalies(ctx context.Context, q model.AnomalyQuery) (*model.AnomalyPage, error)

	// ResolveAnomaly 标记异常已解决。
	ResolveAnomaly(ctx context.Context, anomalyID int64) error

	// CountUnresolvedAnomalies 统计未解决的异常数。
	CountUnresolvedAnomalies(ctx context.Context) (int, error)

	// ——— 手动释放 ———

	// Release 标记沙箱为 releasing 状态（由调谐器实际执行释放）。
	Release(ctx context.Context, sessionID int64) error
}
