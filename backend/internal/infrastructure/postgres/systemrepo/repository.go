// Package systemrepo 是系统设置领域的 PostgreSQL 仓储实现。
package systemrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/system"
)

// Repository 系统设置 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const settingColumns = `id, key, value, description, is_public, version, created_at, created_by, updated_at, updated_by,
	deleted, deleted_at, deleted_by`

func scanSetting(row pgx.Row) (*system.Setting, error) {
	s := &system.Setting{}
	var (
		desc       *string
		valueBytes []byte
		createdBy  *int64
		updatedBy  *int64
		deletedAt  *time.Time
		deletedBy  *int64
	)
	if err := row.Scan(
		&s.ID, &s.Key, &valueBytes, &desc, &s.IsPublic, &s.Version,
		&s.CreatedAt, &createdBy, &s.UpdatedAt, &updatedBy,
		&s.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		s.Description = *desc
	}
	if valueBytes != nil {
		_ = json.Unmarshal(valueBytes, &s.Value)
	}
	if createdBy != nil {
		s.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		s.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		s.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		s.DeletedBy = *deletedBy
	}
	return s, nil
}

// GetByKey 按 key 查询。
func (r *Repository) GetByKey(ctx context.Context, key string) (*system.Setting, error) {
	q := `SELECT ` + settingColumns + ` FROM vo_system_settings WHERE key=$1 AND deleted=false`
	s, err := scanSetting(r.pool.QueryRow(ctx, q, key))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, system.ErrSettingNotFound
		}
		return nil, fmt.Errorf("get system setting: %w", err)
	}
	return s, nil
}

// List 列出设置项。
func (r *Repository) List(ctx context.Context, q system.Query) ([]*system.Setting, error) {
	conds := []string{"deleted = false"}
	args := []any{}
	argIdx := 1
	if q.PublicOnly {
		conds = append(conds, fmt.Sprintf("is_public = $%d", argIdx))
		args = append(args, true)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(key ILIKE $%d OR description ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := joinAnd(conds)
	query := fmt.Sprintf("SELECT %s FROM vo_system_settings WHERE %s ORDER BY key ASC", settingColumns, where)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list system settings: %w", err)
	}
	defer rows.Close()
	var items []*system.Setting
	for rows.Next() {
		s, err := scanSetting(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// Upsert 按 key 插入或更新（保留 description/is_public 若为空时不覆盖）。
func (r *Repository) Upsert(ctx context.Context, s *system.Setting) (*system.Setting, error) {
	now := r.now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	valueBytes, _ := json.Marshal(s.Value)
	// ON CONFLICT (key) DO UPDATE：更新 value、description、is_public、version+1、updated_*。
	const q = `INSERT INTO vo_system_settings (key, value, description, is_public, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,1,$5,$6,$7,$8)
		ON CONFLICT (key) DO UPDATE SET
			value = EXCLUDED.value,
			description = COALESCE(NULLIF(EXCLUDED.description, ''), vo_system_settings.description),
			is_public = EXCLUDED.is_public,
			version = vo_system_settings.version + 1,
			updated_at = EXCLUDED.updated_at,
			updated_by = EXCLUDED.updated_by
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		s.Key, valueBytes, s.Description, s.IsPublic,
		s.CreatedAt, nullableInt64(s.CreatedBy), s.UpdatedAt, nullableInt64(s.UpdatedBy),
	).Scan(&s.ID, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert system setting: %w", err)
	}
	return s, nil
}

// Delete 软删除。
func (r *Repository) Delete(ctx context.Context, key string, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_system_settings SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE key=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), key)
	if err != nil {
		return fmt.Errorf("delete system setting: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return system.ErrSettingNotFound
	}
	return nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
