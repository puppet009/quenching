// Package repository training-service 的 PostgreSQL 仓储实现。
// 编排层依赖 service.ConversationStore 接口；本地无 PG 时用 fake 单测，云上注入本实现。
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// ConversationRepo 对话与消息仓储。
type ConversationRepo struct {
	pool *pgxpool.Pool
}

func NewConversationRepo(pool *pgxpool.Pool) *ConversationRepo {
	return &ConversationRepo{pool: pool}
}

// 编译期保证 ConversationRepo 实现编排层要求的 service.ConversationStore。
// service 不反向依赖 repository，因此这里不存在 import 环。
var _ service.ConversationStore = (*ConversationRepo)(nil)

func (r *ConversationRepo) CreateConversation(ctx context.Context, conv *model.Conversation) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`INSERT INTO ai_conversations
		   (session_id, user_id, conversation_type, ai_model, system_prompt, prompt_version, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		conv.SessionID, conv.UserID, conv.ConversationType,
		conv.AIModel, nullableString(conv.SystemPrompt), nullableString(conv.PromptVersion),
		conv.Status,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	conv.ID = id
	return id, nil
}

func (r *ConversationRepo) GetConversation(ctx context.Context, id, userID int64) (*model.Conversation, error) {
	conv := &model.Conversation{}
	var systemPrompt, promptVersion *string
	var sessionID *int64
	err := r.pool.QueryRow(ctx,
		`SELECT id, session_id, user_id, conversation_type, ai_model,
		        system_prompt, prompt_version, status, message_count, total_tokens, total_cost
		 FROM ai_conversations WHERE id=$1 AND user_id=$2`,
		id, userID,
	).Scan(
		&conv.ID, &sessionID, &conv.UserID, &conv.ConversationType, &conv.AIModel,
		&systemPrompt, &promptVersion, &conv.Status, &conv.MessageCount,
		&conv.TotalTokens, &conv.TotalCost,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	conv.SessionID = sessionID
	if systemPrompt != nil {
		conv.SystemPrompt = *systemPrompt
	}
	if promptVersion != nil {
		conv.PromptVersion = *promptVersion
	}
	return conv, nil
}

func (r *ConversationRepo) ListHistory(ctx context.Context, conversationID int64, limit int) ([]model.HistoryTurn, error) {
	if limit <= 0 {
		limit = 20
	}
	// 先取最近 limit 条，再在内存反转为时间正序，保证发给 AI 的上下文顺序正确。
	rows, err := r.pool.Query(ctx,
		`SELECT role, content FROM (
			SELECT role, content, created_at FROM ai_messages
			WHERE conversation_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2
		) t ORDER BY t.created_at ASC`,
		conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turns := make([]model.HistoryTurn, 0, limit)
	for rows.Next() {
		var t model.HistoryTurn
		if err := rows.Scan(&t.Role, &t.Content); err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

func (r *ConversationRepo) AppendUserMessage(ctx context.Context, conversationID int64, content string) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if scope, ok := service.CurrentChatLease(ctx); ok {
		if conversationID != scope.Lease.ConversationID {
			return 0, service.ErrChatLeaseLost
		}
		if err := checkSessionLease(ctx, tx, scope.UserID, scope.SessionID, scope.Lease); err != nil {
			return 0, err
		}
	}
	content = service.RedactSessionText(content)

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ai_messages (conversation_id, role, content, content_type)
		 VALUES ($1,'user',$2,'text') RETURNING id`,
		conversationID, content).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_conversations SET message_count=message_count+1, updated_at=now() WHERE id=$1`,
		conversationID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func (r *ConversationRepo) AppendAssistantMessage(
	ctx context.Context, conversationID int64, content string,
	tokens, latencyMS int, cost float64, isPitfall bool, pitfallType string,
) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if scope, ok := service.CurrentChatLease(ctx); ok {
		if conversationID != scope.Lease.ConversationID {
			return 0, service.ErrChatLeaseLost
		}
		if err := checkSessionLease(ctx, tx, scope.UserID, scope.SessionID, scope.Lease); err != nil {
			return 0, err
		}
	}
	content = service.RedactSessionText(content)

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ai_messages
		   (conversation_id, role, content, content_type, is_pitfall, pitfall_type, tokens, cost, latency_ms)
		 VALUES ($1,'assistant',$2,'markdown',$3,$4,$5,$6,$7) RETURNING id`,
		conversationID, content, isPitfall, nullableString(pitfallType),
		nullableInt(tokens), nullableCost(cost), nullableInt(latencyMS),
	).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_conversations
		 SET message_count=message_count+1,
		     total_tokens=total_tokens+$2,
		     total_cost=total_cost+$3,
		     updated_at=now()
		 WHERE id=$1`,
		conversationID, tokens, cost); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// AppendHintMessage 落一条提示型助手消息（T3-5）：写 is_hint=true/hint_level，
// 并在同事务内 message_count+1、累计 token/费用。配额按会话从这些消息实时统计，
// 服务重启也不丢次数。hintLevel 取值 1/2/3。
func (r *ConversationRepo) AppendHintMessage(
	ctx context.Context, conversationID int64, content string, hintLevel int,
	tokens, latencyMS int, cost float64,
) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if scope, ok := service.CurrentChatLease(ctx); ok {
		if conversationID != scope.Lease.ConversationID {
			return 0, service.ErrChatLeaseLost
		}
		if err := checkSessionLease(ctx, tx, scope.UserID, scope.SessionID, scope.Lease); err != nil {
			return 0, err
		}
	}
	content = service.RedactSessionText(content)

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ai_messages
		   (conversation_id, role, content, content_type, is_hint, hint_level, tokens, cost, latency_ms)
		 VALUES ($1,'assistant',$2,'markdown',true,$3,$4,$5,$6) RETURNING id`,
		conversationID, content, hintLevel,
		nullableInt(tokens), nullableCost(cost), nullableInt(latencyMS),
	).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_conversations
		 SET message_count=message_count+1,
		     total_tokens=total_tokens+$2,
		     total_cost=total_cost+$3,
		     updated_at=now()
		 WHERE id=$1`,
		conversationID, tokens, cost); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// AppendPitfallMessage 落一条坑点助手消息（T3-9）。确定性模板直出，不经 LLM，
// 因此 tokens/cost/latency 一律不写（NULL）；写全 is_pitfall/pitfall_config_id/
// pitfall_type/confidence_level，并在同事务内 message_count+1、
// training_sessions.pitfall_total+1（供实验统计与 T3-11 复用）。
func (r *ConversationRepo) AppendPitfallMessage(
	ctx context.Context, conversationID int64, content string,
	configID int64, pitfallType, confidenceLevel string,
) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if scope, ok := service.CurrentChatLease(ctx); ok {
		if conversationID != scope.Lease.ConversationID {
			return 0, service.ErrChatLeaseLost
		}
		if err := checkSessionLease(ctx, tx, scope.UserID, scope.SessionID, scope.Lease); err != nil {
			return 0, err
		}
	}
	content = service.RedactSessionText(content)

	var id int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO ai_messages
		   (conversation_id, role, content, content_type,
		    is_pitfall, pitfall_config_id, pitfall_type, confidence_level)
		 VALUES ($1,'assistant',$2,'markdown',true,$3,$4,$5) RETURNING id`,
		conversationID, content, configID,
		nullableString(pitfallType), nullableString(confidenceLevel),
	).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_conversations SET message_count=message_count+1, updated_at=now() WHERE id=$1`,
		conversationID); err != nil {
		return 0, err
	}
	// 经 ai_conversations 反查会话：非训练对话 session_id 为 NULL，该语句不命中任何行。
	if _, err := tx.Exec(ctx,
		`UPDATE training_sessions ts
		 SET pitfall_total=pitfall_total+1, updated_at=now()
		 WHERE id=(SELECT c.session_id FROM ai_conversations c
		           WHERE c.id=$1 AND c.session_id IS NOT NULL)`,
		conversationID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// ListPendingPitfalls 取本对话尚未判定用户反应（user_response_type 为空）
// 的坑点助手消息，按注入先后（id 升序）返回（T3-11）。
func (r *ConversationRepo) ListPendingPitfalls(
	ctx context.Context, conversationID int64,
) ([]service.PendingPitfall, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, pitfall_config_id, created_at
		 FROM ai_messages
		 WHERE conversation_id=$1 AND is_pitfall=true AND user_response_type IS NULL
		 ORDER BY id`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]service.PendingPitfall, 0)
	for rows.Next() {
		var p service.PendingPitfall
		if err := rows.Scan(&p.MessageID, &p.ConfigID, &p.InjectedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkPitfallResponse 回写坑点助手消息的用户反应类型，并在同事务内幂等维护
// training_sessions 的识别/踩坑计数（T3-11）。
//
// 幂等：条件 UPDATE 只在 user_response_type IS NULL 时命中（行锁天然串行化并发判定），
// 影响行数为 0 说明已被并发抢先处理——不再计数，返回 (false,nil)。
// 计数列由本方法按 responseType 选择，service 侧无需关心列名。
func (r *ConversationRepo) MarkPitfallResponse(
	ctx context.Context, conversationID, pitfallMessageID int64, responseType string,
) (bool, error) {
	// 消息回写与计数 +1 必须同事务：要么两者都生效，要么都不生效，
	// 避免"消息已判、计数没加"或并发重复计数。
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// 条件 UPDATE 只在 user_response_type IS NULL 时命中（行锁串行化并发判定），
	// 影响行数为 0 说明已被并发抢先处理——不计数，返回 false。
	ct, err := tx.Exec(ctx,
		`UPDATE ai_messages SET user_response_type=$3
		 WHERE id=$1 AND conversation_id=$2 AND is_pitfall=true AND user_response_type IS NULL`,
		pitfallMessageID, conversationID, responseType)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}

	// 经 ai_conversations 反查会话；非训练对话 session_id 为 NULL 时不命中任何行。
	// dispute/verified/ignore 计识别，agree 计踩坑；其余取值仅回写消息、不计数。
	// counterCol 取自固定常量白名单，不接受外部输入，无注入面。
	var counterCol string
	switch responseType {
	case model.ResponseAgree:
		counterCol = "pitfall_fallen"
	case model.ResponseDispute, model.ResponseVerified, model.ResponseIgnore:
		counterCol = "pitfall_recognized"
	default:
		return true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE training_sessions SET `+counterCol+`=`+counterCol+`+1, updated_at=now()
		 WHERE id=(SELECT c.session_id FROM ai_conversations c
		           WHERE c.id=$1 AND c.session_id IS NOT NULL)`,
		conversationID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

func nullableCost(c float64) any {
	if c <= 0 {
		return nil
	}
	return c
}
