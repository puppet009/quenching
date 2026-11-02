package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"github.com/quenching/review-service/internal/repository"
	"time"
)

var ErrGrowthLeaseLost = model.ErrGrowthLeaseLost
var ErrGrowthBusy = model.ErrGrowthBusy

type GrowthJobLease = model.GrowthJobLease
type GrowthJob = model.GrowthJob

type GrowthJobStore interface {
	Ensure(context.Context, int64, rc.Snapshot) error
	Claim(context.Context, int64, string, time.Duration) (GrowthJobLease, error)
	Renew(context.Context, GrowthJobLease, time.Duration) (bool, error)
	State(context.Context, GrowthJobLease) (GrowthJob, error)
	Commit(context.Context, GrowthJobLease, int64, GrowthJob, *rc.AnalysisArtifact) error
	Release(context.Context, GrowthJobLease) error
	Due(context.Context, int) ([]int64, error)
	Artifacts(context.Context, GrowthJobLease, int) ([]rc.AnalysisArtifact, error)
}
type GrowthSourceReader interface {
	Sources(context.Context, int64, int64, int64, rc.Cursor, int) (rc.SourcePage, error)
}
type GrowthFinalizer interface {
	Finalize(context.Context, GrowthJobLease, GrowthJob) error
}
type GrowthPipeline struct {
	store     GrowthJobStore
	sources   GrowthSourceReader
	ai        GrowthAI
	finalizer GrowthFinalizer
}

