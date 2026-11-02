package model

import cc "github.com/quenching/proto/contentcontract"

type FaultCommand struct {
	CommandID        string   `json:"command_id"`
	SessionID        int64    `json:"session_id"`
	TemplateSlug     string   `json:"template_slug"`
	Action           string   `json:"action"`
	DefectIDs        []int64  `json:"defect_ids"`
	ParametersJSON   string   `json:"parameters_json"`
	AcceptanceDigest string   `json:"acceptance_digest"`
	ExperimentUIDs   []string `json:"experiment_uids"`
}
type FaultCommandResult struct {
	CommandID      string   `json:"command_id"`
	Status         string   `json:"status"`
	ExperimentUIDs []string `json:"experiment_uids"`
	EvidenceRefs   []string `json:"evidence_refs"`
	ResultJSON     string   `json:"result_json"`
}
type TrainableFault struct {
	DefectID         int64    `json:"defect_id"`
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	Difficulty       string   `json:"difficulty"`
	TemplateSlug     string   `json:"template_slug"`
	Capabilities     []string `json:"capabilities"`
	AcceptanceDigest string   `json:"acceptance_digest"`
}
type FaultCombination = cc.FaultCombination
