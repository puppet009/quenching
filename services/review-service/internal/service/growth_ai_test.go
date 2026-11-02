package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/quenching/proto/aiv1"
	rc "github.com/quenching/proto/reviewcontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"testing"
)

type growthRPCStub struct {
	aiv1.AIServiceClient
	model string
}

func (s growthRPCStub) GenerateGrowthReviewStep(ctx context.Context, request *aiv1.GrowthReviewStepRequest, _ ...grpc.CallOption) (*aiv1.GrowthReviewStepResponse, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(request.InputJson), &data); err != nil {
		return nil, err
	}
	for _, key := range []string{"sources", "fragments", "artifacts"} {
		if _, ok := data[key].([]any); !ok {
			return nil, fmt.Errorf("%s must be array", key)
		}
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	if len(md.Get("x-internal-token")) != 1 {
		return nil, fmt.Errorf("internal token absent")
	}
	raw := `{"schema_version":1,"claims":[],"coverage":{}}`
	if request.Mode == "synthesize" {
		raw = `{"schema_version":1,"overview":{},"claims":[],"ability":{"overall_level":"unknown"},"architecture":[],"practices":[]}`
	}
	return &aiv1.GrowthReviewStepResponse{ActualModel: s.model, ResultJson: raw, PromptTokens: 3, CompletionTokens: 4}, nil
}
func TestGrowthRPCUsesArraysAndChecksActualModel(t *testing.T) {
	client := &AIClient{cfg: AIClientConfig{InternalToken: "fixture"}.withDefaults(), raw: growthRPCStub{model: "qwen3.8-flash"}}
	for _, mode := range []string{"analyze", "reduce", "synthesize"} {
		out, err := client.Infer(context.Background(), GrowthInferenceInput{SessionID: 1, SnapshotHash: "frozen", Mode: mode, Metadata: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(mode, err)
		}
		if out.PromptTokens != 3 || out.CompletionTokens != 4 {
			t.Fatal("usage lost")
		}
	}
	client.raw = growthRPCStub{model: "other"}
	if _, err := client.Infer(context.Background(), GrowthInferenceInput{SessionID: 1, SnapshotHash: "frozen", Mode: "analyze", Sources: []rc.Source{}}); err == nil {
		t.Fatal("alternate model accepted")
	}
}
