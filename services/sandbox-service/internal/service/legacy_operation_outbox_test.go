package service

import (
	"context"
	"encoding/json"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacyDeliveryRequiresExactProjectionAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"confirmed", `{"code":0,"data":{"ids":[11,12]}}`, 200, true},
		{"partial", `{"code":0,"data":{"ids":[11]}}`, 200, false},
		{"foreign", `{"code":0,"data":{"ids":[11,99]}}`, 200, false},
		{"failed", `{"code":0,"data":{"ids":[11,12]}}`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Internal-Token") != "secret" || r.URL.Path != "/internal/v1/operation-projections/legacy" {
					t.Error("bad routing/auth")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			err := DeliverLegacyOperations(context.Background(), server.Client(), server.URL, "secret", []oe.LegacyRow{{ID: 11}, {ID: 12}})
			if (err == nil) != tc.ok {
				t.Fatalf("ack: %v", err)
			}
		})
	}
}

func TestLegacyDeliverySplitsAcceptedLargeRowsUnderReceiverLimit(t *testing.T) {
	rows := []oe.LegacyRow{{ID: 1, Detail: []byte(`"` + strings.Repeat("x", 5<<20) + `"`)}, {ID: 2, Detail: []byte(`"` + strings.Repeat("y", 5<<20) + `"`)}}
	batches, e := LegacyProjectionBatches(rows)
	if e != nil || len(batches) != 2 {
		t.Fatalf("large reports blocked: %d %v", len(batches), e)
	}
	for _, batch := range batches {
		raw, e := json.Marshal(oe.LegacyProjection{Rows: batch})
		if e != nil || len(raw) >= 8<<20 {
			t.Fatalf("oversized delivery: %d %v", len(raw), e)
		}
	}
	oversized := oe.LegacyRow{ID: 3, Detail: []byte(`"` + strings.Repeat("z", 9<<20) + `"`)}
	batches, e = LegacyProjectionBatches([]oe.LegacyRow{oversized, rows[0]})
	if e != nil || len(batches) == 0 || batches[0][0].ReplayHash != oe.RequestHash(oversized) || batches[0][0].Detail != nil {
		t.Fatalf("historical oversized row lacks replay proof: %v", e)
	}
	for _, batch := range batches {
		raw, _ := json.Marshal(oe.LegacyProjection{Rows: batch})
		if len(raw) >= 8<<20 {
			t.Fatal("oversized historical replay")
		}
	}
}
