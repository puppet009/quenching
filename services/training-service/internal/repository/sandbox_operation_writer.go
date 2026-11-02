package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/service"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SandboxOperationWriter struct {
	base, token string
	client      *http.Client
}

func NewSandboxOperationWriter(base, token string) *SandboxOperationWriter {
	return &SandboxOperationWriter{strings.TrimRight(base, "/"), token, &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (w *SandboxOperationWriter) Record(ctx context.Context, sid, uid int64, ops []service.OperationInput) (oe.LegacyReceipt, error) {
	var receipt oe.LegacyReceipt
	u, e := url.Parse(w.base)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || w.token == "" || sid <= 0 || uid <= 0 {
		return receipt, errors.New("sandbox operation writer unavailable")
	}
	batch := oe.LegacyBatch{RequestID: uuid.NewString(), SessionID: sid, UserID: uid, Operations: ops}
	raw, e := json.Marshal(batch)
	if e != nil {
		return receipt, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", w.base+"/internal/v1/operations/report", bytes.NewReader(raw))
	if e != nil {
		return receipt, e
	}
	req.Header.Set("X-Internal-Token", w.token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := w.client.Do(req)
	if e != nil {
		return receipt, e
	}
	defer resp.Body.Close()
	var body struct {
		Code int              `json:"code"`
		Data oe.LegacyReceipt `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&body) != nil || body.Code != 0 {
		return receipt, errors.New("sandbox operation report unconfirmed")
	}
	receipt = body.Data
	if receipt.RequestID != batch.RequestID || receipt.SessionID != sid || receipt.UserID != uid || receipt.Written != len(ops) || receipt.Total < int64(receipt.Written) {
		return oe.LegacyReceipt{}, errors.New("operation receipt identity mismatch")
	}
	return receipt, nil
}
