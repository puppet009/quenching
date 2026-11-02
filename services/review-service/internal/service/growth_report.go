package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
)

type GrowthSnapshotStore interface {
	GrowthSourceReader
	Eligibility(context.Context, int64, int64, int64) (rc.Eligibility, error)
	Freeze(context.Context, int64, int64, int64) (rc.Snapshot, error)
	Manifest(context.Context, int64, int64) (rc.Snapshot, error)
	Read(context.Context, int64, int64, int64, string, int, int) (rc.Fragment, error)
}

var ErrGrowthSourceRange = model.ErrGrowthSourceRange

type GrowthJobLoader interface {
	Load(context.Context, int64) (GrowthJob, error)
}
type GrowthReportService struct {
	reviews *ReviewService
	jobs    GrowthJobLoader
	sources GrowthSnapshotStore
}

func NewGrowthReportService(reviews *ReviewService, jobs GrowthJobLoader, sources GrowthSnapshotStore) *GrowthReportService {
	return &GrowthReportService{reviews: reviews, jobs: jobs, sources: sources}
}
func (s *GrowthReportService) owned(ctx context.Context, u, id int64) (GrowthJob, error) {
	rv, err := s.reviews.GetByID(ctx, id)
	if err != nil || rv.UserID != u || u <= 0 {
		return GrowthJob{}, ErrNotFound
	}
	job, err := s.jobs.Load(ctx, id)
	if err != nil {
		return GrowthJob{}, ErrNotFound
	}
	if job.Snapshot.UserID != u || job.Snapshot.SessionID != rv.SessionID {
		return GrowthJob{}, ErrNotFound
	}
	e, err := s.sources.Eligibility(ctx, u, rv.SessionID, rv.ProjectID)
	if err != nil || !e.CanGenerate || e.UserID != u || e.SessionID != rv.SessionID {
		return GrowthJob{}, ErrNotFound
	}
	return job, nil
}
func (s *GrowthReportService) ReadSource(ctx context.Context, u, id int64, ref string, offset, limit int) (rc.Fragment, error) {
	job, err := s.owned(ctx, u, id)
	if err != nil {
		return rc.Fragment{}, err
	}
	if ref == "" || len(ref) > 128 || offset < 0 || limit < 1 || limit > 8192 {
		return rc.Fragment{}, ErrGrowthSourceRange
	}
	fragment, err := s.sources.Read(ctx, u, job.Snapshot.SessionID, job.Snapshot.Version, ref, offset, limit)
	if errors.Is(err, repository.ErrNotFound) {
		err = ErrNotFound
	}
	return fragment, err
}

type GrowthProgress struct {
	Phase            string `json:"phase"`
	ProcessedSources int64  `json:"processed_sources"`
	TotalSources     int64  `json:"total_sources"`
	AuditComplete    bool   `json:"audit_complete"`
	Error            string `json:"error,omitempty"`
}

func (s *GrowthReportService) Progress(ctx context.Context, u, id int64) (GrowthProgress, error) {
	job, err := s.owned(ctx, u, id)
	if err != nil {
		return GrowthProgress{}, err
	}
	processed := job.Cursor.Ordinal - 1
	if processed < 0 {
		processed = 0
	}
	if processed > job.Snapshot.SourceCount {
		processed = job.Snapshot.SourceCount
	}
	return GrowthProgress{Phase: job.Phase, ProcessedSources: processed, TotalSources: job.Snapshot.SourceCount, AuditComplete: job.Snapshot.AuditComplete, Error: job.Error}, nil
}

type GrowthReportWriter interface {
	CompleteGrowth(context.Context, GrowthJobLease, GrowthJob, []model.ReviewSection, rc.GrowthReport) error
}
type growthFinalizer struct {
	sources GrowthSnapshotStore
	writer  GrowthReportWriter
}

