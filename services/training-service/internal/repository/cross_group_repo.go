// 交叉分组（T3-25）的仓储：会话配对与故障目录只读端口。
//
// 跨服务归属：user_profiles 是 user-service 属主表，defects 是 fault-engine 属主表，
// training_sessions 是本服务表。用户分组由 service.UserProfileHTTPClient 经
// user-service 内部接口读写；本文件只保留本服务会话表，以及 session_defects
// 本地查询配合 fault-engine 端口的故障适配。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现编排层要求的会话与故障目录端口。
var (
	_ service.SessionPairStore = (*SessionPairRepo)(nil)
	_ service.DefectCatalog    = (*DefectCatalogRepo)(nil)
)

// ——— 会话配对（training_sessions）———

// SessionPairRepo 会话计数/配对 PG 仓储。
type SessionPairRepo struct {
	pool *pgxpool.Pool
}

// NewSessionPairRepo 创建会话配对仓储。
func NewSessionPairRepo(pool *pgxpool.Pool) *SessionPairRepo {
	return &SessionPairRepo{pool: pool}
}

// groupSessionColumns 查询列，顺序须与 scanGroupSession 一致。
const groupSessionColumns = `id, user_id, COALESCE(project_id, 0), status, ai_mode,
	COALESCE(control_group, ''), COALESCE(pair_session_id, 0)`

// CountSessions 用户历史会话数（排除软删），第几次训练 = 计数 + 1。
func (r *SessionPairRepo) CountSessions(ctx context.Context, userID int64) (int, error) {
	var n int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM training_sessions WHERE user_id=$1 AND deleted_at IS NULL`,
		userID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// GetSession 取单个会话视图；不存在返回 (nil, nil)。
func (r *SessionPairRepo) GetSession(ctx context.Context, sessionID int64) (*service.GroupSession, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+groupSessionColumns+`
		 FROM training_sessions WHERE id=$1 AND deleted_at IS NULL`, sessionID)
	gs, err := scanGroupSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return gs, nil
}

// ListUserSessions 按 id 升序列出用户全部未删除会话。
func (r *SessionPairRepo) ListUserSessions(ctx context.Context, userID int64) ([]service.GroupSession, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+groupSessionColumns+`
		 FROM training_sessions WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.GroupSession, 0)
	for rows.Next() {
		gs, err := scanGroupSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *gs)
	}
	return out, rows.Err()
}

// LinkPair 同事务双向互指 pair_session_id。仅绑定仍未配对的会话，
// 若任一方已被（并发）绑定则不动数据并返回错误，由调用方重新决策。
func (r *SessionPairRepo) LinkPair(ctx context.Context, sessionA, sessionB int64) error {
	if sessionA == sessionB || sessionA <= 0 || sessionB <= 0 {
		return errors.New("配对会话ID非法")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE training_sessions SET pair_session_id=$2, updated_at=now()
		 WHERE id=$1 AND pair_session_id IS NULL`, sessionA, sessionB)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("首场会话已被绑定或不存在，配对中止")
	}
	tag, err = tx.Exec(ctx,
		`UPDATE training_sessions SET pair_session_id=$2, updated_at=now()
		 WHERE id=$1 AND pair_session_id IS NULL`, sessionB, sessionA)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("第二场会话已被绑定或不存在，配对中止")
	}
	return tx.Commit(ctx)
}

func scanGroupSession(row rowScanner) (*service.GroupSession, error) {
	var gs service.GroupSession
	if err := row.Scan(
		&gs.ID, &gs.UserID, &gs.ProjectID, &gs.Status, &gs.AIMode,
		&gs.ControlGroup, &gs.PairSessionID,
	); err != nil {
		return nil, err
	}
	return &gs, nil
}

// ——— 故障目录只读（fault-engine.defects + 本服务 session_defects）———

// DefectCatalogRepo 故障目录只读仓储：defects 字段经 fault-engine 内部接口获取，
// 本服务侧只查 session_defects。
type DefectCatalogRepo struct {
	pool    *pgxpool.Pool
	defects service.DefectReader
}

// NewDefectCatalogRepo 创建故障目录只读仓储。defects 为 fault-engine 只读端口。
func NewDefectCatalogRepo(pool *pgxpool.Pool, defects service.DefectReader) *DefectCatalogRepo {
	return &DefectCatalogRepo{pool: pool, defects: defects}
}

// SessionDefects 取某次会话挂载的故障：本地查 defect_id，再批量回填
// code/category/difficulty（id 升序）。
func (r *DefectCatalogRepo) SessionDefects(ctx context.Context, sessionID int64) ([]service.PairingDefect, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT defect_id
		 FROM session_defects
		 WHERE session_id=$1 AND deleted_at IS NULL
		 ORDER BY defect_id`, sessionID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || r.defects == nil {
		return []service.PairingDefect{}, nil
	}

	views, err := r.defects.BatchDefects(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]service.PairingDefect, 0, len(views))
	for i := range views {
		out = append(out, service.PairingDefect{
			ID:         views[i].ID,
			Code:       views[i].Code,
			Category:   views[i].Category,
			Difficulty: views[i].Difficulty,
		})
	}
	return out, nil
}

// ListProjectDefects 取某项目下在用、未删除的故障（配对候选），直接走
// fault-engine 项目目录接口；保持按 id 升序。
func (r *DefectCatalogRepo) ListProjectDefects(ctx context.Context, projectID int64) ([]service.PairingDefect, error) {
	if r.defects == nil {
		return []service.PairingDefect{}, nil
	}
	views, err := r.defects.ListProjectDefects(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]service.PairingDefect, 0, len(views))
	for i := range views {
		out = append(out, service.PairingDefect{
			ID:         views[i].ID,
			Code:       views[i].Code,
			Category:   views[i].Category,
			Difficulty: views[i].Difficulty,
		})
	}
	return out, nil
}
