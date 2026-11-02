// 主动线索推送的数据端口实现（T3-7）。
//
// ClueRepo 实现 service.ClueStore：
//   - SessionOwner：从 training_sessions 取 running 且未删除会话的属主与开训时间；
//   - LastActivityAnchor：取最近一次有效活动（无活动回落开训时间）作为卡顿周期锚点；
//   - EnsureConversation：复用会话最近一条训练助教对话，没有则自动创建；
//   - CountCluesFromAnchor：按 ai_messages.structured_data 中的主动线索标记做频控计数，
//     以落库事实为准，进程重启/多副本都不丢不重，无需新增迁移或列；
//   - AppendClue：同事务插入带标记的助手消息并累加对话消息数。
//
// 线索消息 is_hint=false（不占用 T3-5 提示配额）、is_pitfall=false，
// tokens/cost/latency 均不写（确定性模板，未经 LLM）。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// 编译期保证实现 service.ClueStore。
var _ service.ClueStore = (*ClueRepo)(nil)

// ClueRepo 主动线索的 PG 数据端口。
type ClueRepo struct {
	pool *pgxpool.Pool
}

// NewClueRepo 创建主动线索数据端口。
func NewClueRepo(pool *pgxpool.Pool) *ClueRepo {
	return &ClueRepo{pool: pool}
}

// SessionOwner 取 running 且未删除会话的属主与开训时间；
// 会话不存在/已结束时 ok=false（不打扰），started_at 为 NULL 时 startedAt 返回零值。
func (r *ClueRepo) SessionOwner(ctx context.Context, sessionID int64) (int64, time.Time, bool, error) {
	var userID int64
	var startedAt *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, started_at FROM training_sessions
		 WHERE id=$1 AND status='running' AND deleted_at IS NULL`,
		sessionID,
	).Scan(&userID, &startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	if startedAt != nil {
		return userID, *startedAt, true, nil
	}
	return userID, time.Time{}, true, nil
}

// anchorActivitySQL 取会话历史上最近一次有效活动时刻（与卡顿扫描同一组活动来源）：
// training_operation_projection 未删除行，或 ai_messages 中 role='user' 的提问。
// 不限时间窗口取全量 max：得到的是"学员最后一次真实动作"，连续卡顿期间稳定不变，
// 一旦恢复并产生新活动才前移，正好作为一次卡顿周期的稳定边界（跨重启、跨副本一致）。
const anchorActivitySQL = `
SELECT max(ts) FROM (
	SELECT max(created_at) AS ts
	FROM training_operation_projection
	WHERE session_id=$1 AND deleted_at IS NULL
	UNION ALL
	SELECT max(m.created_at) AS ts
	FROM ai_messages m
	JOIN ai_conversations c ON c.id = m.conversation_id
	WHERE c.session_id=$1 AND m.role='user'
) a`

// LastActivityAnchor 返回卡顿周期锚点：最近一次有效活动；从无活动时回落 startedAt；
// 两者都缺失（未正式开训）返回零值，由调用方保守跳过。now 仅用于未来时间兜底校验。
func (r *ClueRepo) LastActivityAnchor(
	ctx context.Context, sessionID int64, startedAt, now time.Time,
) (time.Time, error) {
	var last *time.Time
	if err := r.pool.QueryRow(ctx, anchorActivitySQL, sessionID).Scan(&last); err != nil {
		return time.Time{}, err
	}
	if last != nil && !last.After(now) {
		return *last, nil
	}
	// 无活动或活动时间异常（未来时间）：以开训时间回落。
	return startedAt, nil
}

// EnsureConversation 取会话最近一条训练助教对话；没有则自动创建一条 active 对话。
// 后台扫描为单消费者、扫描间隔 30s，先查后插在 MVP 单副本下足够；
// 万一并发插入出多条，也只是多一条空对话，不影响正确性。
func (r *ClueRepo) EnsureConversation(ctx context.Context, sessionID, userID int64) (int64, error) {
	var conversationID int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM ai_conversations
		 WHERE session_id=$1 AND conversation_type=$2
		 ORDER BY id DESC LIMIT 1`,
		sessionID, model.ConvTrainingAssistant,
	).Scan(&conversationID)
	if err == nil {
		return conversationID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	// 无对话：自动创建。ai_model/system_prompt/prompt_version 均留空（线索不经 LLM）。
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO ai_conversations
		   (session_id, user_id, conversation_type, ai_model, system_prompt, prompt_version, status)
		 VALUES ($1,$2,$3,'',NULL,NULL,$4) RETURNING id`,
		sessionID, userID, model.ConvTrainingAssistant, model.ConvStatusActive,
	).Scan(&conversationID); err != nil {
		return 0, err
	}
	return conversationID, nil
}

// CountCluesFromAnchor 统计会话在锚点（含）之后、指定原因的主动线索已落库条数。
// 计数边界用 m.created_at（与锚点同由 PG 时钟产生，天然可比、不受序列化格式影响）；
// 主动线索标记写在 structured_data（000002 已建的 JSONB，无需新迁移）。
func (r *ClueRepo) CountCluesFromAnchor(
	ctx context.Context, sessionID int64, reason string, anchor time.Time,
) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*)
		 FROM ai_messages m
		 JOIN ai_conversations c ON c.id = m.conversation_id
		 WHERE c.session_id=$1
		   AND (m.structured_data->>'proactive_clue')='true'
		   AND (m.structured_data->>'reason')=$2
		   AND m.created_at >= $3`,
		sessionID, reason, anchor,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// clueMark 写入 structured_data 的主动线索标记。
type clueMark struct {
	ProactiveClue bool   `json:"proactive_clue"`
	Reason        string `json:"reason"`
	StallAnchor   string `json:"stall_anchor"` // RFC3339，本次卡顿周期锚点，云端验证可据此区分周期
}

// AppendClue 插入主动线索助手消息并在同事务内累加对话消息数，返回消息 ID。
func (r *ClueRepo) AppendClue(
	ctx context.Context, conversationID int64, content, reason string, anchor time.Time,
) (int64, error) {
	mark, err := json.Marshal(clueMark{
		ProactiveClue: true,
		Reason:        reason,
		StallAnchor:   anchor.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return 0, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ai_messages (conversation_id, role, content, content_type, structured_data)
		 VALUES ($1,'assistant',$2,'markdown',$3) RETURNING id`,
		conversationID, content, string(mark),
	).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_conversations SET message_count=message_count+1, updated_at=now() WHERE id=$1`,
		conversationID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
