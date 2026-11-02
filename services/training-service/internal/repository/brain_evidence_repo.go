package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"reflect"
)

func (r *BrainRepo) SaveEvidence(ctx context.Context, e model.BrainEvidence) (string, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO brain_evidence_refs(id,session_id,kind,actor,occurred_at,payload,parent_decision_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO NOTHING`, e.ID, e.SessionID, e.Kind, e.Actor, e.OccurredAt, e.PayloadJSON, e.ParentDecisionID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		old, err := r.ReadEvidence(ctx, e.ID)
		if err != nil {
			return "", err
		}
		var oldPayload, newPayload any
		if json.Unmarshal([]byte(old.PayloadJSON), &oldPayload) != nil || json.Unmarshal([]byte(e.PayloadJSON), &newPayload) != nil {
			return "", errors.New("invalid evidence payload")
		}
		if old.SessionID != e.SessionID || old.Kind != e.Kind || old.Actor != e.Actor || old.ParentDecisionID != e.ParentDecisionID || !reflect.DeepEqual(oldPayload, newPayload) {
			return "", errors.New("evidence identity conflict")
		}
	}
	return e.ID, nil
}
func (r *BrainRepo) ReadEvidence(ctx context.Context, id string) (*model.BrainEvidence, error) {
	e := &model.BrainEvidence{}
	err := r.pool.QueryRow(ctx, `SELECT id,session_id,sequence,kind,actor,occurred_at,payload::text,parent_decision_id FROM brain_evidence_refs WHERE id=$1`, id).Scan(&e.ID, &e.SessionID, &e.Sequence, &e.Kind, &e.Actor, &e.OccurredAt, &e.PayloadJSON, &e.ParentDecisionID)
	return e, err
}
func (r *BrainRepo) CheckCommand(ctx context.Context, cmd model.BrainCommand) error {
	lease, ok := service.BrainLeaseFromContext(ctx)
	if !ok || lease.SessionID != cmd.SessionID {
		return service.ErrBrainConflict
	}
	var permitted bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brain_tool_executions c JOIN brain_sessions b ON b.session_id=c.session_id JOIN training_sessions s ON s.id=b.session_id WHERE c.command_id=$1 AND c.session_id=$2 AND c.decision_id=$3 AND c.action=$4 AND c.parameters=$5::jsonb AND c.status IN ('pending','accepted','running','unknown') AND b.lease_owner=$6 AND b.lease_version=$7 AND b.lease_until>clock_timestamp() AND b.phase NOT IN ('closing','reviewing','completed','failed') AND s.status NOT IN ('completed','aborted','failed') AND s.deleted_at IS NULL)`, cmd.CommandID, cmd.SessionID, cmd.DecisionID, cmd.Action, cmd.ParametersJSON, lease.Owner, lease.Version).Scan(&permitted)
	if err != nil {
		return err
	}
	if !permitted {
		return service.ErrBrainConflict
	}
	return nil
}

func (r *BrainRepo) LookupCommand(ctx context.Context, id string) (*model.BrainCommand, error) {
	c := &model.BrainCommand{}
	err := r.pool.QueryRow(ctx, `SELECT command_id,session_id,decision_id,observation_version,domain,action,parameters::text,status FROM brain_tool_executions WHERE command_id=$1`, id).Scan(&c.CommandID, &c.SessionID, &c.DecisionID, &c.ObservationVersion, &c.Domain, &c.Action, &c.ParametersJSON, &c.Status)
	return c, err
}
