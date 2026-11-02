package repository

import (
	"context"
	"encoding/json"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
)

func (r *ReviewRepo) CreateGrowth(ctx context.Context, rv *model.Review, snap rc.Snapshot) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO reviews(session_id,user_id,project_id,review_level,generation_status) VALUES($1,$2,$3,$4,$5) ON CONFLICT(session_id) DO NOTHING RETURNING id,created_at,updated_at`, rv.SessionID, rv.UserID, rv.ProjectID, rv.ReviewLevel, rv.GenerationStatus).Scan(&rv.ID, &rv.CreatedAt, &rv.UpdatedAt)
	if err != nil {
		return err
	}
	job := model.GrowthJob{ReviewID: rv.ID, Snapshot: snap, Phase: "analyze", Version: 1, Cursor: rc.Cursor{Ordinal: 1}}
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO growth_review_jobs(review_id,state,phase) VALUES($1,$2,'analyze')`, rv.ID, raw); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *ReviewRepo) GetGrowthReport(ctx context.Context, id int64) (*rc.GrowthReport, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT growth_report FROM reviews WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var out rc.GrowthReport
	err = json.Unmarshal(raw, &out)
	return &out, err
}
func (r *ReviewRepo) CompleteGrowth(ctx context.Context, l model.GrowthJobLease, job model.GrowthJob, sections []model.ReviewSection, report rc.GrowthReport) error {
	if job.Report == nil || job.Cursor.Ordinal <= job.Snapshot.SourceCount || job.Cursor.OffsetBytes != 0 || job.ActualModel != "qwen3.8-flash" || len(sections) < 6 {
		return fmt.Errorf("incomplete growth report")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var version int64
	err = tx.QueryRow(ctx, `SELECT version FROM growth_review_jobs WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND lease_until>clock_timestamp() AND version=$4 AND phase='finalize' FOR UPDATE`, l.ReviewID, l.Token, l.Fence, job.Version).Scan(&version)
	if err != nil {
		return model.ErrGrowthLeaseLost
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return err
	}
	raw = []byte(rc.RedactText(string(raw)))
	tag, err := tx.Exec(ctx, `UPDATE reviews SET generation_status='completed',review_level='method',title='整场实训成长复盘',summary=$4,overall_evaluation=$5,growth_report=$6,ai_model=$7,prompt_version=$8,generation_prompt_tokens=$9,generation_completion_tokens=$10,generation_duration=$11,error_message='',updated_at=now() WHERE id=$1 AND user_id=$2 AND session_id=$3 AND deleted_at IS NULL`, job.ReviewID, job.Snapshot.UserID, job.Snapshot.SessionID, sections[0].Content, sections[1].Content, raw, job.ActualModel, job.PromptVersion, job.PromptTokens, job.CompletionTokens, int(job.LatencyMS))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `DELETE FROM review_sections WHERE review_id=$1`, job.ReviewID); err != nil {
		return err
	}
	for _, s := range sections {
		if _, err = tx.Exec(ctx, `INSERT INTO review_sections(review_id,section_type,section_level,title,content,content_data,sort_order) VALUES($1,$2,$3,$4,$5,$6,$7)`, job.ReviewID, s.SectionType, s.SectionLevel, s.Title, rc.RedactText(s.Content), nullableJSONB(s.ContentData), s.SortOrder); err != nil {
			return err
		}
	}
	job.Phase = "completed"
	job.Version++
	job.Report = &report
	state, _ := json.Marshal(job)
	tag, err = tx.Exec(ctx, `UPDATE growth_review_jobs SET state=$5,phase='completed',version=version+1,lease_token='',lease_until=NULL,retry_at=NULL,updated_at=now() WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND version=$4 AND lease_until>clock_timestamp()`, l.ReviewID, l.Token, l.Fence, version, state)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrGrowthLeaseLost
	}
	return tx.Commit(ctx)
}
