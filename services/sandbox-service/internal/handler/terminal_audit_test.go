package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/quenching/sandbox-service/internal/service"
	"strings"
	"testing"
)

type terminalAuditFixture struct{ events [][]byte }

func (s *terminalAuditFixture) AssertWorkspaceOpen(context.Context, int64) error { return nil }
func (s *terminalAuditFixture) Append(_ context.Context, sid int64, kind string, payload any) error {
	raw, _ := json.Marshal(payload)
	event, err := service.BuildTrainingAuditEvent("test", sid, kind, string(raw))
	s.events = append(s.events, event)
	return err
}
func TestFragmentedTerminalEchoRedactedBeforePersistence(t *testing.T) {
	s := &terminalAuditFixture{}
	var out bytes.Buffer
	w := &auditedTerminalWriter{writer: &out, sink: s, ctx: context.Background(), sid: 7}
	for _, piece := range []string{"export TO", "KEN=frag", "mented-secret\n"} {
		if _, err := w.Write([]byte(piece)); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.events) != 1 {
		t.Fatalf("expected complete line audit, got %d", len(s.events))
	}
	for _, event := range s.events {
		if strings.Contains(string(event), "mented-secret") || strings.Contains(string(event), "frag") {
			t.Fatalf("partial credential persisted: %s", event)
		}
	}
	if out.String() != "export TOKEN=fragmented-secret\n" {
		t.Fatal("terminal output changed")
	}
}

func TestTerminalUnterminatedLineAndOversizedGap(t *testing.T) {
	s := &terminalAuditFixture{}
	var out bytes.Buffer
	w := &auditedTerminalWriter{writer: &out, sink: s, ctx: context.Background(), sid: 7}
	w.Write([]byte("tail"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if len(s.events) == 0 {
		t.Fatal("tail silently omitted")
	}
}
