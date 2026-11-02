package model

import (
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"time"
)

var ErrGrowthLeaseLost = errors.New("growth review lease lost")
var ErrGrowthSourceRange = errors.New("invalid growth source range")
var ErrGrowthBusy = errors.New("growth review not due or leased")

type GrowthJobLease struct {
	ReviewID int64
	Token    string
	Fence    int64
}
type GrowthJob struct {
	ReviewID         int64            `json:"review_id"`
	Snapshot         rc.Snapshot      `json:"snapshot"`
	Phase            string           `json:"phase"`
	Cursor           rc.Cursor        `json:"cursor"`
	Version          int64            `json:"version"`
	ReduceLevel      int              `json:"reduce_level"`
	BatchIndex       int              `json:"batch_index"`
	ReduceOffset     int              `json:"reduce_offset"`
	Attempts         int              `json:"attempts"`
	RetryAt          time.Time        `json:"retry_at"`
	Report           *rc.GrowthReport `json:"report,omitempty"`
	ActualModel      string           `json:"actual_model"`
	PromptVersion    string           `json:"prompt_version"`
	PromptTokens     int64            `json:"prompt_tokens"`
	CompletionTokens int64            `json:"completion_tokens"`
	LatencyMS        float64          `json:"latency_ms"`
	LastRequestID    string           `json:"last_request_id"`
	Error            string           `json:"error,omitempty"`
}