func NewGrowthFinalizer(sources GrowthSnapshotStore, writer GrowthReportWriter, collectors ...SectionCollector) GrowthFinalizer {
	// Legacy collectors are deliberately excluded: growth reports only use frozen sources.
	return &growthFinalizer{sources: sources, writer: writer}
}
func (f *growthFinalizer) Finalize(ctx context.Context, l GrowthJobLease, job GrowthJob) error {
	if job.Report == nil || job.Cursor.Ordinal <= job.Snapshot.SourceCount || job.Cursor.OffsetBytes != 0 {
		return fmt.Errorf("incomplete frozen coverage")
	}
	manifest, err := f.sources.Manifest(ctx, job.Snapshot.UserID, job.Snapshot.SessionID)
	if err != nil {
		return err
	}
	if manifest.Hash != job.Snapshot.Hash {
		return fmt.Errorf("snapshot changed")
	}
	// Read source headers and authoritative assessment/challenge bodies from frozen data, never the model's claims.
	var sources []rc.Source
	var cursor = rc.Cursor{Ordinal: 1}
	texts := map[string]string{}
	for {
		page, e := f.sources.Sources(ctx, job.Snapshot.UserID, job.Snapshot.SessionID, job.Snapshot.Version, cursor, 48*1024)
		if e != nil {
			return e
		}
		if e = validateGrowthPage(page, cursor, job.Snapshot.SourceCount); e != nil {
			return e
		}
		for _, fragment := range page.Fragments {
			src := fragment.Source
			if src.Kind == "assessment" || src.Kind == "challenge" || src.Kind == "assistant_file_change" {
				texts[src.ID] += src.Text
			}
			if fragment.Complete {
				src.Text = texts[src.ID]
				sources = append(sources, src)
			}
		}
		cursor = page.Next
		if page.Done {
			break
		}
	}
	report := *job.Report
	if err = ValidateGrowthReport(report, job.Snapshot, sources); err != nil {
		return err
	}
	report.Overview, _ = json.Marshal(map[string]any{"session_id": job.Snapshot.SessionID, "source_count": job.Snapshot.SourceCount, "snapshot_hash": job.Snapshot.Hash, "audit_complete": job.Snapshot.AuditComplete, "cleanup_status": job.Snapshot.CleanupStatus, "audit_coverage": job.Snapshot.AuditCoverage, "audit_gaps": job.Snapshot.AuditGaps})
	report.Ability = FrozenGrowthAbilityWithAudit(sources, job.Snapshot)
	for i := range report.Practices {
		report.Practices[i].ProjectID = 0
		report.Practices[i].DefectIDs = nil
		report.Practices[i].TargetAvailable = false
	}
	sections, err := BuildGrowthSections(job.ReviewID, report)
	if err != nil {
		return err
	}
	changes := &model.ChangeAnalysis{}
	for _, src := range sources {
		if src.Kind != "assistant_file_change" {
			continue
		}
		var change model.ChangeItem
		if err := json.Unmarshal([]byte(src.Text), &change); err != nil {
			return fmt.Errorf("invalid frozen change: %w", err)
		}
		changes.Changes = append(changes.Changes, change)
	}
	changes.TotalCount = len(changes.Changes)
	if section := BuildChangeSection(job.ReviewID, changes); section != nil {
		sections = append(sections, *section)
	}
	return f.writer.CompleteGrowth(ctx, l, job, sections, report)
}

var growthDimensions = []string{"localization", "evidence", "mechanism", "repair", "regression", "architecture", "hint_dependence"}

func FrozenGrowthAbility(sources []rc.Source) json.RawMessage {
	type dimension struct {
		Level        string   `json:"level"`
		Confidence   float64  `json:"confidence"`
		EvidenceRefs []string `json:"evidence_refs"`
	}
	type ability struct {
		OverallLevel string               `json:"overall_level"`
		Sufficiency  string               `json:"sufficiency"`
		Dimensions   map[string]dimension `json:"dimensions"`
		SourceRefs   []string             `json:"source_refs"`
	}
	result := ability{OverallLevel: "unknown", Sufficiency: "insufficient", Dimensions: map[string]dimension{}, SourceRefs: []string{}}
	verified := 0
	faults := map[int64]bool{}
	for _, s := range sources {
		if s.Trust == "challenge" && s.Actor == "platform" {
			var fact struct {
				Status    string  `json:"status"`
				DefectIDs []int64 `json:"defect_ids"`
			}
			if json.Unmarshal([]byte(s.Text), &fact) == nil && fact.Status == "verified_repair" {
				verified++
				for _, id := range fact.DefectIDs {
					faults[id] = true
				}
			}
		}
		if s.Trust == "assessment" && s.Actor == "platform" {
			var a ability
			if json.Unmarshal([]byte(s.Text), &a) == nil {
				a.SourceRefs = []string{s.ID}
				result = a
			}
		}
	}
	sufficient := verified >= 2 && len(faults) >= 2 && result.Sufficiency == "sufficient" && len(result.Dimensions) == 7
	if result.Dimensions == nil {
		result.Dimensions = map[string]dimension{}
	}
	for _, key := range growthDimensions {
		d, ok := result.Dimensions[key]
		if !ok || d.Level == "unknown" || d.Level == "" || d.Confidence < 0.7 || len(d.EvidenceRefs) == 0 {
			sufficient = false
		}
		if !ok {
			d.Level = "unknown"
			d.EvidenceRefs = []string{}
		}
		result.Dimensions[key] = d
	}
	if !sufficient {
		result.OverallLevel = "unknown"
		if result.Sufficiency == "sufficient" {
			result.Sufficiency = "partial"
		}
	}
	raw, _ := json.Marshal(result)
	return raw
}

func FrozenGrowthAbilityWithAudit(sources []rc.Source, snapshot rc.Snapshot) json.RawMessage {
	raw := FrozenGrowthAbility(sources)
	if !snapshot.AuditComplete || len(snapshot.AuditGaps) > 0 {
		var result map[string]any
		_ = json.Unmarshal(raw, &result)
		result["overall_level"] = "unknown"
		result["sufficiency"] = "partial"
		result["audit_gaps"] = snapshot.AuditGaps
		raw, _ = json.Marshal(result)
	}
	return raw
}
