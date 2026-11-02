package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	oe "github.com/quenching/proto/operationevidence"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func LegacyProjectionReadiness(base, token string) func(context.Context, int64) error {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, sid int64) error {
		u, e := url.Parse(base)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || token == "" || sid <= 0 {
			return errors.New("operation checkpoint unavailable")
		}
		for {
			req, e := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/internal/v1/operations/%d/projection-checkpoint", strings.TrimRight(base, "/"), sid), nil)
			if e != nil {
				return e
			}
			req.Header.Set("X-Internal-Token", token)
			resp, e := client.Do(req)
			if e != nil {
				return e
			}
			var body struct {
				Code int                 `json:"code"`
				Data oe.LegacyCheckpoint `json:"data"`
			}
			e = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body)
			resp.Body.Close()
			if resp.StatusCode != 200 || e != nil || body.Code != 0 || body.Data.SessionID != sid || body.Data.Watermark < 0 || body.Data.Pending < 0 {
				return errors.New("operation checkpoint unconfirmed")
			}
			if body.Data.Pending == 0 {
				return nil
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
