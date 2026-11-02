package service

import (
	"encoding/json"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"strings"
	"testing"
	"time"
)

func TestReviewEncodedFragmentBudgetPreservesEscapedTail(t *testing.T) {
	src := rc.Source{ID: "message:1", Ordinal: 1, Text: strings.Repeat("<", 9000) + "important tail"}
	src.ByteLength = len(src.Text)
	offset := 0
	var text string
	for offset < src.ByteLength {
		fragment, err := BudgetReviewSourceFragment(src, offset, 48*1024-2)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(fragment)
		if len(raw) > 48*1024-2 {
			t.Fatal("encoded fragment over budget")
		}
		if fragment.NextOffsetBytes <= offset {
			t.Fatal("no progress")
		}
		text += fragment.Source.Text
		offset = fragment.NextOffsetBytes
	}
	if text != src.Text || !strings.HasSuffix(text, "important tail") {
		t.Fatal("escaped tail omitted")
	}
}

func TestReviewEligibilityRejectsRunningAndForeignOwner(t *testing.T) {
	e := rc.Eligibility{SessionID: 7, UserID: 10, SessionStatus: "running", ProjectID: 0}
	if _, err := CheckReviewEligibility(e, 11, 7, 0); !errors.Is(err, ErrReviewSourceNotFound) {
		t.Fatal(err)
	}
	got, err := CheckReviewEligibility(e, 10, 7, 0)
	if err != nil || got.CanGenerate {
		t.Fatal(got, err)
	}
	e.BrainPhase = "closing"
	e.CleanupStatus = "pending"
	got, _ = CheckReviewEligibility(e, 10, 7, 0)
	if got.CanGenerate {
		t.Fatal("audit pending")
	}
	e.CleanupStatus = "succeeded"
	got, err = CheckReviewEligibility(e, 10, 7, 0)
	if err != nil || !got.CanGenerate || !got.AuditComplete {
		t.Fatal(got, err)
	}
}
func snapshotFixture() (rc.Eligibility, []rc.Source) {
	e := rc.Eligibility{SessionID: 7, UserID: 10, ProjectID: 0, SessionStatus: "completed", CleanupStatus: "succeeded"}
	sources := []rc.Source{}
	for i := 1; i <= 65; i++ {
		sources = append(sources, rc.Source{ID: fmt.Sprintf("message:%d", i), Kind: "message", Role: "user", Text: fmt.Sprintf("turn %d", i)})
	}
	return e, sources
}
func TestFrozenSnapshotAggregatesAllAssistantConversations(t *testing.T) {
	e, sources := snapshotFixture()
	snap, rows, err := BuildReviewSnapshot(e, sources, []byte(`{}`), time.Unix(0, 0))
	if err != nil || snap.SourceCount != 65 || len(rows) != 65 || rows[64].Ordinal != 65 {
		t.Fatal(snap, err)
	}
}
func TestSnapshotIsStableAcrossLateAudit(t *testing.T) {
	e, sources := snapshotFixture()
	first, _, err := BuildReviewSnapshot(e, sources, []byte(`{}`), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := BuildReviewSnapshot(e, sources, []byte(`{}`), time.Unix(0, 0))
	if err != nil || again.Hash != first.Hash {
		t.Fatal("unstable snapshot")
	}
	sources = append(sources, rc.Source{ID: "late", Kind: "operation", Actor: "student_report", Text: "late report"})
	last, _, _ := BuildReviewSnapshot(e, sources, []byte(`{}`), time.Unix(0, 0))
	if last.Hash == first.Hash {
		t.Fatal("hash not bound to source content")
	}
}
func TestSnapshotRangesCoverLongTailAndRedactNestedSecrets(t *testing.T) {
	e, _ := snapshotFixture()
	long := strings.Repeat("中", 3000) + "important tail" + ` {"password":"sensitive value"}`
	_, rows, err := BuildReviewSnapshot(e, []rc.Source{{ID: "long", Kind: "terminal", Actor: "student", Text: long}}, []byte(`{}`), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	offset := 0
	for i := 0; i < 10; i++ {
		f, err := ReviewSourceFragment(rows[0], offset, 8192)
		if err != nil {
			t.Fatal(err)
		}
		all.WriteString(f.Source.Text)
		if f.Complete {
			break
		}
		if f.NextOffsetBytes <= offset {
			t.Fatal("stalled range")
		}
		offset = f.NextOffsetBytes
	}
	if !strings.Contains(all.String(), "important tail") || strings.Contains(all.String(), "sensitive value") {
		t.Fatal("tail missing or secret leaked")
	}
}
func TestPrivateReviewSourcesRequireClosedSession(t *testing.T) {
	e, sources := snapshotFixture()
	e.SessionStatus = "running"
	if _, _, err := BuildReviewSnapshot(e, sources, []byte(`{}`), time.Now()); !errors.Is(err, ErrReviewSessionActive) {
		t.Fatal(err)
	}
	e.SessionStatus = "completed"
	if _, _, err := BuildReviewSnapshot(e, []rc.Source{{ID: "private", Actor: "director", Kind: "decision", Text: "hidden"}}, []byte(`{}`), time.Now()); err == nil {
		t.Fatal("private director source accepted")
	}
}

func TestSnapshotSelfReportCannotClaimReservedFactType(t *testing.T) {
	e, _ := snapshotFixture()
	_, rows, err := BuildReviewSnapshot(e, []rc.Source{{ID: "report:1", Kind: "fault", Actor: "student_report", Text: "I fixed it", Successful: true}}, []byte(`{}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Trust != "user_statement" || rows[0].Successful {
		t.Fatal("reserved kind promoted self report", rows[0])
	}
}
