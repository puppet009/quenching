package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/content-service/internal/model"
	"slices"
)

type CombinationRepo struct{ pool *pgxpool.Pool }

func NewCombinationRepo(pool *pgxpool.Pool) *CombinationRepo { return &CombinationRepo{pool} }
func (r *CombinationRepo) Combination(ctx context.Context, slug string, ids []int64) (model.FaultCombination, error) {
	var c model.FaultCombination
	if len(ids) != 2 {
		return c, errors.New("two ids required")
	}
	ordered := slices.Clone(ids)
	slices.Sort(ordered)
	var raw []byte
	e := r.pool.QueryRow(ctx, `SELECT id,configuration FROM content_fault_combinations WHERE template_slug=$1 AND first_defect_id=$2 AND second_defect_id=$3`, slug, ordered[0], ordered[1]).Scan(&c.ID, &raw)
	if e == nil {
		var value model.FaultCombination
		e = json.Unmarshal(raw, &value)
		value.ID = c.ID
		c = value
	}
	return c, e
}
func (r *CombinationRepo) SaveCombination(ctx context.Context, c model.FaultCombination) error {
	ids := slices.Clone(c.DefectIDs)
	slices.Sort(ids)
	if len(ids) != 2 || ids[0] <= 0 || ids[0] == ids[1] || c.TemplateSlug == "" {
		return errors.New("invalid combination")
	}
	c.DefectIDs = ids
	raw, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = r.pool.Exec(ctx, `INSERT INTO content_fault_combinations(template_slug,first_defect_id,second_defect_id,configuration) VALUES($1,$2,$3,$4) ON CONFLICT(template_slug,first_defect_id,second_defect_id) DO UPDATE SET configuration=EXCLUDED.configuration`, c.TemplateSlug, ids[0], ids[1], raw)
	return e
}
func (r *CombinationRepo) ListCombinations(ctx context.Context) ([]model.FaultCombination, error) {
	rows, e := r.pool.Query(ctx, `SELECT id,configuration FROM content_fault_combinations ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.FaultCombination{}
	for rows.Next() {
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			return nil, e
		}
		var c model.FaultCombination
		if e = json.Unmarshal(raw, &c); e != nil {
			return nil, e
		}
		c.ID = id
		out = append(out, c)
	}
	return out, rows.Err()
}
