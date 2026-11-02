package operationevidence

import (
	"encoding/json"
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"regexp"
	"strings"
)

var cliSecret = regexp.MustCompile(`(?i)(--?(?:password|passwd|token|api[-_]?key|secret))\s+(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s;]+)`)

func RedactText(s string) string {
	var decoded string
	if json.Unmarshal([]byte(s), &decoded) == nil && strings.HasPrefix(strings.TrimSpace(s), `"`) {
		body, _ := json.Marshal(RedactText(decoded))
		return string(body)
	}
	// Decode before applying CLI rules: a regex over escaped JSON can consume
	// closing quotes and adjacent fields, losing evidence or invalidating it.
	clean := rc.RedactText(s)
	var value any
	if json.Unmarshal([]byte(clean), &value) == nil {
		switch value.(type) {
		case map[string]any, []any:
			body, _ := json.Marshal(redactCLILeaves(value))
			return string(body)
		}
	}
	return cliSecret.ReplaceAllString(clean, "$1 [REDACTED]")
}
func redactCLILeaves(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			v[k] = redactCLILeaves(item)
		}
	case []any:
		for i, item := range v {
			v[i] = redactCLILeaves(item)
		}
	case string:
		return RedactText(v)
	}
	return value
}
func RedactJSON(raw json.RawMessage) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, errors.New("invalid JSON")
	}
	redacted := json.RawMessage(RedactText(string(raw)))
	if !json.Valid(redacted) {
		return nil, errors.New("invalid redacted JSON")
	}
	return redacted, nil
}
func Sanitize(e Evidence) Evidence {
	e.Command = RedactText(e.Command)
	if len(e.Arguments) > 0 {
		e.Arguments, _ = RedactJSON(e.Arguments)
	}
	if len(e.Result) > 0 {
		e.Result, _ = RedactJSON(e.Result)
	}
	return e
}
