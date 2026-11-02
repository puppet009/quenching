package service

import (
	"context"
	"encoding/json"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	oetest "github.com/quenching/proto/operationevidence/testutil"
	"testing"
)

type fileFailStore struct {
	oe.Store
	failBegin, failAppend bool
}

func TestFileAuditPermissionChangeIsRecorded(t *testing.T) {
	store := oetest.NewMemoryStore()
	audit := &FileAudit{Store: store}
	ctx := context.Background()
	e, err := audit.Begin(ctx, NewFileTarget(1, "pod", "app"), "chmod", "/config", "", "student")
	if err != nil {
		t.Fatal(err)
	}
	before := FileState{State: "existed", Content: []byte("same"), SHA256: "same", Mode: "644"}
	after := before
	after.Mode = "600"
	if err = audit.Finish(ctx, e, before, after, nil); err != nil {
		t.Fatal(err)
	}
	v, _ := store.Get(ctx, 1, e.OperationID)
	var result map[string]any
	_ = json.Unmarshal(v.Result, &result)
	if result["before_mode"] != "644" || result["after_mode"] != "600" || result["changed"] != true {
		t.Fatal(result)
	}
}

func (s fileFailStore) Begin(ctx context.Context, sid int64, id, key, hash string, e oe.Evidence) (bool, error) {
	if s.failBegin {
		return false, errors.New("db down")
	}
	return s.Store.Begin(ctx, sid, id, key, hash, e)
}
func (s fileFailStore) Append(ctx context.Context, e oe.Evidence, texts []oe.Text) error {
	if s.failAppend {
		return errors.New("db down")
	}
	return s.Store.Append(ctx, e, texts)
}
func TestFileAuditBeforeUnknownNotNew(t *testing.T) {
	store := oetest.NewMemoryStore()
	a := &FileAudit{Store: store, Read: func(context.Context, FileTarget, string) FileState {
		return FileState{State: "unknown", ReadErr: errors.New("denied")}
	}}
	e, err := a.Begin(context.Background(), NewFileTarget(1, "mall", "app"), "write", "/order", "", "student")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Finish(context.Background(), e, FileState{State: "unknown"}, FileState{State: "existed", Content: []byte("tail")}, nil); err != nil {
		t.Fatal(err)
	}
	v, _ := store.Get(context.Background(), 1, e.OperationID)
	if v.Coverage.Before != "unknown" {
		t.Fatal(v)
	}
}
func TestFileAuditIntentFailureBlocksMutation(t *testing.T) {
	a := &FileAudit{Store: fileFailStore{Store: oetest.NewMemoryStore(), failBegin: true}, Read: func(context.Context, FileTarget, string) FileState { return FileState{State: "missing"} }}
	calls := 0
	err := a.Do(context.Background(), NewFileTarget(1, "mall", "app"), "write", "/order", "", func() error { calls++; return nil })
	if err == nil || calls != 0 {
		t.Fatal(err, calls)
	}
}
func TestFileAuditAfterSaveFailureVisible(t *testing.T) {
	s := oetest.NewMemoryStore()
	a := &FileAudit{Store: fileFailStore{Store: s, failAppend: true}, Read: func(context.Context, FileTarget, string) FileState { return FileState{State: "missing"} }}
	calls := 0
	err := a.Do(context.Background(), NewFileTarget(1, "mall", "app"), "write", "/order", "", func() error { calls++; return nil })
	if !errors.Is(err, ErrFileAuditUnconfirmed) || calls != 1 {
		t.Fatal(err, calls)
	}
}
func TestFileAuditBinaryAndLimit(t *testing.T) {
	for _, raw := range [][]byte{{0, 255}, make([]byte, oe.TextLimit+1)} {
		a := &FileAudit{Store: oetest.NewMemoryStore()}
		e, _ := a.Begin(context.Background(), NewFileTarget(1, "mall", "app"), "write", "/order", "", "student")
		if err := a.Finish(context.Background(), e, FileState{State: "missing"}, FileState{State: "existed", Content: raw}, nil); err != nil {
			t.Fatal(err)
		}
		v, _ := a.Store.Get(context.Background(), 1, e.OperationID)
		if v.Coverage.After == "complete" {
			t.Fatal(v)
		}
	}
}
