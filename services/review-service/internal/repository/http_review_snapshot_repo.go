package repository

import (
	"context"
	"errors"
	"fmt"
	rc "github.com/quenching/proto/reviewcontract"
	"github.com/quenching/review-service/internal/model"
	"net/http"
	"net/url"
)

type HTTPReviewSnapshotRepo struct {
	client *internalClient
	token  string
}

func NewHTTPReviewSnapshotRepo(baseURL, token string) *HTTPReviewSnapshotRepo {
	return &HTTPReviewSnapshotRepo{client: newInternalClient(baseURL, token), token: token}
}
func (r *HTTPReviewSnapshotRepo) call(ctx context.Context, method, path string, body, out any) error {
	if r.token == "" {
		return fmt.Errorf("review snapshot requires internal token")
	}
	err := r.client.do(ctx, method, path, body, out)
	var remote *internalHTTPError
	if errors.As(err, &remote) {
		if remote.Status == 404 {
			return ErrNotFound
		}
		if remote.Status == 400 {
			return model.ErrGrowthSourceRange
		}
	}
	return err
}
func (r *HTTPReviewSnapshotRepo) Eligibility(ctx context.Context, u, s, p int64) (rc.Eligibility, error) {
	var out rc.Eligibility
	err := r.call(ctx, http.MethodGet, fmt.Sprintf("/internal/v1/sessions/%d/review-eligibility?user_id=%d&project_id=%d", s, u, p), nil, &out)
	return out, err
}
func (r *HTTPReviewSnapshotRepo) Freeze(ctx context.Context, u, s, p int64) (rc.Snapshot, error) {
	var out rc.Snapshot
	err := r.call(ctx, http.MethodPost, fmt.Sprintf("/internal/v1/sessions/%d/review-snapshot", s), map[string]int64{"user_id": u, "project_id": p}, &out)
	return out, err
}
func (r *HTTPReviewSnapshotRepo) Manifest(ctx context.Context, u, s int64) (rc.Snapshot, error) {
	var out rc.Snapshot
	err := r.call(ctx, http.MethodGet, fmt.Sprintf("/internal/v1/sessions/%d/review-snapshot?user_id=%d", s, u), nil, &out)
	return out, err
}
func (r *HTTPReviewSnapshotRepo) Sources(ctx context.Context, u, s, v int64, c rc.Cursor, budget int) (rc.SourcePage, error) {
	var out rc.SourcePage
	err := r.call(ctx, http.MethodGet, fmt.Sprintf("/internal/v1/sessions/%d/review-snapshot/sources?user_id=%d&version=%d&ordinal=%d&offset_bytes=%d&budget_bytes=%d", s, u, v, c.Ordinal, c.OffsetBytes, budget), nil, &out)
	return out, err
}
func (r *HTTPReviewSnapshotRepo) Read(ctx context.Context, u, s, v int64, ref string, offset, limit int) (rc.Fragment, error) {
	var out rc.Fragment
	err := r.call(ctx, http.MethodGet, fmt.Sprintf("/internal/v1/sessions/%d/review-snapshot/source?user_id=%d&version=%d&reference=%s&offset_bytes=%d&limit_bytes=%d", s, u, v, url.QueryEscape(ref), offset, limit), nil, &out)
	return out, err
}
