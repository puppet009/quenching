// Package repository 中坑点引擎的会话运行时供给（T3-9）。
//
// 编排层只依赖 service.PitfallRuntime 接口；这里负责把 PG 里的会话状态翻译成
// 纯逻辑引擎所需的 model.PitfallRuntimeContext（轮次/阶段/经过秒数/已触发缺陷），
// 以及按会话加载 active 坑点配置、统计已注入次数。本地无 PG 时由内存 fake 单测覆盖。
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现编排层要求的 service.PitfallRuntime。
var _ service.PitfallRuntime = (*PitfallRuntimeRepo)(nil)

// PitfallRuntimeRepo 坑点引擎的 PG 运行时供给。
//
// 配置读取复用 service.Loader + service.Store（T3-8），本结构只补会话状态侧。
type PitfallRuntimeRepo struct {
	pool   *pgxpool.Pool
	loader *service.Loader
}

// NewPitfallRuntime 创建运行时供给；store 一般即 *PitfallRepo。
func NewPitfallRuntime(pool *pgxpool.Pool, store service.Store) *PitfallRuntimeRepo {
	return &PitfallRuntimeRepo{pool: pool, loader: service.NewLoader(store)}
}

// ActiveConfigs 取某训练会话挂载缺陷下的全部 active 坑点，按缺陷分组展平，
// 组内/组间均有确定顺序（缺陷 id 升序，组内 sort_order,id 升序），
// 与引擎"一轮至多一坑、取最先命中"的优先级口径一致。
func (r *PitfallRuntimeRepo) ActiveConfigs(ctx context.Context, sessionID int64) ([]*model.PitfallConfig, error) {
	defectIDs, err := r.sessionDefectIDs(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(defectIDs) == 0 {
		return nil, nil
	}
	grouped, err := r.loader.LoadByDefects(ctx, defectIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*model.PitfallConfig, 0)
	for _, id := range defectIDs { // defectIDs 已升序，保证跨缺陷优先级确定
		out = append(out, grouped[id]...)
	}
	return out, nil
}

// Snapshot 组装判定时刻的运行时上下文。
func (r *PitfallRuntimeRepo) Snapshot(
	ctx context.Context, sessionID, conversationID int64, _ string,
) (model.PitfallRuntimeContext, error) {
	rc := model.PitfallRuntimeContext{TriggeredDefects: map[int64]bool{}}

	// 实训开始时间：started_at 为空（尚未正式开训）时 ElapsedSec 保持 0。
	var startedAt *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT started_at FROM training_sessions WHERE id=$1`, sessionID).Scan(&startedAt)
	if err != nil {
		return rc, err
	}
	if startedAt != nil {
		if d := int(time.Since(*startedAt).Seconds()); d > 0 {
			rc.ElapsedSec = d
		}
	}

	// 当前轮次：本对话中此前已有多少条用户消息，本轮用户消息已先落库，故 +1。
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM ai_messages WHERE conversation_id=$1 AND role='user'`,
		conversationID).Scan(&rc.Turn); err != nil {
		return rc, err
	}

	// 已触发缺陷集合：triggered 及之后（identified/fixed/timeout）都算已暴露。
	rows, err := r.pool.Query(ctx,
		`SELECT defect_id FROM session_defects
		 WHERE session_id=$1 AND status <> 'dormant'`, sessionID)
	if err != nil {
		return rc, err
	}
	for rows.Next() {
		var defectID int64
		if err := rows.Scan(&defectID); err != nil {
			rows.Close()
			return rc, err
		}
		rc.TriggeredDefects[defectID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return rc, err
	}

	stage, err := r.currentStage(ctx, sessionID)
	if err != nil {
		return rc, err
	}
	rc.Stage = stage
	return rc, nil
}

// InjectedCount 统计某坑点在本会话全部对话中已注入的助手坑点消息数。
func (r *PitfallRuntimeRepo) InjectedCount(ctx context.Context, sessionID, configID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM ai_messages m
		 JOIN ai_conversations c ON c.id = m.conversation_id
		 WHERE c.session_id=$1 AND m.is_pitfall=true AND m.pitfall_config_id=$2`,
		sessionID, configID).Scan(&n)
	return n, err
}

// OperationsSince 取某会话在 since 时刻（严格）之后按时间正序发生的操作（T3-11）。
// 只读消费 training_operation_projection（写入属 T1-23）：表无数据/任务未上线时返回空切片，
// 调用方据此退化为"仅按回复文本判定"。category/target 可空，用 *string 扫描。
func (r *PitfallRuntimeRepo) OperationsSince(
	ctx context.Context, sessionID int64, since time.Time,
) ([]model.PitfallOperation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT operation_type, operation_category, target
		 FROM training_operation_projection
		 WHERE session_id=$1 AND created_at > $2 AND deleted_at IS NULL
		 ORDER BY created_at, id`,
		sessionID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ops := make([]model.PitfallOperation, 0)
	for rows.Next() {
		var op model.PitfallOperation
		var category, target *string
		if err := rows.Scan(&op.Type, &category, &target); err != nil {
			return nil, err
		}
		if category != nil {
			op.Category = *category
		}
		if target != nil {
			op.Target = *target
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

// sessionDefectIDs 取会话挂载的缺陷 id（升序、去重）。
func (r *PitfallRuntimeRepo) sessionDefectIDs(ctx context.Context, sessionID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT defect_id FROM session_defects WHERE session_id=$1 ORDER BY defect_id`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// currentStage 以最近一条操作的类别启发式推断当前排障阶段（MVP 无独立阶段状态机）：
// action（执行命令/改配置/重启）→ fix；analyze → locate；observe → detect。
// 无操作记录时返回空串（stage_enter 类坑点因此不会提前命中）。
func (r *PitfallRuntimeRepo) currentStage(ctx context.Context, sessionID int64) (string, error) {
	var category *string
	err := r.pool.QueryRow(ctx,
		`SELECT operation_category FROM training_operation_projection
		 WHERE session_id=$1 AND operation_category IS NOT NULL
		 ORDER BY created_at DESC, id DESC LIMIT 1`, sessionID).Scan(&category)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if category == nil {
		return "", nil
	}
	switch *category {
	case "action":
		return "fix", nil
	case "analyze":
		return "locate", nil
	case "observe":
		return "detect", nil
	default:
		return "", nil
	}
}
