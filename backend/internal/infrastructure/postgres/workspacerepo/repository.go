// Package workspacerepo 是空间领域的 PostgreSQL 仓储实现。
package workspacerepo

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
	"github.com/vortexops/vortexops/internal/domain/workspace"
)

const pgUniqueViolation = "23505"

// Repository 空间仓储的 PostgreSQL 实现。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建空间仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const wsColumns = `id, uuid, name, display_name, description, logo_url, status, ws_type, owner_id,
	default_registry_id, default_jenkins_id, labels, metadata, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanWorkspace(row pgx.Row) (*workspace.Workspace, error) {
	w := &workspace.Workspace{Labels: map[string]string{}, Metadata: map[string]any{}}
	var (
		displayName       *string
		description       *string
		logoURL           *string
		defaultRegistryID *int64
		defaultJenkinsID  *int64
		labels            []byte
		metadata          []byte
		deletedAt         *time.Time
		deletedBy         *int64
		createdBy         *int64
		updatedBy         *int64
		wsType            *string
	)
	if err := row.Scan(
		&w.ID, &w.UUID, &w.Name, &displayName, &description, &logoURL, &w.Status, &wsType, &w.OwnerID,
		&defaultRegistryID, &defaultJenkinsID, &labels, &metadata, &w.Version, &w.CreatedAt, &createdBy,
		&w.UpdatedAt, &updatedBy, &w.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if wsType != nil {
		w.Type = workspace.Type(*wsType)
	}
	if displayName != nil {
		w.DisplayName = *displayName
	}
	if description != nil {
		w.Description = *description
	}
	if logoURL != nil {
		w.LogoURL = *logoURL
	}
	if defaultRegistryID != nil {
		w.DefaultRegistryID = *defaultRegistryID
	}
	if defaultJenkinsID != nil {
		w.DefaultJenkinsID = *defaultJenkinsID
	}
	if labels != nil {
		_ = json.Unmarshal(labels, &w.Labels)
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &w.Metadata)
	}
	if deletedAt != nil {
		w.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		w.DeletedBy = *deletedBy
	}
	if createdBy != nil {
		w.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		w.UpdatedBy = *updatedBy
	}
	return w, nil
}

