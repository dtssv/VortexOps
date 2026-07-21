// Package db 封装 PostgreSQL 连接池与运行时健康检查。
// 使用 pgx v5 原生连接池（pgxpool），生产级配置：
//   - 最大/最小连接数限制，防止打爆 PG
//   - 连接生命周期与空闲超时，回收坏连接
//   - statement_timeout 防止慢查询占用连接
//   - 健康检查 ping 供 readiness 探针
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/config"
)

// Pool 包装 pgxpool.Pool，提供健康检查与统一关闭。
type Pool struct {
	*pgxpool.Pool
	cfg config.DBConfig
}

// New 创建并校验连接池，返回前会执行一次真实 ping。
func New(ctx context.Context, cfg config.DBConfig) (*Pool, error) {
	if cfg.MaxConns <= 0 {
		return nil, errors.New("db.max_conns must be positive")
	}

	pcfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	pcfg.MaxConns = cfg.MaxConns
	pcfg.MinConns = cfg.MinConns
	pcfg.MaxConnLifetime = cfg.MaxConnLifetime
	pcfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	pcfg.HealthCheckPeriod = cfg.HealthCheckInterval
	if cfg.StatementTimeout > 0 {
		// 在每个连接建立后设置 statement_timeout，避免长查询占用连接。
		pcfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", cfg.StatementTimeout.Milliseconds()))
			return err
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("create db pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &Pool{Pool: pool, cfg: cfg}, nil
}

// HealthCheck 执行一次 SELECT 1，用于 readiness 探针。
func (p *Pool) HealthCheck(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var one int
	if err := p.QueryRow(pingCtx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("db health check: %w", err)
	}
	if one != 1 {
		return errors.New("db health check returned unexpected value")
	}
	return nil
}

// Close 优雅关闭连接池。
func (p *Pool) Close() {
	if p.Pool != nil {
		p.Pool.Close()
	}
}
