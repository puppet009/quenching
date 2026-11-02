package tooljournal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
)

var producerTables = strings.NewReplacer("training_operation_audit_sessions", "training_tool_audit_sessions", "training_operation_intents", "training_tool_intents", "training_operation_events", "training_tool_events", "training_operation_texts", "training_tool_texts", "training_operation_reply_links", "training_tool_reply_links", "training_operation_remote_seals", "training_tool_remote_seals")

func operationSQL(query string, producer bool) string {
	if producer {
		return producerTables.Replace(query)
	}
	return query
}

type operationTx struct {
	pgx.Tx
	producer bool
}

func (t operationTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return t.Tx.Exec(ctx, operationSQL(q, t.producer), args...)
}
func (t operationTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return t.Tx.Query(ctx, operationSQL(q, t.producer), args...)
}
func (t operationTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return t.Tx.QueryRow(ctx, operationSQL(q, t.producer), args...)
}
func (s *PGStore) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return operationTx{Tx: tx, producer: s.Producer}, nil
}
func (s *PGStore) exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	return s.Pool.Exec(ctx, operationSQL(q, s.Producer), args...)
}
func (s *PGStore) query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	return s.Pool.Query(ctx, operationSQL(q, s.Producer), args...)
}
func (s *PGStore) queryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return s.Pool.QueryRow(ctx, operationSQL(q, s.Producer), args...)
}