// Create 创建空间及其默认配额（单事务）。
func (r *Repository) Create(ctx context.Context, w *workspace.Workspace, quota *workspace.Quota) error {
	if w.UUID == uuid.Nil {
		w.UUID = uuid.New()
	}
	now := r.now()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
		w.UpdatedAt = now
	}
	if w.Status == "" {
		w.Status = workspace.StatusActive
	}
	if w.Labels == nil {
		w.Labels = map[string]string{}
	}
	if w.Metadata == nil {
		w.Metadata = map[string]any{}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	labels, _ := json.Marshal(w.Labels)
	metadata, _ := json.Marshal(w.Metadata)
	wsType := w.Type
	if wsType == "" {
		wsType = workspace.TypeApp
	}
	const q = `INSERT INTO vo_workspaces
		(uuid, name, display_name, description, logo_url, status, ws_type, owner_id,
		 default_registry_id, default_jenkins_id, labels, metadata, version,
		 created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, created_at, updated_at, version`
	err = tx.QueryRow(ctx, q,
		w.UUID, w.Name, nullableStr(w.DisplayName), nullableStr(w.Description), nullableStr(w.LogoURL),
		w.Status, wsType, w.OwnerID, nullableInt64(w.DefaultRegistryID), nullableInt64(w.DefaultJenkinsID),
		labels, metadata, w.Version, w.CreatedAt, nullableInt64(w.CreatedBy), w.UpdatedAt, nullableInt64(w.UpdatedBy),
	).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt, &w.Version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return workspace.ErrWorkspaceNameExists
		}
		return fmt.Errorf("insert workspace: %w", err)
	}

	// 创建默认配额行。
	if quota == nil {
		quota = defaultQuota()
	}
	const qQuota = `INSERT INTO vo_workspace_quotas
		(workspace_id, max_applications, max_groups, max_concurrent_builds, max_images_retained, max_members, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	_, err = tx.Exec(ctx, qQuota,
		w.ID, quota.MaxApplications, quota.MaxGroups, quota.MaxConcurrentBuilds,
		quota.MaxImagesRetained, quota.MaxMembers, 1, now, nullableInt64(w.CreatedBy), now, nullableInt64(w.CreatedBy))
	if err != nil {
		return fmt.Errorf("insert workspace quota: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func defaultQuota() *workspace.Quota {
	return &workspace.Quota{
		MaxApplications:     50,
		MaxGroups:           200,
		MaxConcurrentBuilds: 10,
		MaxImagesRetained:   100,
		MaxMembers:          100,
	}
}

// GetByID 按 ID 查询。
func (r *Repository) GetByID(ctx context.Context, id int64) (*workspace.Workspace, error) {
	q := `SELECT ` + wsColumns + ` FROM vo_workspaces WHERE id = $1 AND deleted = false`
	w, err := scanWorkspace(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return w, nil
}

// GetByUUID 按 UUID 查询。
func (r *Repository) GetByUUID(ctx context.Context, id uuid.UUID) (*workspace.Workspace, error) {
	q := `SELECT ` + wsColumns + ` FROM vo_workspaces WHERE uuid = $1 AND deleted = false`
	w, err := scanWorkspace(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return w, nil
}

// GetByName 按名称查询。
func (r *Repository) GetByName(ctx context.Context, name string) (*workspace.Workspace, error) {
	q := `SELECT ` + wsColumns + ` FROM vo_workspaces WHERE name = $1 AND deleted = false`
	w, err := scanWorkspace(r.pool.QueryRow(ctx, q, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return w, nil
}

// GetByTypeAndName 按类型 + 名称查询（用于自动建专用工作空间）。
func (r *Repository) GetByTypeAndName(ctx context.Context, t workspace.Type, name string) (*workspace.Workspace, error) {
	q := `SELECT ` + wsColumns + ` FROM vo_workspaces WHERE ws_type = $1 AND name = $2 AND deleted = false`
	w, err := scanWorkspace(r.pool.QueryRow(ctx, q, t, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return w, nil
}

// Update 更新空间（乐观锁）。
func (r *Repository) Update(ctx context.Context, in workspace.UpdateInput) (*workspace.Workspace, error) {
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
	if in.LogoURL != nil {
		addSet("logo_url", nullableStr(*in.LogoURL))
	}
	if in.Status != nil {
		addSet("status", *in.Status)
	}
	if in.DefaultRegistryID != nil {
		addSet("default_registry_id", nullableInt64(*in.DefaultRegistryID))
	}
	if in.DefaultJenkinsID != nil {
		addSet("default_jenkins_id", nullableInt64(*in.DefaultJenkinsID))
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
		return r.GetByID(ctx, in.ID)
	}
	addSet("updated_at", now)
	addSet("updated_by", nullableInt64(in.UpdatedBy))
	addSet("version", in.Version+1)

	args = append(args, in.ID, in.Version)
	q := fmt.Sprintf(`UPDATE vo_workspaces SET %s WHERE id = $%d AND version = $%d AND deleted = false`,
		strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 区分未找到 vs 版本冲突。
		existing, gerr := r.GetByID(ctx, in.ID)
		if gerr != nil {
			return nil, workspace.ErrWorkspaceNotFound
		}
		if existing.Version != in.Version {
			return nil, domain.ErrConflict
		}
		return nil, workspace.ErrWorkspaceNotFound
	}
	return r.GetByID(ctx, in.ID)
}

// List 分页查询空间。
func (r *Repository) List(ctx context.Context, q workspace.Query) ([]*workspace.Workspace, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	conds = append(conds, "deleted = false")
	if q.OwnerID != 0 {
		conds = append(conds, fmt.Sprintf("owner_id = $%d", argIdx))
		args = append(args, q.OwnerID)
		argIdx++
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, q.Status)
		argIdx++
	}
	if q.Type != "" {
		conds = append(conds, fmt.Sprintf("ws_type = $%d", argIdx))
		args = append(args, q.Type)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR display_name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_workspaces WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count workspaces: %w", err)
	}

	listQ := fmt.Sprintf("SELECT %s FROM vo_workspaces WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		wsColumns, where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query workspaces: %w", err)
	}
	defer rows.Close()

	var items []*workspace.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, w)
	}
	return items, total, rows.Err()
}

// Delete 软删除空间。
func (r *Repository) Delete(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspaces SET deleted=true, deleted_at=$1, deleted_by=$2, status='archived', updated_at=$3, version=version+1 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return workspace.ErrWorkspaceNotFound
	}
	return nil
}

// GetQuota 读取空间配额。
func (r *Repository) GetQuota(ctx context.Context, workspaceID int64) (*workspace.Quota, error) {
	q := &workspace.Quota{}
	err := r.pool.QueryRow(ctx,
		`SELECT max_applications, max_groups, max_concurrent_builds, max_images_retained, max_members, version
		 FROM vo_workspace_quotas WHERE workspace_id = $1 AND deleted = false`, workspaceID,
	).Scan(&q.MaxApplications, &q.MaxGroups, &q.MaxConcurrentBuilds, &q.MaxImagesRetained, &q.MaxMembers, new(int))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrWorkspaceNotFound
		}
		return nil, err
	}
	return q, nil
}

// UpdateQuota 更新配额（乐观锁）。
func (r *Repository) UpdateQuota(ctx context.Context, workspaceID int64, q workspace.Quota, version int, updatedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_quotas SET max_applications=$1, max_groups=$2, max_concurrent_builds=$3,
		 max_images_retained=$4, max_members=$5, version=version+1, updated_at=$6, updated_by=$7
		 WHERE workspace_id=$8 AND version=$9 AND deleted=false`,
		q.MaxApplications, q.MaxGroups, q.MaxConcurrentBuilds, q.MaxImagesRetained, q.MaxMembers,
		r.now(), nullableInt64(updatedBy), workspaceID, version)
	if err != nil {
		return fmt.Errorf("update quota: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// --- 成员 ---

const memberColumns = `id, workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanMember(row pgx.Row) (*workspace.Member, error) {
	m := &workspace.Member{}
	var (
		invitedBy *int64
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status,
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

// scanMemberWithProfile 扫描成员行并附带用户/角色展示字段（用于 ListMembers JOIN 查询）。
func scanMemberWithProfile(row pgx.Row) (*workspace.Member, error) {
	m := &workspace.Member{}
	var (
		invitedBy *int64
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status,
		&m.Version, &m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
		&m.Username, &m.DisplayName, &m.AvatarURL, &m.RoleCode, &m.RoleName,
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

// AddMember 添加成员。已存在（未删除）返回 ErrMemberExists。
func (r *Repository) AddMember(ctx context.Context, m *workspace.Member) error {
	if m.JoinedAt.IsZero() {
		m.JoinedAt = r.now()
	}
	if m.Status == "" {
		m.Status = workspace.MemberStatusActive
	}
	const q = `INSERT INTO vo_workspace_members
		(workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		m.WorkspaceID, m.UserID, m.RoleID, nullableInt64(m.InvitedBy), m.JoinedAt, m.Status,
		1, m.JoinedAt, nullableInt64(m.CreatedBy), m.JoinedAt, nullableInt64(m.CreatedBy),
	).Scan(&m.ID, &m.Version, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return workspace.ErrMemberExists
		}
		return fmt.Errorf("insert member: %w", err)
	}
	return nil
}

