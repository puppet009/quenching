package templates

import (
	"testing"
)

func TestProtocolCapabilities(t *testing.T) {
	p := BehaviorProtocol{ProtocolVersion: 1, EntryService: "frontend", APIService: "mall-portal", Capabilities: []string{"register"}, Operations: map[string][]ProtocolStep{"register": {{Action: "http", Method: "POST", Path: "/sso/register"}}}}
	if err := ValidateBehaviorProtocol(p); err != nil {
		t.Fatal(err)
	}
	p.Capabilities = append(p.Capabilities, "order")
	if ValidateBehaviorProtocol(p) == nil {
		t.Fatal("advertised operation without mapping")
	}
}
func TestRejectExternalProtocolURL(t *testing.T) {
	for _, path := range []string{"https://production.example/pay", "//production.example/pay"} {
		p := BehaviorProtocol{ProtocolVersion: 1, EntryService: "frontend", APIService: "mall-portal", Capabilities: []string{"baseline"}, Operations: map[string][]ProtocolStep{"baseline": {{Action: "http", Method: "GET", Path: path}}}}
		if ValidateBehaviorProtocol(p) == nil {
			t.Fatal("external protocol target accepted")
		}
	}
}
