package service

import (
	"context"
	"encoding/json"
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"strings"
	"testing"
	"time"
)

func TestGrowthBatchProcessesTailBeforeFullCoverage(t *testing.T) {
	page := rc.SourcePage{Fragments: []rc.Fragment{{Source: rc.Source{ID: "e:1", Ordinal: 1, Text: "tail", ByteLength: 9000}, OffsetBytes: 8192, NextOffsetBytes: 9000, Complete: true}}, Next: rc.Cursor{Ordinal: 2}, Done: true}
	in, next, err := BuildGrowthBatch(page, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Fragments) != 1 || next.Ordinal != 2 {
		t.Fatal("tail omitted")
	}
	raw, _ := json.Marshal(in)
	if len(raw) > 80*1024 {
		t.Fatal("over budget")
	}
}

type growthMemoryStore struct {
	job       GrowthJob
	lease     GrowthJobLease
	artifacts map[int][]rc.AnalysisArtifact
	lost      bool
}

func (s *growthMemoryStore) Ensure(_ context.Context, id int64, snap rc.Snapshot) error {
	if s.job.ReviewID == 0 {
		s.job = GrowthJob{ReviewID: id, Snapshot: snap, Phase: "analyze", Cursor: rc.Cursor{Ordinal: 1}, Version: 1}
	}
	return nil
}
func (s *growthMemoryStore) Claim(_ context.Context, id int64, token string, _ time.Duration) (GrowthJobLease, error) {
	s.lease = GrowthJobLease{ReviewID: id, Token: token, Fence: s.lease.Fence + 1}
	return s.lease, nil
}
func (s *growthMemoryStore) Renew(context.Context, GrowthJobLease, time.Duration) (bool, error) {
	return !s.lost, nil
}
func (s *growthMemoryStore) State(context.Context, GrowthJobLease) (GrowthJob, error) {
	return s.job, nil
}
func (s *growthMemoryStore) Commit(_ context.Context, l GrowthJobLease, v int64, next GrowthJob, a *rc.AnalysisArtifact) error {
	if s.lost || l != s.lease || v != s.job.Version {
		return ErrGrowthLeaseLost
	}
	if a != nil {
		if s.artifacts == nil {
			s.artifacts = map[int][]rc.AnalysisArtifact{}
		}
		level := s.job.ReduceLevel
		if s.job.Phase == "reduce" {
			level++
		}
		s.artifacts[level] = append(s.artifacts[level], *a)
	}
	next.Version = v + 1
	s.job = next
	return nil
}
func (s *growthMemoryStore) Release(context.Context, GrowthJobLease) error { return nil }
func (s *growthMemoryStore) Due(context.Context, int) ([]int64, error) {
	return []int64{s.job.ReviewID}, nil
}
func (s *growthMemoryStore) Artifacts(_ context.Context, _ GrowthJobLease, level int) ([]rc.AnalysisArtifact, error) {
	return s.artifacts[level], nil
}

type growthFakeSources struct{ total int64 }

func (s growthFakeSources) Sources(_ context.Context, _, _, _ int64, c rc.Cursor, _ int) (rc.SourcePage, error) {
	src := rc.Source{ID: time.Unix(c.Ordinal, 0).String(), Ordinal: c.Ordinal, Trust: "platform_observation", Text: "x", ByteLength: 1}
	return rc.SourcePage{Fragments: []rc.Fragment{{Source: src, NextOffsetBytes: 1, Complete: true}}, Next: rc.Cursor{Ordinal: c.Ordinal + 1}, Done: c.Ordinal == s.total}, nil
}

type growthFakeAI struct {
	fail  bool
	calls int
	hash  string
}

