//go:build integration

// 集成测试：需要先启动 Python ai-service gRPC server：
//
//	cd services/ai-service && python -m app.grpc_server.server
//
// 运行：
//
//	go test -tags=integration ./internal/service/ -run TestAI -v
//
// 可用环境变量 AI_GRPC_ADDR 覆盖地址（默认 127.0.0.1:9000）。
package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/quenching/proto/aiv1"
)

func addr() string {
	if v := os.Getenv("AI_GRPC_ADDR"); v != "" {
		return v
	}
	return "127.0.0.1:9000"
}

func TestAIChatIntegration(t *testing.T) {
	c, err := NewAIClient(AIClientConfig{Address: addr(), CallTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	defer c.Close()

	resp, err := c.Chat(context.Background(), 1001, 2001, "CPU 使用率突然飙到 95%，怎么定位？", "A1")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Reply == "" {
		t.Fatal("empty reply")
	}
	t.Logf("Chat reply: %s", resp.Reply)
}

func TestAIGenerateReviewIntegration(t *testing.T) {
	c, err := NewAIClient(AIClientConfig{Address: addr(), CallTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	defer c.Close()

	timeline := `[{"t":0,"event":"fault injected: cpu load"},{"t":30,"event":"top check"},{"t":120,"event":"rollback"}]`
	pitfalls := `[{"type":"causal_misjudge","note":"误把表象当根因"}]`
	resp, err := c.GenerateReview(context.Background(), 1001, "cpu_load", timeline, pitfalls)
	if err != nil {
		t.Fatalf("GenerateReview: %v", err)
	}

	var sections map[string]any
	if err := json.Unmarshal([]byte(resp.SectionsJson), &sections); err != nil {
		t.Fatalf("sections not valid JSON: %v\nbody=%s", err, resp.SectionsJson)
	}
	for _, key := range []string{"root_cause", "timeline", "path_comparison", "methodology", "pitfall_analysis", "collab_review"} {
		if _, ok := sections[key]; !ok {
			t.Errorf("sections missing key %q; got=%v", key, sections)
		}
	}
	t.Logf("Review sections JSON: %s", resp.SectionsJson)
}

func TestAIChatStreamIntegration(t *testing.T) {
	c, err := NewAIClient(AIClientConfig{Address: addr(), CallTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	defer c.Close()

	stream, err := c.ChatStream(context.Background(), &aiv1.ChatRequest{
		SessionId:        1001,
		ConversationId:   2001,
		Message:          "内存泄漏怎么定位？",
		AgentLevel:       "A1",
		ConversationType: "training_assistant",
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var gotThinking, gotDelta, gotDone bool
	var text string
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch chunk.Type {
		case aiv1.ChunkType_CHUNK_TYPE_THINKING:
			gotThinking = true
		case aiv1.ChunkType_CHUNK_TYPE_DELTA:
			gotDelta = true
			text += chunk.Delta
		case aiv1.ChunkType_CHUNK_TYPE_DONE:
			gotDone = true
		case aiv1.ChunkType_CHUNK_TYPE_ERROR:
			t.Fatalf("服务端错误帧: %s", chunk.Error)
		}
	}

	if !gotThinking || !gotDone {
		t.Fatalf("缺少思考态/结束帧: thinking=%v done=%v", gotThinking, gotDone)
	}
	// mock 按 6 字切片，应收到多个 delta 块（打字机流而非一整块）。
	if !gotDelta || text == "" {
		t.Fatalf("未收到正文 delta")
	}
	t.Logf("streamed text (%d chars): %s", len(text), text)
}
