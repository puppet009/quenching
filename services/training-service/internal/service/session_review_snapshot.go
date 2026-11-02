package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrReviewSourceNotFound = errors.New("review source not found or not owned")
var ErrReviewSessionActive = errors.New("whole training session must close before review")
var ErrReviewSourceRange = errors.New("invalid review source range")

type SnapshotStore interface {
	Eligibility(context.Context, int64, int64, int64) (rc.Eligibility, error)
	Freeze(context.Context, int64, int64, int64) (rc.Snapshot, error)
	Manifest(context.Context, int64, int64) (rc.Snapshot, error)
	Sources(context.Context, int64, int64, int64, rc.Cursor, int) (rc.SourcePage, error)
	Read(context.Context, int64, int64, int64, string, int, int) (rc.Fragment, error)
}
type SessionReviewSnapshotService struct{ SnapshotStore }

func NewSessionReviewSnapshotService(store SnapshotStore) *SessionReviewSnapshotService {
	return &SessionReviewSnapshotService{store}
}
func CheckReviewEligibility(e rc.Eligibility, u, s, p int64) (rc.Eligibility, error) {
	if u <= 0 || s <= 0 || e.UserID != u || e.SessionID != s || e.ProjectID != p {
		return rc.Eligibility{}, ErrReviewSourceNotFound
	}
	e.CanGenerate = false
	e.AuditComplete = false
	if e.BrainPhase != "" {
		e.CanGenerate = (e.BrainPhase == "closing" || e.BrainPhase == "reviewing" || e.BrainPhase == "completed" || e.BrainPhase == "failed") && (e.CleanupStatus == "succeeded" || e.CleanupStatus == "failed")
		e.AuditComplete = e.CleanupStatus == "succeeded"
	} else {
		e.CanGenerate = e.SessionStatus == "completed" || e.SessionStatus == "failed" || e.SessionStatus == "aborted"
		e.AuditComplete = e.CleanupStatus == "succeeded"
	}
	return e, nil
}
func BuildReviewSnapshot(e rc.Eligibility, sources []rc.Source, metadata json.RawMessage, at time.Time) (rc.Snapshot, []rc.Source, error) {
	valid, err := CheckReviewEligibility(e, e.UserID, e.SessionID, e.ProjectID)
	if err != nil {
		return rc.Snapshot{}, nil, err
	}
	if !valid.CanGenerate {
		return rc.Snapshot{}, nil, ErrReviewSessionActive
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	if !json.Valid(metadata) {
		return rc.Snapshot{}, nil, errors.New("invalid snapshot metadata")
	}
	snap := rc.Snapshot{SessionID: e.SessionID, UserID: e.UserID, ProjectID: e.ProjectID, Version: 1, CleanupStatus: e.CleanupStatus, AuditComplete: valid.AuditComplete, FrozenAt: at.UTC(), SourceCount: int64(len(sources)), SourceURLs: []string{}, Metadata: json.RawMessage(RedactSessionText(string(metadata)))}
	rows := make([]rc.Source, 0, len(sources))
	seen := map[string]bool{}
	for i, src := range sources {
		if src.ID == "" || seen[src.ID] {
			return rc.Snapshot{}, nil, errors.New("duplicate or empty source identity")
		}
		seen[src.ID] = true
		src.Ordinal = int64(i + 1)
		src.Text = RedactSessionText(src.Text)
		src.ByteLength = len(src.Text)
		if src.Actor == "student_report" {
			src.Trust = "user_statement"
			src.Role = "user"
			src.Successful = false
			rows = append(rows, src)
			continue
		}
		switch src.Kind {
		case "message":
			if src.Role == "user" {
				src.Trust = "user_statement"
			} else if src.Role == "assistant" {
				src.Trust = "assistant_suggestion"
			} else {
				return rc.Snapshot{}, nil, errors.New("invalid message role")
			}
			id, _ := strconv.ParseInt(strings.TrimPrefix(src.ID, "message:"), 10, 64)
			if id > snap.MessageWatermark {
				snap.MessageWatermark = id
			}
			src.Successful = false
		case "fault":
			src.Trust = "fault_reference"
			src.Actor = "platform"
		case "challenge":
			src.Trust = "challenge"
			src.Actor = "platform"
		case "assessment":
			src.Trust = "assessment"
			src.Actor = "platform"
			v, _ := strconv.ParseInt(strings.TrimPrefix(src.ID, "assessment:"), 10, 64)
			if v > snap.AssessmentVersion {
				snap.AssessmentVersion = v
			}
		case "operation_evidence", "operation_text":
			if src.Actor != "student" && src.Actor != "assistant" && src.Actor != "platform" {
				return rc.Snapshot{}, nil, errors.New("invalid operation actor")
			}
			src.Trust = "platform_observation"
			src.Successful = false
		case "verification", "assistant_file_change":
			if src.Actor != "platform" {
				return rc.Snapshot{}, nil, errors.New("public audit requires platform actor")
			}
			src.Trust = "platform_observation"
		default:
			if src.Actor == "student_report" {
				src.Trust = "user_statement"
				src.Role = "user"
				src.Successful = false
			} else if src.Actor == "student" || src.Actor == "simulator" {
				src.Trust = "platform_observation"
			} else {
				return rc.Snapshot{}, nil, fmt.Errorf("private source actor: %s", src.Actor)
			}
		}
		rows = append(rows, src)
	}
	SetReviewSnapshotHash(&snap, rows)
	return snap, rows, nil
}

// BudgetReviewSourceFragment chooses bytes using the encoded JSON cost, preserving UTF-8 boundaries.
func BudgetReviewSourceFragment(src rc.Source, offset, budget int) (rc.Fragment, error) {
	full, err := ReviewSourceFragment(src, offset, 8192)
	if err != nil {
		return rc.Fragment{}, err
	}
	encoded, _ := json.Marshal(full)
	if len(encoded) <= budget {
		return full, nil
	}
	low, high := 1, 8192
	var best rc.Fragment
	found := false
	for low <= high {
		mid := (low + high) / 2
		candidate, e := ReviewSourceFragment(src, offset, mid)
		if e != nil {
			low = mid + 1
			continue
		}
		raw, _ := json.Marshal(candidate)
		if len(raw) <= budget {
			best = candidate
			found = true
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if !found {
		return rc.Fragment{}, ErrReviewSourceRange
	}
	return best, nil
}
func SetReviewSnapshotHash(s *rc.Snapshot, rows []rc.Source) {
	s.Hash = ""
	raw, _ := json.Marshal(struct {
		Manifest rc.Snapshot `json:"manifest"`
		Sources  []rc.Source `json:"sources"`
	}{*s, rows})
	sum := sha256.Sum256(raw)
	s.Hash = hex.EncodeToString(sum[:])
}
func ReviewSourceFragment(src rc.Source, offset, limit int) (rc.Fragment, error) {
	if limit <= 0 || limit > 8192 || offset < 0 || offset > len(src.Text) || !utf8.ValidString(src.Text) || !utf8.ValidString(src.Text[:offset]) {
		return rc.Fragment{}, ErrReviewSourceRange
	}
	end := offset + limit
	if end > len(src.Text) {
		end = len(src.Text)
	}
	for end > offset && !utf8.ValidString(src.Text[offset:end]) {
		end--
	}
	if end == offset && end < len(src.Text) {
		return rc.Fragment{}, ErrReviewSourceRange
	}
	src.Text = src.Text[offset:end]
	return rc.Fragment{Source: src, OffsetBytes: offset, NextOffsetBytes: end, Complete: end == src.ByteLength}, nil
}
