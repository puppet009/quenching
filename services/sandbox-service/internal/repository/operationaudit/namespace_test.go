package operationaudit

import (
	"strings"
	"testing"
)

func TestSandboxStoreAlwaysUsesItsOwnedCanonicalTables(t *testing.T) {
	query := "SELECT operation_events.evidence FROM operation_events JOIN operation_intents USING(operation_id)"
	for _, legacy := range []bool{false, true} {
		got := operationSQL(query, legacy)
		if !strings.Contains(got, "sandbox_operation_events.evidence") || !strings.Contains(got, "JOIN sandbox_operation_intents") {
			t.Fatal(got)
		}
	}
}
