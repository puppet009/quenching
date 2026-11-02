package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/training-service/internal/model"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type MemoryEntry struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	Refs        []string `json:"refs"`
	ChallengeID string   `json:"challenge_id,omitempty"`
}
type MemoryDocument struct {
	Entries     []MemoryEntry    `json:"entries"`
	GlobalNotes []int            `json:"global_notes"`
	Challenges  map[string][]int `json:"challenges"`
}

func decodeMemory(raw string) (MemoryDocument, error) {
	if raw == "" {
		raw = `{"entries":[]}`
	}
	var d MemoryDocument
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&d); e != nil {
		return d, e
	}
	if e := decoder.Decode(&struct{}{}); e != io.EOF {
		return d, errors.New("extra JSON in memory")
	}
	if len(d.Entries) > 128 {
		return d, errors.New("too many memory entries")
	}
	return d, nil
}
func validateMemory(raw string, sources []model.MemorySource) (string, error) {
	d, e := decodeMemory(raw)
	if e != nil {
		return "", e
	}
	owned := map[string]model.MemorySource{}
	for _, s := range sources {
		owned[s.ID] = s
	}
	d.GlobalNotes = []int{}
	d.Challenges = map[string][]int{}
	for i := range d.Entries {
		v := &d.Entries[i]
		if v.Text == "" || len([]rune(v.Text)) > 2000 || len(v.Refs) == 0 || len(v.Refs) > 8 {
			return "", errors.New("invalid memory entry")
		}
		user, assistant, observation, successful, scoped := false, false, false, false, v.ChallengeID == ""
		for _, ref := range v.Refs {
			s, ok := owned[ref]
			if !ok {
				return "", errors.New("foreign memory reference")
			}
			user = user || s.Role == "user" || s.Actor == "student_report"
			assistant = assistant || s.Role == "assistant"
			observation = observation || (s.Kind != "message" && s.Actor != "student_report" && s.Role != "user")
			successful = successful || (s.Successful && s.Role != "user" && s.Role != "assistant" && s.Actor != "student_report")
			scoped = scoped || s.ChallengeID == v.ChallengeID
		}
		if !scoped {
			return "", errors.New("unknown challenge scope")
		}
		switch v.Kind {
		case "user_statement":
			if !user {
				return "", errors.New("statement requires user source")
			}
		case "assistant_suggestion":
			if !assistant {
				return "", errors.New("suggestion requires assistant source")
			}
		case "observation":
			if !observation {
				return "", errors.New("observation requires evidence")
			}
		case "verified":
			if !successful {
				return "", errors.New("verified requires successful observation")
			}
		case "hypothesis":
		default:
			return "", errors.New("invalid memory kind")
		}
		v.Text = RedactSessionText(v.Text)
		if v.ChallengeID == "" {
			d.GlobalNotes = append(d.GlobalNotes, i)
		} else {
			d.Challenges[v.ChallengeID] = append(d.Challenges[v.ChallengeID], i)
		}
	}
	if d.Entries == nil {
		d.Entries = []MemoryEntry{}
	}
	data, e := json.Marshal(d)
	if len(data) > 16*1024 {
		return "", errors.New("memory exceeds byte budget")
	}
	return string(data), e
}
func clipMemoryText(s string, n int) (string, bool) {
	s = RedactSessionText(s)
	if len(s) <= n {
		return s, false
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s, true
}

type SessionMemoryService struct {
	store      SessionMemoryStore
	summarizer MemorySummarizer
}

func NewSessionMemoryService(s SessionMemoryStore, m MemorySummarizer) *SessionMemoryService {
	return &SessionMemoryService{s, m}
}
func (s *SessionMemoryService) previousSources(ctx context.Context, uid, sid int64, raw string) ([]model.MemorySource, error) {
	d, e := decodeMemory(raw)
	if e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	var all []model.MemorySource
	var mids []int64
	var eids []string
	flush := func() error {
		if len(mids)+len(eids) == 0 {
			return nil
		}
		p, e := s.store.ReadReferences(ctx, uid, sid, mids, eids)
		if e == nil {
			for _, src := range p.Sources {
				src.Text, _ = clipMemoryText(src.Text, 512)
				src.Truncated = true
				all = append(all, src)
			}
		}
		mids = nil
		eids = nil
		return e
	}
	for _, v := range d.Entries {
		for _, ref := range v.Refs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if strings.HasPrefix(ref, "message:") {
				id, e := strconv.ParseInt(strings.TrimPrefix(ref, "message:"), 10, 64)
				if e != nil || id <= 0 {
					return nil, errors.New("invalid memory reference")
				}
				mids = append(mids, id)
			} else {
				eids = append(eids, ref)
			}
			if len(mids)+len(eids) == 50 {
				if e = flush(); e != nil {
					return nil, e
				}
			}
		}
	}
	if e = flush(); e != nil {
		return nil, e
	}
	return all, nil
}
func (s *SessionMemoryService) Prepare(ctx context.Context, uid, sid int64, current string) (model.AssistantMemoryContext, error) {
	out := model.AssistantMemoryContext{}
	scope, ok := CurrentChatLease(ctx)
	if !ok || scope.UserID != uid || scope.SessionID != sid {
		return out, ErrChatLeaseLost
	}
	page, e := s.store.History(ctx, uid, sid, 0, 20)
	if e != nil {
		return out, e
	}
	m, e := s.store.Load(ctx, uid, sid)
	if e != nil {
		return out, e
	}
	if m == nil {
		return out, errors.New("missing memory snapshot")
	}
	cutoff := int64(0)
	if len(page.Messages) > 0 {
		cutoff = page.Messages[0].ID
	}
	recentBudget := 32 * 1024
	for i := len(page.Messages) - 1; i >= 0; i-- {
		v := page.Messages[i]
		text, tr := clipMemoryText(v.Content, 8*1024)
		text = fmt.Sprintf("[message:%d] ", v.ID) + text
		if tr {
			text += " [truncated; read_session_history]"
			out.Degraded = true
		}
		if len(text) > recentBudget {
			out.Degraded = true
			continue
		}
		recentBudget -= len(text)
		out.History = append(out.History, model.HistoryTurn{Role: v.Role, Content: text})
	}
	for a, b := 0, len(out.History)-1; a < b; a, b = a+1, b-1 {
		out.History[a], out.History[b] = out.History[b], out.History[a]
	}
	if out.Degraded && len(page.Messages) > 0 {
		out.History = append([]model.HistoryTurn{{Role: "user", Content: fmt.Sprintf("[untrusted history metadata] Recent range message:%d to message:%d is partly omitted or truncated; read_session_history can retrieve exact records.", cutoff, page.Messages[len(page.Messages)-1].ID)}}, out.History...)
	}
	for iteration := 0; iteration < 3; iteration++ {
		p, e := s.store.Sources(ctx, uid, sid, m.MessageWatermark, m.EvidenceWatermark, 100)
		if e != nil {
			return out, e
		}
		var selected []model.MemorySource
		mw, ew := m.MessageWatermark, m.EvidenceWatermark
		msgCount, evidenceCount, size := 0, 0, 0
		bounded := false
		for _, v := range p.Sources {
			if v.Kind == "message" && cutoff > 0 && v.Sequence >= cutoff {
				continue
			}
			text, truncated := clipMemoryText(v.Text, 8*1024)
			v.Text = text
			v.Truncated = v.Truncated || truncated
			// A prefix is not complete coverage. Keep this source and later ones pending.
			if v.Truncated {
				out.Degraded = true
				bounded = true
				break
			}
			encoded, _ := json.Marshal(v)
			if size+len(encoded) > 48*1024 {
				bounded = true
				break
			}
			selected = append(selected, v)
			size += len(encoded)
			if v.Kind == "message" {
				msgCount++
				if v.Sequence > mw {
					mw = v.Sequence
				}
			} else {
				evidenceCount++
				if v.Sequence > ew {
					ew = v.Sequence
				}
			}
		}
		if len(selected) == 0 || (msgCount < 20 && evidenceCount < 20 && !p.HasMore && !bounded) {
			break
		}
		previous, e := s.previousSources(ctx, uid, sid, m.MemoryJSON)
		if e != nil {
			return out, e
		}
		inputSources := append(previous, selected...)
		in := model.MemorySummaryInput{SessionID: sid, PreviousJSON: m.MemoryJSON, Sources: inputSources}
		inputRaw, _ := json.Marshal(in)
		if len(inputRaw) > 80*1024 || s.summarizer == nil {
			out.Degraded = true
			break
		}
		result, e := s.summarizer.Summarize(ctx, in)
		if e != nil {
			out.Degraded = true
			break
		}
		if result.ActualModel != "qwen3.8-flash" {
			out.Degraded = true
			break
		}
		valid, e := validateMemory(result.MemoryJSON, inputSources)
		if e != nil {
			out.Degraded = true
			break
		}
		next := model.SessionMemory{SchemaVersion: 1, MessageWatermark: mw, EvidenceWatermark: ew, MemoryJSON: valid, ActualModel: result.ActualModel, PromptTokens: result.PromptTokens, CompletionTokens: result.CompletionTokens}
		applied, e := s.store.SaveCAS(ctx, uid, sid, m.Version, scope.Lease, next)
		if e != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			out.Degraded = true
			break
		}
		if !applied {
			out.Degraded = true
			break
		}
		next.Version = m.Version + 1
		m = &next
	}
	previous, e := s.previousSources(ctx, uid, sid, m.MemoryJSON)
	if e != nil {
		return out, e
	}
	valid, e := validateMemory(m.MemoryJSON, previous)
	if e != nil {
		return out, e
	}
	pending, e := s.store.Sources(ctx, uid, sid, m.MessageWatermark, m.EvidenceWatermark, 100)
	if e != nil {
		return out, e
	}
	if pending.HasMore {
		out.Degraded = true
	}
	unreviewed := []model.MemorySource{}
	observations := []model.MemorySource{}
	messageBudget, evidenceBudget := 16*1024, 16*1024
	for _, v := range pending.Sources {
		if v.Kind == "message" && cutoff > 0 && v.Sequence >= cutoff {
			continue
		}
		text, tr := clipMemoryText(v.Text, 8*1024)
		v.Text = text
		v.Truncated = v.Truncated || tr
		encoded, _ := json.Marshal(v)
		if v.Truncated {
			out.Degraded = true
		}
		if v.Kind == "message" {
			if len(encoded) > messageBudget {
				out.Degraded = true
				continue
			}
			unreviewed = append(unreviewed, v)
			messageBudget -= len(encoded)
		} else {
			if len(encoded) > evidenceBudget {
				out.Degraded = true
				continue
			}
			observations = append(observations, v)
			evidenceBudget -= len(encoded)
		}
	}
	var memory any
	if e = json.Unmarshal([]byte(valid), &memory); e != nil {
		return out, e
	}
	body := map[string]any{"memory": memory, "uncovered_messages": unreviewed, "evidence": observations, "coverage": map[string]int64{"message_id": m.MessageWatermark, "evidence_sequence": m.EvidenceWatermark}, "degraded": out.Degraded}
	if reader, ok := s.store.(interface {
		OperationContext(context.Context, int64, int64) (json.RawMessage, error)
	}); ok {
		operations, err := reader.OperationContext(ctx, uid, sid)
		if err != nil {
			return out, err
		}
		body["operations"] = operations
	}
	encoded, e := json.Marshal(body)
	if e != nil || len(encoded) > 80*1024 {
		return out, errors.New("memory context exceeds budget")
	}
	out.MemoryJSON = string(encoded)
	out.CoverageMessageID = m.MessageWatermark
	out.CoverageEvidenceSequence = m.EvidenceWatermark
	return out, nil
}
func (s *ChatService) EnableSessionMemoryContext(m *SessionMemoryService) { s.memoryContext = m }
