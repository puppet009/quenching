package model

import "time"

type SessionConversation struct {
	ConversationID int64  `json:"conversation_id"`
	SessionID      int64  `json:"session_id"`
	SessionStatus  string `json:"session_status"`
}
type SessionMessage struct {
	ID              int64     `json:"id"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"created_at"`
	IsPitfall       bool      `json:"is_pitfall,omitempty"`
	PitfallType     string    `json:"pitfall_type,omitempty"`
	ConfidenceLevel string    `json:"confidence_level,omitempty"`
	HintLevel       int       `json:"hint_level,omitempty"`
	Tokens          int       `json:"-"`
	LatencyMS       int       `json:"-"`
	Cost            float64   `json:"-"`
}
type SessionHistoryPage struct {
	Messages     []SessionMessage `json:"messages"`
	NextBeforeID int64            `json:"next_before_id"`
	HasMore      bool             `json:"has_more"`
}
type SessionRestore struct {
	SessionConversation
	SessionHistoryPage
}
type ChatLease struct {
	Token          string
	Fence          int64
	ConversationID int64
}
type SessionMemory struct {
	Version           int64  `json:"version"`
	SchemaVersion     int    `json:"schema_version"`
	MessageWatermark  int64  `json:"message_watermark"`
	EvidenceWatermark int64  `json:"evidence_watermark"`
	MemoryJSON        string `json:"memory_json"`
	ActualModel       string `json:"actual_model"`
	PromptTokens      int64  `json:"prompt_tokens"`
	CompletionTokens  int64  `json:"completion_tokens"`
}
type MemorySource struct {
	Actor           string    `json:"actor,omitempty"`
	NextOffsetBytes int       `json:"next_offset_bytes,omitempty"`
	Sequence        int64     `json:"sequence,omitempty"`
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Role            string    `json:"role,omitempty"`
	Text            string    `json:"text"`
	OccurredAt      time.Time `json:"occurred_at"`
	ChallengeID     string    `json:"challenge_id,omitempty"`
	Truncated       bool      `json:"truncated,omitempty"`
	Successful      bool      `json:"successful,omitempty"`
}
type MemorySourcePage struct {
	NextBeforeSequence int64          `json:"next_before_sequence,omitempty"`
	Sources            []MemorySource `json:"sources"`
	MessageWatermark   int64          `json:"message_watermark"`
	EvidenceWatermark  int64          `json:"evidence_watermark"`
	HasMore            bool           `json:"has_more"`
}
type MemorySummaryInput struct {
	SessionID    int64          `json:"session_id"`
	PreviousJSON string         `json:"previous_json"`
	Sources      []MemorySource `json:"sources"`
}
type MemorySummaryResult struct {
	MemoryJSON       string
	ActualModel      string
	PromptTokens     int64
	CompletionTokens int64
}
type AssistantMemoryContext struct {
	History                  []HistoryTurn
	MemoryJSON               string
	Degraded                 bool
	CoverageMessageID        int64
	CoverageEvidenceSequence int64
}
