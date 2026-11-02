package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"strings"
	"testing"
)

type lifecycleSources struct {
	closed   bool
	snapshot rc.Snapshot
	rows     []rc.Source
}

func (s *lifecycleSources) Eligibility(_ context.Context, u, id, p int64) (rc.Eligibility, error) {
	return rc.Eligibility{UserID: u, SessionID: id, ProjectID: p, CanGenerate: s.closed}, nil
}
func (s *lifecycleSources) Freeze(context.Context, int64, int64, int64) (rc.Snapshot, error) {
	if !s.closed {
		return rc.Snapshot{}, errors.New("active")
	}
	return s.snapshot, nil
}
func (s *lifecycleSources) Manifest(context.Context, int64, int64) (rc.Snapshot, error) {
	return s.snapshot, nil
}
func (s *lifecycleSources) Read(_ context.Context, _, _, _ int64, ref string, offset, limit int) (rc.Fragment, error) {
	for _, src := range s.rows {
		if src.ID == ref {
			end := offset + limit
			if end > len(src.Text) {
				end = len(src.Text)
			}
			fragment := rc.Fragment{Source: src, OffsetBytes: offset, NextOffsetBytes: end, Complete: end == len(src.Text)}
			fragment.Source.Text = src.Text[offset:end]
			return fragment, nil
		}
	}
	return rc.Fragment{}, ErrNotFound
}
func (s *lifecycleSources) Sources(ctx context.Context, u, id, v int64, c rc.Cursor, budget int) (rc.SourcePage, error) {
	if u != s.snapshot.UserID || id != s.snapshot.SessionID || v != s.snapshot.Version {
		return rc.SourcePage{}, ErrNotFound
	}
	page := rc.SourcePage{Next: c}
	for page.Next.Ordinal <= int64(len(s.rows)) {
		src := s.rows[page.Next.Ordinal-1]
		fragment, err := s.Read(ctx, u, id, v, src.ID, page.Next.OffsetBytes, 8192)
		if err != nil {
			return page, err
		}
		candidate := append(append([]rc.Fragment{}, page.Fragments...), fragment)
		raw, _ := json.Marshal(candidate)
		if len(raw) > budget {
			break
		}
		page.Fragments = candidate
		if fragment.Complete {
			page.Next = rc.Cursor{Ordinal: page.Next.Ordinal + 1}
		} else {
			page.Next.OffsetBytes = fragment.NextOffsetBytes
		}
	}
	page.Done = page.Next.Ordinal > int64(len(s.rows))
	return page, nil
}

type lifecycleAI struct {
	fail  bool
	tail  bool
	calls int
}

func (a *lifecycleAI) Infer(_ context.Context, in GrowthInferenceInput) (GrowthInferenceResult, error) {
	a.calls++
	if a.fail {
		a.fail = false
		return GrowthInferenceResult{}, errors.New("model interrupted")
	}
	result := GrowthInferenceResult{ActualModel: "qwen3.8-flash", PromptVersion: "growth:v1", PromptTokens: 10, CompletionTokens: 5, RequestID: fmt.Sprint(a.calls)}
	if in.Mode == "synthesize" {
		report := &rc.GrowthReport{SchemaVersion: 1, Overview: json.RawMessage(`{}`), Ability: json.RawMessage(`{"overall_level":"unknown"}`)}
		for _, artifact := range in.Artifacts {
			report.Claims = append(report.Claims, artifact.Claims...)
		}
		report.Architecture = []rc.ArchitectureOption{{Problem: "重试引起重复处理", Option: "幂等约束", Preconditions: "业务键稳定", Benefits: "减少重复副作用", Costs: "唯一键冲突处理", Verification: "重复提交压测", SourceRefs: []string{"challenge:1"}}}
		report.Practices = []rc.Practice{{Dimension: "regression", Goal: "核对修复后的业务结果", Actions: []string{"重复请求并检查数据"}, SuccessCriteria: "业务副作用恰好发生一次", SourceRefs: []string{"challenge:1"}, ProjectID: 999, TargetAvailable: true}}
		result.Report = report
		return result, nil
	}
	artifact := &rc.AnalysisArtifact{SchemaVersion: 1, Coverage: json.RawMessage(`{}`)}
	for _, fragment := range in.Fragments {
		if strings.Contains(fragment.Source.Text, "TAIL_VERIFIED") {
			a.tail = true
		}
		category := "observed"
		if fragment.Source.Trust == "user_statement" || fragment.Source.Trust == "assistant_suggestion" {
			category = fragment.Source.Trust
		}
		artifact.Claims = append(artifact.Claims, rc.Claim{Section: "timeline", Category: category, Text: "记录中的排查过程", SourceRefs: []string{fragment.Source.ID}, ChallengeID: fragment.Source.ChallengeID})
	}
	for _, previous := range in.Artifacts {
		artifact.Claims = append(artifact.Claims, previous.Claims...)
	}
	result.Artifact = artifact
	return result, nil
}

