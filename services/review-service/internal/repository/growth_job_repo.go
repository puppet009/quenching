package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"time"
)

type GrowthJobRepo struct{ pool *pgxpool.Pool }

func NewGrowthJobRepo(pool *pgxpool.Pool) *GrowthJobRepo { return &GrowthJobRepo{pool: pool} }

func (r *GrowthJobRepo) Ensure(ctx context.Context, id int64, s rc.Snapshot) error {
	job := model.GrowthJob{ReviewID: id, Snapshot: s, Phase: "analyze", Version: 1, Cursor: rc.Cursor{Ordinal: 1}}
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO growth_review_jobs(review_id,state,phase) SELECT id,$2,'analyze' FROM reviews WHERE id=$1 AND user_id=$3 AND session_id=$4 AND project_id=$5 AND deleted_at IS NULL ON CONFLICT(review_id) DO NOTHING`, id, raw, s.UserID, s.SessionID, s.ProjectID)
	if err != nil {
		return err
	}
	existing, err := r.Load(ctx, id)
	if err != nil {
		return err
	}
	if existing.Snapshot.Hash != s.Hash || existing.Snapshot.UserID != s.UserID || existing.Snapshot.Version != s.Version {
		return fmt.Errorf("frozen job identity mismatch")
	}
	return nil
}
func (r *GrowthJobRepo) Claim(ctx context.Context, id int64, token string, ttl time.Duration) (model.GrowthJobLease, error) {
	l := model.GrowthJobLease{ReviewID: id, Token: token}
	err := r.pool.QueryRow(ctx, `UPDATE growth_review_jobs SET lease_token=$2,lease_fence=lease_fence+1,lease_until=clock_timestamp()+($3*interval '1 second'),updated_at=now() WHERE review_id=$1 AND phase NOT IN ('completed','failed') AND (lease_until IS NULL OR lease_until<=clock_timestamp()) AND (retry_at IS NULL OR retry_at<=clock_timestamp()) RETURNING lease_fence`, id, token, ttl.Seconds()).Scan(&l.Fence)
	if errors.Is(err, pgx.ErrNoRows) {
		err = model.ErrGrowthBusy
	}
	return l, err
}
func (r *GrowthJobRepo) Renew(ctx context.Context, l model.GrowthJobLease, ttl time.Duration) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE growth_review_jobs SET lease_until=clock_timestamp()+($4*interval '1 second') WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND lease_until>clock_timestamp()`, l.ReviewID, l.Token, l.Fence, ttl.Seconds())
	return tag.RowsAffected() == 1, err
}
func (r *GrowthJobRepo) State(ctx context.Context, l model.GrowthJobLease) (model.GrowthJob, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT state FROM growth_review_jobs WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND lease_until>clock_timestamp()`, l.ReviewID, l.Token, l.Fence).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.GrowthJob{}, model.ErrGrowthLeaseLost
	}
	var job model.GrowthJob
	if err == nil {
		err = json.Unmarshal(raw, &job)
	}
	return job, err
}
func (r *GrowthJobRepo) Load(ctx context.Context, id int64) (model.GrowthJob, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT state FROM growth_review_jobs WHERE review_id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	var job model.GrowthJob
	if err == nil {
		err = json.Unmarshal(raw, &job)
	}
	return job, err
}
func (r *GrowthJobRepo) Commit(ctx context.Context, l model.GrowthJobLease, expected int64, next model.GrowthJob, a *rc.AnalysisArtifact) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT state FROM growth_review_jobs WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND lease_until>clock_timestamp() AND version=$4 FOR UPDATE`, l.ReviewID, l.Token, l.Fence, expected).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrGrowthLeaseLost
	}
	if err != nil {
		return err
	}
	var before model.GrowthJob
	if err = json.Unmarshal(raw, &before); err != nil {
		return err
	}
	if next.ReviewID != before.ReviewID || next.Snapshot.Hash != before.Snapshot.Hash || next.Snapshot.UserID != before.Snapshot.UserID {
		return fmt.Errorf("job identity changed")
	}
	if a != nil {
		artifact, e := json.Marshal(a)
		if e != nil {
			return e
		}
		level := before.ReduceLevel
		if before.Phase == "reduce" {
			level++
		}
		_, err = tx.Exec(ctx, `INSERT INTO growth_review_artifacts(review_id,snapshot_hash,phase,level,batch_index,artifact,request_id,prompt_tokens,completion_tokens) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, l.ReviewID, before.Snapshot.Hash, before.Phase, level, before.BatchIndex, artifact, next.LastRequestID, next.PromptTokens-before.PromptTokens, next.CompletionTokens-before.CompletionTokens)
		if err != nil {
			return err
		}
	}
	next.Version = expected + 1
	raw, err = json.Marshal(next)
	if err != nil {
		return err
	}
	var retryAt any
	if !next.RetryAt.IsZero() {
		retryAt = next.RetryAt
	}
	tag, err := tx.Exec(ctx, `UPDATE growth_review_jobs SET state=$5,version=version+1,phase=$6,retry_at=$7,lease_token='',lease_until=NULL,updated_at=now() WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3 AND version=$4 AND lease_until>clock_timestamp()`, l.ReviewID, l.Token, l.Fence, expected, raw, next.Phase, retryAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return model.ErrGrowthLeaseLost
	}
	if next.Phase == "failed" {
		_, err = tx.Exec(ctx, `UPDATE reviews SET generation_status='failed',updated_at=now() WHERE id=$1 AND generation_status<>'completed'`, l.ReviewID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *GrowthJobRepo) Release(ctx context.Context, l model.GrowthJobLease) error {
	_, err := r.pool.Exec(ctx, `UPDATE growth_review_jobs SET lease_token='',lease_until=NULL WHERE review_id=$1 AND lease_token=$2 AND lease_fence=$3`, l.ReviewID, l.Token, l.Fence)
	return err
}
func (r *GrowthJobRepo) Due(ctx context.Context, limit int) ([]int64, error) {
	if limit < 1 || limit > 32 {
		limit = 32
	}
	rows, err := r.pool.Query(ctx, `SELECT review_id FROM growth_review_jobs WHERE phase NOT IN ('completed','failed') AND (lease_until IS NULL OR lease_until<=clock_timestamp()) AND (retry_at IS NULL OR retry_at<=clock_timestamp()) ORDER BY updated_at,review_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *GrowthJobRepo) Artifacts(ctx context.Context, l model.GrowthJobLease, level int) ([]rc.AnalysisArtifact, error) {
	if _, err := r.State(ctx, l); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT artifact FROM growth_review_artifacts WHERE review_id=$1 AND level=$2 ORDER BY batch_index`, l.ReviewID, level)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rc.AnalysisArtifact
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var a rc.AnalysisArtifact
		if err = json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
