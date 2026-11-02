package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/proto/aiv1"
	"github.com/quenching/training-service/internal/model"
	"google.golang.org/grpc/metadata"
	"time"
)

func (c *AIClient) Summarize(parent context.Context, in model.MemorySummaryInput) (model.MemorySummaryResult, error) {
	out := model.MemorySummaryResult{}
	if c.cfg.InternalToken == "" {
		return out, errors.New("memory internal token unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, 65*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-internal-token", c.cfg.InternalToken)
	raw, e := json.Marshal(in)
	if e != nil {
		return out, e
	}
	res, e := c.raw.SummarizeSessionMemory(ctx, &aiv1.SessionMemoryRequest{SessionId: in.SessionID, PublicInputJson: string(raw)})
	if e != nil {
		return out, e
	}
	if res.Mocked || res.ActualModel != "qwen3.8-flash" {
		return out, errors.New("real qwen3.8-flash memory required")
	}
	out = model.MemorySummaryResult{MemoryJSON: res.MemoryJson, ActualModel: res.ActualModel, PromptTokens: res.PromptTokens, CompletionTokens: res.CompletionTokens}
	return out, nil
}
