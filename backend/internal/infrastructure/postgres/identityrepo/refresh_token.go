package identityrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/identity"
)

// RefreshTokenRepository 刷新令牌的 PostgreSQL 实现。
type RefreshTokenRepository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewRefreshTokenRepository 创建刷新令牌仓储。
func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool, now: time.Now}
}

const refreshColumns = `id, user_id, token_hash, device_id, device_name, ip, user_agent, expires_at, revoked_at, created_at`

func scanRefreshToken(row pgx.Row) (*identity.RefreshToken, error) {
	t := &identity.RefreshToken{}
	var (
		deviceID   *string
		deviceName *string
		ip         *string
		userAgent  *string
		revokedAt  *time.Time
	)
	if err := row.Scan(
		&t.ID, &t.UserID, &t.TokenHash, &deviceID, &deviceName, &ip, &userAgent,
		&t.ExpiresAt, &revokedAt, &t.CreatedAt,
	); err != nil {
		return nil, err
	}
	if deviceID != nil {
		t.DeviceID = *deviceID
	}
	if deviceName != nil {
		t.DeviceName = *deviceName
	}
	if ip != nil {
		t.IP = *ip
	}
	if userAgent != nil {
		t.UserAgent = *userAgent
	}
	if revokedAt != nil {
		t.RevokedAt = revokedAt
	}
	return t, nil
}

// Create 插入刷新令牌记录（仅存哈希）。
func (r *RefreshTokenRepository) Create(ctx context.Context, t *identity.RefreshToken) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = r.now()
	}
	const q = `INSERT INTO vo_refresh_tokens
		(user_id, token_hash, device_id, device_name, ip, user_agent, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`
	err := r.pool.QueryRow(ctx, q,
		t.UserID, t.TokenHash, nullableStr(t.DeviceID), nullableStr(t.DeviceName),
		nullableStr(t.IP), nullableStr(t.UserAgent), t.ExpiresAt, t.CreatedAt,
	).Scan(&t.ID, &t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// GetByHash 按哈希查询。
func (r *RefreshTokenRepository) GetByHash(ctx context.Context, hash string) (*identity.RefreshToken, error) {
	q := `SELECT ` + refreshColumns + ` FROM vo_refresh_tokens WHERE token_hash = $1`
	t, err := scanRefreshToken(r.pool.QueryRow(ctx, q, hash))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrRefreshTokenNotFound
		}
		return nil, err
	}
	return t, nil
}

// Revoke 撤销单个令牌。
func (r *RefreshTokenRepository) Revoke(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_refresh_tokens SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		r.now(), id)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrRefreshTokenNotFound
	}
	return nil
}

// RevokeAllForUser 撤销某用户全部令牌（用于登出全部设备、改密）。
func (r *RefreshTokenRepository) RevokeAllForUser(ctx context.Context, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_refresh_tokens SET revoked_at = $1 WHERE user_id = $2 AND revoked_at IS NULL`,
		r.now(), userID)
	if err != nil {
		return fmt.Errorf("revoke all refresh tokens: %w", err)
	}
	return nil
}

// RevokeExpired 清理已过期令牌（定时任务调用），返回清理行数。
func (r *RefreshTokenRepository) RevokeExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM vo_refresh_tokens WHERE expires_at < $1`,
		r.now())
	if err != nil {
		return 0, fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// nullableStr 空串视为 NULL。
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
