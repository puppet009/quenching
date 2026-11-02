package repository

import (
	"context"
	"encoding/json"
)

type OwnedDefectReader struct{ repo *PGDefectRepo }

func NewOwnedDefectReader(r *PGDefectRepo) *OwnedDefectReader { return &OwnedDefectReader{r} }
func (r *OwnedDefectReader) ListActiveByProject(ctx context.Context, pid int64) ([]*faultDefectView, error) {
	rows, e := r.repo.ListActiveByProject(ctx, pid)
	if e != nil {
		return nil, e
	}
	out := []*faultDefectView{}
	for _, row := range rows {
		raw, e := json.Marshal(row)
		if e != nil {
			return nil, e
		}
		var v faultDefectView
		if e = json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		out = append(out, &v)
	}
	return out, nil
}
