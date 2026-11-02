package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type SessionMemoryRepo struct{ pool *pgxpool.Pool }

func NewSessionMemoryRepo(p *pgxpool.Pool) *SessionMemoryRepo { return &SessionMemoryRepo{p} }

var _ service.SessionMemoryStore = (*SessionMemoryRepo)(nil)

func memoryOwned(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, uid, sid int64) (string, error) {
	var status string
	e := q.QueryRow(ctx, `SELECT status FROM training_sessions WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, sid, uid).Scan(&status)
	if errors.Is(e, pgx.ErrNoRows) {
		e = service.ErrSessionMemoryNotFound
	}
	return status, e
}
func (r *SessionMemoryRepo) Resolve(ctx context.Context, uid, sid int64) (*model.SessionConversation, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var status string
	e = tx.QueryRow(ctx, `SELECT status FROM training_sessions WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL FOR UPDATE`, sid, uid).Scan(&status)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, service.ErrSessionMemoryNotFound
	}
	if e != nil {
		return nil, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT conversation_id FROM session_assistant_identity WHERE user_id=$1 AND session_id=$2`, uid, sid).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `SELECT id FROM ai_conversations WHERE user_id=$1 AND session_id=$2 AND conversation_type='training_assistant' ORDER BY id LIMIT 1`, uid, sid).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			e = tx.QueryRow(ctx, `INSERT INTO ai_conversations(user_id,session_id,conversation_type,ai_model,prompt_version) VALUES($1,$2,'training_assistant','qwen3.8-flash','assistant:v1') RETURNING id`, uid, sid).Scan(&id)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO session_assistant_identity(user_id,session_id,conversation_id) VALUES($1,$2,$3)`, uid, sid, id)
		}
	}
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return &model.SessionConversation{ConversationID: id, SessionID: sid, SessionStatus: status}, nil
}
func memoryLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
func (r *SessionMemoryRepo) history(ctx context.Context, uid, sid, before int64, n int, query string) (model.SessionHistoryPage, error) {
	out := model.SessionHistoryPage{Messages: []model.SessionMessage{}}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return out, e
	}
	n = memoryLimit(n, 50, 100)
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	rows, e := r.pool.Query(ctx, `SELECT m.id,m.role,m.content,m.created_at,m.is_pitfall,coalesce(m.pitfall_type,''),coalesce(m.confidence_level,''),coalesce(m.hint_level,0) FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE c.user_id=$1 AND c.session_id=$2 AND c.conversation_type='training_assistant' AND ($3::bigint=0 OR m.id<$3) AND ($5='' OR m.content ILIKE $6) ORDER BY m.id DESC LIMIT $4`, uid, sid, before, n+1, query, pattern)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var m model.SessionMessage
		if e = rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt, &m.IsPitfall, &m.PitfallType, &m.ConfidenceLevel, &m.HintLevel); e != nil {
			return out, e
		}
		m.Content = service.RedactSessionText(m.Content)
		out.Messages = append(out.Messages, m)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(out.Messages) > n {
		out.HasMore = true
		out.Messages = out.Messages[:n]
	}
	for a, b := 0, len(out.Messages)-1; a < b; a, b = a+1, b-1 {
		out.Messages[a], out.Messages[b] = out.Messages[b], out.Messages[a]
	}
	if len(out.Messages) > 0 {
		out.NextBeforeID = out.Messages[0].ID
	}
	return out, nil
}
func (r *SessionMemoryRepo) History(ctx context.Context, uid, sid, before int64, n int) (model.SessionHistoryPage, error) {
	return r.history(ctx, uid, sid, before, n, "")
}
func (r *SessionMemoryRepo) Search(ctx context.Context, uid, sid int64, query string, before int64, n int) (model.SessionHistoryPage, error) {
	if len([]rune(query)) > 256 {
		return model.SessionHistoryPage{}, errors.New("query too long")
	}
	return r.history(ctx, uid, sid, before, memoryLimit(n, 20, 50), query)
}
func memorySourceText(s string) (string, bool) {
	s = service.RedactSessionText(s)
	if len(s) <= 8192 {
		return s, false
	}
	s = s[:8192]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s, true
}
func messageSource(m model.SessionMessage) model.MemorySource {
	s, tr := memorySourceText(m.Content)
	return model.MemorySource{ID: fmt.Sprintf("message:%d", m.ID), Sequence: m.ID, Kind: "message", Role: m.Role, Text: s, OccurredAt: m.CreatedAt, Truncated: tr}
}
func (r *SessionMemoryRepo) Sources(ctx context.Context, uid, sid, afterMsg, afterEvidence int64, n int) (model.MemorySourcePage, error) {
	out := model.MemorySourcePage{Sources: []model.MemorySource{}, MessageWatermark: afterMsg, EvidenceWatermark: afterEvidence}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return out, e
	}
	n = memoryLimit(n, 100, 100)
	rows, e := r.pool.Query(ctx, `SELECT m.id,m.role,m.content,m.created_at FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE c.user_id=$1 AND c.session_id=$2 AND c.conversation_type='training_assistant' AND m.id>$3 ORDER BY m.id LIMIT $4`, uid, sid, afterMsg, n+1)
	if e != nil {
		return out, e
	}
	count := 0
	for rows.Next() {
		var m model.SessionMessage
		if e = rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); e != nil {
			rows.Close()
			return out, e
		}
		count++
		if count > n {
			out.HasMore = true
			break
		}
		out.Sources = append(out.Sources, messageSource(m))
		out.MessageWatermark = m.ID
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = r.pool.Query(ctx, `SELECT e.id,e.sequence,e.kind,e.actor,e.occurred_at,e.payload::text,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=e.session_id AND c.started_at IS NOT NULL AND c.evidence_refs ? e.id),'') FROM brain_evidence_refs e WHERE e.session_id=$1 AND e.sequence>$2 AND (e.actor IN ('student','student_report','simulator') OR (e.kind='operation_evidence' AND e.actor IN ('assistant','platform'))) ORDER BY e.sequence LIMIT $3`, sid, afterEvidence, n+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	count = 0
	for rows.Next() {
		var id, kind, raw, actor, challenge string
		var seq int64
		var at time.Time
		if e = rows.Scan(&id, &seq, &kind, &actor, &at, &raw, &challenge); e != nil {
			return out, e
		}
		count++
		if count > n {
			out.HasMore = true
			break
		}
		v := publicMemorySource(id, kind, actor, raw, at, challenge)
		v.Sequence = seq
		out.Sources = append(out.Sources, v)
		out.EvidenceWatermark = seq
	}
	return out, rows.Err()
}
func (r *SessionMemoryRepo) Load(ctx context.Context, uid, sid int64) (*model.SessionMemory, error) {
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return nil, e
	}
	m := &model.SessionMemory{SchemaVersion: 1, MemoryJSON: `{"entries":[]}`}
	e := r.pool.QueryRow(ctx, `SELECT version,schema_version,message_watermark,evidence_watermark,memory::text,actual_model,prompt_tokens,completion_tokens FROM session_assistant_memories WHERE user_id=$1 AND session_id=$2 ORDER BY version DESC LIMIT 1`, uid, sid).Scan(&m.Version, &m.SchemaVersion, &m.MessageWatermark, &m.EvidenceWatermark, &m.MemoryJSON, &m.ActualModel, &m.PromptTokens, &m.CompletionTokens)
	if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	return m, e
}
func checkSessionLease(ctx context.Context, tx pgx.Tx, uid, sid int64, l model.ChatLease) error {
	var id int64
	e := tx.QueryRow(ctx, `SELECT i.conversation_id FROM session_assistant_identity i JOIN training_sessions s ON s.id=i.session_id WHERE i.user_id=$1 AND i.session_id=$2 AND i.lease_token=$3 AND i.fence=$4 AND i.lease_expires>now() AND s.user_id=$1 AND s.deleted_at IS NULL AND s.status='running' FOR UPDATE OF i,s`, uid, sid, l.Token, l.Fence).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) || id != l.ConversationID {
		return service.ErrChatLeaseLost
	}
	return e
}
func (r *SessionMemoryRepo) SaveCAS(ctx context.Context, uid, sid, version int64, l model.ChatLease, next model.SessionMemory) (bool, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	if e = checkSessionLease(ctx, tx, uid, sid, l); e != nil {
		return false, e
	}
	var currentMsg, currentEvidence int64
	e = tx.QueryRow(ctx, `SELECT coalesce((SELECT message_watermark FROM session_assistant_memories WHERE user_id=$1 AND session_id=$2 ORDER BY version DESC LIMIT 1),0),coalesce((SELECT evidence_watermark FROM session_assistant_memories WHERE user_id=$1 AND session_id=$2 ORDER BY version DESC LIMIT 1),0)`, uid, sid).Scan(&currentMsg, &currentEvidence)
	if e != nil {
		return false, e
	}
	if next.MessageWatermark < currentMsg || next.EvidenceWatermark < currentEvidence {
		return false, errors.New("watermark regression")
	}
	tag, e := tx.Exec(ctx, `UPDATE session_assistant_identity SET memory_version=memory_version+1 WHERE user_id=$1 AND session_id=$2 AND memory_version=$3`, uid, sid, version)
	if e != nil || tag.RowsAffected() == 0 {
		return false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO session_assistant_memories(user_id,session_id,version,message_watermark,evidence_watermark,memory,actual_model,prompt_tokens,completion_tokens) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)`, uid, sid, version+1, next.MessageWatermark, next.EvidenceWatermark, next.MemoryJSON, next.ActualModel, next.PromptTokens, next.CompletionTokens)
	if e != nil {
		return false, e
	}
	e = tx.Commit(ctx)
	return e == nil, e
}
func (r *SessionMemoryRepo) Acquire(ctx context.Context, uid, sid int64, token string, ttl time.Duration) (model.ChatLease, error) {
	l := model.ChatLease{Token: token}
	if token == "" || ttl <= 0 {
		return l, errors.New("invalid lease")
	}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return l, e
	}
	e := r.pool.QueryRow(ctx, `UPDATE session_assistant_identity i SET lease_token=$3,fence=fence+1,lease_expires=now()+$4*interval '1 second' FROM training_sessions s WHERE i.user_id=$1 AND i.session_id=$2 AND s.id=i.session_id AND s.user_id=$1 AND s.status='running' AND s.deleted_at IS NULL AND (i.lease_expires IS NULL OR i.lease_expires<=now()) RETURNING i.fence,i.conversation_id`, uid, sid, token, ttl.Seconds()).Scan(&l.Fence, &l.ConversationID)
	if errors.Is(e, pgx.ErrNoRows) {
		e = service.ErrChatBusy
	}
	return l, e
}
func (r *SessionMemoryRepo) Renew(ctx context.Context, uid, sid int64, l model.ChatLease, ttl time.Duration) (bool, error) {
	tag, e := r.pool.Exec(ctx, `UPDATE session_assistant_identity i SET lease_expires=now()+$5*interval '1 second' FROM training_sessions s WHERE i.user_id=$1 AND i.session_id=$2 AND i.lease_token=$3 AND i.fence=$4 AND i.lease_expires>now() AND s.id=i.session_id AND s.user_id=$1 AND s.status='running' AND s.deleted_at IS NULL`, uid, sid, l.Token, l.Fence, ttl.Seconds())
	return tag.RowsAffected() == 1, e
}
func (r *SessionMemoryRepo) Release(ctx context.Context, uid, sid int64, l model.ChatLease) error {
	_, e := r.pool.Exec(ctx, `UPDATE session_assistant_identity SET lease_token='',lease_expires=NULL WHERE user_id=$1 AND session_id=$2 AND lease_token=$3 AND fence=$4`, uid, sid, l.Token, l.Fence)
	return e
}
func (r *SessionMemoryRepo) AppendLeasedMessage(ctx context.Context, uid, sid int64, l model.ChatLease, m model.SessionMessage) (int64, error) {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	if e = checkSessionLease(ctx, tx, uid, sid, l); e != nil {
		return 0, e
	}
	if m.Role != "user" && m.Role != "assistant" {
		return 0, errors.New("invalid role")
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO ai_messages(conversation_id,role,content,is_hint,hint_level,tokens,cost,latency_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, l.ConversationID, m.Role, service.RedactSessionText(m.Content), m.HintLevel > 0, m.HintLevel, m.Tokens, m.Cost, m.LatencyMS).Scan(&id)
	if e != nil {
		return 0, e
	}
	_, e = tx.Exec(ctx, `UPDATE ai_conversations SET message_count=message_count+1,total_tokens=total_tokens+$2,total_cost=total_cost+$3,updated_at=now() WHERE id=$1`, l.ConversationID, m.Tokens, m.Cost)
	if e != nil {
		return 0, e
	}
	return id, tx.Commit(ctx)
}
func (r *SessionMemoryRepo) ReadReferences(ctx context.Context, uid, sid int64, messageIDs []int64, evidenceIDs []string) (model.MemorySourcePage, error) {
	out := model.MemorySourcePage{Sources: []model.MemorySource{}}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return out, e
	}
	if len(messageIDs)+len(evidenceIDs) > 50 {
		return out, errors.New("too many references")
	}
	rows, e := r.pool.Query(ctx, `SELECT m.id,m.role,m.content,m.created_at FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE c.user_id=$1 AND c.session_id=$2 AND c.conversation_type='training_assistant' AND m.id=ANY($3::bigint[]) ORDER BY m.id`, uid, sid, messageIDs)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var m model.SessionMessage
		if e = rows.Scan(&m.ID, &m.Role, &m.Content, &m.CreatedAt); e != nil {
			rows.Close()
			return out, e
		}
		out.Sources = append(out.Sources, messageSource(m))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(evidenceIDs) > 0 {
		rows, e = r.pool.Query(ctx, `SELECT e.id,e.kind,e.actor,e.occurred_at,e.payload::text,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=e.session_id AND c.started_at IS NOT NULL AND c.evidence_refs ? e.id),'') FROM brain_evidence_refs e WHERE e.session_id=$1 AND (e.actor IN ('student','student_report','simulator') OR (e.kind='operation_evidence' AND e.actor IN ('assistant','platform'))) AND e.id=ANY($2::text[]) ORDER BY e.sequence`, sid, evidenceIDs)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var id, kind, raw, actor, challenge string
			var at time.Time
			if e = rows.Scan(&id, &kind, &actor, &at, &raw, &challenge); e != nil {
				rows.Close()
				return out, e
			}
			out.Sources = append(out.Sources, publicMemorySource(id, kind, actor, raw, at, challenge))
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	if len(out.Sources) != len(messageIDs)+len(evidenceIDs) {
		return out, service.ErrSessionMemoryNotFound
	}
	return out, nil
}

func publicMemoryRawSource(id, kind, actor, raw string, at time.Time, challenge string) model.MemorySource {
	var input map[string]any
	_ = json.Unmarshal([]byte(raw), &input)
	visible := map[string]any{}
	for _, key := range []string{"target", "data", "direction", "path", "before", "after", "changed", "outcome", "operation_type", "operation_category", "result", "status", "business_verified", "execution_status", "truncated", "reason", "redacted", "operation_id", "kind", "tool", "command", "intent", "coverage", "text_refs", "performed_by", "exit_code"} {
		if v, ok := input[key]; ok {
			visible[key] = v
		}
	}
	if detail, ok := input["detail"].(map[string]any); ok {
		safe := map[string]any{}
		for _, key := range []string{"command", "question", "source", "path", "before", "after", "changed", "exit_code", "stdout", "stderr", "approved", "result"} {
			if v, exists := detail[key]; exists {
				safe[key] = v
			}
		}
		visible["detail"] = safe
	}
	encoded, _ := json.Marshal(visible)
	text := service.RedactSessionText(string(encoded))
	successful := false
	if actor == "simulator" {
		successful = input["business_verified"] == true
	}
	if actor == "student" && kind == "student_operation" {
		successful = input["result"] == "success"
	}
	role := ""
	if actor == "student_report" {
		role = "user"
		successful = false
	}
	return model.MemorySource{ID: id, Kind: kind, Actor: actor, Role: role, Text: text, OccurredAt: at, ChallengeID: challenge, Successful: successful}
}

func publicMemorySource(id, kind, actor, raw string, at time.Time, challenge string) model.MemorySource {
	v := publicMemoryRawSource(id, kind, actor, raw, at, challenge)
	v.Text, v.Truncated = memorySourceText(v.Text)
	if v.Truncated {
		v.NextOffsetBytes = len(v.Text)
	}
	return v
}
func (r *SessionMemoryRepo) ReadSourceRange(ctx context.Context, uid, sid int64, ref string, offset, limit int) (model.MemorySource, error) {
	v := model.MemorySource{ID: ref}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return v, e
	}
	if offset < 0 || limit < 1 || limit > 8192 {
		return v, errors.New("invalid source range")
	}
	if strings.HasPrefix(ref, "optext:") || (strings.HasPrefix(ref, "operation:") && strings.Count(ref, ":") >= 2) {
		return r.readOperationSource(ctx, sid, ref, offset, limit)
	}
	if strings.HasPrefix(ref, "message:") {
		id, e := strconv.ParseInt(strings.TrimPrefix(ref, "message:"), 10, 64)
		if e != nil || id <= 0 {
			return v, service.ErrSessionMemoryNotFound
		}
		v.Kind = "message"
		e = r.pool.QueryRow(ctx, `SELECT m.role,m.content,m.created_at FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE m.id=$3 AND c.user_id=$1 AND c.session_id=$2 AND c.conversation_type='training_assistant'`, uid, sid, id).Scan(&v.Role, &v.Text, &v.OccurredAt)
		if errors.Is(e, pgx.ErrNoRows) {
			e = service.ErrSessionMemoryNotFound
		}
		if e != nil {
			return v, e
		}
	} else {
		var kind, actor, raw, challenge string
		var at time.Time
		e := r.pool.QueryRow(ctx, `SELECT e.kind,e.actor,e.payload::text,e.occurred_at,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=e.session_id AND c.started_at IS NOT NULL AND c.evidence_refs ? e.id),'') FROM brain_evidence_refs e WHERE e.session_id=$1 AND e.id=$2 AND (e.actor IN ('student','student_report','simulator') OR (e.kind='operation_evidence' AND e.actor IN ('assistant','platform')))`, sid, ref).Scan(&kind, &actor, &raw, &at, &challenge)
		if errors.Is(e, pgx.ErrNoRows) {
			e = service.ErrSessionMemoryNotFound
		}
		if e != nil {
			return v, e
		}
		v = publicMemoryRawSource(ref, kind, actor, raw, at, challenge)
	}
	v.Text = service.RedactSessionText(v.Text)
	if offset > len(v.Text) {
		return v, errors.New("offset exceeds source")
	}
	for offset < len(v.Text) && !utf8.RuneStart(v.Text[offset]) {
		offset++
	}
	end := offset + limit
	if end > len(v.Text) {
		end = len(v.Text)
	}
	for end > offset && end < len(v.Text) && !utf8.RuneStart(v.Text[end]) {
		end--
	}
	v.Truncated = end < len(v.Text)
	if v.Truncated {
		v.NextOffsetBytes = end
	}
	v.Text = v.Text[offset:end]
	return v, nil
}

