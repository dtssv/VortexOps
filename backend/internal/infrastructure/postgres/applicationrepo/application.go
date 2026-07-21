// Package applicationrepo 是应用与分组领域的 PostgreSQL 仓储实现。
package applicationrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/application"
)

const pgUniqueViolation = "23505"

// Repository 应用与分组仓储的 PostgreSQL 实现。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const appColumns = `id, uuid, workspace_id, name, code, display_name, description, icon,
	default_git_source_id, default_registry_id, lifecycle, owner_id, labels, metadata, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanApplication(row pgx.Row) (*application.Application, error) {
	a := &application.Application{Labels: map[string]string{}, Metadata: map[string]any{}}
	var (
		displayName        *string
		description        *string
		icon               *string
		defaultGitSourceID *int64
		defaultRegistryID  *int64
		labels             []byte
		metadata           []byte
		createdBy          *int64
		updatedBy          *int64
		deletedAt          *time.Time
		deletedBy          *int64
	)
	if err := row.Scan(
		&a.ID, &a.UUID, &a.WorkspaceID, &a.Name, &a.Code, &displayName, &description, &icon,
		&defaultGitSourceID, &defaultRegistryID, &a.Lifecycle, &a.OwnerID, &labels, &metadata, &a.Version,
		&a.CreatedAt, &createdBy, &a.UpdatedAt, &updatedBy, &a.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if displayName != nil {
		a.DisplayName = *displayName
	}
	if description != nil {
		a.Description = *description
	}
	if icon != nil {
		a.Icon = *icon
	}
	if defaultGitSourceID != nil {
		a.DefaultGitSourceID = *defaultGitSourceID
	}
	if defaultRegistryID != nil {
		a.DefaultRegistryID = *defaultRegistryID
	}
	if labels != nil {
		_ = json.Unmarshal(labels, &a.Labels)
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &a.Metadata)
	}
	if createdBy != nil {
		a.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		a.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		a.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		a.DeletedBy = *deletedBy
	}
	return a, nil
}

