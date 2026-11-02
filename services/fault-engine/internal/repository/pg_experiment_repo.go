// PostgreSQL 版注入实验仓储。
//
// 对应 migrations 000005（experiments 表）。spec 以 JSONB 完整保存 model.InjectSpec，
// 撤销与崩溃补偿时可直接重放；namespace/pod 冗余成列便于人工排查与列表展示。
// 写法对齐 pg_defect_repo.go（pgx/v5 + JSONB）。
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/quenching/fault-engine/internal/model"
)

// PGExperimentRepo experiments 表的 PG 实现。
type PGExperimentRepo struct {
	pool *pgxpool.Pool
}

// 编译期断言：PGExperimentRepo 满足 ExperimentRepository。
var _ ExperimentRepository = (*PGExperimentRepo)(nil)

// NewPGExperimentRepo 创建 PG 实验仓储。
func NewPGExperimentRepo(pool *pgxpool.Pool) *PGExperimentRepo {
	return &PGExperimentRepo{pool: pool}
}

// experimentColumns 查询列顺序，scanExperiment 与之严格对应。
const experimentColumns = `
	id, session_id, source, kind, namespace, pod, spec,
	command, status, experiment_uid, COALESCE(fail_reason, ''),
	created_at, updated_at`

// Create 登记实验。namespace/pod 优先取自 spec，spec 缺失时回退解析 Target（"ns/pod"）。
func (r *PGExperimentRepo) Create(ctx context.Context, e *model.Experiment) error {
	specJSON, err := json.Marshal(e.Spec)
	if err != nil {
		return fmt.Errorf("序列化注入规格失败: %w", err)
	}
	ns, pod := resolveTarget(e)
	source := e.Source
	if source == "" {
		source = model.SourceScenario
	}
	status := e.Status
	if status == "" {
		status = model.StatusCreated
	}

	err = r.pool.QueryRow(ctx, `
		INSERT INTO experiments (
			session_id, source, kind, namespace, pod, spec,
			command, status, experiment_uid, fail_reason
		)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)
		RETURNING id, created_at, updated_at`,
		e.SessionID, source, string(e.Kind), ns, pod, string(specJSON),
		e.Command, string(status), e.ExperimentUID, nullableString(e.FailReason),
	).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return err
	}
	e.Source = source
	e.Status = status
	return nil
}

// MarkRunning 注入成功落 uid 与命令。记录不存在返回 ErrExperimentNotFound。
func (r *PGExperimentRepo) MarkRunning(ctx context.Context, id int64, uid, command string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE experiments
		SET status = 'running', experiment_uid = $2, command = $3,
		    fail_reason = NULL, updated_at = now()
		WHERE id = $1`,
		id, uid, command)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrExperimentNotFound
	}
	return nil
}

// MarkRevoked 恢复成功。仅 created/running 生效，重复撤销幂等成功。
func (r *PGExperimentRepo) MarkRevoked(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE experiments
		SET status = 'revoked', updated_at = now()
		WHERE id = $1 AND status IN ('created', 'running')`, id)
	return err
}

// MarkFailed 失败落原因。
func (r *PGExperimentRepo) MarkFailed(ctx context.Context, id int64, reason string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE experiments
		SET status = 'failed', fail_reason = $2, updated_at = now()
		WHERE id = $1`, id, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrExperimentNotFound
	}
	return nil
}

// GetByUID 按 blade uid 查询，带会话归属校验。
func (r *PGExperimentRepo) GetByUID(ctx context.Context, sessionID int64, uid string) (*model.Experiment, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+experimentColumns+" FROM experiments WHERE session_id = $1 AND experiment_uid = $2",
		sessionID, uid)
	return scanExperiment(row)
}

// ListActiveBySession 查询会话下未终结实验，按 ID 升序（撤销顺序与注入顺序一致）。
func (r *PGExperimentRepo) ListActiveBySession(ctx context.Context, sessionID int64) ([]*model.Experiment, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+experimentColumns+` FROM experiments
		 WHERE session_id = $1 AND status IN ('created', 'running')
		 ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectExperiments(rows)
}

// ListBySession 查询会话下全部实验。
func (r *PGExperimentRepo) ListBySession(ctx context.Context, sessionID int64) ([]*model.Experiment, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT "+experimentColumns+" FROM experiments WHERE session_id = $1 ORDER BY id ASC",
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectExperiments(rows)
}

// ——— 辅助 ———

// resolveTarget 从 spec 取命名空间/Pod；spec 未填时回退解析 Target "namespace/pod"。
func resolveTarget(e *model.Experiment) (ns, pod string) {
	ns = e.Spec.TargetNamespace
	pod = e.Spec.TargetPod
	if ns != "" || pod != "" {
		return ns, pod
	}
	if e.Target == "" {
		return "", ""
	}
	parts := strings.SplitN(e.Target, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

func scanExperiment(row scannable) (*model.Experiment, error) {
	var e model.Experiment
	var specJSON []byte
	var kind, source, status, ns, pod string
	err := row.Scan(
		&e.ID, &e.SessionID, &source, &kind, &ns, &pod, &specJSON,
		&e.Command, &status, &e.ExperimentUID, &e.FailReason,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrExperimentNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Source = source
	e.Kind = model.FaultKind(kind)
	e.Status = model.FaultStatus(status)
	_ = json.Unmarshal(specJSON, &e.Spec)
	// 兼容老数据：spec 内缺目标时用冗余列补齐。
	if e.Spec.TargetNamespace == "" {
		e.Spec.TargetNamespace = ns
	}
	if e.Spec.TargetPod == "" {
		e.Spec.TargetPod = pod
	}
	switch {
	case ns != "" && pod != "":
		e.Target = ns + "/" + pod
	default:
		e.Target = ns + pod
	}
	return &e, nil
}

// experimentRows 抽象 rows 的 Next/Err/Scan（pgx.Rows 天然满足）。
type experimentRows interface {
	Next() bool
	Err() error
	Scan(dest ...interface{}) error
}

func collectExperiments(rows experimentRows) ([]*model.Experiment, error) {
	items := make([]*model.Experiment, 0)
	for rows.Next() {
		e, err := scanExperiment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
