package config

import (
	"fmt"
	"testing"
)

func TestOperationHistoryHasAuthenticatedTrainingRoute(t *testing.T) {
	for i := 0; i < 64; i++ {
		t.Setenv(fmt.Sprintf("ROUTE_%d", i), "")
	}
	for _, r := range loadRoutes() {
		if r.PathPrefix == "/api/v1/sessions" {
			if !r.Auth || r.TargetURL != "http://localhost:8086" {
				t.Fatal(r)
			}
			return
		}
	}
	t.Fatal("operation history unavailable through gateway")
}
