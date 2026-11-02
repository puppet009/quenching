package repository

import (
	"context"
	"encoding/json"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

func (r *BrainRepo) AssessmentVersion(ctx context.Context, sid int64) (int64, error) {
	var version int64
	e := r.pool.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM ability_assessments WHERE session_id=$1`, sid).Scan(&version)
	return version, e
}
func (r *BrainRepo) SaveAssessment(ctx context.Context, a *model.AbilityAssessment) error {
	raw, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `INSERT INTO ability_assessments(session_id,version,assessment) VALUES($1,$2,$3)`, a.SessionID, a.Version, raw)
	return e
}
func (r *BrainRepo) StudentEvidence(ctx context.Context, sid int64) ([]model.BrainEvidence, error) {
	rows, e := r.pool.Query(ctx, `SELECT id,kind,actor,payload::text FROM brain_evidence_refs WHERE session_id=$1 AND actor IN ('student','student_report','simulator') AND kind IN ('operation','file_diff','terminal','business_result') ORDER BY sequence DESC LIMIT 20`, sid)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.BrainEvidence{}
	for rows.Next() {
		v := model.BrainEvidence{SessionID: sid}
		if e = rows.Scan(&v.ID, &v.Kind, &v.Actor, &v.PayloadJSON); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *BrainRepo) ReserveBrainHint(ctx context.Context, sid int64, level int) (int, bool, error) {
	var exists bool
	e := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brain_sessions WHERE session_id=$1)`, sid).Scan(&exists)
	if e != nil || !exists {
		return 0, exists, e
	}
	var budget int
	e = r.pool.QueryRow(ctx, `UPDATE brain_sessions SET hint_budget=hint_budget-1 WHERE session_id=$1 AND hint_level>=$2 AND hint_budget>0 AND phase NOT IN ('closing','reviewing','completed','failed') RETURNING hint_budget`, sid, level).Scan(&budget)
	if e != nil {
		return 0, true, service.ErrHintExhausted
	}
	return budget, true, nil
}
