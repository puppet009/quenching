package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/quenching/sandbox-service/internal/service"
	"k8s.io/client-go/kubernetes/fake"
)

type unavailableTerminalAudit struct{}

func (unavailableTerminalAudit) Append(context.Context, int64, string, any) error {
	return errors.New("audit storage unavailable")
}
func (unavailableTerminalAudit) AssertWorkspaceOpen(context.Context, int64) error { return nil }

type closedTerminalAudit struct{ unavailableTerminalAudit }

func (closedTerminalAudit) AssertWorkspaceOpen(context.Context, int64) error {
	return errors.New("training closed")
}

func TestClosedWorkspaceCannotOpenTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewDesktopHandler(service.NewK8sClient(fake.NewSimpleClientset()), nil)
	h.SetTrainingAudit(closedTerminalAudit{})
	r := gin.New()
	r.GET("/sandboxes/:id/exec", h.Exec)
	srv := httptest.NewServer(r)
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/sandboxes/7/exec?target=portal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame struct{ Type, Message string }
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&frame); err != nil || frame.Type != "error" || !strings.Contains(frame.Message, "冻结") {
		t.Fatalf("closed workspace was not frozen before execution: %v %+v", err, frame)
	}
}

func TestTerminalWorkspaceWatcherStopsIdleConnectionAfterCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time, 1)
	done := make(chan error, 1)
	go func() { done <- watchTerminalWorkspace(ctx, closedTerminalAudit{}, 7, ticks) }()
	ticks <- time.Now()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed workspace did not stop the idle terminal")
		}
	case <-time.After(time.Second):
		t.Fatal("workspace watcher did not stop")
	}
}

func TestTerminalFailureFrameIsDeliveredBeforeConnectionCloses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, auditUnavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "exec failure", true: "audit failure"}[auditUnavailable], func(t *testing.T) {
			h := NewDesktopHandler(service.NewK8sClient(fake.NewSimpleClientset()), nil)
			if auditUnavailable {
				h.SetTrainingAudit(unavailableTerminalAudit{})
			}
			r := gin.New()
			r.GET("/sandboxes/:id/exec", h.Exec)
			srv := httptest.NewServer(r)
			defer srv.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/sandboxes/7/exec?target=portal&container=app", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var frame struct{ Type, Message string }
			if err := conn.ReadJSON(&frame); err != nil || frame.Type != "hello" {
				t.Fatalf("missing handshake: %v %+v", err, frame)
			}
			if err := conn.ReadJSON(&frame); err != nil || frame.Type != "error" || frame.Message == "" {
				t.Fatalf("terminal failure was not delivered: %v %+v", err, frame)
			}
			if auditUnavailable && strings.Contains(frame.Message, "storage") {
				t.Fatal("internal audit error exposed to the browser")
			}
		})
	}
}
