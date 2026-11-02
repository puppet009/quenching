package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type LegacyOperationOutbox interface {
	Pending(context.Context) ([]oe.LegacyRow, error)
	Delivered(context.Context, []int64) error
}

// A report fits the receiver individually; replay older oversized rows by a
// hash that the receiver may acknowledge only against its migrated full copy.
func LegacyProjectionBatches(rows []oe.LegacyRow) ([][]oe.LegacyRow, error) {
	const budget = 7 << 20
	var batches [][]oe.LegacyRow
	var batch []oe.LegacyRow
	size := len(`{"rows":[]}`)
	for _, row := range rows {
		raw, e := json.Marshal(row)
		if e != nil {
			return nil, e
		}
		if len(raw)+len(`{"rows":[]}`) >= 8<<20 {
			row.ReplayHash = oe.RequestHash(row)
			row.Detail = nil
			raw, e = json.Marshal(row)
			if e != nil {
				return nil, e
			}
		}
		if len(batch) > 0 && size+len(raw)+1 > budget {
			batches = append(batches, batch)
			batch = nil
			size = len(`{"rows":[]}`)
		}
		batch = append(batch, row)
		size += len(raw) + 1
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches, nil
}

func DeliverLegacyOperations(ctx context.Context, client *http.Client, base, token string, rows []oe.LegacyRow) error {
	if base == "" || token == "" || len(rows) == 0 {
		return errors.New("legacy projection delivery unavailable")
	}
	raw, e := json.Marshal(oe.LegacyProjection{Rows: rows})
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/internal/v1/operation-projections/legacy", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("X-Internal-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	var body struct {
		Code int `json:"code"`
		Data struct {
			IDs []int64 `json:"ids"`
		} `json:"data"`
	}
	want := []int64{}
	for _, row := range rows {
		want = append(want, row.ID)
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body) != nil || body.Code != 0 || !reflect.DeepEqual(body.Data.IDs, want) {
		return errors.New("projection acknowledgement rejected")
	}
	return nil
}
func RunLegacyOperationOutbox(ctx context.Context, repo LegacyOperationOutbox, base, token string) {
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		rows, e := repo.Pending(ctx)
		if e != nil || len(rows) == 0 {
			continue
		}
		batches, e := LegacyProjectionBatches(rows)
		if e != nil {
			continue
		}
		for _, batch := range batches {
			if DeliverLegacyOperations(ctx, client, base, token, batch) != nil {
				continue
			}
			ids := []int64{}
			for _, row := range batch {
				ids = append(ids, row.ID)
			}
			_ = repo.Delivered(ctx, ids)
		}
	}
}
