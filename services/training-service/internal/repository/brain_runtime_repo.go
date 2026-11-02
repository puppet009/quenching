package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"time"
)

var _ service.BrainDispatchStore = (*BrainRepo)(nil)

func (r *BrainRepo) TemplateSlug(ctx context.Context, sid int64) (string, error) {
	var slug string
	e := r.pool.QueryRow(ctx, `SELECT coalesce(template_slug,'') FROM training_sessions WHERE id=$1 AND deleted_at IS NULL`, sid).Scan(&slug)
	if e == nil && slug == "" {
		e = errors.New("session has no template")
	}
	return slug, e
}
func (r *BrainRepo) SetWaiting(ctx context.Context, l model.BrainLease, reason string) error {
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	version, hasVersion := service.BrainObservationVersionFromContext(ctx)
	tag, e := tx.Exec(ctx, `UPDATE brain_sessions SET reason=$4,updated_at=now() WHERE session_id=$1 AND lease_owner=$2 AND lease_version=$3 AND lease_until>clock_timestamp() AND (NOT $5::boolean OR observation_version=$6)`, l.SessionID, l.Owner, l.Version, reason, hasVersion, version)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return service.ErrBrainConflict
	}
	if hasVersion {
		if _, e = tx.Exec(ctx, `UPDATE brain_events SET processed=true WHERE session_id=$1 AND NOT processed`, l.SessionID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (r *BrainRepo) SetHintPolicy(ctx context.Context, l model.BrainLease, level, budget int) error {
	tag, e := r.pool.Exec(ctx, `UPDATE brain_sessions SET hint_level=$4,hint_budget=$5,observation_version=observation_version+1 WHERE session_id=$1 AND lease_owner=$2 AND lease_version=$3 AND lease_until>clock_timestamp() AND phase NOT IN ('closing','reviewing','completed','failed')`, l.SessionID, l.Owner, l.Version, level, budget)
	if e == nil && tag.RowsAffected() != 1 {
		return service.ErrBrainConflict
	}
	return e
}
func (r *BrainRepo) Freeze(ctx context.Context, sid int64, reason string) error {
	// A user may close immediately before the periodic dispatcher initializes the brain.
	if _, err := r.pool.Exec(ctx, `INSERT INTO brain_sessions(session_id) SELECT id FROM training_sessions WHERE id=$1 AND deleted_at IS NULL AND template_slug IS NOT NULL AND template_slug<>'' ON CONFLICT DO NOTHING`, sid); err != nil {
		return err
	}

	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase='closing',reason=$2,observation_version=observation_version+1,lease_until=NULL,cleanup_status='pending' WHERE session_id=$1 AND phase NOT IN ('completed','reviewing','closing')`, sid, reason)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE brain_tool_executions SET status='cancelled' WHERE session_id=$1 AND status='pending'`, sid)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *BrainRepo) ClaimCommand(ctx context.Context, l model.BrainLease, c model.BrainCommand) error {
	tag, e := r.pool.Exec(ctx, `UPDATE brain_tool_executions c SET status='accepted',attempts=attempts+1,updated_at=now() FROM brain_sessions b,training_sessions s WHERE c.command_id=$1 AND c.session_id=$2 AND c.status='pending' AND b.session_id=c.session_id AND s.id=b.session_id AND b.lease_owner=$3 AND b.lease_version=$4 AND b.lease_until>clock_timestamp() AND b.phase NOT IN ('closing','reviewing','completed','failed') AND s.status NOT IN ('completed','aborted','failed') AND s.deleted_at IS NULL`, c.CommandID, l.SessionID, l.Owner, l.Version)
	if e == nil && tag.RowsAffected() != 1 {
		return service.ErrBrainConflict
	}
	return e
}
func (r *BrainRepo) WakeSessions(ctx context.Context) ([]int64, error) {
	// Initialization and polling use durable rows, so a restart cannot lose a wakeup.
	_, e := r.pool.Exec(ctx, `INSERT INTO brain_sessions(session_id) SELECT id FROM training_sessions WHERE template_slug IS NOT NULL AND template_slug<>'' AND status='running' AND deleted_at IS NULL ON CONFLICT DO NOTHING`)
	if e != nil {
		return nil, e
	}
	_, e = r.pool.Exec(ctx, `INSERT INTO brain_events(session_id,source,event_id,kind) SELECT b.session_id,'lifecycle','initial','sandbox_ready' FROM brain_sessions b JOIN training_sessions s ON s.id=b.session_id WHERE b.phase='sandbox_ready' AND s.status='running' ON CONFLICT DO NOTHING`)
	if e != nil {
		return nil, e
	}
	if e = r.copyManagedOperationEvents(ctx); e != nil {
		return nil, e
	}
	if e = r.copyOperationEvents(ctx); e != nil {
		return nil, e
	}
	rows, e := r.pool.Query(ctx, `SELECT b.session_id FROM brain_sessions b JOIN training_sessions s ON s.id=b.session_id WHERE s.status='running' AND s.deleted_at IS NULL AND b.phase NOT IN ('closing','reviewing','completed','failed') AND ((EXISTS(SELECT 1 FROM brain_events e WHERE e.session_id=b.session_id AND NOT e.processed)) OR (EXISTS(SELECT 1 FROM brain_tool_executions c WHERE c.session_id=b.session_id AND c.status IN ('pending','accepted','running','unknown')))) ORDER BY b.updated_at LIMIT 32`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var sid int64
		if e = rows.Scan(&sid); e != nil {
			return nil, e
		}
		out = append(out, sid)
	}
	return out, rows.Err()
}
func (r *BrainRepo) copyOperationEvents(ctx context.Context) error {
	rows, e := r.pool.Query(ctx, `SELECT o.id,o.session_id,o.created_at,jsonb_build_object('operation_type',o.operation_type,'operation_category',o.operation_category,'target',o.target,'detail',o.detail,'result',o.result)::text FROM training_operation_projection o JOIN brain_sessions b ON b.session_id=o.session_id JOIN training_sessions s ON s.id=b.session_id WHERE o.deleted_at IS NULL AND s.status='running' AND b.phase NOT IN ('closing','reviewing','completed','failed') AND NOT EXISTS(SELECT 1 FROM brain_events e WHERE e.session_id=o.session_id AND e.source='student_audit' AND e.event_id='operation:'||o.id) ORDER BY o.id LIMIT 100`)
	if e != nil {
		return e
	}
	type item struct {
		id, sid int64
		at      time.Time
		raw     string
	}
	items := []item{}
	for rows.Next() {
		var v item
		if e = rows.Scan(&v.id, &v.sid, &v.at, &v.raw); e != nil {
			rows.Close()
			return e
		}
		items = append(items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range items {
		normalized, e := service.NormalizeBrainEvidence("operation", v.raw)
		if e != nil {
			return e
		}
		id := fmt.Sprintf("operation:%d", v.id)
		if _, e = r.SaveEvidence(ctx, model.BrainEvidence{ID: id, SessionID: v.sid, OccurredAt: v.at, Kind: "operation", Actor: "student_report", PayloadJSON: normalized}); e != nil {
			return e
		}
		if _, e = r.AppendEvent(ctx, model.BrainEvent{SessionID: v.sid, Source: "student_audit", EventID: id, Kind: "student_operation", EvidenceRefs: []string{id}}); e != nil && !errors.Is(e, service.ErrBrainConflict) {
			return e
		}
	}
	return nil
}
func (r *BrainRepo) ObservationData(ctx context.Context, sid int64) (map[string]any, []string, error) {
	data := map[string]any{}
	refs := []string{}
	evidence := []model.BrainEvidence{}
	rows, e := r.pool.Query(ctx, `SELECT id,kind,actor,occurred_at,payload::text,parent_decision_id FROM brain_evidence_refs WHERE session_id=$1 ORDER BY sequence DESC LIMIT 50`, sid)
	if e != nil {
		return nil, nil, e
	}
	for rows.Next() {
		v := model.BrainEvidence{SessionID: sid}
		if e = rows.Scan(&v.ID, &v.Kind, &v.Actor, &v.OccurredAt, &v.PayloadJSON, &v.ParentDecisionID); e != nil {
			rows.Close()
			return nil, nil, e
		}
		refs = append(refs, v.ID)
		evidence = append(evidence, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	data["evidence"] = evidence
	for key, query := range map[string]string{
		"ability_assessment":  `SELECT coalesce((SELECT assessment FROM ability_assessments WHERE session_id=$1 ORDER BY version DESC LIMIT 1),'{}'::jsonb)`,
		"hint_usage":          `SELECT jsonb_build_object('user_questions',count(*) FILTER(WHERE m.role='user'),'assistant_messages',count(*) FILTER(WHERE m.role='assistant'),'hints',count(*) FILTER(WHERE m.role='assistant' AND m.is_hint)) FROM ai_messages m JOIN ai_conversations c ON c.id=m.conversation_id WHERE c.session_id=$1`,
		"baseline_operations": `SELECT baseline_operations FROM brain_sessions WHERE session_id=$1`,
		"recent_tool_results": `SELECT coalesce(jsonb_agg(item),'[]') FROM (SELECT jsonb_build_object('command_id',command_id,'action',action,'parameters',parameters,'status',status,'result',result) item FROM brain_tool_executions WHERE session_id=$1 ORDER BY updated_at DESC LIMIT 16) q`,
		"challenges":          `SELECT coalesce(jsonb_agg(item),'[]') FROM (SELECT to_jsonb(c) item FROM brain_challenges c WHERE session_id=$1 ORDER BY challenge_no DESC LIMIT 8) q`,
	} {
		var raw []byte
		if e = r.pool.QueryRow(ctx, query, sid).Scan(&raw); e != nil {
			return nil, nil, e
		}
		data[key] = json.RawMessage(raw)
	}
	return data, refs, nil
}
func (r *BrainRepo) ApplyResult(ctx context.Context, l model.BrainLease, c model.BrainCommand, result model.BrainToolResult) error {
	if c.CommandID != result.CommandID {
		return errors.New("command result identity mismatch")
	}
	if result.Status != "succeeded" && result.Status != "failed" && result.Status != "accepted" && result.Status != "running" && result.Status != "unknown" {
		return errors.New("invalid domain status")
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var sid int64
	e = tx.QueryRow(ctx, `SELECT b.session_id FROM brain_sessions b JOIN training_sessions s ON s.id=b.session_id WHERE b.session_id=$1 AND b.lease_owner=$2 AND b.lease_version=$3 AND b.lease_until>clock_timestamp() AND b.phase NOT IN ('closing','reviewing','completed','failed') AND s.status NOT IN ('completed','aborted','failed') AND s.deleted_at IS NULL FOR UPDATE OF b,s`, l.SessionID, l.Owner, l.Version).Scan(&sid)
	if errors.Is(e, pgx.ErrNoRows) {
		return service.ErrBrainConflict
	}
	if e != nil {
		return e
	}
	var applied bool
	var params string
	e = tx.QueryRow(ctx, `SELECT applied,parameters::text FROM brain_tool_executions WHERE command_id=$1 AND session_id=$2 AND action=$3 FOR UPDATE`, c.CommandID, sid, c.Action).Scan(&applied, &params)
	if e != nil {
		return e
	}
	if applied {
		return nil
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return e
	}
	terminal := result.Status == "succeeded" || result.Status == "failed"
	_, e = tx.Exec(ctx, `UPDATE brain_tool_executions SET status=$2,result=$3,applied=$4,updated_at=now() WHERE command_id=$1`, c.CommandID, result.Status, raw, terminal)
	if e != nil {
		return e
	}
	if !terminal {
		return tx.Commit(ctx)
	}
	payload, e := service.NormalizeBrainEvidence("tool_result", string(raw))
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO brain_evidence_refs(id,session_id,kind,actor,occurred_at,payload,parent_decision_id) VALUES($1,$2,'tool_result','platform',now(),$3,$4) ON CONFLICT DO NOTHING`, "tool:"+c.CommandID, sid, payload, c.DecisionID)
	if e != nil {
		return e
	}
	var p service.BrainToolParameters
	if e = json.Unmarshal([]byte(params), &p); e != nil {
		return e
	}
	var facts struct {
		BusinessVerified bool      `json:"business_verified"`
		ExperimentUIDs   []string  `json:"experiment_uids"`
		ExpiresAt        time.Time `json:"expires_at"`
	}
	_ = json.Unmarshal([]byte(result.ResultJSON), &facts)
	if result.Status == "succeeded" {
		switch c.Action {
		case "list_trainable_faults":
			phase, reason, err := service.BrainCatalogAvailability(result.ResultJSON)
			if err != nil {
				return err
			}
			if reason != "" {
				_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase=$2,reason=$3 WHERE session_id=$1`, sid, phase, reason)
			} else {
				_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase=CASE WHEN baseline_verified THEN 'calibration' ELSE 'baseline_check' END,reason='' WHERE session_id=$1 AND reason='no_verified_faults'`, sid)
			}
			if e != nil {
				return e
			}
		case "run_user_behavior":
			if facts.BusinessVerified {
				operation, _ := json.Marshal(map[string]bool{p.Operation: true})
				_, e = tx.Exec(ctx, `UPDATE brain_sessions SET baseline_operations=baseline_operations||$2::jsonb,phase=CASE WHEN phase='sandbox_ready' THEN 'baseline_check' ELSE phase END WHERE session_id=$1 AND NOT baseline_verified`, sid, operation)
				if e != nil {
					return e
				}
				_, e = tx.Exec(ctx, `UPDATE brain_sessions SET baseline_verified=true,phase='calibration' WHERE session_id=$1 AND NOT baseline_verified AND baseline_operations @> '{"baseline":true,"register":true,"cart":true,"order":true,"pay":true}'`, sid)
				if e != nil {
					return e
				}
			}
		case "inject_fault":
			if len(facts.ExperimentUIDs) == 0 || facts.ExpiresAt.IsZero() {
				return errors.New("fault result lacks UID or deadline")
			}
			var current int
			var baseline bool
			e = tx.QueryRow(ctx, `SELECT current_challenge,baseline_verified FROM brain_sessions WHERE session_id=$1`, sid).Scan(&current, &baseline)
			if e != nil {
				return e
			}
			if !baseline {
				return errors.New("baseline not verified")
			}
			ids, _ := json.Marshal(p.DefectIDs)
			uids, _ := json.Marshal(facts.ExperimentUIDs)
			_, e = tx.Exec(ctx, `INSERT INTO brain_challenges(session_id,challenge_no,status,learning_objective,defect_ids,experiment_uids,accepted_digest,started_at,expires_at) SELECT $1,$2,'active',decision->>'learning_objective',$3,$4,$5,now(),$6 FROM brain_decisions WHERE decision_id=$7`, sid, current+1, ids, uids, p.AcceptanceDigest, facts.ExpiresAt, c.DecisionID)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `UPDATE brain_sessions SET current_challenge=$2,phase=CASE WHEN $2=1 THEN 'calibration' ELSE 'adaptive_training' END WHERE session_id=$1`, sid, current+1)
			if e != nil {
				return e
			}
		case "verify_business":
			if facts.BusinessVerified {
				e = applyRepairEvidence(ctx, tx, sid, c.CommandID)
				if e != nil {
					return e
				}
				_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase='adaptive_training' WHERE session_id=$1 AND EXISTS(SELECT 1 FROM brain_challenges WHERE session_id=$1 AND status IN ('verified_repair','assisted_repair'))`, sid)
				if e != nil {
					return e
				}
			}
		case "revoke_fault":
			uids, _ := json.Marshal(p.ExperimentUIDs)
			_, e = tx.Exec(ctx, `UPDATE brain_challenges SET faults_revoked=true,status=CASE WHEN status='active' THEN 'closed_unresolved' ELSE status END,completed_at=coalesce(completed_at,now()) WHERE session_id=$1 AND status IN ('active','verified_repair','assisted_repair') AND experiment_uids <@ $2::jsonb`, sid, uids)
			if e != nil {
				return e
			}
		case "finish_training":
			_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase='closing',reason=$2,cleanup_status='pending' WHERE session_id=$1`, sid, p.Reason)
			if e != nil {
				return e
			}
		}
	}
	if result.Status == "failed" && (c.Action == "run_user_behavior" || c.Action == "verify_business") {
		_, e = tx.Exec(ctx, `UPDATE brain_sessions SET phase=CASE WHEN baseline_verified THEN phase ELSE 'degraded' END,reason='business verification or runtime failed; inspect evidence before retry' WHERE session_id=$1`, sid)
		if e != nil {
			return e
		}
	}
	_, e = tx.Exec(ctx, `UPDATE brain_sessions SET observation_version=observation_version+1,updated_at=now() WHERE session_id=$1`, sid)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r *BrainRepo) VerifiedRepair(ctx context.Context, sid int64) (bool, bool, error) {
	var repaired, enabled bool
	e := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brain_challenges WHERE session_id=$1 AND status='verified_repair'),EXISTS(SELECT 1 FROM brain_sessions WHERE session_id=$1)`, sid).Scan(&repaired, &enabled)
	return repaired, enabled, e
}
