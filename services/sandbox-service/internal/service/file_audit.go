package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	oe "github.com/quenching/proto/operationevidence"
	"strconv"
	"strings"
	"time"
)

var ErrFileAuditUnconfirmed = errors.New("操作可能已生效，记录未确认；请核对后再操作")

type FileState struct {
	State   string
	Content []byte
	ReadErr error
	Size    int64
	SHA256  string
	Mode    string
}
type FileAudit struct {
	Store oe.Store
	Read  func(context.Context, FileTarget, string) FileState
}
type fileActorKey struct{}

func WithFileActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, fileActorKey{}, actor)
}
func (a *FileAudit) Begin(ctx context.Context, t FileTarget, action, path, destination, actor string) (oe.Evidence, error) {
	id := uuid.NewString()
	args, _ := json.Marshal(map[string]string{"action": action, "path": path, "destination": destination})
	e := oe.Evidence{SchemaVersion: 1, EventID: id + ":1", OperationID: id, SessionID: t.sessionID, Sequence: 1, Actor: actor, Kind: "file", Status: "pending", StartedAt: time.Now().UTC(), Target: oe.Target{Pod: t.pod, Container: t.container}, Arguments: args, Coverage: oe.EmptyCoverage()}
	_, err := a.Store.Begin(ctx, t.sessionID, id, id, oe.RequestHash(args), e)
	return e, err
}
func (a *FileAudit) Finish(ctx context.Context, e oe.Evidence, before, after FileState, executionErr error) error {
	v := oe.Next(e, "succeeded")
	v.Coverage.Execution = "complete"
	if executionErr != nil {
		v.Status = "unknown"
		v.Coverage.Execution = "unknown"
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "result_unconfirmed")
	}
	texts := []oe.Text{}
	states := []FileState{before, after}
	slots := []string{"before", "after"}
	covers := []string{"unknown", "unknown"}
	for i, state := range states {
		if state.State == "missing" {
			covers[i] = "complete"
		} else if state.State == "existed" && state.ReadErr == nil {
			text := oe.CaptureText(e.OperationID, slots[i], state.Content, oe.TextLimit)
			covers[i] = text.Coverage.Output
			texts = append(texts, text)
			v.TextRefs = append(v.TextRefs, text.Ref)
			v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, text.Coverage.ReasonCodes...)
		} else {
			v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "before_unavailable")
		}
	}
	var args map[string]string
	_ = json.Unmarshal(e.Arguments, &args)
	if args["action"] == "chmod" {
		for i, state := range states {
			if state.State == "existed" && state.Mode == "" {
				covers[i] = "unknown"
				v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "permission_state_unavailable")
			}
		}
	}
	v.Coverage.Before = covers[0]
	v.Coverage.After = covers[1]
	v.Result, _ = json.Marshal(map[string]any{"before_mode": before.Mode, "after_mode": after.Mode, "before_state": before.State, "after_state": after.State, "before_size": before.Size, "after_size": after.Size, "before_sha256": before.SHA256, "after_sha256": after.SHA256, "changed": before.State != "unknown" && after.State != "unknown" && (before.State != after.State || before.SHA256 != "" && before.SHA256 != after.SHA256 || before.Mode != "" && after.Mode != "" && before.Mode != after.Mode), "after_read_success": after.State == "existed" && after.ReadErr == nil})
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.Store.Append(saveCtx, v, texts)
}
func (a *FileAudit) Do(ctx context.Context, t FileTarget, action, path, destination string, fn func() error) error {
	actor, _ := ctx.Value(fileActorKey{}).(string)
	if actor == "" {
		actor = "student"
	}
	e, err := a.Begin(ctx, t, action, path, destination, actor)
	if err != nil {
		return err
	}
	before := a.Read(ctx, t, path)
	execErr := fn()
	afterPath := path
	if destination != "" {
		afterPath = destination
	}
	after := a.Read(ctx, t, afterPath)
	if err = a.Finish(ctx, e, before, after, execErr); err != nil {
		return ErrFileAuditUnconfirmed
	}
	return execErr
}
func (c *K8sClient) ReadAuditState(ctx context.Context, t FileTarget, path string) FileState {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := validatePath(path); err != nil {
		return FileState{State: "unknown", ReadErr: err}
	}
	script := `if [ ! -e "$1" ]; then printf 'missing\n'; elif [ -f "$1" ]; then n=$(wc -c < "$1") || exit 1; m=$(stat -c '%a' -- "$1" 2>/dev/null) || m=''; printf 'existed %s %s\n' "$n" "$m"; head -c 1048577 "$1"; else printf 'unknown\n'; fi`
	raw, err := c.runExec(ctx, execOptions{sessionID: t.sessionID, pod: t.pod, container: t.container, command: []string{"/bin/sh", "-c", script, "quenching-audit", path}})
	if err != nil {
		return FileState{State: "unknown", ReadErr: err}
	}
	if bytes.Equal(raw, []byte("missing\n")) {
		return FileState{State: "missing"}
	}
	i := bytes.IndexByte(raw, '\n')
	if i < 0 || !bytes.HasPrefix(raw, []byte("existed ")) {
		return FileState{State: "unknown", ReadErr: fmt.Errorf("file state unavailable")}
	}
	fields := strings.Fields(string(raw[len("existed "):i]))
	if len(fields) == 0 {
		return FileState{State: "unknown", ReadErr: fmt.Errorf("file size unavailable")}
	}
	size, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return FileState{State: "unknown", ReadErr: err}
	}
	content := raw[i+1:]
	state := FileState{State: "existed", Content: content, Size: size}
	if len(fields) > 1 {
		state.Mode = fields[1]
	}
	if int64(len(content)) == size {
		hash := sha256.Sum256(content)
		state.SHA256 = hex.EncodeToString(hash[:])
	}
	return state
}
