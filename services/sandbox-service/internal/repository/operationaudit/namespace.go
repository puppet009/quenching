package operationaudit

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
)

var sandboxTables = strings.NewReplacer("operation_audit_sessions", "sandbox_operation_audit_sessions", "operation_intents", "sandbox_operation_intents", "operation_events", "sandbox_operation_events", "operation_texts", "sandbox_operation_texts", "operation_reply_links", "sandbox_operation_reply_links", "operation_remote_seals", "sandbox_operation_remote_seals")

func operationSQL(query string, _ bool) string { return sandboxTables.Replace(query) }

type operationTx struct {
	pgx.Tx
	sandbox bool
}

func (t operationTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return t.Tx.Exec(ctx, operationSQL(q, t.sandbox), args...)
}
func (t operationTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return t.Tx.Query(ctx, operationSQL(q, t.sandbox), args...)
}
func (t operationTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return t.Tx.QueryRow(ctx, operationSQL(q, t.sandbox), args...)
}
func (s *PGStore) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return operationTx{Tx: tx, sandbox: s.Sandbox}, nil
}
func (s *PGStore) exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return s.Pool.Exec(ctx, operationSQL(q, s.Sandbox), args...)
}
func (s *PGStore) query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return s.Pool.Query(ctx, operationSQL(q, s.Sandbox), args...)
}
func (s *PGStore) queryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return s.Pool.QueryRow(ctx, operationSQL(q, s.Sandbox), args...)
}
