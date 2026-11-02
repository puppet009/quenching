package service

import (
	"github.com/quenching/go-common/templates"
	"testing"
)

func TestSandboxPaymentAdapterRejectsExternalPaths(t *testing.T) {
	p := templates.BehaviorProtocol{ProtocolVersion: 1, EntryService: "frontend", APIService: "mall-portal", Capabilities: []string{"baseline"}, Operations: map[string][]templates.ProtocolStep{"baseline": {{Action: "http", Method: "GET", Path: "/health"}}}, PaymentAdapter: &templates.SandboxPaymentAdapter{InspectPath: "https://external.example/order", CallbackPath: "/sandbox/payment/callback", SecretKey: "sandbox-payment-key"}}
	if templates.ValidateBehaviorProtocol(p) == nil {
		t.Fatal("external payment destination accepted")
	}
}
