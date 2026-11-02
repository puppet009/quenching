package operationevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func Deliver(ctx context.Context, client *http.Client, base, token string, d Delivery) error {
	if base == "" || token == "" {
		return fmt.Errorf("audit delivery unconfigured")
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/internal/v1/operation-evidence", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var ack struct {
		Code int `json:"code"`
		Data struct {
			EventID string `json:"event_id"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&ack); err != nil {
		return err
	}
	if resp.StatusCode != 200 || ack.Code != 0 || ack.Data.EventID != d.Evidence.EventID {
		return fmt.Errorf("audit acknowledgement rejected")
	}
	return nil
}
