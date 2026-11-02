package repository

import (
	"context"
	oe "github.com/quenching/proto/operationevidence"
	"testing"
	"time"
)

func TestHistoricalEmptyTypeReplayPreservesIdentity(t *testing.T) {
	old := oe.LegacyRow{ID: 71, SessionID: 41, Type: "", CreatedAt: time.Now().UTC()}
	same := old
	if e := validateLegacyReplay(old, same); e != nil {
		t.Fatalf("matching historical row blocked: %v", e)
	}
	same.SessionID = 42
	if e := validateLegacyReplay(old, same); e == nil {
		t.Fatal("foreign replay accepted")
	}
	same = old
	same.Result = "changed"
	if e := validateLegacyReplay(old, same); e == nil {
		t.Fatal("changed historical row accepted")
	}
}
func TestHistoricalOversizedReplayRequiresExistingFullEvidence(t *testing.T) {
	old := oe.LegacyRow{ID: 71, SessionID: 41, Type: "view_log", Detail: []byte(`{"original":"retained"}`), CreatedAt: time.Now().UTC()}
	ref := old
	ref.ReplayHash = oe.RequestHash(old)
	ref.Detail = nil
	if e := validateLegacyReplay(old, ref); e != nil {
		t.Fatalf("matching full history rejected: %v", e)
	}
	ref.ReplayHash = "forged"
	if e := validateLegacyReplay(old, ref); e == nil {
		t.Fatal("forged replay proof accepted")
	}
}

func TestPGHistoricalEmptyTypeReplayAcknowledgesMigratedRow(t *testing.T) {
	ctx, _, pool := sessionMemoryDB(t)
	row := oe.LegacyRow{ID: 71, SessionID: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Result: "executed"}
	if _, e := pool.Exec(ctx, `INSERT INTO training_operation_projection(id,session_id,operation_type,created_at) VALUES($1,$2,'',$3)`, row.ID, row.SessionID, row.CreatedAt); e != nil {
		t.Fatal(e)
	}
	repo := NewOperationProjectionRepo(pool)
	if e := repo.IngestLegacy(ctx, oe.LegacyProjection{Rows: []oe.LegacyRow{row}}); e != nil {
		t.Fatalf("migrated blank row blocked queue: %v", e)
	}
	row.Result = "forged"
	if e := repo.IngestLegacy(context.Background(), oe.LegacyProjection{Rows: []oe.LegacyRow{row}}); e == nil {
		t.Fatal("conflicting replay accepted")
	}
}
