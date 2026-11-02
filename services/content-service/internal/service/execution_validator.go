package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func FaultExecutionValidator(base, token string) func(context.Context, json.RawMessage, bool) error {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, config json.RawMessage, pod bool) error {
		if base == "" || token == "" {
			return errors.New("执行器校验未配置")
		}
		raw, err := json.Marshal(map[string]any{"configuration": config, "pod_required": pod})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/internal/v1/faults/validate-config", bytes.NewReader(raw))
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
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Valid bool `json:"valid"`
			} `json:"data"`
		}
		if err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body); err != nil {
			return err
		}
		if resp.StatusCode != 200 || body.Code != 0 || !body.Data.Valid {
			if body.Msg != "" {
				return errors.New(body.Msg)
			}
			return errors.New("执行器拒绝该配置")
		}
		return nil
	}
}
