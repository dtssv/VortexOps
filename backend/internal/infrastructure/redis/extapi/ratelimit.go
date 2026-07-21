// Package extapi 提供对外 API 的 Redis 限流：按 Token 每分钟令牌桶。
package extapi

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const rateLimitWindow = time.Minute

// RateLimiter Token 级每分钟限流。
type RateLimiter struct {
	client goredis.UniversalClient
}

// NewRateLimiter 创建限流器。
func NewRateLimiter(client goredis.UniversalClient) *RateLimiter {
	return &RateLimiter{client: client}
}

func rateKey(tokenID int64) string {
	return fmt.Sprintf("extapi:ratelimit:%d", tokenID)
}

// Allow 尝试消耗一个令牌。limit 为每分钟上限；0 或负数表示不限流。
func (l *RateLimiter) Allow(ctx context.Context, tokenID int64, limit int) (allowed bool, retryAfter time.Duration, err error) {
	if limit <= 0 {
		return true, 0, nil
	}
	key := rateKey(tokenID)
	now := time.Now()
	windowStart := now.Truncate(rateLimitWindow)
	windowKey := fmt.Sprintf("%s:%d", key, windowStart.Unix())

	pipe := l.client.TxPipeline()
	incr := pipe.Incr(ctx, windowKey)
	pipe.Expire(ctx, windowKey, rateLimitWindow+5*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}
	count := incr.Val()
	if count > int64(limit) {
		retry := windowStart.Add(rateLimitWindow).Sub(now)
		if retry < 0 {
			retry = time.Second
		}
		return false, retry, nil
	}
	return true, 0, nil
}

// Remaining 返回当前窗口剩余配额（仅供调试/响应头）。
func (l *RateLimiter) Remaining(ctx context.Context, tokenID int64, limit int) (int, error) {
	if limit <= 0 {
		return limit, nil
	}
	windowStart := time.Now().Truncate(rateLimitWindow)
	windowKey := fmt.Sprintf("%s:%d", rateKey(tokenID), windowStart.Unix())
	n, err := l.client.Get(ctx, windowKey).Int()
	if err == goredis.Nil {
		return limit, nil
	}
	if err != nil {
		return 0, err
	}
	rem := limit - n
	if rem < 0 {
		rem = 0
	}
	return rem, nil
}