type lifecycleReviewRepo struct {
	ReviewRepo
	row    *model.Review
	store  *growthMemoryStore
	report *rc.GrowthReport
	writes int
}

func (r *lifecycleReviewRepo) GetBySessionID(context.Context, int64) (*model.Review, error) {
	if r.row == nil {
		return nil, ErrNotFound
	}
	copy := *r.row
	return &copy, nil
}
func (r *lifecycleReviewRepo) GetByID(context.Context, int64) (*model.Review, error) {
	return r.GetBySessionID(context.Background(), 1)
}
func (r *lifecycleReviewRepo) CreateGrowth(ctx context.Context, rv *model.Review, snap rc.Snapshot) error {
	if r.row != nil {
		return errors.New("unique session")
	}
	rv.ID = 1
	copy := *rv
	r.row = &copy
	return r.store.Ensure(ctx, rv.ID, snap)
}
func (r *lifecycleReviewRepo) CompleteGrowth(ctx context.Context, l GrowthJobLease, job GrowthJob, sections []model.ReviewSection, report rc.GrowthReport) error {
	if len(sections) != 6 || job.Cursor.Ordinal <= job.Snapshot.SourceCount {
		return errors.New("incomplete report")
	}
	next := job
	next.Phase = "completed"
	next.Report = &report
	if err := r.store.Commit(ctx, l, job.Version, next, nil); err != nil {
		return err
	}
	r.row.GenerationStatus = "completed"
	r.report = &report
	r.writes++
	return nil
}

