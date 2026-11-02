package repository

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// 仓储层通用错误。
var (
	ErrDefectNotFound      = errors.New("故障剧本不存在")
	ErrDefectCodeDuplicate = errors.New("剧本编码已存在")

	ErrExperimentNotFound = errors.New("注入实验不存在")
)

// isUniqueViolation 判断是否 PG 唯一约束冲突（SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
