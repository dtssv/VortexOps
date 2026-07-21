// Package redis 封装 go-redis v9 客户端。
// 支持 standalone / sentinel / cluster 三种模式（按配置自动选择）。
// 生产级配置：连接池、超时、重试。
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/vortexops/vortexops/internal/config"
)

// Client 包装 go-redis 客户端，统一 standalone / sentinel / cluster。
type Client struct {
	Universal goredis.UniversalClient
	cfg       config.RedisConfig
}

// New 根据配置创建 Redis 客户端。
// 单地址且无 sentinel -> standalone；多地址且无 sentinel -> cluster；有 sentinel master -> sentinel。
func New(ctx context.Context, cfg config.RedisConfig) (*Client, error) {
	if len(cfg.Addrs) == 0 {
		return nil, errors.New("redis.addrs is required")
	}

	opts := &goredis.UniversalOptions{
		Addrs:        cfg.Addrs,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxRetries:   cfg.MaxRetries,
	}
	if cfg.SentinelMaster != "" {
		opts.MasterName = cfg.SentinelMaster
	}

	client := goredis.NewUniversalClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Client{Universal: client, cfg: cfg}, nil
}

// HealthCheck 执行 PING，用于 readiness 探针。
func (c *Client) HealthCheck(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Universal.Ping(pingCtx).Err(); err != nil {
		return fmt.Errorf("redis health check: %w", err)
	}
	return nil
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.Universal != nil {
		return c.Universal.Close()
	}
	return nil
}
