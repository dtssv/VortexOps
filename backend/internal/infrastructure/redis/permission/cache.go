// Package permission 是权限缓存（Redis 实现）。
// 缓存用户权限 code 集合，TTL 5 分钟，角色/绑定变更时主动 evict。
package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache Redis 权限缓存。
type Cache struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// New 创建权限缓存。
func New(client redis.UniversalClient) *Cache {
	return &Cache{client: client, ttl: 5 * time.Minute}
}

func key(userID int64) string {
	return fmt.Sprintf("perm:user:%d", userID)
}

// GetUserPermissions 读取用户权限集。
func (c *Cache) GetUserPermissions(ctx context.Context, userID int64) ([]string, error) {
	raw, err := c.client.Get(ctx, key(userID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// SetUserPermissions 写入用户权限集。
func (c *Cache) SetUserPermissions(ctx context.Context, userID int64, codes []string, ttl time.Duration) error {
	data, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key(userID), data, ttl).Err()
}

// EvictUserPermissions 失效用户权限缓存。
func (c *Cache) EvictUserPermissions(ctx context.Context, userID int64) error {
	return c.client.Del(ctx, key(userID)).Err()
}
