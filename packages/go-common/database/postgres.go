// Package database 提供 PostgreSQL（pgx/v5 连接池）与 migrate 封装。
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGConfig 数据库连接配置。
type PGConfig struct {
	DSN             string // postgres://user:pass@host:5432/db?sslmode=disable
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
}

// poolConfig sets the application SQL session defaults before opening connections.
func poolConfig(cfg PGConfig) (*pgxpool.Config, error) {
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 20
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN 失败: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	// TIMESTAMPTZ keeps the same instant. Only SQL input/output and day boundaries
	// use this zone; never shift existing rows or remove their time-zone semantics.
	poolCfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	return poolCfg, nil
}

// NewPG 创建并健康检查连接池。
func NewPG(ctx context.Context, cfg PGConfig) (*pgxpool.Pool, error) {
	poolCfg, err := poolConfig(cfg)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("连接 PG 失败: %w", err)
	}
	return pool, nil
}
