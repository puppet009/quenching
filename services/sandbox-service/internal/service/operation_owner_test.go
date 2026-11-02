package service

import (
	"context"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"testing"
)

type countingCanonicalStore struct{ calls int }

func (s *countingCanonicalStore) Ingest(context.Context, oe.Delivery) error { s.calls++; return nil }
func TestCanonicalIngestChecksSourceSessionBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owned bool
		err   error
	}{{"unknown", false, nil}, {"unavailable", false, errors.New("offline")}, {"known", true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &countingCanonicalStore{}
			err := CanonicalOperationIngest(store, func(_ context.Context, sid, uid int64) (bool, error) {
				if sid != 41 || uid != 0 {
					t.Fatal("wrong scope")
				}
				return tc.owned, tc.err
			})(context.Background(), oe.Delivery{Evidence: oe.Evidence{SessionID: 41}})
			if tc.owned {
				if err != nil || store.calls != 1 {
					t.Fatalf("known session: %v %d", err, store.calls)
				}
			} else if err == nil || store.calls != 0 {
				t.Fatalf("write before confirmation: %v %d", err, store.calls)
			}
		})
	}
}
