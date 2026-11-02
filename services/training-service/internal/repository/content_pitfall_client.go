package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/training-service/internal/model"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ContentPitfallReader struct {
	base, token string
	client      *http.Client
}

func NewContentPitfallReader(base, token string) *ContentPitfallReader {
	return &ContentPitfallReader{strings.TrimRight(base, "/"), token, &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (r *ContentPitfallReader) call(ctx context.Context, method, path string, in, out any) error {
	u, e := url.Parse(r.base)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || r.token == "" {
		return errors.New("content pitfall catalog unavailable")
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, method, r.base+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("X-Internal-Token", r.token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := r.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	var body struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body) != nil || body.Code != 0 || len(body.Data) == 0 || string(body.Data) == "null" {
		return errors.New("content pitfall catalog rejected query")
	}
	return json.Unmarshal(body.Data, out)
}
func (r *ContentPitfallReader) ListActiveByDefectIDs(ctx context.Context, ids []int64) ([]model.PitfallStoredRow, error) {
	out := []model.PitfallStoredRow{}
	if len(ids) == 0 {
		return out, nil
	}
	e := r.call(ctx, "POST", "/internal/v1/catalog/pitfalls/by-defects", map[string]any{"ids": ids}, &out)
	if e != nil {
		return nil, e
	}
	allowed := map[int64]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	for _, row := range out {
		if row.ID <= 0 || !allowed[row.DefectID] || row.Status != "active" {
			return nil, errors.New("pitfall catalog response outside requested scope")
		}
	}
	return out, nil
}
func (r *ContentPitfallReader) GetByID(ctx context.Context, id int64) (*model.PitfallStoredRow, error) {
	var out model.PitfallStoredRow
	if e := r.call(ctx, "GET", fmt.Sprintf("/internal/v1/catalog/pitfalls/%d", id), nil, &out); e != nil {
		return nil, e
	}
	if out.ID != id {
		return nil, errors.New("pitfall identity mismatch")
	}
	return &out, nil
}
