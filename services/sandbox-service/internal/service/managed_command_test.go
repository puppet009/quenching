package service

import (
	"context"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type managedFake struct {
	calls atomic.Int32
	code  *int
	err   error
	block bool
}

func TestManagedCommandStopsWhenTrainingCompletes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closed atomic.Bool
	f := &managedFake{block: true}
	s := NewManagedCommandService(oetest.NewMemoryStore(), f, func(context.Context, int64, int64) error { return nil })
	s.Root = ctx
	s.WorkspaceOpen = func(context.Context, int64) error {
		if closed.Load() {
			return errors.New("training completed")
		}
		return nil
	}
	e, err := s.Start(ctx, 1, 1, ManagedCommandRequest{Key: "before-close", Pod: "mall", Command: "sleep 100"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for f.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.calls.Load() == 0 {
		t.Fatal("command did not start")
	}
	closed.Store(true)
	for time.Now().Before(deadline) {
		current, err := s.Get(ctx, 1, 1, e.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		if oe.Terminal(current.Status) {
			if current.Status != "unknown" || current.ExitCode != nil {
				t.Fatal("cancelled execution claimed a result", current)
			}
			if _, err := s.Start(ctx, 1, 1, ManagedCommandRequest{Key: "after-close", Pod: "mall", Command: "echo changed"}); err == nil {
				t.Fatal("accepted command after training ended")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("managed command remained active after training ended")
}

func (f *managedFake) Prepare(context.Context, int64, oe.Target) (oe.Target, error) {
	return oe.Target{Pod: "mall", PodUID: "uid", Container: "app", Cwd: "/"}, nil
}
func (f *managedFake) Execute(ctx context.Context, _ oe.Target, _ string, out, stderr io.Writer) (*int, error) {
	f.calls.Add(1)
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out.Write([]byte("Authorization: Bear"))
	out.Write([]byte("er hidden-secret\ntail"))
	stderr.Write([]byte("warning"))
	return f.code, f.err
}
func waitManaged(t *testing.T, s *ManagedCommandService, id string) oe.Evidence {
	t.Helper()
	for i := 0; i < 100; i++ {
		e, err := s.Get(context.Background(), 1, 1, id)
		if err != nil {
			t.Fatal(err)
		}
		if oe.Terminal(e.Status) {
			return e
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("not completed")
	return oe.Evidence{}
}
func TestManagedCommandExecOnce(t *testing.T) {
	zero := 0
	f := &managedFake{code: &zero}
	s := NewManagedCommandService(oetest.NewMemoryStore(), f, func(context.Context, int64, int64) error { return nil })
	req := ManagedCommandRequest{Key: "key", Pod: "mall", Command: "echo test"}
	e, err := s.Start(context.Background(), 1, 1, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(context.Background(), 1, 1, req); err != nil {
		t.Fatal(err)
	}
	e = waitManaged(t, s, e.OperationID)
	if e.ExitCode == nil || *e.ExitCode != 0 || f.calls.Load() != 1 {
		t.Fatal(e, f.calls.Load())
	}
	v, _ := s.Store.Read(context.Background(), 1, e.TextRefs[0])
	if strings.Contains(v.Content, "hidden-secret") || !strings.Contains(v.Content, "tail") {
		t.Fatal(v)
	}
	req.Command = "other"
	if _, err = s.Start(context.Background(), 1, 1, req); !errors.Is(err, oe.ErrConflict) {
		t.Fatal(err)
	}
}
func TestManagedCommandNonzeroAndUnknown(t *testing.T) {
	seven := 7
	for _, f := range []*managedFake{{code: &seven}, {err: errors.New("connection lost")}} {
		s := NewManagedCommandService(oetest.NewMemoryStore(), f, func(context.Context, int64, int64) error { return nil })
		e, err := s.Start(context.Background(), 1, 1, ManagedCommandRequest{Key: "k", Pod: "mall", Command: "false"})
		if err != nil {
			t.Fatal(err)
		}
		e = waitManaged(t, s, e.OperationID)
		if f.code != nil && (e.Status != "failed" || e.ExitCode == nil || *e.ExitCode != 7) {
			t.Fatal(e)
		}
		if f.code == nil && (e.Status != "unknown" || e.ExitCode != nil) {
			t.Fatal(e)
		}
	}
}
func TestManagedCommandCancelNoReplay(t *testing.T) {
	f := &managedFake{block: true}
	s := NewManagedCommandService(oetest.NewMemoryStore(), f, func(context.Context, int64, int64) error { return nil })
	req := ManagedCommandRequest{Key: "k", Pod: "mall", Command: "sleep 9"}
	e, err := s.Start(context.Background(), 1, 1, req)
	if err != nil {
		t.Fatal(err)
	}
	s.Cancel(context.Background(), 1, 1, e.OperationID)
	e = waitManaged(t, s, e.OperationID)
	if e.Status != "unknown" {
		t.Fatal(e)
	}
	s.Start(context.Background(), 1, 1, req)
	if f.calls.Load() != 1 {
		t.Fatal(f.calls.Load())
	}
}
func TestManagedCommandBounds(t *testing.T) {
	s := NewManagedCommandService(oetest.NewMemoryStore(), &managedFake{}, func(context.Context, int64, int64) error { return nil })
	for _, r := range []ManagedCommandRequest{{Key: "k", Pod: "mall", Command: strings.Repeat("x", 16385)}, {Key: "k", Pod: "mall", Command: "true", TimeoutSeconds: 301}} {
		if _, err := s.Start(context.Background(), 1, 1, r); err == nil {
			t.Fatal("bound")
		}
	}
}
