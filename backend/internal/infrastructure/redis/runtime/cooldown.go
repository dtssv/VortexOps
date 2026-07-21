// Package runtime - cooldown.go 提供 Pod 异常通知的冷却存储。
// 键规范：cd:pod:{clusterID}:{namespace}:{podName}:{reason}
// 同一 Pod 同一原因在冷却窗口内只通知一次，避免 Informer 高频事件导致通知风暴。
package runtime

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const cooldownKeyPrefix = "cd:pod:"

func cooldownKey(clusterID int64, ns, podName, reason string) string {
	return fmt.Sprintf("%s%d:%s:%s:%s", cooldownKeyPrefix, clusterID, ns, podName, reason)
}

// CooldownStore 提供 Pod 异常通知的冷却判断。
// Acquire 命中冷却（返回 false）；首次或冷却过期则写入并返回 true（允许通知）。
type CooldownStore struct {
	rdb    goredis.UniversalClient
	ttl    time.Duration
}

// NewCooldownStore 创建冷却存储。ttl 为冷却窗口，默认 10 分钟。
func NewCooldownStore(rdb goredis.UniversalClient, ttl time.Duration) *CooldownStore {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &CooldownStore{rdb: rdb, ttl: ttl}
}

// Acquire 尝试获取通知权限。返回 true 表示可通知（已写入冷却标记）；
// false 表示在冷却窗口内（重复事件，应抑制）。
func (c *CooldownStore) Acquire(ctx context.Context, clusterID int64, ns, podName, reason string) (bool, error) {
	if c == nil || c.rdb == nil {
		// 未配置冷却存储时不抑制，允许通知。
		return true, nil
	}
	key := cooldownKey(clusterID, ns, podName, reason)
	// SET NX：键不存在时写入并返回 OK；已存在则返回 nil。
	ok, err := c.rdb.SetNX(ctx, key, "1", c.ttl).Result()
	if err != nil {
		return true, err
	}
	return ok, nil
}

// Release 主动清除冷却（用于 pod 恢复正常后允许下次异常立即通知）。
func (c *CooldownStore) Release(ctx context.Context, clusterID int64, ns, podName, reason string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Del(ctx, cooldownKey(clusterID, ns, podName, reason)).Err()
}

// ReleaseAll 清除某 Pod 所有原因的冷却（pod 恢复正常时调用）。
// 通过 SCAN cd:pod:{clusterID}:{namespace}:{podName}:* 匹配并删除。
func (c *CooldownStore) ReleaseAll(ctx context.Context, clusterID int64, ns, podName string) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	pattern := fmt.Sprintf("%s%d:%s:%s:*", cooldownKeyPrefix, clusterID, ns, podName)
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
