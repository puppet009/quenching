package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/quenching/sandbox-service/internal/model"
	"github.com/quenching/sandbox-service/internal/service"
	"time"
)

const elasticLock int64 = 1009202602

func (s *PGStore) WithElasticLock(ctx context.Context, wait bool, fn func(context.Context) error) error {
	c, e := s.pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer c.Release()
	if wait {
		_, e = c.Exec(ctx, `SELECT pg_advisory_lock($1)`, elasticLock)
	} else {
		var ok bool
		e = c.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, elasticLock).Scan(&ok)
		if e == nil && !ok {
			return nil
		}
	}
	if e != nil {
		return e
	}
	defer func() {
		u, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := c.Exec(u, `SELECT pg_advisory_unlock($1)`, elasticLock); err != nil {
			_ = c.Conn().Close(u)
		}
	}()
	return fn(ctx)
}
func (s *PGStore) ElasticState(ctx context.Context) (model.ElasticState, error) {
	v := model.ElasticState{Policies: []model.ElasticPolicy{}, Holds: []model.ScalingHold{}, Audit: []model.ElasticAudit{}}
	rows, e := s.pool.Query(ctx, `SELECT policy,version FROM resource_elastic_policies ORDER BY namespace,name`)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var raw []byte
		var version int64
		if e = rows.Scan(&raw, &version); e != nil {
			rows.Close()
			return v, e
		}
		var p model.ElasticPolicy
		if e = json.Unmarshal(raw, &p); e != nil {
			rows.Close()
			return v, e
		}
		p.Version = version
		v.Policies = append(v.Policies, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = s.pool.Query(ctx, `SELECT namespace,hold_id,reason,created_at FROM resource_scaling_holds ORDER BY created_at`)
	if e != nil {
		return v, e
	}
	for rows.Next() {
		var h model.ScalingHold
		if e = rows.Scan(&h.Namespace, &h.ID, &h.Reason, &h.CreatedAt); e != nil {
			rows.Close()
			return v, e
		}
		v.Holds = append(v.Holds, h)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	rows, e = s.pool.Query(ctx, `SELECT namespace,name,action,detail,created_at FROM resource_elastic_audit ORDER BY id DESC LIMIT 100`)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var a model.ElasticAudit
		if e = rows.Scan(&a.Namespace, &a.Name, &a.Action, &a.Detail, &a.CreatedAt); e != nil {
			return v, e
		}
		v.Audit = append(v.Audit, a)
	}
	return v, rows.Err()
}
func (s *PGStore) InsertElasticPolicy(ctx context.Context, p model.ElasticPolicy) error {
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.pool.Exec(ctx, `INSERT INTO resource_elastic_policies(namespace,name,policy)VALUES($1,$2,$3)ON CONFLICT DO NOTHING`, p.Namespace, p.Name, raw)
	return e
}
func (s *PGStore) SaveElasticPolicy(ctx context.Context, p *model.ElasticPolicy, plan *model.ResourcePlan) error {
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	result, e := tx.Exec(ctx, `UPDATE resource_elastic_policies SET policy=$3,version=version+1 WHERE namespace=$1 AND name=$2 AND version=$4`, p.Namespace, p.Name, raw, p.Version)
	if e != nil {
		return e
	}
	if result.RowsAffected() != 1 {
		return service.ErrResourceConflict
	}
	if plan != nil {
		raw, e = json.Marshal(plan)
		if e != nil {
			return e
		}
		result, e = tx.Exec(ctx, `UPDATE sandboxes SET resource_plan=$2 WHERE session_id=$1 AND namespace=$3 AND resources_admitted AND status IN ('ready','running') AND completed_at IS NULL AND deleted_at IS NULL`, p.SessionID, raw, p.Namespace)
		if e != nil {
			return e
		}
		if result.RowsAffected() != 1 {
			return errors.New("training allocation no longer active")
		}
	}
	if e = tx.Commit(ctx); e == nil {
		p.Version++
	}
	return e
}
func (s *PGStore) AcquireScalingHold(ctx context.Context, h model.ScalingHold) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO resource_scaling_holds(namespace,hold_id,reason)VALUES($1,$2,$3)ON CONFLICT DO NOTHING`, h.Namespace, h.ID, h.Reason)
	return e
}
func (s *PGStore) ReleaseScalingHold(ctx context.Context, ns, id string) error {
	_, e := s.pool.Exec(ctx, `DELETE FROM resource_scaling_holds WHERE namespace=$1 AND hold_id=$2`, ns, id)
	return e
}
func (s *PGStore) DeleteElasticNamespace(ctx context.Context, ns string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	for _, table := range []string{"resource_elastic_policies", "resource_scaling_holds"} {
		if _, e = tx.Exec(ctx, `DELETE FROM `+table+` WHERE namespace=$1`, ns); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *PGStore) AuditElastic(ctx context.Context, a model.ElasticAudit) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO resource_elastic_audit(namespace,name,action,detail)VALUES($1,$2,$3,$4)`, a.Namespace, a.Name, a.Action, a.Detail)
	return e
}
