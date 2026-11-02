package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/quenching/fault-engine/internal/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ContentDefectReader struct {
	base, token string
	client      *http.Client
}

func NewContentDefectReader(base, token string) *ContentDefectReader {
	return &ContentDefectReader{strings.TrimRight(base, "/"), token, &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (r *ContentDefectReader) get(ctx context.Context, path string, out any) error {
	u, err := url.Parse(r.base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || r.token == "" {
		return errors.New("content catalog unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", r.token)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrDefectNotFound
	}
	var body struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body) != nil || body.Code != 0 || len(body.Data) == 0 || string(body.Data) == "null" {
		return errors.New("content catalog rejected query")
	}
	return json.Unmarshal(body.Data, out)
}
func (r *ContentDefectReader) GetByID(ctx context.Context, id int64) (*model.Defect, error) {
	if id <= 0 {
		return nil, ErrDefectNotFound
	}
	var d model.Defect
	if err := r.get(ctx, fmt.Sprintf("/internal/v1/catalog/defects/%d", id), &d); err != nil {
		return nil, err
	}
	if d.ID != id {
		return nil, errors.New("catalog identity mismatch")
	}
	return &d, nil
}
func (r *ContentDefectReader) GetByCode(ctx context.Context, pid int64, code string) (*model.Defect, error) {
	q := url.Values{"project_id": {strconv.FormatInt(pid, 10)}, "code": {code}}
	var d model.Defect
	if err := r.get(ctx, "/internal/v1/catalog/defects/by-code?"+q.Encode(), &d); err != nil {
		return nil, err
	}
	if d.ID <= 0 || d.ProjectID != pid || d.Code != code {
		return nil, errors.New("catalog identity mismatch")
	}
	return &d, nil
}
func (r *ContentDefectReader) List(ctx context.Context, q DefectQuery) (*DefectPage, error) {
	v := url.Values{"project_id": {strconv.FormatInt(q.ProjectID, 10)}, "template_slug": {q.TemplateSlug}, "keyword": {q.Keyword}, "category": {q.Category}, "difficulty": {q.Difficulty}, "defect_type": {q.DefectType}, "page": {strconv.Itoa(q.Page)}, "page_size": {strconv.Itoa(q.PageSize)}}
	if q.IsActive != nil {
		v.Set("is_active", strconv.FormatBool(*q.IsActive))
	}
	var p DefectPage
	if err := r.get(ctx, "/internal/v1/catalog/defects?"+v.Encode(), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
