package model

type BehaviorCommand struct {
	CommandID      string `json:"command_id"`
	SessionID      int64  `json:"session_id"`
	TemplateSlug   string `json:"template_slug"`
	Operation      string `json:"operation"`
	ParametersJSON string `json:"parameters_json"`
}
type BehaviorResult struct {
	CommandID    string   `json:"command_id"`
	Status       string   `json:"status"`
	EvidenceRefs []string `json:"evidence_refs"`
	ResultJSON   string   `json:"result_json"`
}
type SandboxOrder struct {
	OrderID     string `json:"order_id"`
	AccountRef  string `json:"account_ref"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}
type PaymentReceipt struct {
	CommandID    string   `json:"command_id"`
	ReceiptID    string   `json:"receipt_id"`
	Status       string   `json:"status"`
	OrderID      string   `json:"order_id"`
	AmountMinor  int64    `json:"amount_minor"`
	Currency     string   `json:"currency"`
	EvidenceRefs []string `json:"evidence_refs"`
}
