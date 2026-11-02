package service

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
	"regexp"
	"strings"
	"time"
)

var errBrainDomainRejected = errors.New("domain definitively rejected command")

type BrainCommandLookup interface {
	LookupCommand(context.Context, string) (*model.BrainCommand, error)
}
type BrainDomainClients struct {
	sandboxURL, faultURL, token string
	lookup                      BrainCommandLookup
	http                        *http.Client
}

func NewBrainDomainClients(sandboxURL, faultURL, token string, lookup BrainCommandLookup) *BrainDomainClients {
	return &BrainDomainClients{sandboxURL: strings.TrimRight(sandboxURL, "/"), faultURL: strings.TrimRight(faultURL, "/"), token: token, lookup: lookup, http: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var brainCommandID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func (c *BrainDomainClients) commandURL(domain string, cmd model.BrainCommand, withID bool) (string, error) {
	if !brainCommandID.MatchString(cmd.CommandID) || cmd.SessionID <= 0 {
		return "", errors.New("invalid command scope")
	}
	var base, path string
	switch domain {
	case "sandbox":
		base = c.sandboxURL
		path = fmt.Sprintf("/internal/v1/sandboxes/%d/behavior/commands", cmd.SessionID)
	case "fault":
		base = c.faultURL
		path = "/internal/v1/faults/commands"
	default:
		return "", errors.New("unknown domain")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("domain endpoint unavailable")
	}
	if withID {
		path += "/" + url.PathEscape(cmd.CommandID)
	}
	return base + path, nil
}
func (c *BrainDomainClients) request(ctx context.Context, method, endpoint string, body any) (model.BrainToolResult, error) {
	if c.token == "" {
		return model.BrainToolResult{}, errors.New("internal authorization unavailable")
	}
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return model.BrainToolResult{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return model.BrainToolResult{}, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return model.BrainToolResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var refusal struct {
			Rejected bool `json:"command_rejected"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&refusal) == nil && refusal.Rejected {
			return model.BrainToolResult{}, errBrainDomainRejected
		}
		return model.BrainToolResult{}, fmt.Errorf("domain request status %d", resp.StatusCode)
	}
	var reply struct {
		Code int                   `json:"code"`
		Data model.BrainToolResult `json:"data"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024))
	if err = dec.Decode(&reply); err != nil {
		return model.BrainToolResult{}, err
	}
	if reply.Code != 0 {
		return model.BrainToolResult{}, errors.New("domain request failed")
	}
	return reply.Data, nil
}
func (c *BrainDomainClients) GetCommand(ctx context.Context, domain, commandID string) (model.BrainToolResult, error) {
	if c.lookup == nil {
		return model.BrainToolResult{}, errors.New("command lookup unavailable")
	}
	cmd, err := c.lookup.LookupCommand(ctx, commandID)
	if err != nil {
		return model.BrainToolResult{}, err
	}
	if cmd == nil || cmd.CommandID != commandID || cmd.Domain != domain {
		return model.BrainToolResult{}, errors.New("command ownership mismatch")
	}
	endpoint, err := c.commandURL(domain, *cmd, true)
	if err != nil {
		return model.BrainToolResult{}, err
	}
	result, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err == nil && result.CommandID != commandID {
		err = errors.New("foreign command result")
	}
	return result, err
}
func (c *BrainDomainClients) Submit(ctx context.Context, cmd model.BrainCommand, payload any) (model.BrainToolResult, error) {
	endpoint, err := c.commandURL(cmd.Domain, cmd, false)
	if err != nil {
		return model.BrainToolResult{}, err
	}
	result, err := c.request(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		if errors.Is(err, errBrainDomainRejected) {
			return model.BrainToolResult{CommandID: cmd.CommandID, Status: "failed", ResultJSON: `{"reason":"domain rejected command before business mutation"}`}, err
		}
		return model.BrainToolResult{CommandID: cmd.CommandID, Status: "unknown", ResultJSON: `{"reason":"submission outcome unknown; query status before retry"}`}, err
	}
	if result.CommandID != cmd.CommandID {
		return model.BrainToolResult{}, errors.New("foreign command result")
	}
	return result, nil
}

func (c *BrainDomainClients) ListTrainableFaults(ctx context.Context, slug string) (json.RawMessage, error) {
	if c.token == "" || slug == "" {
		return nil, errors.New("fault catalog unavailable")
	}
	catalog := map[string]json.RawMessage{}
	for key, suffix := range map[string]string{"faults": "", "combinations": "/combinations"} {
		req, e := http.NewRequestWithContext(ctx, "GET", c.faultURL+"/internal/v1/faults/trainable/"+url.PathEscape(slug)+suffix, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("X-Internal-Token", c.token)
		res, e := c.http.Do(req)
		if e != nil {
			return nil, e
		}
		var reply struct {
			Code int             `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&reply)
		res.Body.Close()
		if res.StatusCode != 200 || decodeErr != nil || reply.Code != 0 {
			return nil, errors.New("fault catalog unavailable")
		}
		catalog[key] = reply.Data
	}
	return json.Marshal(catalog)
}
