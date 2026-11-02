// HTTP 次生故障执行通道单测（T3-12）。
// 用 httptest 模拟 fault-engine，验证请求契约（路径/JSON 字段）、响应解析、
// 默认命名空间兜底、业务错误与非 200 状态的处理。本机无需真实故障引擎。
package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newEnvelopeServer 返回记录请求体并按给定响应信封应答的测试服务。
func newEnvelopeServer(t *testing.T, status int, resp faultRuntimeEnvelope, gotBody any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if gotBody != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, gotBody); err != nil {
				t.Fatalf("服务端解析请求体失败: %v body=%s", err, string(raw))
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestFaultEngineInjectSuccess(t *testing.T) {
	var got injectPayload
	srv := newEnvelopeServer(t, http.StatusOK, faultRuntimeEnvelope{
		Code: 0,
		Msg:  "ok",
		Data: struct {
			ExperimentUID string `json:"experiment_uid"`
		}{ExperimentUID: "blade-uid-123"},
	}, &got)
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL, DefaultNamespace: "sandbox-ns"})
	uid, err := c.Inject(context.Background(), ConsequenceInjectRequest{
		SessionID:   42,
		Kind:        "cpu",
		CPUPercent:  80,
		DelayMillis: 0,
	})
	if err != nil {
		t.Fatalf("注入应成功: %v", err)
	}
	if uid != "blade-uid-123" {
		t.Fatalf("uid 解析错误: %s", uid)
	}
	// 请求体字段名必须与 fault-engine InjectOnceReq 的 JSON 契约一致。
	if got.SessionID != 42 || got.Kind != "cpu" || got.CPUPercent != 80 {
		t.Fatalf("注入请求字段错误: %+v", got)
	}
	if got.Namespace != "sandbox-ns" {
		t.Fatalf("应回落到默认命名空间，实际 %q", got.Namespace)
	}
}

func TestFaultEngineInjectNamespaceFallbackAndExplicit(t *testing.T) {
	cases := []struct {
		name    string
		defNS   string
		reqNS   string
		wantNS  string
		defPod  string
		reqPod  string
		wantPod string
	}{
		{"请求显式指定优先", "default-ns", "req-ns", "req-ns", "def-pod", "req-pod", "req-pod"},
		{"请求留空回落默认", "default-ns", "", "default-ns", "def-pod", "", "def-pod"},
		{"都留空则为空（引擎再兜底/报错）", "", "", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got injectPayload
			srv := newEnvelopeServer(t, http.StatusOK, faultRuntimeEnvelope{
				Code: 0,
				Data: struct {
					ExperimentUID string `json:"experiment_uid"`
				}{ExperimentUID: "u"},
			}, &got)
			defer srv.Close()

			c := NewFaultEngineHTTPClient(FaultEngineClientConfig{
				BaseURL: srv.URL, DefaultNamespace: tc.defNS, DefaultPod: tc.defPod,
			})
			if _, err := c.Inject(context.Background(), ConsequenceInjectRequest{
				SessionID: 1, Kind: "memory", Namespace: tc.reqNS, Pod: tc.reqPod,
			}); err != nil {
				t.Fatalf("注入失败: %v", err)
			}
			if got.Namespace != tc.wantNS {
				t.Fatalf("namespace: want %q got %q", tc.wantNS, got.Namespace)
			}
			if got.Pod != tc.wantPod {
				t.Fatalf("pod: want %q got %q", tc.wantPod, got.Pod)
			}
		})
	}
}

func TestFaultEngineInjectBusinessError(t *testing.T) {
	srv := newEnvelopeServer(t, http.StatusBadRequest, faultRuntimeEnvelope{
		Code: 10001, Msg: "目标命名空间不能为空",
	}, nil)
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL})
	_, err := c.Inject(context.Background(), ConsequenceInjectRequest{SessionID: 1, Kind: "cpu"})
	if err == nil || !strings.Contains(err.Error(), "目标命名空间不能为空") {
		t.Fatalf("应透传引擎业务错误，实际: %v", err)
	}
}

func TestFaultEngineInjectNonOKStatus(t *testing.T) {
	srv := newEnvelopeServer(t, http.StatusBadGateway, faultRuntimeEnvelope{}, nil)
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL})
	if _, err := c.Inject(context.Background(), ConsequenceInjectRequest{SessionID: 1, Kind: "cpu"}); err == nil {
		t.Fatal("非 200 应返回错误")
	}
}

func TestFaultEngineInjectEmptyUID(t *testing.T) {
	srv := newEnvelopeServer(t, http.StatusOK, faultRuntimeEnvelope{Code: 0, Msg: "ok"}, nil)
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL})
	if _, err := c.Inject(context.Background(), ConsequenceInjectRequest{SessionID: 1, Kind: "cpu"}); err == nil {
		t.Fatal("成功响应但缺 experiment_uid 应报错")
	}
}

func TestFaultEngineInjectInvalidRequest(t *testing.T) {
	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: "http://unused"})
	if _, err := c.Inject(context.Background(), ConsequenceInjectRequest{Kind: "cpu"}); err == nil {
		t.Fatal("SessionID 缺失应报错")
	}
	if _, err := c.Inject(context.Background(), ConsequenceInjectRequest{SessionID: 1, Kind: ""}); err == nil {
		t.Fatal("Kind 缺失应报错")
	}
}

func TestFaultEngineRevoke(t *testing.T) {
	var got revokePayload
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(faultRuntimeEnvelope{Code: 0, Msg: "ok"})
	}))
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL})
	if err := c.Revoke(context.Background(), 7, "uid-7"); err != nil {
		t.Fatalf("撤销应成功: %v", err)
	}
	if gotPath != "/api/v1/runtime/revoke" {
		t.Fatalf("撤销路径错误: %s", gotPath)
	}
	if got.SessionID != 7 || got.UID != "uid-7" {
		t.Fatalf("撤销请求字段错误: %+v", got)
	}

	// 参数校验，不应发请求。
	if err := c.Revoke(context.Background(), 0, "u"); err == nil {
		t.Fatal("SessionID 非法应报错")
	}
	if err := c.Revoke(context.Background(), 1, ""); err == nil {
		t.Fatal("uid 为空应报错")
	}
}

func TestFaultEngineStopSession(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(faultRuntimeEnvelope{Code: 0, Msg: "ok"})
	}))
	defer srv.Close()

	c := NewFaultEngineHTTPClient(FaultEngineClientConfig{BaseURL: srv.URL + "/"}) // 尾斜杠应被规整
	if err := c.StopSession(context.Background(), 99); err != nil {
		t.Fatalf("停止会话应成功: %v", err)
	}
	if gotPath != "/api/v1/runtime/sessions/99/stop" {
		t.Fatalf("停止路径错误: %s", gotPath)
	}
	if err := c.StopSession(context.Background(), 0); err == nil {
		t.Fatal("SessionID 非法应报错")
	}
}