func (a *growthFakeAI) Infer(_ context.Context, in GrowthInferenceInput) (GrowthInferenceResult, error) {
	a.calls++
	if a.hash != "" && a.hash != in.SnapshotHash {
		return GrowthInferenceResult{}, errors.New("snapshot changed")
	}
	a.hash = in.SnapshotHash
	if a.fail {
		return GrowthInferenceResult{}, errors.New("offline")
	}
	if in.Mode == "synthesize" {
		return GrowthInferenceResult{Report: &rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`)}}, nil
	}
	return GrowthInferenceResult{Artifact: &rc.AnalysisArtifact{SchemaVersion: 1, Coverage: json.RawMessage(`{}`)}}, nil
}

func TestGrowthPipelineResumesSameSnapshotAfterRestart(t *testing.T) {
	ctx := context.Background()
	store := &growthMemoryStore{}
	snap := rc.Snapshot{SessionID: 1, UserID: 2, Hash: "frozen", Version: 1, SourceCount: 55}
	_ = store.Ensure(ctx, 1, snap)
	ai := &growthFakeAI{}
	for i := 0; i < 55; i++ {
		p := NewGrowthPipeline(store, growthFakeSources{55}, ai)
		if err := p.Step(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if i < 54 && store.job.Phase != "analyze" {
			t.Fatal("premature completion")
		}
	}
	if store.job.Cursor.Ordinal != 56 || store.job.Snapshot.Hash != "frozen" || len(store.artifacts[0]) != 55 {
		t.Fatal("missing earlier sources")
	}
}
func TestGrowthLeaseRejectsLateInference(t *testing.T) {
	s := &growthMemoryStore{lost: true, job: GrowthJob{Version: 1}}
	if !errors.Is(s.Commit(context.Background(), GrowthJobLease{}, 1, s.job, nil), ErrGrowthLeaseLost) {
		t.Fatal("late commit accepted")
	}
}
func TestGrowthModelFailureDoesNotAdvanceCursor(t *testing.T) {
	s := &growthMemoryStore{}
	_ = s.Ensure(context.Background(), 1, rc.Snapshot{SessionID: 1, UserID: 2, Version: 1, Hash: "frozen", SourceCount: 2})
	before := s.job.Cursor
	p := NewGrowthPipeline(s, growthFakeSources{2}, &growthFakeAI{fail: true})
	if p.Step(context.Background(), 1) == nil {
		t.Fatal("failure hidden")
	}
	if s.job.Cursor != before || s.job.Attempts != 1 {
		t.Fatal("failed model advanced cursor")
	}
}
func TestGrowthReductionConvergesWithinBudget(t *testing.T) {
	s := &growthMemoryStore{}
	_ = s.Ensure(context.Background(), 1, rc.Snapshot{SessionID: 1, UserID: 2, Version: 1, Hash: "frozen", SourceCount: 55})
	p := NewGrowthPipeline(s, growthFakeSources{55}, &growthFakeAI{})
	for i := 0; i < 70 && s.job.Phase != "finalize"; i++ {
		if err := p.Step(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
	}
	if s.job.Phase != "finalize" || s.job.Report == nil {
		t.Fatal("did not converge")
	}
	if p.Step(context.Background(), 1) == nil {
		t.Fatal("completed without finalizer")
	}
}

func TestGrowthReductionActuallyCombinesOversizedHistory(t *testing.T) {
	store := &growthMemoryStore{artifacts: map[int][]rc.AnalysisArtifact{}}
	_ = store.Ensure(context.Background(), 1, rc.Snapshot{SessionID: 1, UserID: 2, Version: 1, Hash: "frozen", SourceCount: 55})
	store.job.Phase = "reduce"
	for i := 0; i < 55; i++ {
		a := rc.AnalysisArtifact{SchemaVersion: 1, Coverage: growthArtifactCoverage(nil, []rc.Source{{ID: "e:1", Trust: "platform_observation", Actor: "platform"}})}
		for n := 0; n < 12; n++ {
			a.Claims = append(a.Claims, rc.Claim{Section: "timeline", Category: "observed", Text: strings.Repeat("x", 1000), SourceRefs: []string{"e:1"}})
		}
		store.artifacts[0] = append(store.artifacts[0], a)
	}
	ai := &growthFakeAI{}
	p := NewGrowthPipeline(store, growthFakeSources{55}, ai)
	for i := 0; i < 25 && store.job.Phase != "finalize"; i++ {
		before := ai.calls
		if err := p.Step(context.Background(), 1); err != nil {
			t.Fatal(err)
		}
		if ai.calls-before > 1 {
			t.Fatal("multiple model calls in one step")
		}
	}
	if store.job.Phase != "finalize" || store.job.ReduceLevel < 1 || ai.calls < 2 {
		t.Fatal("reduction did not converge")
	}
}
