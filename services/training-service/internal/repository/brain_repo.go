package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"time"
)

type BrainRepo struct{ pool *pgxpool.Pool }

func NewBrainRepo(pool *pgxpool.Pool) *BrainRepo { return &BrainRepo{pool: pool} }

var _ service.BrainStore = (*BrainRepo)(nil)

func (r *BrainRepo) Ensure(ctx context.Context, sid int64) error {
	tag, err := r.pool.Exec(ctx, `INSERT INTO brain_sessions(session_id,phase) SELECT id,CASE WHEN status IN ('completed','aborted','failed') THEN 'closing' ELSE 'sandbox_ready' END FROM training_sessions WHERE id=$1 AND deleted_at IS NULL AND template_slug IS NOT NULL AND template_slug<>'' ON CONFLICT DO NOTHING`, sid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, err = r.State(ctx, sid)
	}
	return err
}
func (r *BrainRepo) State(ctx context.Context, sid int64) (*service.BrainSessionState, error) {
	s := &service.BrainSessionState{}
	err := r.pool.QueryRow(ctx, `SELECT session_id,phase,observation_version,baseline_verified,current_challenge,hint_level,hint_budget,reason,cleanup_status,EXISTS(SELECT 1 FROM brain_challenges c WHERE c.session_id=brain_sessions.session_id AND c.status='active'),EXISTS(SELECT 1 FROM brain_challenges c WHERE c.session_id=brain_sessions.session_id AND c.status IN ('verified_repair','assisted_repair') AND NOT c.faults_revoked) FROM brain_sessions WHERE session_id=$1`, sid).Scan(&s.SessionID, &s.Phase, &s.ObservationVersion, &s.BaselineVerified, &s.CurrentChallenge, &s.HintLevel, &s.HintBudget, &s.Reason, &s.CleanupStatus, &s.ActiveChallenge, &s.PendingFaultRevoke)
	return s, err
}
func (r *BrainRepo) Acquire(ctx context.Context, sid int64, owner string, ttl time.Duration) (*model.BrainLease, error) {
	if sid <= 0 || owner == "" || ttl <= 0 {
		return nil, service.ErrBrainConflict
	}
	lease := &model.BrainLease{SessionID: sid, Owner: owner}
	err := r.pool.QueryRow(ctx, `UPDATE brain_sessions b SET lease_owner=$2,lease_version=b.lease_version+1,lease_until=clock_timestamp()+make_interval(secs=>$3) FROM training_sessions s WHERE b.session_id=$1 AND s.id=b.session_id AND s.deleted_at IS NULL AND s.status NOT IN ('completed','aborted','failed') AND b.phase NOT IN ('closing','reviewing','completed','failed') AND (b.lease_until IS NULL OR b.lease_until<=clock_timestamp()) RETURNING b.lease_version,b.lease_until`, sid, owner, ttl.Seconds()).Scan(&lease.Version, &lease.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, service.ErrBrainConflict
	}
	return lease, err
}
func (r *BrainRepo) Renew(ctx context.Context, l model.BrainLease, ttl time.Duration) error {
	tag, err := r.pool.Exec(ctx, `UPDATE brain_sessions b SET lease_until=clock_timestamp()+make_interval(secs=>$4) FROM training_sessions s WHERE b.session_id=$1 AND s.id=b.session_id AND b.lease_owner=$2 AND b.lease_version=$3 AND b.lease_until>clock_timestamp() AND b.phase NOT IN ('closing','reviewing','completed','failed') AND s.status NOT IN ('completed','aborted','failed') AND s.deleted_at IS NULL`, l.SessionID, l.Owner, l.Version, ttl.Seconds())
	if err == nil && tag.RowsAffected() != 1 {
		return service.ErrBrainConflict
	}
	return err
}
func (r *BrainRepo) Release(ctx context.Context, l model.BrainLease) error {
	_, err := r.pool.Exec(ctx, `UPDATE brain_sessions SET lease_owner='',lease_until=NULL WHERE session_id=$1 AND lease_owner=$2 AND lease_version=$3`, l.SessionID, l.Owner, l.Version)
	return err
}
func (r *BrainRepo) AppendEvent(ctx context.Context, event model.BrainEvent) (bool, error) {
	if err := service.ValidateBrainEvent(event); err != nil {
		return false, err
	}
	refs, err := json.Marshal(event.EvidenceRefs)
	if err != nil {
		return false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	// Lock the state before the session, consistently with CommitDecision.
	var phase string
	if err = tx.QueryRow(ctx, `SELECT phase FROM brain_sessions WHERE session_id=$1 FOR UPDATE`, event.SessionID).Scan(&phase); err != nil {
		return false, err
	}
	if phase == "completed" || phase == "closing" || phase == "reviewing" || phase == "failed" {
		return false, service.ErrBrainConflict
	}
	tag, err := tx.Exec(ctx, `INSERT INTO brain_events(session_id,source,event_id,kind,evidence_refs) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, event.SessionID, event.Source, event.EventID, event.Kind, refs)
	if err != nil {
		return false, err
	}
	fresh := tag.RowsAffected() == 1
	if fresh {
		_, err = tx.Exec(ctx, `UPDATE brain_sessions SET observation_version=observation_version+1,updated_at=now() WHERE session_id=$1`, event.SessionID)
		if err != nil {
			return false, err
		}
	}
	return fresh, tx.Commit(ctx)
}
func (r *BrainRepo) CommitDecision(ctx context.Context, l model.BrainLease, expected int64, d model.BrainDecision, intents []model.BrainCommand) error {
	if err := service.ValidateBrainCommit(l, expected, d, intents); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sid int64
	err = tx.QueryRow(ctx, `SELECT b.session_id FROM brain_sessions b JOIN training_sessions s ON s.id=b.session_id WHERE b.session_id=$1 AND b.lease_owner=$2 AND b.lease_version=$3 AND b.lease_until>clock_timestamp() AND b.observation_version=$4 AND b.phase NOT IN ('closing','reviewing','completed','failed') AND s.status NOT IN ('completed','aborted','failed') AND s.deleted_at IS NULL FOR UPDATE OF b,s`, l.SessionID, l.Owner, l.Version, expected).Scan(&sid)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.ErrBrainConflict
	}
	if err != nil {
		return err
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO brain_decisions(decision_id,session_id,observation_version,decision,actual_model,request_id,prompt_version,prompt_tokens,completion_tokens,latency_ms) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, d.DecisionID, sid, expected, raw, d.ActualModel, d.RequestID, d.PromptVersion, d.PromptTokens, d.CompletionTokens, d.LatencyMS)
	if err != nil {
		return err
	}
	for _, cmd := range intents {
		_, err = tx.Exec(ctx, `INSERT INTO brain_tool_executions(command_id,session_id,decision_id,observation_version,domain,action,parameters) VALUES($1,$2,$3,$4,$5,$6,$7)`, cmd.CommandID, sid, d.DecisionID, expected, cmd.Domain, cmd.Action, cmd.ParametersJSON)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE brain_events SET processed=true WHERE session_id=$1 AND NOT processed`, sid)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE brain_sessions SET observation_version=observation_version+1,updated_at=now() WHERE session_id=$1`, sid)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *BrainRepo) PendingCommands(ctx context.Context, sid int64) ([]model.BrainCommand, error) {
	rows, err := r.pool.Query(ctx, `SELECT command_id,session_id,decision_id,observation_version,domain,action,parameters::text,status FROM brain_tool_executions WHERE session_id=$1 AND status IN ('pending','accepted','running','unknown') ORDER BY updated_at,command_id`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.BrainCommand{}
	for rows.Next() {
		var cmd model.BrainCommand
		if err = rows.Scan(&cmd.CommandID, &cmd.SessionID, &cmd.DecisionID, &cmd.ObservationVersion, &cmd.Domain, &cmd.Action, &cmd.ParametersJSON, &cmd.Status); err != nil {
			return nil, err
		}
		out = append(out, cmd)
	}
	return out, rows.Err()
}
func (r *BrainRepo) CompleteCommand(ctx context.Context, id string, result model.BrainToolResult) error {
	if id == "" || id != result.CommandID {
		return errors.New("command result identity mismatch")
	}
	switch result.Status {
	case "accepted", "running", "unknown", "succeeded", "failed":
	default:
		return errors.New("invalid result status")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE brain_tool_executions SET status=$2,result=$3,updated_at=now() WHERE command_id=$1 AND status IN ('pending','accepted','running','unknown')`, id, result.Status, raw)
	if err == nil && tag.RowsAffected() == 0 {
		var same bool
		err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brain_tool_executions WHERE command_id=$1 AND status=$2 AND result=$3::jsonb)`, id, result.Status, raw).Scan(&same)
		if err == nil && !same {
			return service.ErrBrainConflict
		}
	}
	return err
}
