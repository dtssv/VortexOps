// Package identityrepo 是身份领域的 PostgreSQL 仓储实现。
// 使用 pgx v5 直接执行参数化 SQL，所有查询走预编译路径。
package identityrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/identity"
)

const pgUniqueViolation = "23505"

// UserRepository 用户仓储的 PostgreSQL 实现。
type UserRepository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewUserRepository 创建用户仓储。
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool, now: time.Now}
}

const userColumns = `id, uuid, username, email, phone, display_name, avatar_url, password_hash,
	auth_source, external_id, status, last_login_at, last_login_ip, password_changed_at,
	must_change_password, locale, timezone, metadata,
	mfa_enabled, mfa_secret, mfa_backup_codes,
	version, created_at, created_by, updated_at, updated_by`

func scanUser(row pgx.Row) (*identity.User, error) {
	u := &identity.User{Metadata: map[string]any{}, MFABackupCodes: []string{}}
	var (
		lastLoginAt       *time.Time
		passwordChangedAt *time.Time
		metadata          []byte
		backupCodes       []byte
		// 可空字符串列：DB 允许 NULL，需用 *string 接收再解引用，避免 pgx 把 NULL 扫入非指针 string 报错。
		phone       *string
		avatarURL   *string
		externalID  *string
		lastLoginIP *string
		mfaSecret   *string
	)
	if err := row.Scan(
		&u.ID, &u.UUID, &u.Username, &u.Email, &phone, &u.DisplayName, &avatarURL,
		&u.PasswordHash, &u.AuthSource, &externalID, &u.Status, &lastLoginAt, &lastLoginIP,
		&passwordChangedAt, &u.MustChangePassword, &u.Locale, &u.Timezone, &metadata,
		&u.MFAEnabled, &mfaSecret, &backupCodes,
		&u.Version, &u.CreatedAt, &u.CreatedBy, &u.UpdatedAt, &u.UpdatedBy,
	); err != nil {
		return nil, err
	}
	if phone != nil {
		u.Phone = *phone
	}
	if avatarURL != nil {
		u.AvatarURL = *avatarURL
	}
	if externalID != nil {
		u.ExternalID = *externalID
	}
	if lastLoginIP != nil {
		u.LastLoginIP = *lastLoginIP
	}
	if mfaSecret != nil {
		u.MFASecret = *mfaSecret
	}
	if lastLoginAt != nil {
		u.LastLoginAt = *lastLoginAt
	}
	if passwordChangedAt != nil {
		u.PasswordChangedAt = *passwordChangedAt
	}
	if metadata != nil {
		_ = scanJSONB(metadata, &u.Metadata)
	}
	if backupCodes != nil {
		_ = scanJSONBSlice(backupCodes, &u.MFABackupCodes)
	}
	return u, nil
}

// Create 插入新用户。
func (r *UserRepository) Create(ctx context.Context, u *identity.User) error {
	if u.UUID == uuid.Nil {
		u.UUID = uuid.New()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = r.now()
		u.UpdatedAt = u.CreatedAt
	}
	const q = `INSERT INTO vo_users
		(uuid, username, email, phone, display_name, avatar_url, password_hash,
		 auth_source, external_id, status, must_change_password, locale, timezone, metadata,
		 version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id, created_at, updated_at, version`
	metadata, _ := jsonbMarshal(u.Metadata)
	err := r.pool.QueryRow(ctx, q,
		u.UUID, u.Username, u.Email, u.Phone, u.DisplayName, u.AvatarURL, u.PasswordHash,
		u.AuthSource, u.ExternalID, u.Status, u.MustChangePassword, u.Locale, u.Timezone, metadata,
		u.Version, u.CreatedAt, nullableInt64(u.CreatedBy), u.UpdatedAt, nullableInt64(u.UpdatedBy),
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			if strings.Contains(pgErr.ConstraintName, "username") {
				return identity.ErrUsernameExists
			}
			if strings.Contains(pgErr.ConstraintName, "email") {
				return identity.ErrEmailExists
			}
			return identity.ErrUsernameExists
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// GetByID 按 ID 查询。
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*identity.User, error) {
	q := `SELECT ` + userColumns + ` FROM vo_users WHERE id = $1 AND deleted = false`
	return scanUser(r.pool.QueryRow(ctx, q, id))
}

// GetByUUID 按 UUID 查询。
func (r *UserRepository) GetByUUID(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	q := `SELECT ` + userColumns + ` FROM vo_users WHERE uuid = $1 AND deleted = false`
	return scanUser(r.pool.QueryRow(ctx, q, id))
}

// GetByUsername 按用户名查询。
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*identity.User, error) {
	q := `SELECT ` + userColumns + ` FROM vo_users WHERE username = $1 AND deleted = false`
	return scanUser(r.pool.QueryRow(ctx, q, username))
}