// CreateApplication 创建应用。
func (r *Repository) CreateApplication(ctx context.Context, a *application.Application) error {
	if a.UUID == uuid.Nil {
		a.UUID = uuid.New()
	}
	now := r.now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
		a.UpdatedAt = now
	}
	if a.Lifecycle == "" {
		a.Lifecycle = application.LifecycleActive
	}
	if a.Labels == nil {
		a.Labels = map[string]string{}
	}
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	labels, _ := json.Marshal(a.Labels)
	metadata, _ := json.Marshal(a.Metadata)
	const q = `INSERT INTO vo_applications
		(uuid, workspace_id, name, code, display_name, description, icon, default_git_source_id, default_registry_id,
		 lifecycle, owner_id, labels, metadata, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		a.UUID, a.WorkspaceID, a.Name, a.Code, nullableStr(a.DisplayName), nullableStr(a.Description), nullableStr(a.Icon),
		nullableInt64(a.DefaultGitSourceID), nullableInt64(a.DefaultRegistryID), a.Lifecycle, a.OwnerID,
		labels, metadata, a.Version, a.CreatedAt, nullableInt64(a.CreatedBy), a.UpdatedAt, nullableInt64(a.CreatedBy),
	).Scan(&a.ID, &a.Version, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			if pgErr.ConstraintName == "uk_app_ws_code" {
				return application.ErrApplicationCodeExists
			}
			return application.ErrApplicationNameExists
		}
		return fmt.Errorf("insert application: %w", err)
	}
	return nil
}

// GetApplicationByID 按 ID 查询。
func (r *Repository) GetApplicationByID(ctx context.Context, id int64) (*application.Application, error) {
	q := `SELECT ` + appColumns + ` FROM vo_applications WHERE id=$1 AND deleted=false`
	a, err := scanApplication(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrApplicationNotFound
		}
		return nil, err
	}
	return a, nil
}

// GetApplicationByUUID 按 UUID 查询。
func (r *Repository) GetApplicationByUUID(ctx context.Context, id uuid.UUID) (*application.Application, error) {
	q := `SELECT ` + appColumns + ` FROM vo_applications WHERE uuid=$1 AND deleted=false`
	a, err := scanApplication(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrApplicationNotFound
		}
		return nil, err
	}
	return a, nil
}

// GetApplicationByName 按工作空间内名称查询。
func (r *Repository) GetApplicationByName(ctx context.Context, workspaceID int64, name string) (*application.Application, error) {
	q := `SELECT ` + appColumns + ` FROM vo_applications WHERE workspace_id=$1 AND name=$2 AND deleted=false`
	a, err := scanApplication(r.pool.QueryRow(ctx, q, workspaceID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrApplicationNotFound
		}
		return nil, err
	}
	return a, nil
}

// GetApplicationByCode 按工作空间内 code 查询。
func (r *Repository) GetApplicationByCode(ctx context.Context, workspaceID int64, code string) (*application.Application, error) {
	q := `SELECT ` + appColumns + ` FROM vo_applications WHERE workspace_id=$1 AND code=$2 AND deleted=false`
	a, err := scanApplication(r.pool.QueryRow(ctx, q, workspaceID, code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrApplicationNotFound
		}
		return nil, err
	}
	return a, nil
}

// UpdateApplication 更新应用（乐观锁）。
func (r *Repository) UpdateApplication(ctx context.Context, in application.UpdateApplicationInput) (*application.Application, error) {
	now := r.now()
	var (
		sets   []string
		args   []any
		argIdx = 1
	)
	addSet := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}
	if in.DisplayName != nil {
		addSet("display_name", nullableStr(*in.DisplayName))
	}
	if in.Description != nil {
		addSet("description", nullableStr(*in.Description))
	}
	if in.Icon != nil {
		addSet("icon", nullableStr(*in.Icon))
	}
	if in.Lifecycle != nil {
		addSet("lifecycle", *in.Lifecycle)
	}
	if in.DefaultGitSourceID != nil {
		addSet("default_git_source_id", nullableInt64(*in.DefaultGitSourceID))
	}
	if in.DefaultRegistryID != nil {
		addSet("default_registry_id", nullableInt64(*in.DefaultRegistryID))
	}
	if in.Labels != nil {
		b, _ := json.Marshal(in.Labels)
		addSet("labels", b)
	}
	if in.Metadata != nil {
		b, _ := json.Marshal(in.Metadata)
		addSet("metadata", b)
	}
	if len(sets) == 0 {
		return r.GetApplicationByID(ctx, in.ID)
	}
	addSet("updated_at", now)
	addSet("updated_by", nullableInt64(in.UpdatedBy))
	addSet("version", in.Version+1)

	args = append(args, in.ID, in.Version)
	q := fmt.Sprintf(`UPDATE vo_applications SET %s WHERE id=$%d AND version=$%d AND deleted=false`,
		strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("update application: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, gerr := r.GetApplicationByID(ctx, in.ID)
		if gerr != nil {
			return nil, application.ErrApplicationNotFound
		}
		if existing.Version != in.Version {
			return nil, domain.ErrConflict
		}
		return nil, application.ErrApplicationNotFound
	}
	return r.GetApplicationByID(ctx, in.ID)
}

// ListApplications 分页查询应用。
func (r *Repository) ListApplications(ctx context.Context, q application.ApplicationQuery) ([]*application.Application, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	conds = append(conds, "deleted = false")
	if q.WorkspaceID != 0 {
		conds = append(conds, fmt.Sprintf("workspace_id = $%d", argIdx))
		args = append(args, q.WorkspaceID)
		argIdx++
	}
	if q.OwnerID != 0 {
		conds = append(conds, fmt.Sprintf("owner_id = $%d", argIdx))
		args = append(args, q.OwnerID)
		argIdx++
	}
	if q.Lifecycle != "" {
		conds = append(conds, fmt.Sprintf("lifecycle = $%d", argIdx))
		args = append(args, q.Lifecycle)
		argIdx++
	}
	if q.AppType != "" {
		conds = append(conds, fmt.Sprintf("metadata->>'app_type' = $%d", argIdx))
		args = append(args, q.AppType)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR display_name ILIKE $%d OR code ILIKE $%d)", argIdx, argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_applications WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count applications: %w", err)
	}

	listQ := fmt.Sprintf("SELECT %s FROM vo_applications WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		appColumns, where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query applications: %w", err)
	}
	defer rows.Close()
	var items []*application.Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, a)
	}
	return items, total, rows.Err()
}

// DeleteApplication 软删除应用。
func (r *Repository) DeleteApplication(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_applications SET deleted=true, deleted_at=$1, deleted_by=$2, lifecycle='archived', updated_at=$3, version=version+1 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete application: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return application.ErrApplicationNotFound
	}
	return nil
}

// --- 应用成员 ---

const appMemberColumns = `id, application_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

