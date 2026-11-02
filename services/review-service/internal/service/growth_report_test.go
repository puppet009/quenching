package service

import (
	"context"
	"encoding/json"
	"errors"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"strings"
	"testing"
)

type frozenChangeCollector struct{ calls int }

func (c *frozenChangeCollector) CollectAndBuild(context.Context, int64, int64) (*model.ReviewSection, error) {
	c.calls++
	return &model.ReviewSection{SectionType: "assistant_changes", Content: "late external change"}, nil
}

type frozenSectionsWriter struct{ sections [][]model.ReviewSection }

func (w *frozenSectionsWriter) CompleteGrowth(_ context.Context, _ GrowthJobLease, _ GrowthJob, sections []model.ReviewSection, _ rc.GrowthReport) error {
	w.sections = append(w.sections, sections)
	if len(w.sections) == 1 {
		return errors.New("first write interrupted")
	}
	return nil
}
func TestGrowthFinalizeUsesFrozenChangesAcrossRetry(t *testing.T) {
	raw := `{"path":"order.go","before_existed":true,"before_content":"old","after_content":"` + strings.Repeat("x", 9000) + `FROZEN_TAIL"}`
	sources := &lifecycleSources{closed: true, snapshot: rc.Snapshot{SessionID: 1, UserID: 2, Version: 1, Hash: "frozen", SourceCount: 1}, rows: []rc.Source{{ID: "file-change:1", Kind: "assistant_file_change", Actor: "platform", Trust: "platform_observation", Ordinal: 1, Text: raw, ByteLength: len(raw)}}}
	writer := &frozenSectionsWriter{}
	collector := &frozenChangeCollector{}
	finalizer := NewGrowthFinalizer(sources, writer, collector)
	job := GrowthJob{ReviewID: 1, Snapshot: sources.snapshot, Phase: "finalize", Cursor: rc.Cursor{Ordinal: 2}, Report: &rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`)}}
	if finalizer.Finalize(context.Background(), GrowthJobLease{}, job) == nil {
		t.Fatal("first write did not fail")
	}
	if err := finalizer.Finalize(context.Background(), GrowthJobLease{}, job); err != nil {
		t.Fatal(err)
	}
	if collector.calls != 0 {
		t.Fatal("live collector changed frozen report")
	}
	first, _ := json.Marshal(writer.sections[0])
	second, _ := json.Marshal(writer.sections[1])
	hasTail := false
	for _, section := range writer.sections[0] {
		hasTail = hasTail || strings.Contains(string(section.ContentData), "FROZEN_TAIL")
	}
	if string(first) != string(second) || !hasTail {
		t.Fatal("frozen change missing or retry drift")
	}
}

type growthOwnedReviewRepo struct{ ReviewRepo }

func (growthOwnedReviewRepo) GetByID(context.Context, int64) (*model.Review, error) {
	return &model.Review{ID: 1, UserID: 2, SessionID: 3, ProjectID: 4}, nil
}

type growthOwnedSources struct {
	GrowthSnapshotStore
	active bool
	reads  int
}

func (s *growthOwnedSources) Eligibility(_ context.Context, u, id, p int64) (rc.Eligibility, error) {
	return rc.Eligibility{UserID: u, SessionID: id, ProjectID: p, CanGenerate: !s.active}, nil
}
func (s *growthOwnedSources) Read(_ context.Context, u, id, v int64, ref string, offset, limit int) (rc.Fragment, error) {
	s.reads++
	return rc.Fragment{Source: rc.Source{ID: ref, Text: "frozen"}, Complete: true}, nil
}
func (s *growthMemoryStore) Load(context.Context, int64) (GrowthJob, error) { return s.job, nil }
func TestGrowthSourceOwnership(t *testing.T) {
	jobs := &growthMemoryStore{job: GrowthJob{Snapshot: rc.Snapshot{UserID: 2, SessionID: 3, ProjectID: 4, Version: 1}}}
	sources := &growthOwnedSources{}
	reports := NewGrowthReportService(NewReviewService(growthOwnedReviewRepo{}, nil, nil), jobs, sources)
	if _, err := reports.ReadSource(context.Background(), 99, 1, "e:1", 0, 8192); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign source", err)
	}
	if sources.reads != 0 {
		t.Fatal("read before ownership check")
	}
	sources.active = true
	if _, err := reports.ReadSource(context.Background(), 2, 1, "fault:1", 0, 8192); !errors.Is(err, ErrNotFound) {
		t.Fatal("running fault answer exposed")
	}
	sources.active = false
	if _, err := reports.ReadSource(context.Background(), 2, 1, "e:1", 0, 8192); err != nil {
		t.Fatal(err)
	}
}

type growthDispatchProducer struct {
	fail  bool
	count int
}

func (p *growthDispatchProducer) Publish(context.Context, *ReviewTask) error {
	p.count++
	if p.fail {
		return errors.New("queue offline")
	}
	return nil
}
func TestCheckpointCommitThenQueueFailureIsRecoverable(t *testing.T) {
	jobs := &growthMemoryStore{job: GrowthJob{ReviewID: 1, Phase: "analyze"}}
	producer := &growthDispatchProducer{fail: true}
	dispatcher := NewGrowthDispatcher(jobs, producer)
	if dispatcher.Dispatch(context.Background()) == nil {
		t.Fatal("queue failure hidden")
	}
	producer.fail = false
	if err := dispatcher.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if producer.count != 2 || jobs.job.Phase != "analyze" {
		t.Fatal("durable due job lost")
	}
}
func TestGrowthAbilityUsesFrozenAssessmentAndUnknownDimensions(t *testing.T) {
	var unknown map[string]any
	if err := json.Unmarshal(FrozenGrowthAbility(nil), &unknown); err != nil {
		t.Fatal(err)
	}
	if unknown["overall_level"] != "unknown" || len(unknown["dimensions"].(map[string]any)) != 7 {
		t.Fatal("unknown became zero")
	}
	sources := []rc.Source{{ID: "a:1", Trust: "assessment", Actor: "platform", Text: `{"overall_level":"intermediate","sufficiency":"sufficient","dimensions":{}}`}}
	if err := json.Unmarshal(FrozenGrowthAbility(sources), &unknown); err != nil {
		t.Fatal(err)
	}
	if unknown["overall_level"] != "unknown" {
		t.Fatal("insufficient promotion accepted")
	}
}
