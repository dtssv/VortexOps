package extapirepo

import (
	"context"
	"time"

	"github.com/vortexops/vortexops/internal/domain/extapi"
)

// IdempotencyBackend 幂等存储后端（Redis）。
type IdempotencyBackend interface {
	GetIdempotency(ctx context.Context, key string) (*extapi.IdempotencyRecord, error)
	SetIdempotency(ctx context.Context, rec *extapi.IdempotencyRecord, ttl time.Duration) error
}

// CompositeRepository 组合 Postgres 与 Redis 幂等，实现 extapi.Repository。
type CompositeRepository struct {
	*Repository
	idem IdempotencyBackend
}

// NewComposite 创建组合仓储。
func NewComposite(pg *Repository, idem IdempotencyBackend) *CompositeRepository {
	return &CompositeRepository{Repository: pg, idem: idem}
}

// GetIdempotency 从 Redis 读取幂等记录。
func (c *CompositeRepository) GetIdempotency(ctx context.Context, key string) (*extapi.IdempotencyRecord, error) {
	return c.idem.GetIdempotency(ctx, key)
}

// SetIdempotency 写入 Redis 幂等记录。
func (c *CompositeRepository) SetIdempotency(ctx context.Context, rec *extapi.IdempotencyRecord, ttl time.Duration) error {
	return c.idem.SetIdempotency(ctx, rec, ttl)
}