// scanAppMemberWithUser 扫描成员 + join 的用户/角色展示字段。
func scanAppMemberWithUser(row pgx.Row) (*application.Member, error) {
	m := &application.Member{}
	var (
		invitedBy *int64
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&m.ID, &m.ApplicationID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status,
		&m.Version, &m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
		&m.UserName, &m.DisplayName, &m.Email, &m.RoleName,
	); err != nil {
		return nil, err
	}
	if invitedBy != nil {
		m.InvitedBy = *invitedBy
	}
	if createdBy != nil {
		m.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		m.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		m.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		m.DeletedBy = *deletedBy
	}
	return m, nil
}

func scanAppMember(row pgx.Row) (*application.Member, error) {
	m := &application.Member{}
	var (
		invitedBy *int64
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&m.ID, &m.ApplicationID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status,
		&m.Version, &m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if invitedBy != nil {
		m.InvitedBy = *invitedBy
	}
	if createdBy != nil {
		m.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		m.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		m.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		m.DeletedBy = *deletedBy
	}
	return m, nil
}

// AddAppMember 添加应用成员。
func (r *Repository) AddAppMember(ctx context.Context, m *application.Member) error {
	if m.JoinedAt.IsZero() {
		m.JoinedAt = r.now()
	}
	if m.Status == "" {
		m.Status = application.MemberStatusActive
	}
	const q = `INSERT INTO vo_application_members
		(application_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		m.ApplicationID, m.UserID, m.RoleID, nullableInt64(m.InvitedBy), m.JoinedAt, m.Status,
		1, m.JoinedAt, nullableInt64(m.CreatedBy), m.JoinedAt, nullableInt64(m.CreatedBy),
	).Scan(&m.ID, &m.Version, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return application.ErrAppMemberExists
		}
		return fmt.Errorf("insert app member: %w", err)
	}
	return nil
}

// GetAppMember 查询应用成员。
func (r *Repository) GetAppMember(ctx context.Context, applicationID, userID int64) (*application.Member, error) {
	q := `SELECT ` + appMemberColumns + ` FROM vo_application_members WHERE application_id=$1 AND user_id=$2 AND deleted=false`
	m, err := scanAppMember(r.pool.QueryRow(ctx, q, applicationID, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrAppMemberNotFound
		}
		return nil, err
	}
	return m, nil
}

// ListAppMembers 分页查询应用成员（join vo_users / vo_roles 填充展示字段）。
func (r *Repository) ListAppMembers(ctx context.Context, applicationID int64, offset, limit int) ([]*application.Member, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_application_members WHERE application_id=$1 AND deleted=false`, applicationID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count app members: %w", err)
	}
	const q = `SELECT m.id, m.application_id, m.user_id, m.role_id, m.invited_by, m.joined_at, m.status,
		m.version, m.created_at, m.created_by, m.updated_at, m.updated_by, m.deleted, m.deleted_at, m.deleted_by,
		COALESCE(u.username, ''), COALESCE(u.display_name, ''), COALESCE(u.email, ''), COALESCE(r.name, '')
		FROM vo_application_members m
		LEFT JOIN vo_users u ON u.id = m.user_id
		LEFT JOIN vo_roles r ON r.id = m.role_id
		WHERE m.application_id=$1 AND m.deleted=false
		ORDER BY m.joined_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, applicationID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query app members: %w", err)
	}
	defer rows.Close()
	var items []*application.Member
	for rows.Next() {
		m, err := scanAppMemberWithUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

// UpdateAppMemberRole 更新应用成员角色。
func (r *Repository) UpdateAppMemberRole(ctx context.Context, applicationID, userID, roleID, updatedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_application_members SET role_id=$1, updated_at=$2, updated_by=$3, version=version+1
		 WHERE application_id=$4 AND user_id=$5 AND deleted=false`,
		roleID, r.now(), nullableInt64(updatedBy), applicationID, userID)
	if err != nil {
		return fmt.Errorf("update app member role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return application.ErrAppMemberNotFound
	}
	return nil
}

// RemoveAppMember 软删除应用成员。
func (r *Repository) RemoveAppMember(ctx context.Context, applicationID, userID, removedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_application_members SET deleted=true, deleted_at=$1, deleted_by=$2, status='removed', updated_at=$3, version=version+1
		 WHERE application_id=$4 AND user_id=$5 AND deleted=false`,
		r.now(), nullableInt64(removedBy), r.now(), applicationID, userID)
	if err != nil {
		return fmt.Errorf("remove app member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return application.ErrAppMemberNotFound
	}
	return nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
