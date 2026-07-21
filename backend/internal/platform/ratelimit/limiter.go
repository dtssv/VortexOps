// Package ratelimit 提供基于 Redis 的全局与租户级限流。
package ratelimit

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const keyPrefix = "ratelimit:"

// Limiter Redis 滑动窗口限流器。
type Limiter struct {
	client goredis.UniversalClient
}

// New 创建限流器。
func New(client goredis.UniversalClient) *Limiter {
	return &Limiter{client: client}
}

// Config 限流配置。
type Config struct {
	Key      string
	Limit    int64
	Window   time.Duration
}

// Allow 检查是否允许请求；允许时递增计数并返回 true。
func (l *Limiter) Allow(ctx context.Context, cfg Config) (bool, error) {
	if cfg.Limit <= 0 || cfg.Window <= 0 {
		return true, nil
	}
	key := keyPrefix + cfg.Key
	now := time.Now().UnixMilli()
	windowStart := now - cfg.Window.Milliseconds()

	pipe := l.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, goredis.Z{Score: float64(now), Member: fmt.Sprintf("%d", now)})
	pipe.Expire(ctx, key, cfg.Window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("rate limit check: %w", err)
	}
	return countCmd.Val() < cfg.Limit, nil
}

// AllowGlobal 全局限流（如 apiserver 总 QPS）。
func (l *Limiter) AllowGlobal(ctx context.Context, limit int64, window time.Duration) (bool, error) {
	return l.Allow(ctx, Config{Key: "global", Limit: limit, Window: window})
}

// AllowTenant 租户（workspace）级限流。
func (l *Limiter) AllowTenant(ctx context.Context, workspaceID int64, limit int64, window time.Duration) (bool, error) {
	return l.Allow(ctx, Config{
		Key:    fmt.Sprintf("tenant:%d", workspaceID),
		Limit:  limit,
		Window: window,
	})
}

// MiddlewareConfig HTTP 中间件限流参数。
type MiddlewareConfig struct {
	GlobalLimit  int64
	GlobalWindow time.Duration
	TenantLimit  int64
	TenantWindow time.Duration
	WorkspaceID  func(ctx context.Context) int64
}
