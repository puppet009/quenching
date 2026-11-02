package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/sandbox-service/internal/model"
	"net/http"
	"strings"
	"time"
)

type BehaviorDelivery struct {
	SessionID int64
	Result    model.BehaviorResult
}
type BehaviorOutboxStore interface {
	PendingResults(context.Context) ([]BehaviorDelivery, error)
	MarkDelivered(context.Context, string, string) error
}

func BuildBehaviorEvidenceEvent(sid int64, result model.BehaviorResult) ([]byte, error) {
	if sid <= 0 || !behaviorCommandID.MatchString(result.CommandID) {
		return nil, errors.New("invalid result identity")
	}
	var payload map[string]any
	if json.Unmarshal([]byte(result.ResultJSON), &payload) != nil || payload == nil {
		return nil, errors.New("invalid business result")
	}
	payload["command_id"] = result.CommandID
	payload["status"] = result.Status
	// Different outcomes of an initially unknown command are separate immutable evidence.
	id := "behavior:" + result.CommandID + ":" + result.Status
	raw, _ := json.Marshal(payload)
	return json.Marshal(map[string]any{"event": map[string]any{"session_id": sid, "source": "sandbox", "event_id": id, "kind": "business_result", "evidence_refs": []string{id}}, "evidence": []any{map[string]any{"id": id, "session_id": sid, "kind": "business_result", "actor": "simulator", "payload_json": string(raw)}}})
}
func RunBehaviorOutbox(ctx context.Context, store BehaviorOutboxStore, baseURL, token string) {
	if baseURL == "" || token == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			deliveries, err := store.PendingResults(ctx)
			if err != nil {
				continue
			}
			for _, delivery := range deliveries {
				raw, err := BuildBehaviorEvidenceEvent(delivery.SessionID, delivery.Result)
				if err != nil {
					continue
				}
				req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+"/internal/v1/brain/events", bytes.NewReader(raw))
				if err != nil {
					continue
				}
				req.Header.Set("X-Internal-Token", token)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				var ack struct {
					Code int `json:"code"`
				}
				decodeErr := json.NewDecoder(resp.Body).Decode(&ack)
				resp.Body.Close()
				if resp.StatusCode == 200 && decodeErr == nil && ack.Code == 0 {
					_ = store.MarkDelivered(ctx, delivery.Result.CommandID, delivery.Result.Status)
				}
			}
		}
	}
}