// Evidence pages never depend on summary references; all public originals remain discoverable.
func (r *SessionMemoryRepo) EvidenceHistory(ctx context.Context, uid, sid, before int64, n int) (model.MemorySourcePage, error) {
	out := model.MemorySourcePage{Sources: []model.MemorySource{}}
	if _, e := memoryOwned(ctx, r.pool, uid, sid); e != nil {
		return out, e
	}
	if before < 0 {
		return out, errors.New("invalid evidence cursor")
	}
	n = memoryLimit(n, 20, 50)
	rows, e := r.pool.Query(ctx, `SELECT e.id,e.sequence,e.kind,e.actor,e.occurred_at,e.payload::text,coalesce((SELECT CASE WHEN count(*)=1 THEN 'challenge:'||min(c.challenge_no)::text ELSE '' END FROM brain_challenges c WHERE c.session_id=e.session_id AND c.started_at IS NOT NULL AND c.evidence_refs ? e.id),'') FROM brain_evidence_refs e WHERE e.session_id=$1 AND (e.actor IN ('student','student_report','simulator') OR (e.kind='operation_evidence' AND e.actor IN ('assistant','platform'))) AND ($2::bigint=0 OR e.sequence<$2) ORDER BY e.sequence DESC LIMIT $3`, sid, before, n+1)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, actor, raw, challenge string
		var seq int64
		var at time.Time
		if e = rows.Scan(&id, &seq, &kind, &actor, &at, &raw, &challenge); e != nil {
			return out, e
		}
		if len(out.Sources) == n {
			out.HasMore = true
			break
		}
		v := publicMemorySource(id, kind, actor, raw, at, challenge)
		v.Sequence = seq
		out.Sources = append(out.Sources, v)
		out.NextBeforeSequence = seq
	}
	for a, b := 0, len(out.Sources)-1; a < b; a, b = a+1, b-1 {
		out.Sources[a], out.Sources[b] = out.Sources[b], out.Sources[a]
	}
	return out, rows.Err()
}
