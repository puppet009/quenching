package operationaudit

import (
	"context"
	"encoding/json"
	"time"
)

func (s *PGStore) RequestCancel(ctx context.Context, sid int64, id string) error {
	tag, err := s.exec(ctx, `UPDATE operation_intents SET cancel_requested=true WHERE session_id=$1 AND operation_id=$2`, sid, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *PGStore) CancelRequested(ctx context.Context, sid int64, id string) (bool, error) {
	var requested bool
	err := s.queryRow(ctx, `SELECT cancel_requested FROM operation_intents WHERE session_id=$1 AND operation_id=$2`, sid, id).Scan(&requested)
	return requested, err
}

// No remote execution is retried. The maximal command deadline has elapsed before recovery.
func (s *PGStore) RecoverExpired(ctx context.Context, age time.Duration) error {
	rows, err := s.query(ctx, `SELECT evidence FROM operation_intents WHERE evidence->>'status' IN ('pending','running') AND (evidence->>'started_at')::timestamptz < now()-($1::double precision*interval '1 second')`, age.Seconds())
	if err != nil {
		return err
	}
	var pending []Evidence
	for rows.Next() {
		var raw []byte
		if rows.Scan(&raw) != nil {
			break
		}
		var e Evidence
		if json.Unmarshal(raw, &e) == nil {
			pending = append(pending, e)
		}
	}
	rows.Close()
	for _, e := range pending {
		v := Next(e, "unknown")
		v.ExitCode = nil
		v.Coverage.Execution = "unknown"
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "result_unconfirmed")
		_ = s.Append(ctx, v, nil)
	}
	return nil
}
func (s *PGStore) RunRecovery(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RecoverExpired(ctx, 330*time.Second)
		}
	}
}