func TestWholeSessionGrowthReviewLifecycle(t *testing.T) {
	ctx := context.Background()
	sources := &lifecycleSources{}
	for i := 1; i <= 65; i++ {
		text := fmt.Sprint("历史消息", i)
		if i == 1 {
			text = strings.Repeat("x", 13000) + "TAIL_VERIFIED"
		}
		trust := "assistant_suggestion"
		actor := "assistant"
		if i%2 == 1 {
			trust = "user_statement"
			actor = "student_report"
		}
		sources.rows = append(sources.rows, rc.Source{ID: fmt.Sprint("message:", i), Ordinal: int64(i), Kind: "message", Trust: trust, Actor: actor, Text: text, ByteLength: len(text)})
	}
	for i, text := range []string{`{"status":"verified_repair","defect_ids":[1]}`, `{"status":"verification_failed","defect_ids":[2]}`, `{"overall_level":"unknown","sufficiency":"partial","dimensions":{}}`, `{"overall_level":"intermediate","sufficiency":"sufficient","dimensions":{}}`} {
		kind := "challenge"
		if i >= 2 {
			kind = "assessment"
		}
		id := fmt.Sprintf("%s:%d", kind, i%2+1)
		sources.rows = append(sources.rows, rc.Source{ID: id, Ordinal: int64(len(sources.rows) + 1), Kind: kind, Trust: kind, Actor: "platform", ChallengeID: fmt.Sprint("c", i%2+1), Text: text, ByteLength: len(text)})
	}
	sources.snapshot = rc.Snapshot{SessionID: 1, UserID: 2, ProjectID: 0, Hash: "frozen-v1", Version: 1, SourceCount: int64(len(sources.rows)), MessageWatermark: 65, AssessmentVersion: 2, AuditComplete: false, CleanupStatus: "failed"}
	store := &growthMemoryStore{}
	repo := &lifecycleReviewRepo{store: store}
	producer := &growthDispatchProducer{fail: true}
	reviews := NewReviewService(repo, producer, nil).WithReviewEligibility(sources).WithGrowthSnapshots(sources)
	for i := 0; i < 2; i++ {
		if _, err := reviews.StartGeneration(ctx, 1, 2, 0); err == nil {
			t.Fatal("training generated report")
		}
	}
	if repo.row != nil || producer.count != 0 {
		t.Fatal("per-fault report")
	}
	sources.closed = true
	rv, err := reviews.StartGeneration(ctx, 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	repo.row.TechScore = 82
	repo.row.CollabScore = 71
	if _, err = reviews.StartGeneration(ctx, 1, 2, 0); err != nil {
		t.Fatal(err)
	}
	if rv.ID != 1 || producer.count != 1 {
		t.Fatal("duplicate session report")
	}
	dispatcher := NewGrowthDispatcher(store, producer)
	if dispatcher.Dispatch(ctx) == nil {
		t.Fatal("queue outage hidden")
	}
	producer.fail = false
	if err = dispatcher.Dispatch(ctx); err != nil {
		t.Fatal(err)
	}
	ai := &lifecycleAI{fail: true}
	pipeline := NewGrowthPipeline(store, sources, ai).WithFinalizer(NewGrowthFinalizer(sources, repo))
	cursor := store.job.Cursor
	if pipeline.Step(ctx, 1) == nil || store.job.Cursor != cursor {
		t.Fatal("model interruption advanced coverage")
	}
	oldLease := store.lease
	oldVersion := store.job.Version
	for i := 0; i < 30 && store.job.Phase != "completed"; i++ {
		if err = pipeline.Step(ctx, 1); err != nil {
			t.Fatal(err)
		}
		if store.job.Phase == "completed" && !ai.tail {
			t.Fatal("unread tail marked complete")
		}
	}
	if store.job.Phase != "completed" || repo.writes != 1 || store.job.Snapshot.Hash != "frozen-v1" || !ai.tail {
		t.Fatal("whole session incomplete", store.job.Phase)
	}
	if repo.row.TechScore != 82 || repo.row.CollabScore != 71 {
		t.Fatal("score overwritten")
	}
	if err = store.Commit(ctx, oldLease, oldVersion, store.job, nil); !errors.Is(err, ErrGrowthLeaseLost) {
		t.Fatal("late inference accepted")
	}
	var ability map[string]any
	_ = json.Unmarshal(repo.report.Ability, &ability)
	if ability["overall_level"] != "unknown" {
		t.Fatal("unverified second repair promoted ability")
	}
	if repo.report.Practices[0].TargetAvailable || repo.report.Practices[0].ProjectID != 0 {
		t.Fatal("model project link trusted")
	}
	if err = ValidateGrowthClaims(repo.report.Claims, sources.rows, nil); err != nil {
		t.Fatal(err)
	}
	reports := NewGrowthReportService(reviews, store, sources)
	if _, err = reports.ReadSource(ctx, 99, 1, "message:1", 0, 8192); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign source exposed")
	}
	tail, err := reports.ReadSource(ctx, 2, 1, "message:1", 8192, 8192)
	if err != nil || !tail.Complete || !strings.Contains(tail.Source.Text, "TAIL_VERIFIED") {
		t.Fatal("owned tail unavailable")
	}
}
