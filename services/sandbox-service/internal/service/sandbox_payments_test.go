package service

import (
	"context"
	"fmt"
	"github.com/quenching/sandbox-service/internal/model"
	"testing"
	"time"
)

func TestPaymentWrongAmount(t *testing.T) {
	order := model.SandboxOrder{OrderID: "8", AccountRef: "1", AmountMinor: 100, Currency: "CNY"}
	facts := SandboxPaymentOrder{OrderID: "8", AccountRef: "1", AmountMinor: 101, Currency: "CNY", Status: 0}
	if ValidateSandboxPayment(order, facts) == nil {
		t.Fatal("wrong amount accepted")
	}
	facts.AmountMinor = 100
	if ValidateSandboxPayment(order, facts) != nil {
		t.Fatal("valid facts rejected")
	}
}
func TestDuplicateCallback(t *testing.T) {
	now := time.Unix(1000, 0)
	body := []byte(`{"order_id":"8"}`)
	signed := SignSandboxPayment("key", now.Unix(), body)
	if !VerifySandboxPaymentSignature("key", 1000, body, signed, now) {
		t.Fatal("valid signed callback rejected")
	}
	if VerifySandboxPaymentSignature("key", 1000, []byte(`{"order_id":"9"}`), signed, now) {
		t.Fatal("tampered callback accepted")
	}
	if VerifySandboxPaymentSignature("key", 1000, body, signed, now.Add(121*time.Second)) {
		t.Fatal("expired callback accepted")
	}
}
func TestPaymentUnknownNoResubmit(t *testing.T) {
	if PaymentMaySubmit("unknown") || PaymentMaySubmit("running") {
		t.Fatal("ambiguous callback replayed")
	}
}
func TestPaymentAfterClose(t *testing.T) {
	if SandboxPaymentActive(&model.Sandbox{Status: model.StatusReleased}) {
		t.Fatal("closed sandbox paid")
	}
	if SandboxPaymentActive(&model.Sandbox{Status: model.StatusReady, CompletedAt: time.Now()}) {
		t.Fatal("completed sandbox paid")
	}
}

type paymentScenarioFixture struct {
	receipt   model.PaymentReceipt
	callbacks int
}

func (f *paymentScenarioFixture) GetBySessionID(context.Context, int64) (*model.Sandbox, error) {
	return &model.Sandbox{Status: model.StatusReady}, nil
}
func (f *paymentScenarioFixture) ReservePayment(_ context.Context, _ int64, id string, order model.SandboxOrder) (model.PaymentReceipt, error) {
	f.receipt = model.PaymentReceipt{CommandID: id, Status: "pending", OrderID: order.OrderID}
	return f.receipt, nil
}
func (f *paymentScenarioFixture) ClaimPayment(context.Context, int64, string) (bool, error) {
	f.receipt.Status = "running"
	return true, nil
}
func (f *paymentScenarioFixture) ReadPayment(context.Context, int64, string) (model.PaymentReceipt, error) {
	return f.receipt, nil
}
func (f *paymentScenarioFixture) RecordPayment(_ context.Context, _ int64, _ string, status, _ string) error {
	f.receipt.Status = status
	return nil
}
func (f *paymentScenarioFixture) Inspect(context.Context, int64, string) (SandboxPaymentOrder, error) {
	status := 0
	if f.callbacks > 0 {
		status = 1
	}
	return SandboxPaymentOrder{OrderID: "8", AccountRef: "1", AmountMinor: 100, Currency: "CNY", Status: status}, nil
}
func (f *paymentScenarioFixture) Callback(ctx context.Context, sid int64, id string, o model.SandboxOrder) (SandboxPaymentOrder, error) {
	f.callbacks++
	return f.Inspect(ctx, sid, o.OrderID)
}
func TestPaymentSimulatedOutcomes(t *testing.T) {
	for _, scenario := range []string{"success", "decline", "duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			f := &paymentScenarioFixture{}
			s := NewSandboxPaymentService(f, f, f)
			receipt, err := s.PayScenario(context.Background(), 1, "c1", model.SandboxOrder{OrderID: "8", AccountRef: "1", AmountMinor: 100, Currency: "CNY"}, scenario)
			if err != nil {
				t.Fatal(err)
			}
			want := "succeeded"
			if scenario == "decline" {
				want = "failed"
			}
			if receipt.Status != want {
				t.Fatal(fmt.Sprintf("scenario outcome %s", receipt.Status))
			}
			if scenario == "decline" && f.callbacks != 0 {
				t.Fatal("declined payment applied")
			}
			if scenario == "duplicate" && f.callbacks != 2 {
				t.Fatal("duplicate callback was not exercised")
			}
		})
	}
}
