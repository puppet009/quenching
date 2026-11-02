// T3-4 训练对话的脱敏故障上下文仓储（拆库后版本）。
//
// 跨服务归属：session_defects / training_sessions 是本服务表，defects 是
// fault-engine 属主表。原先共库 JOIN defects 的只读访问，已替换为：
// 本地查 session_defects 的 defect_id/status → 调 fault-engine 内部接口
// 批量回填脱敏字段，编排层（service.ChatService）无需改动。
//
// 安全红线：回填数据来自 fault-engine 的内部脱敏投影（白名单列），
// root_cause、fix_config、trigger_config、manifestation_config、
// amplifier_config 等答案或注入细节列不会经内部接口流出。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/service"
)

var _ service.FaultContextProvider = (*FaultContextRepo)(nil)

// FaultContextRepo 脱敏故障上下文只读仓储。
type FaultContextRepo struct {
	pool    *pgxpool.Pool
	defects service.DefectReader
}

// NewFaultContextRepo 创建脱敏故障上下文仓储。defects 为 fault-engine 只读端口。
func NewFaultContextRepo(pool *pgxpool.Pool, defects service.DefectReader) *FaultContextRepo {
	return &FaultContextRepo{pool: pool, defects: defects}
}

// localDefectRow 本地 session_defects 查询的中间承载。
type localDefectRow struct {
	defectID int64
	status   string
}

// LoadFaultContext 取某次训练会话中已暴露（status 非 dormant）的故障上下文。
// 会话没有已暴露故障（含非训练对话）时返回 (nil, nil)，编排层据此退化为
// 无上下文引导；该结果不是错误。
func (r *FaultContextRepo) LoadFaultContext(ctx context.Context, sessionID int64) (*service.FaultContext, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT defect_id, COALESCE(status, '')
		 FROM session_defects
		 WHERE session_id=$1 AND deleted_at IS NULL
		 ORDER BY triggered_at NULLS LAST, defect_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	local := make([]localDefectRow, 0)
	for rows.Next() {
		var l localDefectRow
		if err := rows.Scan(&l.defectID, &l.status); err != nil {
			return nil, err
		}
		local = append(local, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 仅保留已暴露（非 dormant）故障。
	exposed := make([]localDefectRow, 0, len(local))
	for _, l := range local {
		if l.status != "dormant" {
			exposed = append(exposed, l)
		}
	}
	if len(exposed) == 0 || r.defects == nil {
		// 故障尚未暴露：不带任何上下文，避免助教"未卜先知"。
		return nil, nil
	}

	ids := make([]int64, 0, len(exposed))
	for _, l := range exposed {
		ids = append(ids, l.defectID)
	}
	views, err := r.defects.BatchDefects(ctx, ids)
	if err != nil {
		return nil, err
	}
	viewByID := make(map[int64]service.FaultDefectView, len(views))
	for i := range views {
		viewByID[views[i].ID] = views[i]
	}

	// 按本地顺序（triggered_at, defect_id）拼装，保证与原 JOIN 排序口径一致。
	fc := &service.FaultContext{SessionID: sessionID}
	for _, l := range exposed {
		v, ok := viewByID[l.defectID]
		if !ok {
			// 故障在 fault-engine 侧已删除：跳过，不用占位项污染上下文。
			continue
		}
		fc.Faults = append(fc.Faults, service.FaultContextItem{
			Name:        v.Name,
			Category:    v.Category,
			Difficulty:  v.Difficulty,
			Description: v.Description,
			Status:      l.status,
		})
	}
	if len(fc.Faults) == 0 {
		return nil, nil
	}

	// 会话难度（T3-5 提示配额也要用）；读不到不影响故障上下文本身。
	var difficulty *string
	switch err := r.pool.QueryRow(ctx,
		`SELECT difficulty FROM training_sessions WHERE id=$1 AND deleted_at IS NULL`,
		sessionID).Scan(&difficulty); {
	case err == nil:
		if difficulty != nil {
			fc.Difficulty = *difficulty
		}
	case errors.Is(err, pgx.ErrNoRows):
		// 会话行异常缺失：忽略，难度留空。
	default:
		return nil, err
	}
	return fc, nil
}
