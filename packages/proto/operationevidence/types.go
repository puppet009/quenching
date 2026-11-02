package operationevidence

import (
	"encoding/json"
	"errors"
	"time"
)

const TextLimit = 1 << 20

var ErrConflict = errors.New("operation key conflict")
var ErrNotFound = errors.New("operation not found")
var ErrClosed = errors.New("workspace closed")

type Target struct {
	Pod       string `json:"pod"`
	PodUID    string `json:"pod_uid"`
	Container string `json:"container"`
	Cwd       string `json:"cwd"`
}
type Coverage struct {
	Command       string   `json:"command"`
	Execution     string   `json:"execution"`
	Output        string   `json:"output"`
	Before        string   `json:"before"`
	After         string   `json:"after"`
	ReasonCodes   []string `json:"reason_codes"`
	OriginalBytes int64    `json:"original_bytes"`
	SavedBytes    int64    `json:"saved_bytes"`
}
type Evidence struct {
	ApprovalID     string          `json:"approval_id,omitempty"`
	SchemaVersion  int             `json:"schema_version"`
	EventID        string          `json:"event_id"`
	OperationID    string          `json:"operation_id"`
	SessionID      int64           `json:"session_id"`
	Sequence       int64           `json:"sequence"`
	Actor          string          `json:"actor"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	ConversationID int64           `json:"conversation_id,omitempty"`
	ReplyMessageID int64           `json:"reply_message_id,omitempty"`
	Round          int             `json:"round"`
	Tool           string          `json:"tool,omitempty"`
	Command        string          `json:"command,omitempty"`
	Intent         string          `json:"intent,omitempty"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Approval       string          `json:"approval,omitempty"`
	Target         Target          `json:"target"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at"`
	ExitCode       *int            `json:"exit_code"`
	Coverage       Coverage        `json:"coverage"`
	TextRefs       []string        `json:"text_refs"`
}
type Text struct {
	Ref         string   `json:"ref"`
	OperationID string   `json:"operation_id"`
	Slot        string   `json:"slot"`
	Content     string   `json:"content"`
	ByteLength  int      `json:"byte_length"`
	Coverage    Coverage `json:"coverage"`
}
type Fragment struct {
	Ref             string `json:"ref"`
	Text            string `json:"text"`
	OffsetBytes     int    `json:"offset_bytes"`
	NextOffsetBytes int    `json:"next_offset_bytes"`
	Complete        bool   `json:"complete"`
}
type AuditSeal struct {
	SessionID    int64     `json:"session_id"`
	Producer     string    `json:"producer"`
	Watermark    int64     `json:"watermark"`
	Acknowledged int64     `json:"acknowledged"`
	Pending      int       `json:"pending"`
	State        string    `json:"state"`
	Deadline     time.Time `json:"deadline"`
	Gaps         []string  `json:"gaps"`
}
type Page struct {
	Items      []Evidence `json:"items"`
	NextBefore int64      `json:"next_before"`
	HasMore    bool       `json:"has_more"`
}
type Delivery struct {
	Evidence Evidence `json:"evidence"`
	Texts    []Text   `json:"texts"`
}

func Terminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "cancelled" || status == "unknown"
}
func EmptyCoverage() Coverage {
	return Coverage{Command: "unavailable", Execution: "unknown", Output: "unavailable", Before: "unavailable", After: "unavailable", ReasonCodes: []string{}}
}
