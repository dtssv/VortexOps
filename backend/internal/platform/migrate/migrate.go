// Package migrate 提供 VortexOps 数据库迁移命令。
// 基于 golang-migrate，从 migrations 目录加载 SQL 文件，支持 up/down/version/force。
package migrate

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // 驱动注册
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/vortexops/vortexops/internal/config"
	"github.com/vortexops/vortexops/migrations"
)

// New 创建 migrate 实例，从 embed 的 migrations 目录加载。
func New(cfg config.DBConfig) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("create migration source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, cfg.AdminDSN())
	if err != nil {
		return nil, fmt.Errorf("create migrate instance: %w", err)
	}
	return m, nil
}

// Run 执行迁移命令。command: up / down / version / force <version>。
func Run(cfg config.DBConfig, command string, arg int) error {
	m, err := New(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	switch command {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate up: %w", err)
		}
		return nil
	case "down":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate down: %w", err)
		}
		return nil
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			return fmt.Errorf("get version: %w", err)
		}
		fmt.Printf("version=%d dirty=%v\n", v, dirty)
		return nil
	case "force":
		if err := m.Force(arg); err != nil {
			return fmt.Errorf("force version %d: %w", arg, err)
		}
		return nil
	case "steps":
		if err := m.Steps(arg); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate steps %d: %w", arg, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown migrate command: %s", command)
	}
}

// WaitForReady 轮询直到迁移就绪或超时，供启动时确保 schema 存在。
func WaitForReady(cfg config.DBConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		m, err := New(cfg)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}
		_, _, verr := m.Version()
		_, _ = m.Close()
		if verr == nil {
			return nil
		}
		lastErr = verr
		time.Sleep(time.Second)
	}
	return fmt.Errorf("migration not ready within %s: %w", timeout, lastErr)
}
