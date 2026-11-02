package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	oe "github.com/quenching/proto/operationevidence"
	"time"
)

var ErrToolOutcomeUnconfirmed = errors.New("operation may have taken effect; audit outcome unconfirmed")

type ToolAudit struct{ Store oe.Store }

func (a *ToolAudit) Begin(ctx context.Context, scope ChatLeaseScope, conv int64, round int, call ToolCall) (oe.Evidence, error) {
	id := uuid.NewString()
	e := oe.Evidence{SchemaVersion: 1, OperationID: id, EventID: id + ":1", SessionID: scope.SessionID, Sequence: 1, Actor: "assistant", Kind: "tool", Status: "pending", ToolCallID: call.ID, ConversationID: conv, Round: round, Tool: call.Name, Arguments: json.RawMessage(call.ArgumentsJSON), StartedAt: time.Now().UTC(), Coverage: oe.EmptyCoverage()}
	if _, err := a.Store.Begin(ctx, scope.SessionID, id, "tool:"+id, oe.RequestHash(call), e); err != nil {
		return e, err
	}
	return oe.Sanitize(e), nil
}
func (a *ToolAudit) Advance(ctx context.Context, e oe.Evidence, status, approval string) (oe.Evidence, error) {
	v := oe.Next(e, status)
	if approval != "" {
		v.Approval = approval
	}
	err := a.Store.Append(ctx, v, nil)
	return v, err
}
func (a *ToolAudit) Finish(ctx context.Context, e oe.Evidence, result ToolResult) error {
	status := "succeeded"
	if result.IsError {
		status = "failed"
	}
	if result.OutcomeUnconfirmed {
		status = "unknown"
	}
	v := oe.Next(e, status)
	v.Coverage.Execution = "complete"
	if status == "unknown" {
		v.Coverage.Execution = "unknown"
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, "result_unconfirmed")
	}
	var texts []oe.Text
	if isSessionHistoryTool(e.Tool) {
		var input struct {
			Sources []struct {
				ID string `json:"id"`
			} `json:"sources"`
			HasMore bool `json:"has_more"`
		}
		_ = json.Unmarshal([]byte(result.ContentJSON), &input)
		refs := []string{}
		for _, s := range input.Sources {
			refs = append(refs, s.ID)
		}
		v.Result, _ = json.Marshal(map[string]any{"source_refs": refs, "has_more": input.HasMore, "is_error": result.IsError})
		v.Coverage.Output = "complete"
	} else {
		t := oe.CaptureText(e.OperationID, "result", []byte(result.ContentJSON), oe.TextLimit)
		texts = []oe.Text{t}
		v.TextRefs = []string{t.Ref}
		v.Coverage.Output = t.Coverage.Output
		v.Coverage.ReasonCodes = append(v.Coverage.ReasonCodes, t.Coverage.ReasonCodes...)
		v.Coverage.OriginalBytes = t.Coverage.OriginalBytes
		v.Coverage.SavedBytes = t.Coverage.SavedBytes
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return a.Store.Append(saveCtx, v, texts)
}
func (s *ChatService) EnableToolAudit(a *ToolAudit) { s.toolAudit = a }
func (s *ChatService) runAuditedToolCalls(ctx context.Context, sid, conv int64, calls []ToolCall, defs []ToolDef, send func(StreamEvent) bool) ([]ToolResult, bool) {
	allowed := map[string]ToolDef{}
	for _, d := range defs {
		allowed[d.Name] = d
	}
	results := []ToolResult{}
	scope, ok := CurrentChatLease(ctx)
	if !ok || scope.SessionID != sid {
		return results, false
	}
	for _, call := range calls {
		def, ok := allowed[call.Name]
		if !ok {
			results = append(results, ToolResult{CallID: call.ID, IsError: true, ContentJSON: `{"error":"tool is not allowed"}`})
			continue
		}
		e, err := s.toolAudit.Begin(ctx, scope, conv, call.Round, call)
		if err != nil {
			send(StreamEvent{Type: StreamError, ErrorMsg: "工具记录暂不可用，尚未执行"})
			return results, false
		}
		call.ConversationID = conv
		call.OperationID = e.OperationID
		display := ToolDisplay(call)
		emit := func(state, summary, detail, approval string) {
			send(StreamEvent{Type: StreamStep, Step: &StepEvent{StepID: e.OperationID, OperationID: e.OperationID, State: state, Tool: call.Name, Display: display, Summary: summary, DetailJSON: oe.RedactText(detail), ApprovalID: approval}})
		}
		if def.RequiresApproval {
			approval := s.approvals.NewApproval()
			call.ApprovalID = approval
			e.ApprovalID = approval
			e, err = s.toolAudit.Advance(ctx, e, "pending", "requested")
			if err != nil {
				return results, false
			}
			emit("awaiting_approval", "等待确认", call.ArgumentsJSON, approval)
			decision, received := s.approvals.Await(ctx, approval)
			if !received || !decision.Approved {
				reason := "rejected"
				if !received {
					reason = "timed_out"
				}
				saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				_, err = s.toolAudit.Advance(saveCtx, e, "cancelled", reason)
				cancel()
				emit("rejected", "用户未批准，未执行", "", "")
				if err != nil {
					return results, false
				}
				results = append(results, ToolResult{CallID: call.ID, IsError: true, ContentJSON: `{"status":"cancelled","reason":"not approved; do not retry"}`})
				continue
			}
			e, err = s.toolAudit.Advance(ctx, e, "pending", "approved")
			if err != nil {
				return results, false
			}
		}
		if err = s.assertSessionReplyLease(ctx); err != nil {
			return results, false
		}
		e, err = s.toolAudit.Advance(ctx, e, "running", "")
		if err != nil {
			return results, false
		}
		emit("started", "执行中", "", "")
		var content string
		var execErr error
		if isSessionHistoryTool(call.Name) && s.historyTools != nil {
			content, execErr = s.historyTools.Execute(ctx, scope.UserID, scope.SessionID, call)
		} else if s.toolExecutor != nil {
			content, execErr = s.toolExecutor.Execute(ctx, sid, call)
		} else {
			execErr = fmt.Errorf("tool unavailable")
		}
		result := ToolResult{OperationID: e.OperationID, CallID: call.ID, ContentJSON: oe.RedactText(content), IsError: execErr != nil, OutcomeUnconfirmed: errors.Is(execErr, ErrToolOutcomeUnconfirmed) || ctx.Err() != nil}
		if execErr != nil {
			raw, _ := json.Marshal(map[string]any{"error": "tool failed", "outcome_unconfirmed": result.OutcomeUnconfirmed})
			result.ContentJSON = string(raw)
		}
		if err = s.toolAudit.Finish(ctx, e, result); err != nil {
			emit("unknown", "操作可能已生效，记录未确认", "", "")
			results = append(results, ToolResult{CallID: call.ID, IsError: true, OutcomeUnconfirmed: true, ContentJSON: `{"outcome_unconfirmed":true,"reason":"audit result unavailable; do not retry mutation"}`})
			return results, false
		}
		state := "completed"
		summary := ToolResultSummary(call, result.ContentJSON)
		if result.IsError {
			state = "failed"
			summary = "执行失败"
		}
		if result.OutcomeUnconfirmed {
			state = "unknown"
			summary = "操作结果未确认"
		}
		emit(state, summary, result.ContentJSON, "")
		results = append(results, result)
	}
	return results, true
}
