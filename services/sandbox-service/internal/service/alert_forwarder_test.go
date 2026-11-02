// AlertClient 单元测试（第三批）：验证转发 / 注册请求的路径、请求体、
// X-Internal-API-Key 头、非 2xx 报错，以及 BaseURL 留空不启用。
package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// NewAlertClient("") 返回 nil；nil 接收者上 Enabled 安全返回 false。
func TestNewAlertClient_DisabledByDefault(t *testing.T) {
	c := NewAlertClient("", "token")
	if c != nil {
		t.Fatal("BaseURL 为空应返回 nil")
	}
	var nilClient *AlertClient
	if nilClient.Enabled() {
		t.Error("nil 客户端 Enabled 应为 false")
	}
}

// Forward：POST 到 events 路径，携带完整信号 JSON 与鉴权头。
func TestAlertClient_Forward(t *testing.T) {
	var (
		gotPath  string
		gotToken string
		gotBody  map[string]interface{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Internal-API-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAlertClient(srv.URL, "secret-key")
	sig := BehaviorSignal{
		SandboxID: 1001,
		SessionID: 1001,
		Type:      SignalHTTPError,
		Severity:  "error",
		URL:       "/api/orders",
		Status:    500,
		Step:      "下单",
		Message:   "Internal Server Error",
	}
	if err := c.Forward(context.Background(), sig); err != nil {
		t.Fatalf("转发不应报错: %v", err)
	}

	if gotPath != "/api/v1/internal/alerts/events" {
		t.Errorf("路径应为 /api/v1/internal/alerts/events, got %q", gotPath)
	}
	if gotToken != "secret-key" {
		t.Errorf("应携带 X-Internal-API-Key, got %q", gotToken)
	}
	if gotBody["type"] != SignalHTTPError {
		t.Errorf("请求体 type 不匹配, got %v", gotBody["type"])
	}
	if gotBody["sandbox_id"].(float64) != 1001 {
		t.Errorf("请求体 sandbox_id 不匹配, got %v", gotBody["sandbox_id"])
	}
	if gotBody["status"].(float64) != 500 {
		t.Errorf("请求体 status 不匹配, got %v", gotBody["status"])
	}
}

// Register：POST 到 register 路径，请求体只带 sandbox_id。
func TestAlertClient_Register(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]int64
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewAlertClient(srv.URL, "")
	if !c.Enabled() {
		t.Error("已配置 BaseURL 应 Enabled")
	}
	if err := c.Register(context.Background(), 2002); err != nil {
		t.Fatalf("注册不应报错: %v", err)
	}

	if gotPath != "/api/v1/internal/alerts/register" {
		t.Errorf("路径应为 /api/v1/internal/alerts/register, got %q", gotPath)
	}
	if gotBody["sandbox_id"] != 2002 {
		t.Errorf("请求体 sandbox_id 应为 2002, got %d", gotBody["sandbox_id"])
	}
}

// 非 2xx 响应返回错误。
func TestAlertClient_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewAlertClient(srv.URL, "wrong")
	if err := c.Register(context.Background(), 1); err == nil {
		t.Error("401 响应应返回错误")
	}
}
