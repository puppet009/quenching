package repository

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/quenching/sandbox-service/internal/model"
)

// pgconnCT Exec 返回的命令标签类型别名（便于 pg_store 中声明变量）。
type pgconnCT = pgconn.CommandTag

// scannable pgx.Row 与 pgx.Rows 共同满足的最小 Scan 接口，
// 让 scanSandbox/scanMetrics 一处实现、两种调用复用。
type scannable interface {
	Scan(dest ...any) error
}

// rows 收集多条记录所需的最小 Rows 接口。
type rows interface {
	scannable
	Next() bool
	Err() error
}

// NullTime 可空 TIMESTAMPTZ 扫描类型。
// 直接用 *time.Time 接收 NULL 列在 pgx v5 下可行但语义隐晦，统一显式包装：
// NULL -> Valid=false（对应 model 零值时间），非空 -> Valid=true。
type NullTime = sql.NullTime

// itoa int -> string，分页拼占位符用（strconv 薄封装，保持调用点简洁）。
func itoa(n int) string { return strconv.Itoa(n) }

// normalizePage 归一化分页参数：页码从 1 起，默认 10 条/页，上限 100。
func normalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// collectSandboxes 遍历 Rows 收集沙箱列表。
func collectSandboxes(r rows) ([]model.Sandbox, error) {
	result := make([]model.Sandbox, 0)
	for r.Next() {
		sb, err := scanSandbox(r)
		if err != nil {
			return nil, err
		}
		result = append(result, sb)
	}
	return result, r.Err()
}

// scanMetrics 扫描一条资源指标快照（列顺序须与各查询 SELECT 一致）。
func scanMetrics(row scannable) (model.ResourceMetrics, error) {
	var m model.ResourceMetrics
	var ts time.Time
	if err := row.Scan(
		&m.SessionID, &m.Namespace, &m.CPUCoresM, &m.CPUPercent,
		&m.MemoryBytes, &m.MemoryPercent,
		&m.PodCount, &m.PodRunning, &m.PodAbnormal, &ts,
	); err != nil {
		return model.ResourceMetrics{}, err
	}
	m.Timestamp = ts
	return m, nil
}

// 编译期保证 pgx.Rows 满足本地 rows 接口。
var _ rows = (pgx.Rows)(nil)