// GetMember 查询单个成员。
func (r *Repository) GetMember(ctx context.Context, workspaceID, userID int64) (*workspace.Member, error) {
	q := `SELECT ` + memberColumns + ` FROM vo_workspace_members WHERE workspace_id=$1 AND user_id=$2 AND deleted=false`
	m, err := scanMember(r.pool.QueryRow(ctx, q, workspaceID, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, workspace.ErrMemberNotFound
		}
		return nil, err
	}
	return m, nil
}

// ListMembers 分页查询成员。
func (r *Repository) ListMembers(ctx context.Context, q workspace.MemberQuery) ([]*workspace.Member, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
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
	if q.UserID != 0 {
		conds = append(conds, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, q.UserID)
		argIdx++
	}
	if q.RoleID != 0 {
		conds = append(conds, fmt.Sprintf("role_id = $%d", argIdx))
		args = append(args, q.RoleID)
		argIdx++
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, q.Status)
		argIdx++
	}
	where := strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_workspace_members WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count members: %w", err)
	}

	listQ := fmt.Sprintf(`SELECT m.id, m.workspace_id, m.user_id, m.role_id, m.invited_by, m.joined_at, m.status, m.version,
		m.created_at, m.created_by, m.updated_at, m.updated_by, m.deleted, m.deleted_at, m.deleted_by,
		COALESCE(u.username,''), COALESCE(u.display_name,''), COALESCE(u.avatar_url,''),
		COALESCE(r.code,''), COALESCE(r.name,'')
		FROM vo_workspace_members m
		LEFT JOIN vo_users u ON u.id = m.user_id AND u.deleted_at IS NULL
		LEFT JOIN vo_roles r ON r.id = m.role_id
		WHERE %s ORDER BY m.joined_at DESC LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()
	var items []*workspace.Member
	for rows.Next() {
		m, err := scanMemberWithProfile(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

// UpdateMemberRole 更新成员角色。
func (r *Repository) UpdateMemberRole(ctx context.Context, workspaceID, userID, roleID, updatedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_members SET role_id=$1, updated_at=$2, updated_by=$3, version=version+1
		 WHERE workspace_id=$4 AND user_id=$5 AND deleted=false`,
		roleID, r.now(), nullableInt64(updatedBy), workspaceID, userID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return workspace.ErrMemberNotFound
	}
	return nil
}

