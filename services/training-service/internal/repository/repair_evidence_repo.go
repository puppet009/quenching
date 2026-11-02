package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"time"
)

func applyRepairEvidence(ctx context.Context, tx pgx.Tx, sid int64, command string) error {
	var no int64
	var w service.ChallengeWindow
	err := tx.QueryRow(ctx, `SELECT challenge_no,started_at,expires_at FROM brain_challenges WHERE session_id=$1 AND status='active' ORDER BY challenge_no DESC LIMIT 1`, sid).Scan(&no, &w.StartedAt, &w.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT command_id,result,updated_at FROM brain_tool_executions WHERE session_id=$1 AND action='verify_business' AND status IN ('succeeded','failed') AND updated_at>=$2 ORDER BY updated_at`, sid, w.StartedAt)
	if err != nil {
		return err
	}
	var before, after service.BusinessProof
	for rows.Next() {
		var id string
		var raw []byte
		var at time.Time
		if err = rows.Scan(&id, &raw, &at); err != nil {
			rows.Close()
			return err
		}
		var result model.BrainToolResult
		var facts struct {
			Verified        *bool  `json:"business_verified"`
			Truncated       bool   `json:"truncated"`
			ExecutionStatus string `json:"execution_status"`
		}
		if json.Unmarshal(raw, &result) != nil || json.Unmarshal([]byte(result.ResultJSON), &facts) != nil || facts.Verified == nil || facts.Truncated || facts.ExecutionStatus == "unknown" {
			continue
		}
		proof := service.BusinessProof{Verified: *facts.Verified, Ref: "tool:" + id, OccurredAt: at}
		if id == command {
			after = proof
		} else if !proof.Verified {
			before = proof
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT evidence FROM training_operation_intents WHERE session_id=$1 AND (evidence->>'started_at')::timestamptz>=$2 ORDER BY created_at`, sid, w.StartedAt)
	if err != nil {
		return err
	}
	var operations []oe.Evidence
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var e oe.Evidence
		if err = json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return err
		}
		operations = append(operations, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	decision := service.EvaluateRepair(operations, before, after, w)
	refs, _ := json.Marshal(decision.EvidenceRefs)
	if decision.Status == "verified_repair" || decision.Status == "assisted_repair" {
		_, err = tx.Exec(ctx, `UPDATE brain_challenges SET status=$3,repair_status=$3,repair_reason=$4,completed_at=$5,evidence_refs=$6 WHERE session_id=$1 AND challenge_no=$2 AND status='active' AND expires_at>clock_timestamp()`, sid, no, decision.Status, decision.Reason, time.Now(), refs)
	} else {
		_, err = tx.Exec(ctx, `UPDATE brain_challenges SET repair_status=$3,repair_reason=$4,evidence_refs=$5 WHERE session_id=$1 AND challenge_no=$2 AND status='active'`, sid, no, decision.Status, decision.Reason, refs)
	}
	return err
}
