package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
)

func TestInternalSandboxRoutesRegisterTogether(t *testing.T) {
	for _, behaviorFirst := range []bool{false, true} {
		name := "readonly-first"
		if behaviorFirst {
			name = "behavior-first"
		}
		t.Run(name, func(t *testing.T) {
			defer func() {
				if err := recover(); err != nil {
					t.Fatalf("internal sandbox route registration panicked: %v", err)
				}
			}()
			r := gin.New()
			behavior := NewBehaviorCommandsHandler(nil, "internal")
			if behaviorFirst {
				behavior.RegisterRoutes(r)
			}
			NewInternalReadonlyHandler(nil, "internal").RegisterRoutes(r)
			NewInternalLifecycleHandler(nil, nil, nil, "internal").RegisterRoutes(r)
			OperationAuditHandler{Token: "internal"}.Register(r)
			if !behaviorFirst {
				behavior.RegisterRoutes(r)
			}
			NewSandboxPaymentsHandler(nil, nil).RegisterRoutes(r)

			for _, route := range []struct{ method, path string }{
				{http.MethodGet, "/internal/v1/sandboxes/41/targets"},
				{http.MethodPost, "/internal/v1/sandboxes/41/activity"},
				{http.MethodGet, "/internal/v1/sandboxes/41/audit-seal"},
				{http.MethodPost, "/internal/v1/sandboxes/41/behavior/commands"},
				{http.MethodGet, "/internal/v1/sandboxes/41/behavior/commands/c1"},
			} {
				req := httptest.NewRequest(route.method, route.path, nil)
				out := httptest.NewRecorder()
				r.ServeHTTP(out, req)
				if out.Code != http.StatusUnauthorized {
					t.Errorf("%s %s: status %d, want 401", route.method, route.path, out.Code)
				}
			}
		})
	}
}

func TestBehaviorAndPaymentRoutesPreserveSessionScope(t *testing.T) {
	commands := service.NewBehaviorCommandService(routeCommandStore{}, nil)
	payments := service.NewSandboxPaymentService(routePaymentStore{}, nil, nil)
	r := gin.New()
	NewBehaviorCommandsHandler(commands, "internal").RegisterRoutes(r)
	NewSandboxPaymentsHandler(payments, commands).RegisterRoutes(r)

	type responseData struct {
		CommandID string `json:"command_id"`
		SessionID int64  `json:"session_id"`
		Status    string `json:"status"`
		ReceiptID string `json:"receipt_id"`
	}
	for _, tc := range []struct {
		method, path, header, token string
		want                        responseData
	}{
		{http.MethodGet, "/internal/v1/sandboxes/41/behavior/commands/c1", "X-Internal-Token", "internal", responseData{CommandID: "c1", Status: "accepted"}},
		{http.MethodPost, "/internal/v1/behavior-runner/41/claim", "X-Runner-Token", "runner", responseData{CommandID: "c1", SessionID: 41}},
		{http.MethodGet, "/internal/v1/behavior-runner/41/payments/c1", "X-Runner-Token", "runner", responseData{CommandID: "c1", ReceiptID: "receipt-41", Status: "succeeded"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(tc.header, tc.token)
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", out.Code, out.Body.String())
			}
			var response struct {
				Code int          `json:"code"`
				Data responseData `json:"data"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Code != 0 || response.Data != tc.want {
				t.Fatalf("response %+v, want data %+v", response, tc.want)
			}
		})
	}
}

// The stores replace PostgreSQL only; handlers, authentication and services run unchanged.
type routeCommandStore struct{ service.BehaviorCommandStore }

func (routeCommandStore) GetCommand(_ context.Context, sid int64, id string) (model.BehaviorResult, error) {
	if sid != 41 || id != "c1" {
		return model.BehaviorResult{}, errors.New("command outside session")
	}
	return model.BehaviorResult{CommandID: "c1", Status: "accepted"}, nil
}

func (routeCommandStore) RunnerTokenHash(_ context.Context, sid int64) (string, error) {
	if sid != 41 {
		return "", errors.New("runner outside session")
	}
	sum := sha256.Sum256([]byte("runner"))
	return hex.EncodeToString(sum[:]), nil
}

func (routeCommandStore) ClaimCommand(_ context.Context, sid int64) (*model.BehaviorCommand, error) {
	if sid != 41 {
		return nil, errors.New("claim outside session")
	}
	return &model.BehaviorCommand{SessionID: 41, CommandID: "c1"}, nil
}

type routePaymentStore struct{ service.SandboxPaymentStore }

func (routePaymentStore) ReadPayment(_ context.Context, sid int64, id string) (model.PaymentReceipt, error) {
	if sid != 41 || id != "c1" {
		return model.PaymentReceipt{}, errors.New("payment outside session")
	}
	return model.PaymentReceipt{CommandID: "c1", ReceiptID: "receipt-41", Status: "succeeded"}, nil
}
