package contentcontract

import "time"

type PitfallStoredRow struct {
	ID               int64
	DefectID         int64
	PitfallType      string
	Name             string
	Description      string
	TriggerCondition []byte // JSONB
	ErrorContent     string
	ConfidenceLevel  string
	RecognitionRules []byte // JSONB
	PitfallRules     []byte // JSONB
	CorrectApproach  string
	Severity         string
	Status           string
	SortOrder        int
	CreatedBy        int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