func (p *GrowthPipeline) HasJob(ctx context.Context, id int64) (bool, error) {
	loader, ok := p.store.(GrowthJobLoader)
	if !ok {
		return false, fmt.Errorf("job loader not configured")
	}
	_, err := loader.Load(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func NewGrowthPipeline(store GrowthJobStore, sources GrowthSourceReader, ai GrowthAI) *GrowthPipeline {
	return &GrowthPipeline{store: store, sources: sources, ai: ai}
}
func (p *GrowthPipeline) WithFinalizer(f GrowthFinalizer) *GrowthPipeline { p.finalizer = f; return p }
func (p *GrowthPipeline) Step(parent context.Context, id int64) error {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	lease, err := p.store.Claim(parent, id, hex.EncodeToString(token), 360*time.Second)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 340*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopped:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				ok, e := p.store.Renew(ctx, lease, 360*time.Second)
				if e != nil || !ok {
					cancel()
					return
				}
			}
		}
	}()
	job, err := p.store.State(ctx, lease)
	if err != nil {
		_ = p.store.Release(context.WithoutCancel(parent), lease)
		return err
	}
	next := job
	var artifact *rc.AnalysisArtifact
	workErr := func() error {
		if job.Phase == "finalize" {
			if p.finalizer == nil {
				return fmt.Errorf("growth finalizer not configured")
			}
			return p.finalizer.Finalize(ctx, lease, job)
		}
		in := GrowthInferenceInput{SessionID: job.Snapshot.SessionID, SnapshotHash: job.Snapshot.Hash}
		meta := map[string]any{"source_urls": job.Snapshot.SourceURLs, "audit_complete": job.Snapshot.AuditComplete, "cleanup_status": job.Snapshot.CleanupStatus, "audit_coverage": job.Snapshot.AuditCoverage, "audit_gaps": job.Snapshot.AuditGaps}
		in.Metadata, _ = json.Marshal(meta)
		switch job.Phase {
		case "analyze":
			page, e := p.sources.Sources(ctx, job.Snapshot.UserID, job.Snapshot.SessionID, job.Snapshot.Version, job.Cursor, 48*1024)
			if e != nil {
				return e
			}
			if e = validateGrowthPage(page, job.Cursor, job.Snapshot.SourceCount); e != nil {
				return e
			}
			batch, _, e := BuildGrowthBatch(page, in.Metadata)
			if e != nil {
				return e
			}
			in.Fragments = batch.Fragments
			in.Mode = "analyze"
			if len(page.Fragments) == 0 {
				next.Phase = "synthesize"
				next.Cursor = page.Next
				return nil
			}
			result, e := p.ai.Infer(ctx, in)
			if e != nil {
				return e
			}
			if result.Artifact == nil {
				return fmt.Errorf("missing analysis")
			}
			artifact = result.Artifact
			if e = ValidateGrowthClaims(artifact.Claims, fragmentSources(in.Fragments), job.Snapshot.SourceURLs); e != nil {
				return e
			}
			artifact.Coverage = growthArtifactCoverage(page.Fragments, nil)
			next.Cursor = page.Next
			next.BatchIndex++
			if page.Done {
				next.Phase = "reduce"
				next.BatchIndex = 0
				next.ReduceOffset = 0
			}
			recordGrowthUsage(&next, result)
		case "reduce", "synthesize":
			all, e := p.store.Artifacts(ctx, lease, job.ReduceLevel)
			if e != nil {
				return e
			}
			if job.ReduceOffset > len(all) {
				return fmt.Errorf("invalid reduction cursor")
			}
			selected := all[job.ReduceOffset:]
			in.Mode = "synthesize"
			prepare := func(rows []rc.AnalysisArtifact) {
				in.Artifacts = make([]rc.AnalysisArtifact, len(rows))
				for i, a := range rows {
					a.Coverage = json.RawMessage(`{}`)
					in.Artifacts[i] = a
				}
				in.Sources = growthSourceIndex(rows)
			}
			prepare(selected)
			encoded, _ := json.Marshal(in)
			if job.ReduceOffset == 0 && len(encoded) <= 80*1024 {
				result, e := p.ai.Infer(ctx, in)
				if e != nil {
					return e
				}
				if result.Report == nil {
					return fmt.Errorf("missing synthesis")
				}
				if e = ValidateGrowthReport(*result.Report, job.Snapshot, in.Sources); e != nil {
					return e
				}
				next.Report = result.Report
				next.Phase = "finalize"
				recordGrowthUsage(&next, result)
				return nil
			}
			if len(selected) == 0 {
				next.ReduceLevel++
				next.ReduceOffset = 0
				next.BatchIndex = 0
				return nil
			}
			in.Mode = "reduce"
			count := 0
			for i := 1; i <= len(selected); i++ {
				prepare(selected[:i])
				encoded, _ = json.Marshal(in)
				if len(encoded) > 80*1024 {
					break
				}
				count = i
			}
			if count == 0 || (count == 1 && len(selected) > 1) {
				return fmt.Errorf("artifacts cannot be reduced within budget")
			}
			prepare(selected[:count])
			if count == 1 {
				a := selected[0]
				artifact = &a
			} else {
				result, e := p.ai.Infer(ctx, in)
				if e != nil {
					return e
				}
				if result.Artifact == nil {
					return fmt.Errorf("missing reduction")
				}
				artifact = result.Artifact
				if e = ValidateGrowthClaims(artifact.Claims, in.Sources, job.Snapshot.SourceURLs); e != nil {
					return e
				}
				artifact.Coverage = growthArtifactCoverage(nil, in.Sources)
				recordGrowthUsage(&next, result)
			}
			next.ReduceOffset += count
			next.BatchIndex++
			if next.ReduceOffset == len(all) {
				next.ReduceLevel++
				next.ReduceOffset = 0
				next.BatchIndex = 0
			}
		default:
			return fmt.Errorf("invalid growth phase")
		}
		return nil
	}()
	if job.Phase == "finalize" && workErr == nil {
		return nil
	}
	if workErr != nil {
		next = job
		next.Attempts++
		next.Error = "复盘步骤暂未完成"
		next.RetryAt = time.Now().Add(time.Duration(5*next.Attempts) * time.Second)
		artifact = nil
		if next.Attempts >= 3 {
			next.Phase = "failed"
		}
	} else {
		next.Attempts = 0
		next.RetryAt = time.Time{}
		next.Error = ""
	}
	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer writeCancel()
	if err = p.store.Commit(writeCtx, lease, job.Version, next, artifact); err != nil {
		return err
	}
	return workErr
}
func fragmentSources(fragments []rc.Fragment) []rc.Source {
	var out []rc.Source
	for _, f := range fragments {
		out = append(out, f.Source)
	}
	return out
}
func recordGrowthUsage(job *GrowthJob, result GrowthInferenceResult) {
	job.ActualModel = result.ActualModel
	job.PromptVersion = result.PromptVersion
	job.LastRequestID = result.RequestID
	job.PromptTokens += result.PromptTokens
	job.CompletionTokens += result.CompletionTokens
	job.LatencyMS += result.LatencyMS
}
