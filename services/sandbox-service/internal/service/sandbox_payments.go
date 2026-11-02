package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/quenching/sandbox-service/internal/model"
	"strconv"
	"time"
)

type SandboxPaymentOrder struct {
	OrderID     string `json:"order_id"`
	AccountRef  string `json:"account_ref"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Status      int    `json:"status"`
	ReceiptID   string `json:"receipt_id"`
}

func ValidateSandboxPayment(claim model.SandboxOrder, facts SandboxPaymentOrder) error {
	if claim.OrderID == "" || claim.AccountRef == "" || claim.AmountMinor <= 0 || claim.OrderID != facts.OrderID || claim.AccountRef != facts.AccountRef || claim.AmountMinor != facts.AmountMinor || claim.Currency != "CNY" || facts.Currency != "CNY" || (facts.Status != 0 && facts.Status != 1) {
		return errors.New("payment order facts mismatch")
	}
	return nil
}
func SignSandboxPayment(key string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
func VerifySandboxPaymentSignature(key string, timestamp int64, body []byte, signature string, now time.Time) bool {
	delta := now.Unix() - timestamp
	if key == "" || delta > 120 || delta < -120 {
		return false
	}
	return hmac.Equal([]byte(SignSandboxPayment(key, timestamp, body)), []byte(signature))
}
func SandboxPaymentActive(sb *model.Sandbox) bool {
	return sb != nil && (sb.Status == model.StatusReady || sb.Status == model.StatusRunning) && sb.CompletedAt.IsZero()
}
func PaymentMaySubmit(status string) bool { return status == "pending" }

type SandboxPaymentStore interface {
	ReservePayment(context.Context, int64, string, model.SandboxOrder) (model.PaymentReceipt, error)
	ClaimPayment(context.Context, int64, string) (bool, error)
	ReadPayment(context.Context, int64, string) (model.PaymentReceipt, error)
	RecordPayment(context.Context, int64, string, string, string) error
}
type SandboxPaymentAdapter interface {
	Inspect(context.Context, int64, string) (SandboxPaymentOrder, error)
	Callback(context.Context, int64, string, model.SandboxOrder) (SandboxPaymentOrder, error)
}
type SandboxPaymentService struct {
	store     SandboxPaymentStore
	sandboxes BehaviorSandboxReader
	adapter   SandboxPaymentAdapter
}

func NewSandboxPaymentService(store SandboxPaymentStore, sandboxes BehaviorSandboxReader, adapter SandboxPaymentAdapter) *SandboxPaymentService {
	return &SandboxPaymentService{store, sandboxes, adapter}
}
func (s *SandboxPaymentService) Pay(ctx context.Context, sid int64, id string, order model.SandboxOrder) (model.PaymentReceipt, error) {
	return s.PayScenario(ctx, sid, id, order, "success")
}

func (s *SandboxPaymentService) PayScenario(ctx context.Context, sid int64, id string, order model.SandboxOrder, scenario string) (model.PaymentReceipt, error) {
	switch scenario {
	case "success", "decline", "delay", "duplicate":
	default:
		return model.PaymentReceipt{}, errors.New("unknown sandbox payment scenario")
	}
	if !behaviorCommandID.MatchString(id) {
		return model.PaymentReceipt{}, errors.New("invalid payment command")
	}
	sb, err := s.sandboxes.GetBySessionID(ctx, sid)
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	if !SandboxPaymentActive(sb) {
		return model.PaymentReceipt{}, errors.New("payment sandbox closed")
	}
	facts, err := s.adapter.Inspect(ctx, sid, order.OrderID)
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	if err = ValidateSandboxPayment(order, facts); err != nil {
		return model.PaymentReceipt{}, err
	}
	receipt, err := s.store.ReservePayment(ctx, sid, id, order)
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	if receipt.Status == "succeeded" {
		return receipt, nil
	}
	if facts.Status == 1 && facts.ReceiptID != "" {
		if err = s.store.RecordPayment(ctx, sid, id, "succeeded", ""); err != nil {
			return receipt, err
		}
		return s.Get(ctx, sid, id)
	}
	if !PaymentMaySubmit(receipt.Status) {
		return receipt, nil
	}
	// A single durable claim controls all writers; crashed running calls stay unknown.
	claimed, err := s.store.ClaimPayment(ctx, sid, id)
	if err != nil {
		return receipt, err
	}
	if !claimed {
		return s.Get(ctx, sid, id)
	}
	sb, err = s.sandboxes.GetBySessionID(ctx, sid)
	if err != nil || !SandboxPaymentActive(sb) {
		_ = s.store.RecordPayment(ctx, sid, id, "cancelled", "sandbox closed before callback")
		return s.Get(ctx, sid, id)
	}
	if scenario == "decline" {
		if err = s.store.RecordPayment(ctx, sid, id, "failed", "simulated rejection"); err != nil {
			return receipt, err
		}
		return s.Get(ctx, sid, id)
	}
	if scenario == "delay" {
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			_ = s.store.RecordPayment(context.WithoutCancel(ctx), sid, id, "cancelled", "delay cancelled before callback")
			return receipt, ctx.Err()
		case <-timer.C:
		}
	}
	facts, err = s.adapter.Callback(ctx, sid, id, order)
	if err == nil && scenario == "duplicate" {
		_, _ = s.adapter.Callback(ctx, sid, id, order)
	}
	status, reason := "succeeded", ""
	if err != nil || ValidateSandboxPayment(order, facts) != nil || facts.Status != 1 {
		status = "unknown"
		reason = "callback outcome unconfirmed; inspect before retry"
	}
	if err = s.store.RecordPayment(ctx, sid, id, status, reason); err != nil {
		return receipt, err
	}
	return s.Get(ctx, sid, id)
}
func (s *SandboxPaymentService) Get(ctx context.Context, sid int64, id string) (model.PaymentReceipt, error) {
	return s.store.ReadPayment(ctx, sid, id)
}