// GetByEmail 按邮箱查询。
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	q := `SELECT ` + userColumns + ` FROM vo_users WHERE email = $1 AND deleted = false`
	return scanUser(r.pool.QueryRow(ctx, q, email))
}

// GetByExternalID 按认证来源与外部 ID 查询用户（SSO 回查）。
func (r *UserRepository) GetByExternalID(ctx context.Context, source identity.AuthSource, externalID string) (*identity.User, error) {
	q := `SELECT ` + userColumns + ` FROM vo_users WHERE auth_source = $1 AND external_id = $2 AND deleted = false`
	return scanUser(r.pool.QueryRow(ctx, q, source, externalID))
}

// Update 更新用户（乐观锁，version 不匹配返回冲突错误）。
func (r *UserRepository) Update(ctx context.Context, u *identity.User) error {
	u.UpdatedAt = r.now()
	u.Version++
	metadata, _ := jsonbMarshal(u.Metadata)
	backupCodes, _ := jsonbMarshalSlice(u.MFABackupCodes)
	const q = `UPDATE vo_users SET
		email=$1, phone=$2, display_name=$3, avatar_url=$4, password_hash=$5,
		status=$6, last_login_at=$7, last_login_ip=$8, password_changed_at=$9,
		must_change_password=$10, locale=$11, timezone=$12, metadata=$13,
		mfa_enabled=$14, mfa_secret=$15, mfa_backup_codes=$16,
		version=$17, updated_at=$18, updated_by=$19
		WHERE id=$20 AND version=$21 AND deleted=false`
	tag, err := r.pool.Exec(ctx, q,
		u.Email, u.Phone, u.DisplayName, u.AvatarURL, u.PasswordHash,
		u.Status, nullableTime(u.LastLoginAt), u.LastLoginIP, nullableTime(u.PasswordChangedAt),
		u.MustChangePassword, u.Locale, u.Timezone, metadata,
		u.MFAEnabled, nullableStr(u.MFASecret), backupCodes,
		u.Version, u.UpdatedAt, nullableInt64(u.UpdatedBy),
		u.ID, u.Version-1,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			if strings.Contains(pgErr.ConstraintName, "email") {
				return identity.ErrEmailExists
			}
		}
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// UpdateLastLogin 更新最后登录时间与 IP。
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID int64, at time.Time, ip string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_users SET last_login_at=$1, last_login_ip=$2, updated_at=$3, version=version+1 WHERE id=$4 AND deleted=false`,
		at, ip, r.now(), userID)
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}

// List 分页查询用户。
func (r *UserRepository) List(ctx context.Context, q identity.UserQuery) ([]*identity.User, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	if q.Username != "" {
		conds = append(conds, fmt.Sprintf("username = $%d", argIdx))
		args = append(args, q.Username)
		argIdx++
	}
	if q.Email != "" {
		conds = append(conds, fmt.Sprintf("email = $%d", argIdx))
		args = append(args, q.Email)
		argIdx++
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, q.Status)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(username ILIKE $%d OR email ILIKE $%d OR display_name ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := "deleted = false"
	if len(conds) > 0 {
		where += " AND " + strings.Join(conds, " AND ")
	}

	var total int64
	countQ := "SELECT count(*) FROM vo_users WHERE " + where
	if err := r.pool.QueryRow(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	listQ := fmt.Sprintf("SELECT %s FROM vo_users WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		userColumns, where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []*identity.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// Delete 软删除用户。
func (r *UserRepository) Delete(ctx context.Context, id int64, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_users SET deleted=true, deleted_at=$1, deleted_by=$2, status='disabled', updated_at=$3, version=version+1 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrUserNotFound
	}
	return nil
}

// 将 pgx.ErrNoRows 转换为领域错误。供 service 层调用。
func TranslateErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrUserNotFound
	}
	return err
}
