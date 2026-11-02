package reviewcontract

import (
	"encoding/json"
	"regexp"
	"strings"
)

var credentialText = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|secret|token|jwt|authorization)\s*["']?\s*[=:]\s*(?:(?:Bearer|Basic)\s+[a-zA-Z0-9._~+/=-]+|"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;"']+)`)
var bearerText = regexp.MustCompile(`(?i)Bearer\s+[a-zA-Z0-9._~+/=-]+`)

// RedactText sanitizes decoded leaves before JSON escaping can hide credentials.
func RedactText(text string) string {
	var parsed any
	if json.Unmarshal([]byte(text), &parsed) == nil {
		switch parsed.(type) {
		case map[string]any, []any:
			raw, _ := json.Marshal(redactValue(parsed))
			return string(raw)
		}
	}
	return bearerText.ReplaceAllString(credentialText.ReplaceAllString(text, "$1=[REDACTED]"), "Bearer [REDACTED]")
}
func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			key := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			if key == "password" || key == "passwd" || key == "authorization" || key == "api_key" || key == "apikey" || key == "secret" || key == "token" || key == "jwt" || strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_password") || strings.HasSuffix(key, "_secret") {
				x[k] = "[REDACTED]"
			} else {
				x[k] = redactValue(val)
			}
		}
	case []any:
		for i, val := range x {
			x[i] = redactValue(val)
		}
	case string:
		return RedactText(x)
	}
	return v
}
