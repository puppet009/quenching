//go:build integration

// 端到端：HTTP SSE -> 编排 -> 真实 gRPC stream -> Python ai-service（PG 用内存 store 替代）。
//
// 前置：cd services/ai-service && python -m app.grpc_server.server
// 运行：go test -tags=integration ./internal/handler/ -run TestSSEEndToEnd -v
package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/quenching/training-service/internal/service"
)

func TestSSEEndToEnd(t *testing.T) {
	client, err := service.NewAIClient(service.AIClientConfig{Address: "127.0.0.1:9000", CallTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("ai client: %v", err)
	}
	defer client.Close()

	svc := service.NewChatService(newMemStore(), service.NewAIChatStreamer(client), "mock", "")
	ts := httptest.NewServer(newTestRouter(svc))
	defer ts.Close()

	// 1) 带头创建对话。
	createBody := doRequest(t, ts, http.MethodPost, "/api/v1/ai/conversations",
		`{"conversation_type":"training_assistant"}`)
	var created struct {
		Data struct {
			ConversationID int64 `json:"conversation_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatalf("解析创建结果: %v body=%s", err, createBody)
	}
	if created.Data.ConversationID == 0 {
		t.Fatalf("未返回 conversation_id: %s", createBody)
	}

	// 2) 流式发消息，逐行读 SSE。
	req, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/ai/conversations/1/messages/stream",
		strings.NewReader(`{"message":"接口大量超时怎么排查？"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "7")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	sresp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d", sresp.StatusCode)
	}
	if ct := sresp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%s", ct)
	}

	var types []string
	var deltaCount int
	r := bufio.NewReader(sresp.Body)
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			var ev struct {
				Type  string `json:"type"`
				Delta string `json:"delta"`
			}
			if jerr := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); jerr != nil {
				t.Fatalf("帧 JSON 非法: %v", jerr)
			}
			types = append(types, ev.Type)
			if ev.Type == "delta" && ev.Delta != "" {
				deltaCount++
			}
			if ev.Type == "done" {
				break
			}
			if ev.Type == "error" {
				t.Fatalf("收到错误帧: %s", line)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(types) == 0 || types[0] != "thinking" || types[len(types)-1] != "done" {
		t.Fatalf("事件序列异常: %v", types)
	}
	if deltaCount < 2 {
		t.Fatalf("应收到多个 delta 块（打字机），实际 %d，序列=%v", deltaCount, types)
	}
	t.Logf("SSE 端到端通过：%d 个事件，%d 个 delta 块", len(types), deltaCount)
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, body string) []byte {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s -> %d: %s", method, path, resp.StatusCode, data)
	}
	return data
}
