package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
)

// ---- 测试用内存依赖（实现 service 的两个端口） ----

type memStore struct {
	convs map[int64]*model.Conversation
	msgs  map[int64][]model.HistoryTurn
	next  int64
}

func newMemStore() *memStore {
	return &memStore{convs: map[int64]*model.Conversation{}, msgs: map[int64][]model.HistoryTurn{}, next: 1}
}

func (m *memStore) CreateConversation(_ context.Context, c *model.Conversation) (int64, error) {
	id := m.next
	m.next++
	c.ID = id
	cp := *c
	cp.Status = model.ConvStatusActive
	m.convs[id] = &cp
	return id, nil
}

func (m *memStore) GetConversation(_ context.Context, id, uid int64) (*model.Conversation, error) {
	c := m.convs[id]
	if c == nil || c.UserID != uid {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (m *memStore) ListHistory(_ context.Context, id int64, _ int) ([]model.HistoryTurn, error) {
	return m.msgs[id], nil
}

func (m *memStore) AppendUserMessage(_ context.Context, id int64, content string) (int64, error) {
	m.msgs[id] = append(m.msgs[id], model.HistoryTurn{Role: model.RoleUser, Content: content})
	return int64(len(m.msgs[id])), nil
}

func (m *memStore) AppendAssistantMessage(_ context.Context, id int64, content string, _, _ int, _ float64, _ bool, _ string) (int64, error) {
	m.msgs[id] = append(m.msgs[id], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	return int64(len(m.msgs[id])), nil
}

// T3-5：SSE 测试不涉及提示落库，给端口一个与普通助手消息等价的空实现。
func (m *memStore) AppendHintMessage(_ context.Context, id int64, content string, _ int, _, _ int, _ float64) (int64, error) {
	m.msgs[id] = append(m.msgs[id], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	return int64(len(m.msgs[id])), nil
}

func (m *memStore) AppendPitfallMessage(
	_ context.Context, id int64, content string, _ int64, _, _ string,
) (int64, error) {
	m.msgs[id] = append(m.msgs[id], model.HistoryTurn{Role: model.RoleAssistant, Content: content})
	return int64(len(m.msgs[id])), nil
}

// T3-11：SSE 测试不涉及反应判定，给端口一个"永无待判定坑点"的空实现即可。
func (m *memStore) ListPendingPitfalls(_ context.Context, _ int64) ([]service.PendingPitfall, error) {
	return nil, nil
}

func (m *memStore) MarkPitfallResponse(
	_ context.Context, _, _ int64, _ string,
) (bool, error) {
	return false, nil
}

// ---- 坑点运行时 fake（T3-9 SSE 帧断言用） ----

type ssePitfallRuntime struct {
	configs []*model.PitfallConfig
}

func (r *ssePitfallRuntime) ActiveConfigs(_ context.Context, _ int64) ([]*model.PitfallConfig, error) {
	return r.configs, nil
}

func (r *ssePitfallRuntime) Snapshot(_ context.Context, _, _ int64, q string) (model.PitfallRuntimeContext, error) {
	return model.PitfallRuntimeContext{
		Turn:             1,
		Question:         q,
		TriggeredDefects: map[int64]bool{100: true},
	}, nil
}

func (r *ssePitfallRuntime) InjectedCount(_ context.Context, _, _ int64) (int, error) {
	return 0, nil
}

// OperationsSince T3-11：SSE 帧测试无操作时间线，返回空切片（判定退化为仅文本）。
func (r *ssePitfallRuntime) OperationsSince(
	_ context.Context, _ int64, _ time.Time,
) ([]model.PitfallOperation, error) {
	return nil, nil
}

type scriptStreamer struct{ events []service.StreamEvent }

func (s *scriptStreamer) StreamChat(_ context.Context, _ service.ChatStreamInput) (<-chan service.StreamEvent, error) {
	ch := make(chan service.StreamEvent, len(s.events))
	for _, e := range s.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func newTestRouter(svc *service.ChatService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewConversationHandler(svc).RegisterRoutes(r)
	return r
}

func TestSSEStreamFrames(t *testing.T) {
	store := newMemStore()
	st := &scriptStreamer{events: []service.StreamEvent{
		{Type: service.StreamThinking},
		{Type: service.StreamDelta, Delta: "你好"},
		{Type: service.StreamDelta, Delta: "世界"},
		{Type: service.StreamDone, MessageID: 42},
	}}
	svc := service.NewChatService(store, st, "mock", "")
	r := newTestRouter(svc)

	// 创建对话
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/conversations",
		strings.NewReader(`{"conversation_type":"training_assistant"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "7")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ConversationID int64 `json:"conversation_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Data.ConversationID == 0 {
		t.Fatalf("解析创建结果失败: %v body=%s", err, w.Body.String())
	}

	// 发消息走 SSE
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost,
		"/api/v1/ai/conversations/1/messages/stream", strings.NewReader(`{"message":"在吗"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-User-Id", "7")
	r.ServeHTTP(w2, req2)

	if ct := w2.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type 应为 text/event-stream，实际 %s", ct)
	}
	body := w2.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("不是 SSE 帧格式: %q", body)
	}

	// 解析所有帧
	var types []service.StreamEventType
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev service.StreamEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("帧不是合法 JSON: %v", err)
		}
		types = append(types, ev.Type)
	}
	want := []service.StreamEventType{
		service.StreamThinking, service.StreamDelta, service.StreamDelta, service.StreamDone,
	}
	if len(types) != len(want) {
		t.Fatalf("帧数错误 got=%v want=%v body=%q", types, want, body)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("第 %d 帧类型 got=%s want=%s", i, types[i], want[i])
		}
	}
	if !strings.Contains(body, "你好") || !strings.Contains(body, `"type":"done"`) ||
		!strings.Contains(body, `"message_id":`) {
		t.Fatalf("帧内容缺失: %q", body)
	}
}

func TestSSEMissingUserHeader(t *testing.T) {
	svc := service.NewChatService(newMemStore(), &scriptStreamer{}, "mock", "")
	r := newTestRouter(svc)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/conversations", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("缺少用户头应 401，实际 %d", w.Code)
	}
}

// T3-9：坑点确定性直出经 SSE 下发时，坑点标记字段必须出现在 data: 行中，
// 前端据此渲染"这是一条坑点建议"；同时不应有 LLM 路径的 thinking 帧。
func TestSSEPitfallFramesCarryMarkers(t *testing.T) {
	store := newMemStore()
	st := &scriptStreamer{} // 无任何事件：若错误走到 LLM，通道立即结束且不会有正文
	cfg := &model.PitfallConfig{
		ID: 9, DefectID: 100,
		PitfallType:     model.TypeInfoOmission,
		ErrorContent:    "不用看日志，直接重启就行",
		ConfidenceLevel: model.ConfidenceLow,
		Status:          model.StatusActive,
		Trigger: model.TriggerCondition{
			When: model.WhenAlways, Probability: 1, MaxTimes: 1,
		},
	}
	svc := service.NewChatService(store, st, "mock", "")
	svc.EnablePitfall(&ssePitfallRuntime{configs: []*model.PitfallConfig{cfg}}, service.NewEngine(nil))
	r := newTestRouter(svc)

	// 创建带 session 的训练对话（坑点仅在训练对话判定）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/ai/conversations",
		strings.NewReader(`{"conversation_type":"training_assistant","session_id":500}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "7")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodPost,
		"/api/v1/ai/conversations/1/messages/stream", strings.NewReader(`{"message":"怎么办"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-User-Id", "7")
	r.ServeHTTP(w2, req2)

	body := w2.Body.String()
	for _, want := range []string{
		`"type":"delta"`,
		`"is_pitfall":true`,
		`"pitfall_config_id":9`,
		`"pitfall_type":"info_omission"`,
		`"confidence_level":"low"`,
		`"type":"done"`,
		"不用看日志，直接重启就行",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("坑点 SSE 帧缺少 %s\nbody=%q", want, body)
		}
	}
	if strings.Contains(body, `"type":"thinking"`) {
		t.Fatalf("坑点确定性直出不应出现 LLM thinking 帧: %q", body)
	}
	// 恰好两帧 data（delta + done）
	if n := strings.Count(body, "data: "); n != 2 {
		t.Fatalf("坑点路径应只有 delta+done 两帧，实际 %d: %q", n, body)
	}
}
