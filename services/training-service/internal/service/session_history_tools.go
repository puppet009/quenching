package service

import (
	"context"
	"encoding/json"
	"errors"
	oe "github.com/quenching/proto/operationevidence"
	"github.com/quenching/training-service/internal/model"
	"io"
	"strconv"
	"strings"
)

type SessionHistoryTools struct {
	store      SessionMemoryStore
	operations oe.Store
}

func NewSessionHistoryTools(s SessionMemoryStore) *SessionHistoryTools {
	return &SessionHistoryTools{store: s}
}
func (h *SessionHistoryTools) WithOperations(s oe.Store) *SessionHistoryTools {
	h.operations = s
	return h
}
func SessionHistoryToolDefs() []ToolDef {
	return []ToolDef{
		{Name: "list_session_operations", Description: "按独立操作游标回溯受管理命令、文件修改与助手工具记录，保留执行者及覆盖缺口；原文用read_session_history分段读取。", ParametersJSON: `{"type":"object","properties":{"before_sequence":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":10}},"additionalProperties":false}`},
		{Name: "list_session_evidence", Description: "按序列翻查本班次公开操作和验证证据，包括未进入摘要的早期记录。用next_before_sequence继续回溯，再按引用读取原文。只读，身份服务端固定。", ParametersJSON: `{"type":"object","properties":{"before_sequence":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`},
		{Name: "search_session_history", Description: "检索本班次早期对话，避免重复已尝试步骤。只读，身份由服务端固定。", ParametersJSON: `{"type":"object","properties":{"query":{"type":"string","maxLength":256},"before_id":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["query"],"additionalProperties":false}`},
		{Name: "read_session_history", Description: "读取本班次原文或公开操作引用；可按before_id分页回溯。只读，不能读取隐藏根因。", ParametersJSON: `{"type":"object","properties":{"message_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":50},"evidence_ids":{"type":"array","items":{"type":"string"},"maxItems":50},"before_id":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":50},"reference":{"type":"string","maxLength":128},"offset_bytes":{"type":"integer","minimum":0},"limit_bytes":{"type":"integer","minimum":1,"maximum":8192}},"additionalProperties":false}`},
	}
}
func isSessionHistoryTool(name string) bool {
	return name == "list_session_operations" || name == "search_session_history" || name == "read_session_history" || name == "list_session_evidence"
}
func (h *SessionHistoryTools) Execute(ctx context.Context, uid, sid int64, call ToolCall) (string, error) {
	if _, e := h.store.Resolve(ctx, uid, sid); e != nil {
		return "", e
	}
	var p struct {
		BeforeSequence int64    `json:"before_sequence"`
		Reference      string   `json:"reference"`
		OffsetBytes    int      `json:"offset_bytes"`
		LimitBytes     int      `json:"limit_bytes"`
		Query          string   `json:"query"`
		BeforeID       int64    `json:"before_id"`
		Limit          int      `json:"limit"`
		MessageIDs     []int64  `json:"message_ids"`
		EvidenceIDs    []string `json:"evidence_ids"`
	}
	dec := json.NewDecoder(strings.NewReader(call.ArgumentsJSON))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&p); e != nil {
		return "", e
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return "", errors.New("extra tool JSON")
	}
	if p.BeforeSequence < 0 || (call.Name != "list_session_evidence" && call.Name != "list_session_operations" && p.BeforeSequence != 0) || p.BeforeID < 0 || p.Limit < 0 || p.Limit > 50 || len([]rune(p.Query)) > 256 || len(p.MessageIDs)+len(p.EvidenceIDs) > 50 {
		return "", errors.New("invalid history query")
	}
	if p.Limit == 0 {
		p.Limit = 20
	}
	seen := map[string]bool{}
	for _, id := range p.MessageIDs {
		if id <= 0 {
			return "", errors.New("invalid message ID")
		}
	}
	for _, id := range p.EvidenceIDs {
		if id == "" || seen[id] {
			return "", errors.New("invalid evidence ID")
		}
		seen[id] = true
	}
	if call.Name == "list_session_operations" {
		if h.operations == nil || p.Query != "" || p.BeforeID != 0 || p.Reference != "" || p.OffsetBytes != 0 || p.LimitBytes != 0 || len(p.MessageIDs)+len(p.EvidenceIDs) > 0 || p.Limit > 10 {
			return "", errors.New("invalid operations page")
		}
		page, err := h.operations.List(ctx, sid, p.BeforeSequence, p.Limit)
		if err != nil {
			return "", err
		}
		sources := []model.MemorySource{}
		budget := 12 * 1024
		for _, e := range page.Items {
			src := OperationReviewSource(e)
			text, tr := clipMemoryText(src.Text, 8192)
			v := model.MemorySource{ID: src.ID, Kind: src.Kind, Actor: src.Actor, Text: text, Truncated: tr, OccurredAt: src.OccurredAt}
			raw, _ := json.Marshal(v)
			if len(raw) > budget {
				return "", errors.New("operations page exceeds budget; retry smaller limit")
			}
			budget -= len(raw)
			sources = append(sources, v)
		}
		raw, err := json.Marshal(map[string]any{"sources": sources, "has_more": page.HasMore, "next_before_sequence": page.NextBefore, "trust": "untrusted_data"})
		return string(raw), err
	}
	out := model.MemorySourcePage{Sources: []model.MemorySource{}}
	next := int64(0)
	more := false
	switch call.Name {
	case "list_session_evidence":
		if p.Query != "" || p.BeforeID != 0 || p.Reference != "" || p.OffsetBytes != 0 || p.LimitBytes != 0 || len(p.MessageIDs)+len(p.EvidenceIDs) > 0 {
			return "", errors.New("invalid evidence page")
		}
		page, e := h.store.EvidenceHistory(ctx, uid, sid, p.BeforeSequence, p.Limit)
		if e != nil {
			return "", e
		}
		out = page
		next = page.NextBeforeSequence
		more = page.HasMore

	case "search_session_history":
		if strings.TrimSpace(p.Query) == "" || len(p.MessageIDs)+len(p.EvidenceIDs) > 0 || p.Reference != "" || p.OffsetBytes != 0 || p.LimitBytes != 0 {
			return "", errors.New("query required")
		}
		page, e := h.store.Search(ctx, uid, sid, p.Query, p.BeforeID, p.Limit)
		if e != nil {
			return "", e
		}
		next = page.NextBeforeID
		more = page.HasMore
		for _, m := range page.Messages {
			out.Sources = append(out.Sources, model.MemorySource{ID: messageID(m.ID), Kind: "message", Role: m.Role, Text: m.Content, OccurredAt: m.CreatedAt})
		}
	case "read_session_history":
		if p.Reference != "" {
			if p.OffsetBytes < 0 || p.LimitBytes < 0 || p.LimitBytes > 8192 || len(p.Reference) > 128 || p.Query != "" || len(p.MessageIDs)+len(p.EvidenceIDs) > 0 || p.BeforeID != 0 {
				return "", errors.New("invalid range read")
			}
			if p.LimitBytes == 0 {
				p.LimitBytes = 8192
			}
			source, e := h.store.ReadSourceRange(ctx, uid, sid, p.Reference, p.OffsetBytes, p.LimitBytes)
			if e != nil {
				return "", e
			}
			out.Sources = []model.MemorySource{source}
			break
		}
		if p.OffsetBytes != 0 || p.LimitBytes != 0 {
			return "", errors.New("reference required for range")
		}

		if p.Query != "" {
			return "", errors.New("read does not accept query")
		}
		if len(p.MessageIDs)+len(p.EvidenceIDs) > 0 {
			page, e := h.store.ReadReferences(ctx, uid, sid, p.MessageIDs, p.EvidenceIDs)
			if e != nil {
				return "", e
			}
			out = page
		} else {
			page, e := h.store.History(ctx, uid, sid, p.BeforeID, p.Limit)
			if e != nil {
				return "", e
			}
			next = page.NextBeforeID
			more = page.HasMore
			for _, m := range page.Messages {
				out.Sources = append(out.Sources, model.MemorySource{ID: messageID(m.ID), Kind: "message", Role: m.Role, Text: m.Content, OccurredAt: m.CreatedAt})
			}
		}
	default:
		return "", errors.New("unknown session history tool")
	}
	pagination := call.Name == "list_session_evidence" || call.Name == "search_session_history" || (len(p.MessageIDs)+len(p.EvidenceIDs) == 0 && p.Reference == "")
	if pagination {
		for a, b := 0, len(out.Sources)-1; a < b; a, b = a+1, b-1 {
			out.Sources[a], out.Sources[b] = out.Sources[b], out.Sources[a]
		}
	}
	budget := 12 * 1024
	bounded := []model.MemorySource{}
	truncated := false
	omitted := []string{}
	for _, v := range out.Sources {
		text, tr := clipMemoryText(v.Text, 8*1024)
		v.Text = text
		v.Truncated = v.Truncated || tr
		if tr && v.NextOffsetBytes == 0 {
			v.NextOffsetBytes = len(text)
		}
		encoded, _ := json.Marshal(v)
		if len(encoded) > budget {
			truncated = true
			omitted = append(omitted, v.ID)
			continue
		}
		budget -= len(encoded)
		bounded = append(bounded, v)
		truncated = truncated || v.Truncated
	}
	if pagination && len(omitted) > 0 && len(bounded) > 0 {
		more = true
		next = 0
		for _, v := range bounded {
			id, e := strconv.ParseInt(strings.TrimPrefix(v.ID, "message:"), 10, 64)
			if call.Name == "list_session_evidence" {
				id = v.Sequence
				e = nil
			}
			if e == nil && (next == 0 || id < next) {
				next = id
			}
		}
	}
	if pagination {
		for a, b := 0, len(bounded)-1; a < b; a, b = a+1, b-1 {
			bounded[a], bounded[b] = bounded[b], bounded[a]
		}
	}
	payload := map[string]any{"sources": bounded, "next_before_id": next, "has_more": more, "truncated": truncated, "trust": "untrusted_data", "omitted_refs": omitted}
	if call.Name == "list_session_evidence" {
		delete(payload, "next_before_id")
		payload["next_before_sequence"] = next
	}
	if truncated {
		payload["note"] = "结果有截断；按引用或更小limit继续读取，不代表已读完整历史"
	}
	data, e := json.Marshal(payload)
	return string(data), e
}
func messageID(id int64) string                                         { return "message:" + strconv.FormatInt(id, 10) }
func (s *ChatService) EnableSessionHistoryTools(h *SessionHistoryTools) { s.historyTools = h }
