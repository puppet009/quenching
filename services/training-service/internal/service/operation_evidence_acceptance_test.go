package service

import (
	"context"
	"encoding/json"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	rc "github.com/quenching/proto/reviewcontract"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Offline composition of actual audit, history, attribution, snapshot and close services.
// PostgreSQL/Kubernetes producer execution is covered separately by explicit integration gates.
func TestOperationEvidenceWholeSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := oetest.NewMemoryStore()
	now := time.Now()
	var admitted atomic.Int32
	var wg sync.WaitGroup
	initial := oe.Evidence{SchemaVersion: 1, OperationID: "command", EventID: "command:1", Sequence: 1, SessionID: 7, Actor: "student", Kind: "command", Status: "pending", StartedAt: now, Coverage: oe.EmptyCoverage()}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fresh, err := store.Begin(ctx, 7, "command", "retry-key", "hash", initial)
			if err != nil {
				t.Error(err)
			}
			if fresh {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatal(admitted.Load())
	}
	command := oe.Next(initial, "unknown")
	command.Coverage.ReasonCodes = []string{"result_unconfirmed"}
	command.ExitCode = nil
	text := oe.CaptureText(command.OperationID, "stdout", []byte("Authorization: Bea"+"rer hidden-value\n"+strings.Repeat("中", 3000)+" SOURCE TAIL"), oe.TextLimit)
	command.TextRefs = []string{text.Ref}
	if err := store.Append(ctx, command, []oe.Text{text}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.Content, "hidden-value") {
		t.Fatal("secret saved")
	}
	offset := 0
	body := ""
	for {
		f, err := oe.ReadFragment(text, offset, 8192)
		if err != nil {
			t.Fatal(err)
		}
		body += f.Text
		if f.Complete {
			break
		}
		offset = f.NextOffsetBytes
	}
	if !strings.HasSuffix(body, "SOURCE TAIL") {
		t.Fatal("tail missing")
	}
	if _, err := store.Read(ctx, 8, text.Ref); err == nil {
		t.Fatal("foreign session read")
	}
	audit := &ToolAudit{Store: store}
	scope := ChatLeaseScope{SessionID: 7, UserID: 10}
	read, err := audit.Begin(ctx, scope, 107, 1, ToolCall{ID: "read", Name: "read_file", ArgumentsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	read, err = audit.Advance(ctx, read, "running", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = audit.Finish(ctx, read, ToolResult{ContentJSON: `{"content":"observation"}`}); err != nil {
		t.Fatal(err)
	}
	reject, _ := audit.Begin(ctx, scope, 107, 1, ToolCall{ID: "reject", Name: "write_file", ArgumentsJSON: `{}`})
	reject, err = audit.Advance(ctx, reject, "cancelled", "rejected")
	if err != nil {
		t.Fatal(err)
	}
	write, _ := audit.Begin(ctx, scope, 107, 2, ToolCall{ID: "write", Name: "write_file", ArgumentsJSON: `{}`})
	write, err = audit.Advance(ctx, write, "running", "approved")
	if err != nil {
		t.Fatal(err)
	}
	audit.Store = failingToolStore{Store: store, failAppend: true}
	if audit.Finish(ctx, write, ToolResult{ContentJSON: `{"changed":true}`}) == nil {
		t.Fatal("outcome loss hidden")
	}
	retained, _ := store.Get(ctx, 7, write.OperationID)
	if retained.Status != "running" {
		t.Fatal(retained)
	}
	history := NewSessionHistoryTools(&historyFake{}).WithOperations(store)
	raw, err := history.Execute(ctx, 10, 7, ToolCall{Name: "list_session_operations", ArgumentsJSON: `{"limit":10}`})
	if err != nil || !strings.Contains(raw, "assistant") {
		t.Fatal(raw, err)
	}
	if _, err = history.Execute(ctx, 11, 7, ToolCall{Name: "list_session_operations", ArgumentsJSON: `{}`}); err == nil {
		t.Fatal("foreign user read")
	}
	es, before, after, window := repairFixture()
	es[0].Actor = "assistant"
	if EvaluateRepair(es, before, after, window).Status != "assisted_repair" {
		t.Fatal("assistant became student")
	}
	store.Closed = true
	if _, err = store.Begin(ctx, 7, "late", "new-key", "hash", initial); err != oe.ErrClosed {
		t.Fatal(err)
	}
	local := &sealFake{seal: oe.AuditSeal{Producer: "training", State: "pending", Deadline: now.Add(time.Minute), Pending: 1}}
	remote := &sealFake{seal: oe.AuditSeal{SessionID: 7, Producer: "sandbox", State: "pending", Deadline: now.Add(time.Minute), Pending: 1}}
	reports := &cleanupTestReview{}
	cleanup := NewBrainCleanupCoordinator(&cleanupTestStore{}, cleanupTestFault{}, cleanupTestSandbox{}, reports).WithOperationSeal(&OperationSealService{Local: local, Remote: remote})
	if err = cleanup.Close(ctx, 7); err != nil || reports.calls != 0 {
		t.Fatal(err, reports.calls)
	}
	local.seal = oe.EvaluateSeal(local.seal, now.Add(61*time.Second))
	remote.seal = oe.EvaluateSeal(remote.seal, now.Add(61*time.Second))
	if err = cleanup.Close(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if err = cleanup.Close(ctx, 7); err != nil || reports.calls != 1 {
		t.Fatal(err, reports.calls)
	}
	sources := []rc.Source{OperationReviewSource(command), OperationReviewSource(reject)}
	snap, rows, err := BuildReviewSnapshot(rc.Eligibility{SessionID: 7, UserID: 10, ProjectID: 0, SessionStatus: "completed"}, sources, json.RawMessage(`{}`), now)
	if err != nil {
		t.Fatal(err)
	}
	ApplyOperationCoverage(&snap, []oe.Evidence{command, reject, retained}, []oe.AuditSeal{local.seal, remote.seal})
	SetReviewSnapshotHash(&snap, rows)
	if snap.AuditComplete || len(snap.AuditGaps) == 0 {
		t.Fatal(snap)
	}
	originalHash := snap.Hash
	encoded, _ := json.Marshal(snap)
	var restored rc.Snapshot
	_ = json.Unmarshal(encoded, &restored)
	SetReviewSnapshotHash(&restored, rows)
	if restored.Hash != originalHash {
		t.Fatal("frozen coverage lost")
	}
}
