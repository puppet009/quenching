package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/quenching/fault-engine/internal/model"
	"net/url"
)

type PGFaultCommandRepo struct {
	pool    *pgxpool.Pool
	catalog *ContentDefectReader
}

func NewPGFaultCommandRepo(p *pgxpool.Pool) *PGFaultCommandRepo { return &PGFaultCommandRepo{pool: p} }
func (r *PGFaultCommandRepo) Get(ctx context.Context, id string) (model.FaultCommand, model.FaultCommandResult, error) {
	var c model.FaultCommand
	var result model.FaultCommandResult
	var intent, raw []byte
	err := r.pool.QueryRow(ctx, `SELECT intent,result FROM fault_commands WHERE command_id=$1`, id).Scan(&intent, &raw)
	if err == nil {
		err = json.Unmarshal(intent, &c)
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return c, result, err
}
func (r *PGFaultCommandRepo) Reserve(ctx context.Context, c model.FaultCommand) (model.FaultCommand, model.FaultCommandResult, bool, error) {
	result := model.FaultCommandResult{CommandID: c.CommandID, Status: "unknown", ExperimentUIDs: []string{}, EvidenceRefs: []string{}, ResultJSON: `{"reason":"reserved; query status before any retry"}`}
	intent, e := json.Marshal(c)
	if e != nil {
		return c, result, false, e
	}
	raw, _ := json.Marshal(result)
	tag, e := r.pool.Exec(ctx, `INSERT INTO fault_commands(command_id,session_id,intent,result) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, c.CommandID, c.SessionID, intent, raw)
	if e != nil {
		return c, result, false, e
	}
	if tag.RowsAffected() == 1 {
		return c, result, true, nil
	}
	old, res, e := r.Get(ctx, c.CommandID)
	return old, res, false, e
}
func (r *PGFaultCommandRepo) Save(ctx context.Context, id string, result model.FaultCommandResult) error {
	raw, e := json.Marshal(result)
	if e != nil {
		return e
	}
	tag, e := r.pool.Exec(ctx, `UPDATE fault_commands SET result=$2,updated_at=now() WHERE command_id=$1 AND result->>'status'='unknown'`, id, raw)
	if e == nil && tag.RowsAffected() != 1 {
		return errors.New("command is missing or terminal")
	}
	return e
}
func (r *PGFaultCommandRepo) WithCatalog(base, token string) *PGFaultCommandRepo {
	r.catalog = NewContentDefectReader(base, token)
	return r
}
func (r *PGFaultCommandRepo) Combination(ctx context.Context, slug string, ids []int64) (model.FaultCombination, error) {
	var c model.FaultCombination
	if r.catalog == nil || len(ids) != 2 {
		return c, errors.New("content combination catalog unavailable")
	}
	values := url.Values{"template_slug": {slug}, "defect_ids": {fmt.Sprintf("%d,%d", ids[0], ids[1])}}
	err := r.catalog.get(ctx, "/internal/v1/catalog/combinations/lookup?"+values.Encode(), &c)
	return c, err
}
func (r *PGFaultCommandRepo) ListCombinations(ctx context.Context) ([]model.FaultCombination, error) {
	var out []model.FaultCombination
	if r.catalog == nil {
		return nil, errors.New("content combination catalog unavailable")
	}
	err := r.catalog.get(ctx, "/internal/v1/catalog/combinations", &out)
	return out, err
}
