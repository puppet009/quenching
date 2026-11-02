package contentcontract

type FaultCombination struct {
	ID               int64   `json:"id"`
	DefectIDs        []int64 `json:"defect_ids"`
	TemplateSlug     string  `json:"template_slug"`
	Status           string  `json:"status"`
	AcceptanceDigest string  `json:"acceptance_digest"`
	EvidenceJSON     string  `json:"evidence_json"`
}
