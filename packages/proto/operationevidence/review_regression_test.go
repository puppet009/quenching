package operationevidence

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactJSONKeepsCLILeafAndAdjacentFields(t *testing.T) {
	raw := json.RawMessage(`{"command":"curl --token hidden-value","keep":"tail","nested":"{\"command\":\"run --password nested-secret\",\"keep\":true}"}`)
	result, err := RedactJSON(raw)
	if err != nil || !json.Valid(result) {
		t.Fatalf("invalid redacted JSON: %s %v", result, err)
	}
	if strings.Contains(string(result), "hidden-value") || strings.Contains(string(result), "nested-secret") {
		t.Fatal("secret retained")
	}
	var fields map[string]any
	_ = json.Unmarshal(result, &fields)
	if fields["keep"] != "tail" {
		t.Fatal("adjacent evidence lost")
	}
	var nested map[string]any
	if json.Unmarshal([]byte(fields["nested"].(string)), &nested) != nil || nested["keep"] != true {
		t.Fatal("nested structure lost")
	}
}
func TestEventHashSurvivesJSONBCanonicalization(t *testing.T) {
	a := Evidence{Arguments: json.RawMessage(`{"z":1,"a":{"q":true,"b":2}}`)}
	b := Evidence{Arguments: json.RawMessage(`{ "a": {"b": 2, "q": true}, "z": 1 }`)}
	if RequestHash(a) != RequestHash(b) {
		t.Fatal("identical delivery rejected after jsonb normalization")
	}
}

func TestRedactJSONPreservesRootString(t *testing.T) {
	raw := json.RawMessage(`"run --token hidden-value"`)
	result, err := RedactJSON(raw)
	var text string
	if err != nil || json.Unmarshal(result, &text) != nil || strings.Contains(text, "hidden-value") || !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("root string lost: %s %v", result, err)
	}
}
