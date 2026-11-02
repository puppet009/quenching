package operationevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type Store interface {
	Begin(context.Context, int64, string, string, string, Evidence) (bool, error)
	Append(context.Context, Evidence, []Text) error
	Get(context.Context, int64, string) (Evidence, error)
	ByKey(context.Context, int64, string) (Evidence, error)
	List(context.Context, int64, int64, int) (Page, error)
	Read(context.Context, int64, string) (Text, error)
}

func RequestHash(v any) string {
	raw, _ := json.Marshal(v)
	// JSONB normalizes object ordering/whitespace, including nested RawMessage.
	// Preserve numeric precision while comparing semantic delivery identities.
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&canonical) == nil {
		raw, _ = json.Marshal(canonical)
	}
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}
func Next(e Evidence, status string) Evidence {
	e.Sequence++
	e.EventID = fmt.Sprintf("%s:%d", e.OperationID, e.Sequence)
	e.Status = status
	if Terminal(status) {
		now := time.Now().UTC()
		e.FinishedAt = &now
	}
	return e
}
func ValidateTransition(previous, next Evidence) error {
	if err := Validate(next); err != nil {
		return err
	}
	if previous.OperationID != next.OperationID || previous.SessionID != next.SessionID || previous.Actor != next.Actor || previous.Kind != next.Kind || previous.ToolCallID != next.ToolCallID {
		return ErrConflict
	}
	if next.Sequence != previous.Sequence+1 || Terminal(previous.Status) {
		return ErrConflict
	}
	return nil
}
