package reviewcontract

import (
	"encoding/json"
	"time"
)

type Eligibility struct {
	SessionID     int64  `json:"session_id"`
	UserID        int64  `json:"user_id"`
	ProjectID     int64  `json:"project_id"`
	SessionStatus string `json:"session_status"`
	BrainPhase    string `json:"brain_phase"`
	CleanupStatus string `json:"cleanup_status"`
	CanGenerate   bool   `json:"can_generate"`
	AuditComplete bool   `json:"audit_complete"`
}
type Source struct {
	Ordinal     int64     `json:"ordinal"`
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Actor       string    `json:"actor"`
	Role        string    `json:"role,omitempty"`
	Trust       string    `json:"trust"`
	Text        string    `json:"text"`
	ChallengeID string    `json:"challenge_id,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
	Successful  bool      `json:"successful,omitempty"`
	Incomplete  bool      `json:"incomplete,omitempty"`
	ByteLength  int       `json:"byte_length"`
}
type Snapshot struct {
	AuditCoverage     json.RawMessage `json:"audit_coverage,omitempty"`
	AuditGaps         []string        `json:"audit_gaps,omitempty"`
	SessionID         int64           `json:"session_id"`
	UserID            int64           `json:"user_id"`
	ProjectID         int64           `json:"project_id"`
	Version           int64           `json:"version"`
	Hash              string          `json:"hash"`
	CleanupStatus     string          `json:"cleanup_status"`
	AuditComplete     bool            `json:"audit_complete"`
	SourceCount       int64           `json:"source_count"`
	FrozenAt          time.Time       `json:"frozen_at"`
	MessageWatermark  int64           `json:"message_watermark"`
	EvidenceWatermark int64           `json:"evidence_watermark"`
	AssessmentVersion int64           `json:"assessment_version"`
	SourceURLs        []string        `json:"source_urls"`
	Metadata          json.RawMessage `json:"metadata"`
}
type Cursor struct {
	Ordinal     int64 `json:"ordinal"`
	OffsetBytes int   `json:"offset_bytes"`
}
type Fragment struct {
	Source          Source `json:"source"`
	OffsetBytes     int    `json:"offset_bytes"`
	NextOffsetBytes int    `json:"next_offset_bytes"`
	Complete        bool   `json:"complete"`
}
type SourcePage struct {
	Fragments []Fragment `json:"fragments"`
	Next      Cursor     `json:"next"`
	Done      bool       `json:"done"`
}
type Claim struct {
	ComparisonChallengeIDs []string `json:"comparison_challenge_ids,omitempty"`
	Section                string   `json:"section"`
	Category               string   `json:"category"`
	Text                   string   `json:"text"`
	SourceRefs             []string `json:"source_refs"`
	ChallengeID            string   `json:"challenge_id,omitempty"`
	SourceURLs             []string `json:"source_urls"`
}
type AnalysisArtifact struct {
	SchemaVersion int             `json:"schema_version"`
	Claims        []Claim         `json:"claims"`
	Coverage      json.RawMessage `json:"coverage"`
}
type ArchitectureOption struct {
	Problem       string   `json:"problem"`
	Option        string   `json:"option"`
	Preconditions string   `json:"preconditions"`
	Benefits      string   `json:"benefits"`
	Costs         string   `json:"costs"`
	Verification  string   `json:"verification"`
	SourceRefs    []string `json:"source_refs"`
}
type Practice struct {
	Dimension       string   `json:"dimension"`
	Goal            string   `json:"goal"`
	Actions         []string `json:"actions"`
	SuccessCriteria string   `json:"success_criteria"`
	SourceRefs      []string `json:"source_refs"`
	ProjectID       int64    `json:"project_id,omitempty"`
	DefectIDs       []int64  `json:"defect_ids"`
	TargetAvailable bool     `json:"target_available"`
}
type GrowthReport struct {
	SchemaVersion int                  `json:"schema_version"`
	Overview      json.RawMessage      `json:"overview"`
	Claims        []Claim              `json:"claims"`
	Ability       json.RawMessage      `json:"ability"`
	Architecture  []ArchitectureOption `json:"architecture"`
	Practices     []Practice           `json:"practices"`
}
