package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/review-service/internal/model"
)

// ReviewStore 组合复盘主表与章节明细两类仓储，满足 service.ReviewRepo 与
// service.ReviewTaskRepo 两个仓储端口（main.go 装配直接注入，结构化满足）。
// 两个内嵌仓储共享同一连接池；章节方法来自 ReviewSectionRepo，
// 状态/重试方法来自 ReviewRepo。
type ReviewStore struct {
	*ReviewRepo
	*ReviewSectionRepo
}

// NewReviewStore 创建组合仓储。
func NewReviewStore(pool *pgxpool.Pool) *ReviewStore {
	return &ReviewStore{
		ReviewRepo:        NewReviewRepo(pool),
		ReviewSectionRepo: NewReviewSectionRepo(pool),
	}
}

// ReviewSectionRepo 复盘章节明细读写（T2-30）。
type ReviewSectionRepo struct {
	pool *pgxpool.Pool
}

// NewReviewSectionRepo 创建章节仓储。
func NewReviewSectionRepo(pool *pgxpool.Pool) *ReviewSectionRepo {
	return &ReviewSectionRepo{pool: pool}
}

// ReplaceSections 在单个事务内整章重写一份复盘的全部章节（幂等）。
//
// worker 失败重试 / 消息 requeue 时可能重复执行：先按 review_id 删除旧章节
// 再批量插入，保证不会出现重复行，也不会残留上一版章节。
// sections 为空时不执行任何写入（调用方应先拦截空章节，按生成失败处理）。
func (r *ReviewSectionRepo) ReplaceSections(ctx context.Context, reviewID int64, sections []model.ReviewSection) error {
	if r == nil || r.pool == nil {
		return errors.New("review section repo: pg pool is not initialized")
	}
	if len(sections) == 0 {
		return errors.New("review section repo: no sections to write")
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM review_sections WHERE review_id = $1`, reviewID); err != nil {
		return fmt.Errorf("clear old sections: %w", err)
	}

	batch := &pgx.Batch{}
	for _, s := range sections {
		// review_id 以入参为准，避免调用方漏填/错填。
		batch.Queue(`
			INSERT INTO review_sections
				(review_id, section_type, section_level, title, content, content_data, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			reviewID, s.SectionType, s.SectionLevel, s.Title, s.Content, nullableJSONB(s.ContentData), s.SortOrder)
	}
	br := tx.SendBatch(ctx, batch)
	for range sections {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("insert section: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("flush section batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit sections: %w", err)
	}
	return nil
}

// ListByReviewID 按复盘 ID 读取全部章节，按 sort_order、id 升序。
// 无章节时返回空切片（非 nil）、nil 错误，由调用方结合复盘状态判断。
func (r *ReviewSectionRepo) ListByReviewID(ctx context.Context, reviewID int64) ([]model.ReviewSection, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("review section repo: pg pool is not initialized")
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, review_id, section_type, section_level, title, content, content_data,
			sort_order, created_at, updated_at
		FROM review_sections
		WHERE review_id = $1
		ORDER BY sort_order, id`, reviewID)
	if err != nil {
		return nil, fmt.Errorf("query sections: %w", err)
	}
	defer rows.Close()

	var out []model.ReviewSection
	for rows.Next() {
		var s model.ReviewSection
		var contentData []byte // JSONB NULL → nil
		if err := rows.Scan(
			&s.ID, &s.ReviewID, &s.SectionType, &s.SectionLevel, &s.Title,
			&s.Content, &contentData, &s.SortOrder, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan section: %w", err)
		}
		s.ContentData = contentData
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sections: %w", err)
	}
	return out, nil
}

// nullableJSONB 把空 JSON 字节转为 nil（写 SQL NULL），非空原样写入。
func nullableJSONB(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}