// RemoveMember 软删除成员。
func (r *Repository) RemoveMember(ctx context.Context, workspaceID, userID, removedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_members SET deleted=true, deleted_at=$1, deleted_by=$2, status='removed', updated_at=$3, version=version+1
		 WHERE workspace_id=$4 AND user_id=$5 AND deleted=false`,
		r.now(), nullableInt64(removedBy), r.now(), workspaceID, userID)
	if err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return workspace.ErrMemberNotFound
	}
	return nil
}

// CountMembers 统计空间成员数（未删除）。
func (r *Repository) CountMembers(ctx context.Context, workspaceID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_workspace_members WHERE workspace_id=$1 AND deleted=false`, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}
	return n, nil
}

// --- 集群绑定 ---

const bindingColumns = `id, uuid, workspace_id, cluster_id, namespace, role, auto_create_namespace, resource_quota, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanBinding(row pgx.Row) (*workspace.ClusterBinding, error) {
	b := &workspace.ClusterBinding{ResourceQuota: map[string]any{}}
	var (
		resourceQuota []byte
		createdBy     *int64
		updatedBy     *int64
		deletedAt     *time.Time
		deletedBy     *int64
	)
	if err := row.Scan(
		&b.ID, &b.UUID, &b.WorkspaceID, &b.ClusterID, &b.Namespace, &b.Role, &b.AutoCreateNS,
		&resourceQuota, &b.Version, &b.CreatedAt, &createdBy, &b.UpdatedAt, &updatedBy, &b.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if resourceQuota != nil {
		_ = json.Unmarshal(resourceQuota, &b.ResourceQuota)
	}
	if createdBy != nil {
		b.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		b.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		b.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		b.DeletedBy = *deletedBy
	}
	return b, nil
}

// AddClusterBinding 添加集群绑定。
func (r *Repository) AddClusterBinding(ctx context.Context, b *workspace.ClusterBinding) error {
	if b.UUID == uuid.Nil {
		b.UUID = uuid.New()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = r.now()
		b.UpdatedAt = b.CreatedAt
	}
	if b.Role == "" {
		b.Role = workspace.ClusterRolePrimary
	}
	rq, _ := json.Marshal(b.ResourceQuota)
	const q = `INSERT INTO vo_workspace_clusters
		(uuid, workspace_id, cluster_id, namespace, role, auto_create_namespace, resource_quota, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		b.UUID, b.WorkspaceID, b.ClusterID, b.Namespace, b.Role, b.AutoCreateNS, rq,
		1, b.CreatedAt, nullableInt64(b.CreatedBy), b.UpdatedAt, nullableInt64(b.CreatedBy),
	).Scan(&b.ID, &b.Version, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("insert cluster binding: %w", err)
	}
	return nil
}

// ListClusterBindings 列出空间绑定的集群。
func (r *Repository) ListClusterBindings(ctx context.Context, workspaceID int64) ([]*workspace.ClusterBinding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+bindingColumns+` FROM vo_workspace_clusters WHERE workspace_id=$1 AND deleted=false ORDER BY created_at ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query cluster bindings: %w", err)
	}
	defer rows.Close()
	var items []*workspace.ClusterBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// RemoveClusterBinding 解绑集群。
func (r *Repository) RemoveClusterBinding(ctx context.Context, workspaceID, clusterID, removedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_clusters SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE workspace_id=$4 AND cluster_id=$5 AND deleted=false`,
		r.now(), nullableInt64(removedBy), r.now(), workspaceID, clusterID)
	if err != nil {
		return fmt.Errorf("remove cluster binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return workspace.ErrClusterBindingNotFound
	}
	return nil
}

// CountApplications 统计空间下应用数（用于配额校验）。
func (r *Repository) CountApplications(ctx context.Context, workspaceID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_applications WHERE workspace_id=$1 AND deleted=false`, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count applications: %w", err)
	}
	return n, nil
}

// CountGroups 统计空间下分组数（用于配额校验）。
func (r *Repository) CountGroups(ctx context.Context, workspaceID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_groups g JOIN vo_applications a ON a.id=g.application_id
		 WHERE a.workspace_id=$1 AND g.deleted=false AND a.deleted=false`, workspaceID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count groups: %w", err)
	}
	return n, nil
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
