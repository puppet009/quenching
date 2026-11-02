package service

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	oe "github.com/quenching/proto/operationevidence"
	"io"
	"strings"
	"sync"
	"time"
)

type ManagedCommandRequest struct {
	Key            string `json:"key"`
	Pod            string `json:"pod"`
	Container      string `json:"container"`
	Cwd            string `json:"cwd"`
	Command        string `json:"command"`
	Intent         string `json:"intent"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
type ManagedExecutor interface {
	Prepare(context.Context, int64, oe.Target) (oe.Target, error)
	Execute(context.Context, oe.Target, string, io.Writer, io.Writer) (*int, error)
}
type ManagedCommandService struct {
	Store         oe.Store
	Executor      ManagedExecutor
	Owned         func(context.Context, int64, int64) error
	WorkspaceOpen func(context.Context, int64) error
	Root          context.Context
	mu            sync.Mutex
	cancel        map[string]context.CancelFunc
}

func NewManagedCommandService(s oe.Store, x ManagedExecutor, owner func(context.Context, int64, int64) error) *ManagedCommandService {
	return &ManagedCommandService{Store: s, Executor: x, Owned: owner, Root: context.Background(), cancel: map[string]context.CancelFunc{}}
}
func (s *ManagedCommandService) Start(ctx context.Context, uid, sid int64, r ManagedCommandRequest) (oe.Evidence, error) {
	if err := s.Owned(ctx, uid, sid); err != nil {
		return oe.Evidence{}, err
	}
	if s.WorkspaceOpen != nil {
		if err := s.WorkspaceOpen(ctx, sid); err != nil {
			return oe.Evidence{}, err
		}
	}
	if r.TimeoutSeconds == 0 {
		r.TimeoutSeconds = 120
	}
	if r.Intent == "" {
		r.Intent = "diagnose"
	}
	if r.Cwd == "" {
		r.Cwd = "/"
	}
	if r.Key == "" || len(r.Key) > 128 || r.Pod == "" || strings.TrimSpace(r.Command) == "" || len(r.Command) > 16384 || strings.ContainsRune(r.Command, 0) || strings.ContainsRune(r.Cwd, 0) || !strings.HasPrefix(r.Cwd, "/") || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 300 || (r.Intent != "diagnose" && r.Intent != "repair") {
		return oe.Evidence{}, fmt.Errorf("invalid command request")
	}
	target, err := s.Executor.Prepare(ctx, sid, oe.Target{Pod: r.Pod, Container: r.Container, Cwd: r.Cwd})
	if err != nil {
		return oe.Evidence{}, err
	}
	id := uuid.NewString()
	e := oe.Evidence{SchemaVersion: 1, EventID: id + ":1", OperationID: id, SessionID: sid, Sequence: 1, Actor: "student", Kind: "command", Status: "pending", Command: r.Command, Intent: r.Intent, Target: target, StartedAt: time.Now().UTC(), Coverage: oe.EmptyCoverage()}
	e.Coverage.Command = "complete"
	applied, err := s.Store.Begin(ctx, sid, id, r.Key, oe.RequestHash(r), e)
	if err != nil {
		return oe.Evidence{}, err
	}
	if !applied {
		return s.Store.ByKey(ctx, sid, r.Key)
	}
	running := oe.Next(e, "running")
	if err = s.Store.Append(ctx, running, nil); err != nil {
		return e, err
	}
	runCtx, cancel := context.WithTimeout(s.Root, time.Duration(r.TimeoutSeconds)*time.Second)
	s.mu.Lock()
	s.cancel[id] = cancel
	s.mu.Unlock()
	go s.run(context.WithValue(runCtx, managedSessionKey{}, sid), cancel, running, r.Command)
	return oe.Sanitize(running), nil
}
func (s *ManagedCommandService) run(ctx context.Context, cancel context.CancelFunc, e oe.Evidence, command string) {
	defer cancel()
	defer func() { s.mu.Lock(); delete(s.cancel, e.OperationID); s.mu.Unlock() }()
	store, watchCancel := s.Store.(interface {
		CancelRequested(context.Context, int64, string) (bool, error)
	})
	if watchCancel || s.WorkspaceOpen != nil {
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if s.WorkspaceOpen != nil && s.WorkspaceOpen(ctx, e.SessionID) != nil {
						cancel()
						return
					}
					if watchCancel {
						requested, err := store.CancelRequested(ctx, e.SessionID, e.OperationID)
						if err != nil || requested {
							cancel()
							return
						}
					}
				}
			}
		}()
	}
	out := &boundedCommandOutput{Limit: oe.TextLimit}
	stderr := &boundedCommandOutput{Limit: oe.TextLimit}
	var code *int
	var err error
	if s.WorkspaceOpen != nil {
		err = s.WorkspaceOpen(ctx, e.SessionID)
	}
	if err == nil {
		code, err = s.Executor.Execute(ctx, e.Target, command, out, stderr)
	}
	status := "unknown"
	if code != nil {
		status = "succeeded"
		if *code != 0 {
			status = "failed"
		}
	}
	if ctx.Err() != nil {
		code = nil
		status = "unknown"
	}
	next := oe.Next(e, status)
	next.ExitCode = code
	rawOut := out.Data
	if out.Total > int64(len(rawOut)) {
		rawOut = append(append([]byte{}, rawOut...), 'x')
	}
	stdout := oe.CaptureText(e.OperationID, "stdout", rawOut, oe.TextLimit)
	budget := oe.TextLimit - len(stdout.Content)
	rawErr := stderr.Data
	if stderr.Total > int64(len(rawErr)) {
		rawErr = append(append([]byte{}, rawErr...), 'x')
	}
	errText := oe.CaptureText(e.OperationID, "stderr", rawErr, budget)
	if out.Total > int64(len(out.Data)) {
		stdout.Coverage.Output = "partial"
		stdout.Coverage.ReasonCodes = append(stdout.Coverage.ReasonCodes, "output_limit_exceeded")
	}
	if stderr.Total > int64(len(stderr.Data)) {
		errText.Coverage.Output = "partial"
		errText.Coverage.ReasonCodes = append(errText.Coverage.ReasonCodes, "output_limit_exceeded")
	}
	next.TextRefs = []string{stdout.Ref, errText.Ref}
	next.Coverage.Output = "complete"
	next.Coverage.Execution = "complete"
	if code == nil {
		next.Coverage.Execution = "unknown"
		next.Coverage.ReasonCodes = append(next.Coverage.ReasonCodes, "result_unconfirmed")
	}
	for _, v := range []oe.Text{stdout, errText} {
		if v.Coverage.Output != "complete" {
			next.Coverage.Output = "partial"
		}
		next.Coverage.ReasonCodes = append(next.Coverage.ReasonCodes, v.Coverage.ReasonCodes...)
		next.Coverage.SavedBytes += int64(v.ByteLength)
	}
	next.Coverage.OriginalBytes = out.Total + stderr.Total
	if err != nil {
		next.Result = []byte(`{"execution_error":true}`)
	}
	saveCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if saveErr := s.Store.Append(saveCtx, next, []oe.Text{stdout, errText}); saveErr != nil {
		unknown := oe.Next(e, "unknown")
		unknown.Coverage.Execution = "unknown"
		unknown.Coverage.ReasonCodes = append(unknown.Coverage.ReasonCodes, "result_unconfirmed")
		_ = s.Store.Append(saveCtx, unknown, nil)
	}
}
func (s *ManagedCommandService) Get(ctx context.Context, uid, sid int64, id string) (oe.Evidence, error) {
	if err := s.Owned(ctx, uid, sid); err != nil {
		return oe.Evidence{}, err
	}
	return s.Store.Get(ctx, sid, id)
}
func (s *ManagedCommandService) Cancel(ctx context.Context, uid, sid int64, id string) (oe.Evidence, error) {
	e, err := s.Get(ctx, uid, sid, id)
	if err != nil {
		return e, err
	}
	if store, ok := s.Store.(interface {
		RequestCancel(context.Context, int64, string) error
	}); ok {
		if err = store.RequestCancel(ctx, sid, id); err != nil {
			return e, err
		}
	}
	s.mu.Lock()
	cancel := s.cancel[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return e, nil
}

type boundedCommandOutput struct {
	mu    sync.Mutex
	Data  []byte
	Total int64
	Limit int
}

func (w *boundedCommandOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Total += int64(len(p))
	n := w.Limit - len(w.Data)
	if n > len(p) {
		n = len(p)
	}
	if n > 0 {
		w.Data = append(w.Data, p[:n]...)
	}
	return len(p), nil
}
