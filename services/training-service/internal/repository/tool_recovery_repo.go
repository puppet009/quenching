package repository

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	oe "github.com/quenching/proto/operationevidence"
)

// The reply lease spans approval waits and tool execution. No fixed command
// timeout is applied to a live approval, and expired work is never executed.
func OrphanedToolOperations(pool *pgxpool.Pool) func(context.Context) ([]oe.Evidence, error) {
	return func(ctx context.Context) ([]oe.Evidence, error) {
		rows, err := pool.Query(ctx, `SELECT i.evidence FROM training_tool_intents i WHERE i.evidence->>'kind'='tool' AND i.evidence->>'status' IN ('pending','running') AND NOT EXISTS(SELECT 1 FROM session_assistant_identity a WHERE a.session_id=i.session_id AND a.conversation_id=(i.evidence->>'conversation_id')::bigint AND a.lease_token<>'' AND a.lease_expires>clock_timestamp()) ORDER BY i.created_at LIMIT 100`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		events := []oe.Evidence{}
		for rows.Next() {
			var raw []byte
			var e oe.Evidence
			if err = rows.Scan(&raw); err == nil {
				err = json.Unmarshal(raw, &e)
			}
			if err != nil {
				return nil, err
			}
			events = append(events, e)
		}
		return events, rows.Err()
	}
}
