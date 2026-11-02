package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/proto/aiv1"
	rc "github.com/quenching/proto/reviewcontract"
	"google.golang.org/grpc/metadata"
	"io"
)

type GrowthInferenceInput struct {
	SessionID    int64                 `json:"session_id"`
	SnapshotHash string                `json:"snapshot_hash"`
	Mode         string                `json:"mode"`
	Sources      []rc.Source           `json:"sources"`
	Fragments    []rc.Fragment         `json:"fragments"`
	Artifacts    []rc.AnalysisArtifact `json:"artifacts"`
	Metadata     json.RawMessage       `json:"metadata"`
}
type GrowthInferenceResult struct {
	Artifact                              *rc.AnalysisArtifact
	Report                                *rc.GrowthReport
	ActualModel, RequestID, PromptVersion string
	PromptTokens, CompletionTokens        int64
	LatencyMS                             float64
}
type GrowthAI interface {
	Infer(context.Context, GrowthInferenceInput) (GrowthInferenceResult, error)
}

func strictGrowthJSON(raw string, out any) error {
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF {
		return fmt.Errorf("trailing growth JSON")
	}
	return nil
}
func (c *AIClient) Infer(ctx context.Context, in GrowthInferenceInput) (GrowthInferenceResult, error) {
	var out GrowthInferenceResult
	if in.Sources == nil {
		in.Sources = []rc.Source{}
	}
	if in.Fragments == nil {
		in.Fragments = []rc.Fragment{}
	}
	if in.Artifacts == nil {
		in.Artifacts = []rc.AnalysisArtifact{}
	}
	if c.cfg.InternalToken == "" || in.SessionID <= 0 || in.SnapshotHash == "" {
		return out, fmt.Errorf("growth inference requires internal token and frozen identity")
	}
	if in.Mode != "analyze" && in.Mode != "reduce" && in.Mode != "synthesize" {
		return out, fmt.Errorf("invalid growth mode")
	}
	data, err := json.Marshal(in)
	if err != nil || len(data) > 80*1024 {
		return out, fmt.Errorf("growth input exceeds budget")
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-internal-token", c.cfg.InternalToken)
	resp, err := c.raw.GenerateGrowthReviewStep(ctx, &aiv1.GrowthReviewStepRequest{SessionId: in.SessionID, Mode: in.Mode, InputJson: string(data), SnapshotHash: in.SnapshotHash})
	if err != nil {
		return out, fmt.Errorf("ai growth step: %w", err)
	}
	if resp.GetMocked() || resp.GetActualModel() != "qwen3.8-flash" {
		return out, fmt.Errorf("actual qwen3.8-flash required")
	}
	sources := append([]rc.Source{}, in.Sources...)
	for _, f := range in.Fragments {
		sources = append(sources, f.Source)
	}
	var meta struct {
		SourceURLs    []string `json:"source_urls"`
		AuditComplete bool     `json:"audit_complete"`
		AuditGaps     []string `json:"audit_gaps"`
	}
	_ = json.Unmarshal(in.Metadata, &meta)
	if in.Mode == "synthesize" {
		if len(resp.GetResultJson()) > 48*1024 {
			return out, fmt.Errorf("report exceeds budget")
		}
		out.Report = &rc.GrowthReport{}
		err = strictGrowthJSON(resp.GetResultJson(), out.Report)
		if err == nil {
			err = ValidateGrowthReport(*out.Report, rc.Snapshot{SourceURLs: meta.SourceURLs, AuditComplete: meta.AuditComplete, AuditGaps: meta.AuditGaps}, sources)
		}
	} else {
		if len(resp.GetResultJson()) > 16*1024 {
			return out, fmt.Errorf("artifact exceeds budget")
		}
		out.Artifact = &rc.AnalysisArtifact{}
		err = strictGrowthJSON(resp.GetResultJson(), out.Artifact)
		if err == nil && out.Artifact.SchemaVersion != 1 {
			err = fmt.Errorf("invalid artifact schema")
		}
		if err == nil {
			err = ValidateGrowthClaims(out.Artifact.Claims, sources, meta.SourceURLs)
		}
		out.Artifact.Coverage = json.RawMessage(`{}`)
	}
	if err != nil {
		return GrowthInferenceResult{}, err
	}
	// Sanitize every decoded output leaf before any checkpoint or report is persisted.
	if out.Artifact != nil {
		raw, _ := json.Marshal(out.Artifact)
		_ = json.Unmarshal([]byte(rc.RedactText(string(raw))), out.Artifact)
	}
	if out.Report != nil {
		raw, _ := json.Marshal(out.Report)
		_ = json.Unmarshal([]byte(rc.RedactText(string(raw))), out.Report)
	}
	out.ActualModel = resp.GetActualModel()
	out.RequestID = resp.GetRequestId()
	out.PromptVersion = resp.GetPromptVersion()
	out.PromptTokens = resp.GetPromptTokens()
	out.CompletionTokens = resp.GetCompletionTokens()
	out.LatencyMS = resp.GetLatencyMs()
	return out, nil
}
