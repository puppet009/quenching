package tooljournal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *PGStore) Seal(ctx context.Context, sid int64, producer string, now time.Time) (AuditSeal, error) {
	seal := AuditSeal{SessionID: sid, Producer: producer, Gaps: []string{}}
	tx, err := s.begin(ctx)
	if err != nil {
		return seal, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO training_operation_audit_sessions(session_id,sealed,deadline,seal_state) VALUES($1,true,$2,'pending') ON CONFLICT DO NOTHING`, sid, now.Add(time.Minute))
	if err != nil {
		return seal, err
	}
	var raw []byte
	var deadline *time.Time
	err = tx.QueryRow(ctx, `SELECT deadline,seal_state,gaps FROM training_operation_audit_sessions WHERE session_id=$1 FOR UPDATE`, sid).Scan(&deadline, &seal.State, &raw)
	if err != nil {
		return seal, err
	}
	if deadline == nil {
		d := now.Add(time.Minute)
		deadline = &d
	}
	seal.Deadline = *deadline
	_ = json.Unmarshal(raw, &seal.Gaps)
	_, err = tx.Exec(ctx, `UPDATE training_operation_audit_sessions SET sealed=true,deadline=$2 WHERE session_id=$1`, sid, deadline)
	if err != nil {
		return seal, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM training_operation_intents WHERE session_id=$1 AND evidence->>'status' IN ('pending','running')`, sid).Scan(&seal.Pending)
	if err != nil {
		return seal, err
	}
	err = tx.QueryRow(ctx, `SELECT coalesce(max(id),0),count(*) FILTER(WHERE NOT delivered) FROM training_operation_events WHERE session_id=$1`, sid).Scan(&seal.Watermark, &seal.Acknowledged)
	if err != nil {
		return seal, err
	}
	pendingDelivery := seal.Acknowledged
	seal.Acknowledged = seal.Watermark
	if pendingDelivery > 0 {
		seal.Acknowledged = 0
	}
	seal.Pending += int(pendingDelivery)
	seal = EvaluateSeal(seal, now)
	gaps, _ := json.Marshal(seal.Gaps)
	_, err = tx.Exec(ctx, `UPDATE training_operation_audit_sessions SET seal_state=$2,watermark=$3,gaps=$4 WHERE session_id=$1`, sid, seal.State, seal.Watermark, gaps)
	if err != nil {
		return seal, err
	}
	if err = tx.Commit(ctx); err != nil {
		return seal, err
	}
	if seal.State == "timed_out" {
		rows, e := s.query(ctx, `SELECT evidence FROM training_operation_intents WHERE session_id=$1 AND evidence->>'status' IN ('pending','running')`, sid)
		if e != nil {
			return seal, e
		}
		var pending []Evidence
		for rows.Next() {
			var body []byte
			if rows.Scan(&body) == nil {
				var v Evidence
				if json.Unmarshal(body, &v) == nil {
					pending = append(pending, v)
				}
			}
		}
		rows.Close()
		for _, v := range pending {
			u := Next(v, "unknown")
			u.Coverage.Execution = "unknown"
			u.Coverage.ReasonCodes = append(u.Coverage.ReasonCodes, "result_unconfirmed", "audit_delivery_timeout")
			_ = s.Append(ctx, u, nil)
		}
	}
	return seal, nil
}
func (s *PGStore) SaveRemoteSeal(ctx context.Context, sid int64, seal AuditSeal) error {
	if seal.SessionID != sid || seal.Producer != "sandbox" {
		return fmt.Errorf("invalid remote seal")
	}
	raw, _ := json.Marshal(seal)
	_, err := s.exec(ctx, `INSERT INTO training_operation_remote_seals(session_id,seal) VALUES($1,$2) ON CONFLICT(session_id) DO UPDATE SET seal=EXCLUDED.seal`, sid, raw)
	return err
}
