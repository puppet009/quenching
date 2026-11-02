package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

type BehaviorCommandRepo struct{ pool *pgxpool.Pool }

func NewBehaviorCommandRepo(pool *pgxpool.Pool) *BehaviorCommandRepo {
	return &BehaviorCommandRepo{pool}
}
func (r *BehaviorCommandRepo) InsertCommand(ctx context.Context, c model.BehaviorCommand) (model.BehaviorResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.BehaviorResult{}, err
	}
	defer tx.Rollback(ctx)
	var sid int64
	err = tx.QueryRow(ctx, `SELECT session_id FROM sandboxes WHERE session_id=$1 AND status IN ('ready','running') AND completed_at IS NULL FOR UPDATE`, c.SessionID).Scan(&sid)
	if err != nil {
		return model.BehaviorResult{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO behavior_commands(command_id,session_id,template_slug,operation,parameters) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, c.CommandID, c.SessionID, c.TemplateSlug, c.Operation, c.ParametersJSON)
	if err != nil {
		return model.BehaviorResult{}, err
	}
	var old model.BehaviorCommand
	var status string
	err = tx.QueryRow(ctx, `SELECT command_id,session_id,template_slug,operation,parameters::text,status FROM behavior_commands WHERE command_id=$1`, c.CommandID).Scan(&old.CommandID, &old.SessionID, &old.TemplateSlug, &old.Operation, &old.ParametersJSON, &status)
	if err != nil {
		return model.BehaviorResult{}, err
	}
	if !service.SameBehaviorCommand(old, c) {
		return model.BehaviorResult{}, errors.New("command identity collision")
	}
	if err = tx.Commit(ctx); err != nil {
		return model.BehaviorResult{}, err
	}
	return r.GetCommand(ctx, c.SessionID, c.CommandID)
}
func (r *BehaviorCommandRepo) GetCommand(ctx context.Context, sid int64, id string) (model.BehaviorResult, error) {
	_, err := r.pool.Exec(ctx, `UPDATE behavior_commands SET status='unknown',updated_at=now() WHERE command_id=$1 AND session_id=$2 AND status='running' AND claimed_until<clock_timestamp()`, id, sid)
	if err != nil {
		return model.BehaviorResult{}, err
	}
	result := model.BehaviorResult{CommandID: id, ResultJSON: "{}", EvidenceRefs: []string{}}
	var raw []byte
	err = r.pool.QueryRow(ctx, `SELECT status,result FROM behavior_commands WHERE command_id=$1 AND session_id=$2`, id, sid).Scan(&result.Status, &raw)
	if err == nil && len(raw) > 0 {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (r *BehaviorCommandRepo) ClaimCommand(ctx context.Context, sid int64) (*model.BehaviorCommand, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var scope int64
	if err = tx.QueryRow(ctx, `SELECT session_id FROM sandboxes WHERE session_id=$1 AND status IN ('ready','running') AND completed_at IS NULL FOR UPDATE`, sid).Scan(&scope); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE behavior_commands SET status='unknown',updated_at=now() WHERE session_id=$1 AND status='running' AND claimed_until<clock_timestamp()`, sid); err != nil {
		return nil, err
	}
	var busy bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM behavior_commands WHERE session_id=$1 AND status IN ('running','unknown'))`, sid).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, nil
	}
	c := &model.BehaviorCommand{}
	err = tx.QueryRow(ctx, `SELECT command_id,session_id,template_slug,operation,parameters::text FROM behavior_commands WHERE session_id=$1 AND status='accepted' ORDER BY created_at,command_id LIMIT 1 FOR UPDATE`, sid).Scan(&c.CommandID, &c.SessionID, &c.TemplateSlug, &c.Operation, &c.ParametersJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE behavior_commands SET status='running',claimed_until=clock_timestamp()+interval '90 seconds',updated_at=now() WHERE command_id=$1`, c.CommandID); err != nil {
		return nil, err
	}
	return c, tx.Commit(ctx)
}
func (r *BehaviorCommandRepo) FinishCommand(ctx context.Context, sid int64, result model.BehaviorResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE behavior_commands SET status=$3,result=$4,delivered=false,updated_at=now() WHERE command_id=$1 AND session_id=$2 AND status IN ('running','unknown')`, result.CommandID, sid, result.Status, raw)
	if err == nil && tag.RowsAffected() == 0 {
		var same bool
		err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM behavior_commands WHERE command_id=$1 AND session_id=$2 AND result=$3::jsonb)`, result.CommandID, sid, raw).Scan(&same)
		if err == nil && !same {
			return errors.New("runner result state conflict")
		}
	}
	return err
}
func (r *BehaviorCommandRepo) RunnerTokenHash(ctx context.Context, sid int64) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT token_hash FROM behavior_runner_credentials c JOIN sandboxes s ON s.session_id=c.session_id WHERE c.session_id=$1 AND s.status IN ('ready','running') AND s.completed_at IS NULL`, sid).Scan(&hash)
	return hash, err
}
func (r *BehaviorCommandRepo) SetRunnerTokenHash(ctx context.Context, sid int64, hash string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO behavior_runner_credentials(session_id,token_hash) VALUES($1,$2) ON CONFLICT(session_id) DO UPDATE SET token_hash=EXCLUDED.token_hash`, sid, hash)
	return err
}

func (r *BehaviorCommandRepo) PendingResults(ctx context.Context) ([]service.BehaviorDelivery, error) {
	rows, err := r.pool.Query(ctx, `SELECT session_id,command_id,status,COALESCE(result,'{}') FROM behavior_commands WHERE NOT delivered AND status IN ('succeeded','failed','unknown') ORDER BY updated_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.BehaviorDelivery{}
	for rows.Next() {
		var d service.BehaviorDelivery
		var raw []byte
		if err = rows.Scan(&d.SessionID, &d.Result.CommandID, &d.Result.Status, &raw); err != nil {
			return nil, err
		}
		if string(raw) != "{}" {
			if err = json.Unmarshal(raw, &d.Result); err != nil {
				return nil, err
			}
		} else {
			d.Result.ResultJSON = "{}"
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (r *BehaviorCommandRepo) MarkDelivered(ctx context.Context, id, status string) error {
	_, err := r.pool.Exec(ctx, `UPDATE behavior_commands SET delivered=true WHERE command_id=$1 AND status=$2`, id, status)
	return err
}
