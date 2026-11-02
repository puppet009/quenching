package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type SandboxPaymentAdapter struct {
	InspectPath  string `json:"inspect_path"`
	CallbackPath string `json:"callback_path"`
	SecretKey    string `json:"secret_key"`
}

type BehaviorProtocol struct {
	PaymentAdapter  *SandboxPaymentAdapter    `json:"payment_adapter,omitempty"`
	ProtocolVersion int                       `json:"protocol_version"`
	EntryService    string                    `json:"entry_service"`
	APIService      string                    `json:"api_service"`
	Capabilities    []string                  `json:"capabilities"`
	Operations      map[string][]ProtocolStep `json:"operations"`
}
type ProtocolStep struct {
	Action string                    `json:"action"`
	Method string                    `json:"method,omitempty"`
	Path   string                    `json:"path,omitempty"`
	Form   map[string]any            `json:"form,omitempty"`
	Query  map[string]any            `json:"query,omitempty"`
	Body   any                       `json:"body,omitempty"`
	Save   map[string]string         `json:"save,omitempty"`
	Assert map[string]map[string]any `json:"assert,omitempty"`
}

func ValidateBehaviorProtocol(p BehaviorProtocol) error {
	if p.ProtocolVersion != 1 || p.EntryService == "" || p.APIService == "" || len(p.Capabilities) == 0 {
		return fmt.Errorf("invalid protocol identity")
	}
	if a := p.PaymentAdapter; a != nil {
		for _, path := range []string{a.InspectPath, a.CallbackPath} {
			if !strings.HasPrefix(path, "/sandbox/payment/") || strings.Contains(path, "..") || strings.Contains(path, ":") {
				return fmt.Errorf("invalid sandbox payment path")
			}
		}
		if a.SecretKey != "sandbox-payment-key" {
			return fmt.Errorf("invalid sandbox payment secret key")
		}
	}
	allowed := map[string]bool{"baseline": true, "register": true, "login": true, "browse": true, "cart": true, "order": true, "pay": true, "verify": true}
	seen := map[string]bool{}
	for _, capability := range p.Capabilities {
		if !allowed[capability] || seen[capability] || len(p.Operations[capability]) == 0 {
			return fmt.Errorf("invalid capability mapping")
		}
		seen[capability] = true
	}
	for operation, steps := range p.Operations {
		if !seen[operation] {
			return fmt.Errorf("unadvertised operation")
		}
		for _, step := range steps {
			switch step.Action {
			case "sandbox_pay":
				if p.PaymentAdapter == nil {
					return fmt.Errorf("sandbox payment adapter required")
				}
			case "generate_account":
			case "ui":
				if step.Path != "/" {
					return fmt.Errorf("invalid UI path")
				}
			case "http":
				if !strings.HasPrefix(step.Path, "/") || strings.HasPrefix(step.Path, "//") || strings.ContainsAny(step.Path, "\\\r\n") || strings.Contains(step.Path, "..") {
					return fmt.Errorf("external or invalid protocol path")
				}
				if step.Method != "GET" && step.Method != "POST" {
					return fmt.Errorf("invalid protocol method")
				}
			default:
				return fmt.Errorf("unsupported protocol action")
			}
			for key := range step.Save {
				if key == "__proto__" || key == "constructor" || key == "prototype" || strings.Contains(key, ".") {
					return fmt.Errorf("unsafe state key")
				}
			}
		}
	}
	return nil
}
func loadBehaviorProtocol(dir, relative string) (*BehaviorProtocol, error) {
	path := filepath.Clean(relative)
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("protocol file escapes template")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	target, err := filepath.EvalSymlinks(filepath.Join(dir, path))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("protocol symlink escapes template")
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	var protocol BehaviorProtocol
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&protocol); err != nil {
		return nil, err
	}
	return &protocol, ValidateBehaviorProtocol(protocol)
}
