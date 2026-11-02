package service

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

type OperationOwnerCheck func(context.Context, int64, int64) (bool, error)

func TrainingOperationOwner(base, token string) OperationOwnerCheck {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, sid, uid int64) (bool, error) {
		u, e := url.Parse(base)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || token == "" || sid <= 0 || uid < 0 {
			return false, errors.New("operation owner unavailable")
		}
		req, e := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/internal/v1/sessions/%d/operation-owner?user_id=%d", strings.TrimRight(base, "/"), sid, uid), nil)
		if e != nil {
			return false, e
		}
		req.Header.Set("X-Internal-Token", token)
		resp, e := client.Do(req)
		if e != nil {
			return false, e
		}
		defer resp.Body.Close()
		var body struct {
			Code int `json:"code"`
			Data struct {
				Owned     bool  `json:"owned"`
				SessionID int64 `json:"session_id"`
				UserID    int64 `json:"user_id"`
			} `json:"data"`
		}
		if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body) != nil || body.Code != 0 || body.Data.SessionID != sid || body.Data.UserID != uid {
			return false, errors.New("operation owner query unconfirmed")
		}
		return body.Data.Owned, nil
	}
}

type CanonicalIngestPort interface {
	Ingest(context.Context, oe.Delivery) error
}

func CanonicalOperationIngest(store CanonicalIngestPort, owner OperationOwnerCheck) func(context.Context, oe.Delivery) error {
	return func(ctx context.Context, d oe.Delivery) error {
		if owner == nil {
			return errors.New("operation owner unavailable")
		}
		known, e := owner(ctx, d.Evidence.SessionID, 0)
		if e != nil {
			return e
		}
		if !known {
			return oe.ErrNotFound
		}
		return store.Ingest(ctx, d)
	}
}
