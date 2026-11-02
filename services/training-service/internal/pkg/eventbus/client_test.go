// 事件总线客户端单元测试（T1-19）。
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ========== 频道命名测试 ==========

func TestSandboxChannel(t *testing.T) {
	result := SandboxChannel("sandbox-123")
	expected := "sandbox:sandbox-123"
	if result != expected {
		t.Errorf("期望 %s, got %s", expected, result)
	}
}

func TestUserChannel(t *testing.T) {
	result := UserChannel(12345)
	expected := "user:12345"
	if result != expected {
		t.Errorf("期望 %s, got %s", expected, result)
	}
}

func TestSessionChannel(t *testing.T) {
	result := SessionChannel("sess-abc")
	expected := "session:sess-abc"
	if result != expected {
		t.Errorf("期望 %s, got %s", expected, result)
	}
}

func TestRoomChannel(t *testing.T) {
	result := RoomChannel("room-xyz")
	expected := "room:room-xyz"
	if result != expected {
		t.Errorf("期望 %s, got %s", expected, result)
	}
}

// ========== HTTP 发布器测试 ==========

func TestHTTPPublisher_PublishToChannel(t *testing.T) {
	// 创建 mock 服务器
	var received publishRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证路径
		if r.URL.Path != "/api/v1/events/publish" {
			t.Errorf("路径错误: %s", r.URL.Path)
		}
		// 验证 API Key header
		if r.Header.Get("X-Internal-API-Key") != "test-key" {
			t.Error("缺少或错误的 API Key")
		}
		// 验证方法
		if r.Method != http.MethodPost {
			t.Errorf("方法错误: %s", r.Method)
		}

		// 读取请求体
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL, "test-key")

	err := publisher.PublishToChannel(context.Background(), "sandbox:123", EventMetrics, MetricsEventData{
		SandboxID: "sandbox-123",
		Timestamp: 1234567890,
		Metrics: []MetricPoint{
			{Name: "cpu_usage", Value: 45.5, Unit: "%"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 验证请求内容
	if received.TargetType != "channel" {
		t.Errorf("target_type 应为 channel, got %s", received.TargetType)
	}
	if received.Target != "sandbox:123" {
		t.Errorf("target 应为 sandbox:123, got %s", received.Target)
	}
	if received.EventType != "metrics" {
		t.Errorf("event_type 应为 metrics, got %s", received.EventType)
	}
}

func TestHTTPPublisher_PublishToUser(t *testing.T) {
	var received publishRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL, "test-key")

	err := publisher.PublishToUser(context.Background(), 10086, EventAlert, AlertEventData{
		AlertID:  "alert-001",
		Severity: "critical",
		Message:  "CPU 使用率过高",
	})
	if err != nil {
		t.Fatal(err)
	}

	if received.TargetType != "user" {
		t.Errorf("target_type 应为 user, got %s", received.TargetType)
	}
	if received.Target != "10086" {
		t.Errorf("target 应为 10086, got %s", received.Target)
	}
}

func TestHTTPPublisher_Broadcast(t *testing.T) {
	var received publishRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL, "test-key")

	err := publisher.Broadcast(context.Background(), EventStatusChange, StatusChangeEventData{
		EntityType: "sandbox",
		EntityID:   "sandbox-123",
		NewStatus:  "running",
	})
	if err != nil {
		t.Fatal(err)
	}

	if received.TargetType != "broadcast" {
		t.Errorf("target_type 应为 broadcast, got %s", received.TargetType)
	}
	if received.Target != "" {
		t.Errorf("broadcast 的 target 应为空, got %s", received.Target)
	}
}

func TestHTTPPublisher_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL, "test-key")

	err := publisher.PublishToChannel(context.Background(), "test", EventMetrics, nil)
	if err == nil {
		t.Error("服务器错误时应返回 error")
	}
}

func TestHTTPPublisher_BusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":50001,"msg":"internal error"}`))
	}))
	defer server.Close()

	publisher := NewHTTPPublisher(server.URL, "test-key")

	err := publisher.PublishToChannel(context.Background(), "test", EventMetrics, nil)
	if err == nil {
		t.Error("业务错误时应返回 error")
	}
}

// ========== 事件数据结构测试 ==========

func TestMetricsEventData_Serialization(t *testing.T) {
	data := MetricsEventData{
		SandboxID: "sandbox-123",
		Timestamp: 1726000000000,
		Metrics: []MetricPoint{
			{Name: "cpu_usage", Value: 45.5, Unit: "%", Service: "user-service"},
			{Name: "memory_usage", Value: 62.3, Unit: "%", Service: "order-service"},
			{Name: "qps", Value: 120.5, Unit: "次/秒", Service: ""},
		},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var decoded MetricsEventData
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SandboxID != "sandbox-123" {
		t.Error("sandbox_id 不匹配")
	}
	if len(decoded.Metrics) != 3 {
		t.Errorf("应有 3 个指标, got %d", len(decoded.Metrics))
	}
	if decoded.Metrics[0].Value != 45.5 {
		t.Errorf("第一个指标值应为 45.5, got %.2f", decoded.Metrics[0].Value)
	}
}

func TestAlertEventData_Serialization(t *testing.T) {
	data := AlertEventData{
		AlertID:   "alert-001",
		SandboxID: "sandbox-123",
		Severity:  "critical",
		Metric:    "cpu_usage",
		Value:     95.5,
		Threshold: 90.0,
		Message:   "CPU 使用率超过 90%",
		Timestamp: 1726000000000,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var decoded AlertEventData
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Severity != "critical" {
		t.Errorf("级别应为 critical, got %s", decoded.Severity)
	}
	if decoded.Value != 95.5 {
		t.Errorf("值应为 95.5, got %.2f", decoded.Value)
	}
}

func TestLogLineEventData_Serialization(t *testing.T) {
	data := LogLineEventData{
		SandboxID: "sandbox-123",
		Timestamp: 1726000000000,
		Line:      "ERROR something went wrong",
		Service:   "order-service",
		Level:     "ERROR",
		Pod:       "order-7f9d8c7b5-x2k4m",
		Labels:    map[string]string{"trace_id": "abc123"},
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var decoded LogLineEventData
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Level != "ERROR" {
		t.Errorf("级别应为 ERROR, got %s", decoded.Level)
	}
	if decoded.Labels["trace_id"] != "abc123" {
		t.Error("label 不匹配")
	}
}

func TestStatusChangeEventData_Serialization(t *testing.T) {
	data := StatusChangeEventData{
		EntityType: "sandbox",
		EntityID:   "sandbox-123",
		OldStatus:  "creating",
		NewStatus:  "running",
		Reason:     "沙箱创建完成",
		Timestamp:  1726000000000,
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	var decoded StatusChangeEventData
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.NewStatus != "running" {
		t.Errorf("新状态应为 running, got %s", decoded.NewStatus)
	}
}

// 避免 fmt 未使用警告
var _ = fmt.Sprintf
