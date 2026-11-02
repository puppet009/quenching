package repository

import (
	"context"
	"encoding/json"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/model"
	"github.com/quenching/training-service/internal/service"
	"strings"
)

// Replay durable metadata into the existing memory/brain sequence, never remote execution.
func (r *BrainRepo) copyManagedOperationEvents(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT o.evidence FROM training_operation_events o JOIN brain_sessions b ON b.session_id=o.session_id WHERE b.phase NOT IN ('closing','reviewing','completed','failed') AND o.evidence->>'status' IN ('succeeded','failed','unknown','cancelled') AND NOT EXISTS(SELECT 1 FROM brain_events e WHERE e.session_id=o.session_id AND e.source='student_audit' AND e.event_id='operation:'||o.event_id) ORDER BY o.id LIMIT 100`)
	if err != nil {
		return err
	}
	var events []oe.Evidence
	for rows.Next() {
		var raw []byte
		var e oe.Evidence
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &e)
		}
		if err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range events {
		// Keep commands/results out of director plans; these are public observed receipts.
		raw, _ := json.Marshal(map[string]any{"operation_id": e.OperationID, "kind": e.Kind, "status": e.Status, "tool": e.Tool, "command": e.Command, "target": e.Target, "intent": e.Intent, "coverage": e.Coverage, "text_refs": e.TextRefs, "performed_by": e.Actor, "exit_code": e.ExitCode, "trust": "untrusted_data"})
		id := "operation:" + e.EventID
		if _, err = r.SaveEvidence(ctx, model.BrainEvidence{ID: id, SessionID: e.SessionID, Kind: "operation_evidence", Actor: e.Actor, OccurredAt: e.StartedAt, PayloadJSON: string(raw)}); err != nil {
			return err
		}
		if _, err = r.AppendEvent(ctx, model.BrainEvent{SessionID: e.SessionID, Source: "student_audit", EventID: id, Kind: "student_operation", EvidenceRefs: []string{id}}); err != nil && !errors.Is(err, service.ErrBrainConflict) {
			return err
		}
	}
	return nil
}

func (r *SessionMemoryRepo) readOperationSource(ctx context.Context, sid int64, ref string, offset, limit int) (model.MemorySource, error) {
	v := model.MemorySource{ID: ref, Kind: "operation_evidence"}
	var e oe.Evidence
	store := NewOperationStore(r.pool)
	if strings.HasPrefix(ref, "optext:") {
		text, err := store.Read(ctx, sid, ref)
		if err != nil {
			return v, service.ErrSessionMemoryNotFound
		}
		fragment, err := oe.ReadFragment(text, offset, limit)
		if err != nil {
			return v, err
		}
		e, err = store.Get(ctx, sid, text.OperationID)
		if err != nil {
			return v, err
		}
		v.Kind = "operation_text"
		v.Actor = e.Actor
		v.Text = fragment.Text
		v.OccurredAt = e.StartedAt
		v.Truncated = !fragment.Complete
		v.NextOffsetBytes = fragment.NextOffsetBytes
		return v, nil
	}
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT evidence FROM training_operation_events WHERE session_id=$1 AND event_id=$2`, sid, strings.TrimPrefix(ref, "operation:")).Scan(&raw)
	if err != nil {
		return v, service.ErrSessionMemoryNotFound
	}
	if err = json.Unmarshal(raw, &e); err != nil {
		return v, err
	}
	source := service.OperationReviewSource(e)
	source.ByteLength = len(source.Text)
	fragment, err := service.ReviewSourceFragment(source, offset, limit)
	if err != nil {
		return v, err
	}
	v.Actor = e.Actor
	v.Text = fragment.Source.Text
	v.OccurredAt = e.StartedAt
	v.Truncated = !fragment.Complete
	v.NextOffsetBytes = fragment.NextOffsetBytes
	return v, nil
}

func (r *SessionMemoryRepo) OperationContext(ctx context.Context, uid, sid int64) (json.RawMessage, error) {
	if _, err := memoryOwned(ctx, r.pool, uid, sid); err != nil {
		return nil, err
	}
	page, err := NewOperationStore(r.pool).List(ctx, sid, 0, 10)
	if err != nil {
		return nil, err
	}
	sources := []model.MemorySource{}
	budget := 12 * 1024
	for _, e := range page.Items {
		src := service.OperationReviewSource(e)
		text, tr := memorySourceText(src.Text)
		v := model.MemorySource{ID: src.ID, Kind: src.Kind, Actor: src.Actor, Text: text, Truncated: tr, OccurredAt: src.OccurredAt}
		raw, _ := json.Marshal(v)
		if len(raw) > budget {
			continue
		}
		budget -= len(raw)
		sources = append(sources, v)
	}
	return json.Marshal(map[string]any{"sources": sources, "has_more": page.HasMore, "next_before_sequence": page.NextBefore, "trust": "untrusted_data", "note": "操作成功不是业务修复证明；原文用read_session_history读取，早期记录用list_session_operations回溯。"})
}
