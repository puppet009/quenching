// service.ChangeRecorder 的 PostgreSQL 实现（M5）。
//
// 助手经审批的 write_file 成功后落一条 ai_change_records，含写入前后快照。
// 只负责写入；查询复盘数据在 pg_internal_review_input_repo 一侧聚合。
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/service"
)

// PGChangeRecordRepo 修改登记 PG 仓储。
type PGChangeRecordRepo struct {
	pool *pgxpool.Pool
}

// NewPGChangeRecordRepo 创建修改登记仓储。
func NewPGChangeRecordRepo(pool *pgxpool.Pool) *PGChangeRecordRepo {
	return &PGChangeRecordRepo{pool: pool}
}

// 编译期断言：满足 service.ChangeRecorder。
var _ service.ChangeRecorder = (*PGChangeRecordRepo)(nil)

// Record 写入一条修改登记。conversation_id 为 0 时存 NULL。
func (r *PGChangeRecordRepo) Record(ctx context.Context, rec service.ChangeRecord) error {
	var conversationID *int64
	if rec.ConversationID > 0 {
		conversationID = &rec.ConversationID
	}
	var approvalID *string
	if rec.ApprovalID != "" {
		approvalID = &rec.ApprovalID
	}
	var beforeContent *string
	if rec.BeforeExisted {
		beforeContent = &rec.BeforeContent
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_change_records
		    (session_id, conversation_id, approval_id, pod, container, path,
		     before_existed, before_content, after_content)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		rec.SessionID, conversationID, approvalID, rec.Pod, rec.Container, rec.Path,
		rec.BeforeExisted, beforeContent, rec.AfterContent,
	)
	if err != nil {
		return fmt.Errorf("insert ai_change_records: %w", err)
	}
	return nil
}
