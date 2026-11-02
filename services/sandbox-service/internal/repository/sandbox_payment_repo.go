package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/sandbox-service/internal/model"
)

type SandboxPaymentRepo struct{ pool *pgxpool.Pool }

func NewSandboxPaymentRepo(pool *pgxpool.Pool) *SandboxPaymentRepo { return &SandboxPaymentRepo{pool} }
func (r *SandboxPaymentRepo) ReservePayment(ctx context.Context, sid int64, id string, order model.SandboxOrder) (model.PaymentReceipt, error) {
	_, err := r.pool.Exec(ctx, `INSERT INTO sandbox_payments(command_id,session_id,order_id,account_ref,amount_minor,currency,receipt_id) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, id, sid, order.OrderID, order.AccountRef, order.AmountMinor, order.Currency, fmt.Sprintf("sandbox-%d-%s", sid, id))
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	var valid bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandbox_payments WHERE command_id=$1 AND session_id=$2 AND order_id=$3 AND account_ref=$4 AND amount_minor=$5 AND currency=$6)`, id, sid, order.OrderID, order.AccountRef, order.AmountMinor, order.Currency).Scan(&valid)
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	if !valid {
		return model.PaymentReceipt{}, errors.New("payment identity collision or already reserved order")
	}
	return r.ReadPayment(ctx, sid, id)
}
func (r *SandboxPaymentRepo) ClaimPayment(ctx context.Context, sid int64, id string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE sandbox_payments SET status='running',updated_at=now() WHERE session_id=$1 AND command_id=$2 AND status='pending'`, sid, id)
	return tag.RowsAffected() == 1, err
}
func (r *SandboxPaymentRepo) ReadPayment(ctx context.Context, sid int64, id string) (model.PaymentReceipt, error) {
	_, err := r.pool.Exec(ctx, `UPDATE sandbox_payments SET status='unknown',reason='callback owner interrupted',updated_at=now() WHERE session_id=$1 AND command_id=$2 AND status='running' AND updated_at<now()-interval '60 seconds'`, sid, id)
	if err != nil {
		return model.PaymentReceipt{}, err
	}
	var receipt model.PaymentReceipt
	err = r.pool.QueryRow(ctx, `SELECT command_id,receipt_id,status,order_id,amount_minor,currency FROM sandbox_payments WHERE session_id=$1 AND command_id=$2`, sid, id).Scan(&receipt.CommandID, &receipt.ReceiptID, &receipt.Status, &receipt.OrderID, &receipt.AmountMinor, &receipt.Currency)
	return receipt, err
}
func (r *SandboxPaymentRepo) RecordPayment(ctx context.Context, sid int64, id, status, reason string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE sandbox_payments SET status=$3,reason=$4,updated_at=now() WHERE session_id=$1 AND command_id=$2 AND status IN ('pending','running','unknown')`, sid, id, status, reason)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("payment result conflict")
	}
	return err
}
