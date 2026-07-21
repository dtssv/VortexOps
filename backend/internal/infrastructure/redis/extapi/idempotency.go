// Package extapi 提供对外 API 的 Redis 基础设施：幂等键 TTL 存储。
package extapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/vortexops/vortexops/internal/domain/extapi"
)

const defaultIdempotencyTTL = 24 * time.Hour

// IdempotencyStore Redis 幂等键存储。
type IdempotencyStore struct {
	client goredis.UniversalClient
	ttl    time.Duration
}

// NewIdempotencyStore 创建幂等存储。
func NewIdempotencyStore(client goredis.UniversalClient) *IdempotencyStore {
	return &IdempotencyStore{client: client, ttl: defaultIdempotencyTTL}
}

func idempotencyKey(key string) string {
	return fmt.Sprintf("extapi:idem:%s", key)
}

// Get 读取幂等记录。
func (s *IdempotencyStore) Get(ctx context.Context, key string) (*extapi.IdempotencyRecord, error) {
	raw, err := s.client.Get(ctx, idempotencyKey(key)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var rec extapi.IdempotencyRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Set 写入幂等记录。
func (s *IdempotencyStore) Set(ctx context.Context, rec *extapi.IdempotencyRecord, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = s.ttl
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, idempotencyKey(rec.Key), data, ttl).Err()
}

// GetIdempotency 实现 extapi.Repository 幂等读。
func (s *IdempotencyStore) GetIdempotency(ctx context.Context, key string) (*extapi.IdempotencyRecord, error) {
	return s.Get(ctx, key)
}

// SetIdempotency 实现 extapi.Repository 幂等写。
func (s *IdempotencyStore) SetIdempotency(ctx context.Context, rec *extapi.IdempotencyRecord, ttl time.Duration) error {
	return s.Set(ctx, rec, ttl)
}
