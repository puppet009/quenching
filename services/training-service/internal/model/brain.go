package model

import (
	"github.com/quenching/proto/aiv1"
	"time"
)

type BrainToolDef = aiv1.ToolDef

type BrainPhase string

const (
	BrainSandboxReady     BrainPhase = "sandbox_ready"
	BrainBaselineCheck    BrainPhase = "baseline_check"
	BrainCalibration      BrainPhase = "calibration"
	BrainAdaptiveTraining BrainPhase = "adaptive_training"
	BrainClosing          BrainPhase = "closing"
	BrainReviewing        BrainPhase = "reviewing"
	BrainCompleted        BrainPhase = "completed"
	BrainDegraded         BrainPhase = "degraded"
	BrainFailed           BrainPhase = "failed"
)

type BrainObservation struct {
	SessionID          int64             `json:"session_id"`
	ObservationVersion int64             `json:"observation_version"`
	Phase              BrainPhase        `json:"phase"`
	EvidenceRefs       []string          `json:"evidence_refs"`
	AllowedTools       []*BrainToolDef   `json:"allowed_tools"`
	ToolResults        []BrainToolResult `json:"tool_results"`
	PreviousCalls      []*aiv1.ToolCall  `json:"previous_calls"`
	ContextJSON        string            `json:"context_json"`
}
type BrainDecision struct {
	DecisionID         string   `json:"decision_id"`
	ObservationVersion int64    `json:"observation_version"`
	LearningObjective  string   `json:"learning_objective"`
	Action             string   `json:"action"`
	ParametersJSON     string   `json:"parameters_json"`
	EvidenceRefs       []string `json:"evidence_refs"`
	ReasonSummary      string   `json:"reason_summary"`
	NextObservation    string   `json:"next_observation"`
	ActualModel        string   `json:"-"`
	RequestID          string   `json:"-"`
	PromptVersion      string   `json:"-"`
	PromptTokens       int64    `json:"-"`
	CompletionTokens   int64    `json:"-"`
	LatencyMS          float64  `json:"-"`
	Mocked             bool     `json:"-"`
}
type BrainToolResult struct {
	CommandID    string   `json:"command_id"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs"`
	ResultJSON   string   `json:"result_json"`
}
type BrainCommand struct {
	CommandID          string `json:"command_id"`
	SessionID          int64  `json:"session_id"`
	DecisionID         string `json:"decision_id"`
	ObservationVersion int64  `json:"observation_version"`
	Domain             string `json:"domain"`
	Action             string `json:"action"`
	ParametersJSON     string `json:"parameters_json"`
	Status             string `json:"status"`
}
type BrainLease struct {
	SessionID int64
	Owner     string
	Version   int64
	ExpiresAt time.Time
}
type BrainEvidence struct {
	ID               string    `json:"id"`
	SessionID        int64     `json:"session_id"`
	Sequence         int64     `json:"sequence"`
	Kind             string    `json:"kind"`
	Actor            string    `json:"actor"`
	OccurredAt       time.Time `json:"occurred_at"`
	PayloadJSON      string    `json:"payload_json"`
	ParentDecisionID string    `json:"parent_decision_id"`
}
type DimensionAssessment struct {
	Level        string   `json:"level"`
	Confidence   float64  `json:"confidence"`
	EvidenceRefs []string `json:"evidence_refs"`
}
type AbilityAssessment struct {
	OverallLevel     string                         `json:"overall_level"`
	RequestID        string                         `json:"request_id"`
	PromptVersion    string                         `json:"prompt_version"`
	PromptTokens     int64                          `json:"prompt_tokens"`
	CompletionTokens int64                          `json:"completion_tokens"`
	LatencyMS        float64                        `json:"latency_ms"`
	SessionID        int64                          `json:"session_id"`
	Version          int64                          `json:"version"`
	Dimensions       map[string]DimensionAssessment `json:"dimensions"`
	EvidenceRefs     []string                       `json:"evidence_refs"`
	Sufficiency      string                         `json:"sufficiency"`
	Model            string                         `json:"model"`
}
type BrainEvent struct {
	SessionID    int64    `json:"session_id"`
	EventID      string   `json:"event_id"`
	Source       string   `json:"source"`
	Kind         string   `json:"kind"`
	EvidenceRefs []string `json:"evidence_refs"`
}
