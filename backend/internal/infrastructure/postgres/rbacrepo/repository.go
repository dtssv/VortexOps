// Package rbacrepo 是权限领域的 PostgreSQL 仓储实现。
package rbacrepo

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
	"github.com/vortexops/vortexops/internal/domain/rbac"
)

const pgUniqueViolation = "23505"

// Repository 权限领域 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

// --- 权限 ---

const permColumns = `id, code, name, category, scope, description, sort_order, enabled, version, created_at,
	created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanPermission(row pgx.Row) (*rbac.Permission, error) {
	p := &rbac.Permission{}
	var (
		desc      *string
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&p.ID, &p.Code, &p.Name, &p.Category, &p.Scope, &desc, &p.SortOrder, &p.Enabled, &p.Version,
		&p.CreatedAt, &createdBy, &p.UpdatedAt, &updatedBy, &p.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		p.Description = *desc
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		p.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		p.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		p.DeletedBy = *deletedBy
	}
	return p, nil
}

// CreatePermission 创建权限。
func (r *Repository) CreatePermission(ctx context.Context, p *rbac.Permission) error {
	now := r.now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
		p.UpdatedAt = now
	}
	const q = `INSERT INTO vo_permissions (code, name, category, scope, description, sort_order, enabled, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.Code, p.Name, p.Category, p.Scope, nullableStr(p.Description), p.SortOrder, p.Enabled, p.Version,
		p.CreatedAt, nullableInt64(p.CreatedBy), p.UpdatedAt, nullableInt64(p.CreatedBy),
	).Scan(&p.ID, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return rbac.ErrPermissionCodeExists
		}
		return fmt.Errorf("insert permission: %w", err)
	}
	return nil
}

// GetPermissionByID 按 ID 查询权限。
func (r *Repository) GetPermissionByID(ctx context.Context, id int64) (*rbac.Permission, error) {
	q := `SELECT ` + permColumns + ` FROM vo_permissions WHERE id=$1 AND deleted=false`
	p, err := scanPermission(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrPermissionNotFound
		}
		return nil, err
	}
	return p, nil
}

// GetPermissionByCode 按 code 查询权限。
func (r *Repository) GetPermissionByCode(ctx context.Context, code string) (*rbac.Permission, error) {
	q := `SELECT ` + permColumns + ` FROM vo_permissions WHERE code=$1 AND deleted=false`
	p, err := scanPermission(r.pool.QueryRow(ctx, q, code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrPermissionNotFound
		}
		return nil, err
	}
	return p, nil
}

// ListPermissions 分页查询权限。
func (r *Repository) ListPermissions(ctx context.Context, q rbac.PermissionQuery) ([]*rbac.Permission, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if q.Category != "" {
		conds = append(conds, fmt.Sprintf("category = $%d", len(args)+1))
		args = append(args, q.Category)
	}
	if q.Scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, q.Scope)
	}
	if q.Enabled != nil {
		conds = append(conds, fmt.Sprintf("enabled = $%d", len(args)+1))
		args = append(args, *q.Enabled)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_permissions WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count permissions: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_permissions WHERE %s ORDER BY sort_order ASC, id ASC LIMIT $%d OFFSET $%d",
		permColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Permission
	for rows.Next() {
		p, err := scanPermission(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	return items, total, rows.Err()
}

// UpdatePermission 更新权限。
func (r *Repository) UpdatePermission(ctx context.Context, p *rbac.Permission) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_permissions SET name=$1, description=$2, sort_order=$3, enabled=$4, updated_at=$5, updated_by=$6, version=version+1
		 WHERE id=$7 AND version=$8 AND deleted=false`,
		p.Name, nullableStr(p.Description), p.SortOrder, p.Enabled, now, nullableInt64(p.UpdatedBy), p.ID, p.Version)
	if err != nil {
		return fmt.Errorf("update permission: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeletePermission 软删除权限。
func (r *Repository) DeletePermission(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_permissions SET deleted=true, deleted_at=$1, deleted_by=$2, enabled=false, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete permission: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return rbac.ErrPermissionNotFound
	}
	return nil
}

// --- 菜单 ---

const menuColumns = `id, uuid, parent_id, code, name, name_en, path, icon, component, menu_type, scope,
	permission_code, visible, sort_order, keep_alive, external_link, metadata, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanMenu(row pgx.Row) (*rbac.Menu, error) {
	m := &rbac.Menu{Metadata: map[string]any{}}
	var (
		parentID       *int64
		nameEN         *string
		path           *string
		icon           *string
		component      *string
		permissionCode *string
		externalLink   *string
		metadata       []byte
		createdBy      *int64
		updatedBy      *int64
		deletedAt      *time.Time
		deletedBy      *int64
	)
	if err := row.Scan(
		&m.ID, &m.UUID, &parentID, &m.Code, &m.Name, &nameEN, &path, &icon, &component, &m.MenuType, &m.Scope,
		&permissionCode, &m.Visible, &m.SortOrder, &m.KeepAlive, &externalLink, &metadata, &m.Version,
		&m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if parentID != nil {
		m.ParentID = *parentID
	}
	if nameEN != nil {
		m.NameEN = *nameEN
	}
	if path != nil {
		m.Path = *path
	}
	if icon != nil {
		m.Icon = *icon
	}
	if component != nil {
		m.Component = *component
	}
	if permissionCode != nil {
		m.PermissionCode = *permissionCode
	}
	if externalLink != nil {
		m.ExternalLink = *externalLink
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &m.Metadata)
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

// CreateMenu 创建菜单。
func (r *Repository) CreateMenu(ctx context.Context, m *rbac.Menu) error {
	if m.UUID == uuid.Nil {
		m.UUID = uuid.New()
	}
	now := r.now()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
		m.UpdatedAt = now
	}
	if m.MenuType == "" {
		m.MenuType = rbac.MenuTypeMenu
	}
	if m.Metadata == nil {
		m.Metadata = map[string]any{}
	}
	metadataBytes, _ := json.Marshal(m.Metadata)
	const q = `INSERT INTO vo_menus (uuid, parent_id, code, name, name_en, path, icon, component, menu_type, scope,
		permission_code, visible, sort_order, keep_alive, external_link, metadata, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		m.UUID, nullableInt64(m.ParentID), m.Code, m.Name, nullableStr(m.NameEN), nullableStr(m.Path),
		nullableStr(m.Icon), nullableStr(m.Component), m.MenuType, m.Scope, nullableStr(m.PermissionCode),
		m.Visible, m.SortOrder, m.KeepAlive, nullableStr(m.ExternalLink), metadataBytes, m.Version,
		m.CreatedAt, nullableInt64(m.CreatedBy), m.UpdatedAt, nullableInt64(m.CreatedBy),
	).Scan(&m.ID, &m.Version, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return rbac.ErrMenuCodeExists
		}
		return fmt.Errorf("insert menu: %w", err)
	}
	return nil
}

// GetMenuByID 按 ID 查询菜单。
func (r *Repository) GetMenuByID(ctx context.Context, id int64) (*rbac.Menu, error) {
	q := `SELECT ` + menuColumns + ` FROM vo_menus WHERE id=$1 AND deleted=false`
	m, err := scanMenu(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrMenuNotFound
		}
		return nil, err
	}
	return m, nil
}

// GetMenuByCode 按 code 查询菜单。
func (r *Repository) GetMenuByCode(ctx context.Context, code string) (*rbac.Menu, error) {
	q := `SELECT ` + menuColumns + ` FROM vo_menus WHERE code=$1 AND deleted=false`
	m, err := scanMenu(r.pool.QueryRow(ctx, q, code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrMenuNotFound
		}
		return nil, err
	}
	return m, nil
}

// ListMenus 列出菜单（按 scope 过滤，按 sort_order 排序）。
func (r *Repository) ListMenus(ctx context.Context, scope rbac.PermissionScope) ([]*rbac.Menu, error) {
	q := `SELECT ` + menuColumns + ` FROM vo_menus WHERE deleted=false`
	args := []any{}
	if scope != "" {
		q += ` AND scope=$1`
		args = append(args, scope)
	}
	q += ` ORDER BY sort_order ASC, id ASC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query menus: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Menu
	for rows.Next() {
		m, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// UpdateMenu 更新菜单。
func (r *Repository) UpdateMenu(ctx context.Context, m *rbac.Menu) error {
	now := r.now()
	metadataBytes, _ := json.Marshal(m.Metadata)
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_menus SET parent_id=$1, name=$2, name_en=$3, path=$4, icon=$5, component=$6, menu_type=$7,
		 permission_code=$8, visible=$9, sort_order=$10, keep_alive=$11, external_link=$12, metadata=$13,
		 updated_at=$14, updated_by=$15, version=version+1
		 WHERE id=$16 AND version=$17 AND deleted=false`,
		nullableInt64(m.ParentID), m.Name, nullableStr(m.NameEN), nullableStr(m.Path), nullableStr(m.Icon),
		nullableStr(m.Component), m.MenuType, nullableStr(m.PermissionCode), m.Visible, m.SortOrder, m.KeepAlive,
		nullableStr(m.ExternalLink), metadataBytes, now, nullableInt64(m.UpdatedBy), m.ID, m.Version)
	if err != nil {
		return fmt.Errorf("update menu: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteMenu 软删除菜单。
func (r *Repository) DeleteMenu(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_menus SET deleted=true, deleted_at=$1, deleted_by=$2, visible=false, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete menu: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return rbac.ErrMenuNotFound
	}
	return nil
}

// --- 角色 ---

const roleColumns = `id, uuid, scope, scope_id, code, name, description, is_builtin, is_default, enabled, metadata,
	version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanRole(row pgx.Row) (*rbac.Role, error) {
	role := &rbac.Role{Metadata: map[string]any{}}
	var (
		desc      *string
		metadata  []byte
		scopeID   *int64
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&role.ID, &role.UUID, &role.Scope, &scopeID, &role.Code, &role.Name, &desc, &role.IsBuiltin,
		&role.IsDefault, &role.Enabled, &metadata, &role.Version, &role.CreatedAt, &createdBy, &role.UpdatedAt,
		&updatedBy, &role.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		role.Description = *desc
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &role.Metadata)
	}
	if scopeID != nil {
		role.ScopeID = *scopeID
	}
	if createdBy != nil {
		role.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		role.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		role.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		role.DeletedBy = *deletedBy
	}
	return role, nil
}

// CreateRole 创建角色。
func (r *Repository) CreateRole(ctx context.Context, role *rbac.Role) error {
	if role.UUID == uuid.Nil {
		role.UUID = uuid.New()
	}
	now := r.now()
	if role.CreatedAt.IsZero() {
		role.CreatedAt = now
		role.UpdatedAt = now
	}
	if role.Metadata == nil {
		role.Metadata = map[string]any{}
	}
	metadataBytes, _ := json.Marshal(role.Metadata)
	const q = `INSERT INTO vo_roles (uuid, scope, scope_id, code, name, description, is_builtin, is_default, enabled, metadata, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		role.UUID, role.Scope, nullableInt64(role.ScopeID), role.Code, role.Name, nullableStr(role.Description),
		role.IsBuiltin, role.IsDefault, role.Enabled, metadataBytes, role.Version,
		role.CreatedAt, nullableInt64(role.CreatedBy), role.UpdatedAt, nullableInt64(role.CreatedBy),
	).Scan(&role.ID, &role.Version, &role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return rbac.ErrRoleCodeExists
		}
		return fmt.Errorf("insert role: %w", err)
	}
	return nil
}

// GetRoleByID 按 ID 查询角色。
func (r *Repository) GetRoleByID(ctx context.Context, id int64) (*rbac.Role, error) {
	q := `SELECT ` + roleColumns + ` FROM vo_roles WHERE id=$1 AND deleted=false`
	role, err := scanRole(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

// GetRoleByCode 按 scope+scope_id+code 查询角色。
func (r *Repository) GetRoleByCode(ctx context.Context, scope rbac.RoleScope, scopeID int64, code string) (*rbac.Role, error) {
	q := `SELECT ` + roleColumns + ` FROM vo_roles WHERE scope=$1 AND COALESCE(scope_id,0)=COALESCE($2,0) AND code=$3 AND deleted=false`
	role, err := scanRole(r.pool.QueryRow(ctx, q, scope, nullableInt64(scopeID), code))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

// ListRoles 分页查询角色。
func (r *Repository) ListRoles(ctx context.Context, q rbac.RoleQuery) ([]*rbac.Role, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if q.Scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, q.Scope)
	}
	if q.ScopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, q.ScopeID)
	}
	if q.Enabled != nil {
		conds = append(conds, fmt.Sprintf("enabled = $%d", len(args)+1))
		args = append(args, *q.Enabled)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_roles WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_roles WHERE %s ORDER BY is_builtin DESC, name ASC LIMIT $%d OFFSET $%d",
		roleColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, role)
	}
	return items, total, rows.Err()
}

// UpdateRole 更新角色。
func (r *Repository) UpdateRole(ctx context.Context, role *rbac.Role) error {
	now := r.now()
	metadataBytes, _ := json.Marshal(role.Metadata)
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_roles SET name=$1, description=$2, is_default=$3, enabled=$4, metadata=$5, updated_at=$6, updated_by=$7, version=version+1
		 WHERE id=$8 AND version=$9 AND deleted=false`,
		role.Name, nullableStr(role.Description), role.IsDefault, role.Enabled, metadataBytes,
		now, nullableInt64(role.UpdatedBy), role.ID, role.Version)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteRole 软删除角色（内置角色不可删）。
func (r *Repository) DeleteRole(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_roles SET deleted=true, deleted_at=$1, deleted_by=$2, enabled=false, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false AND is_builtin=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 可能是内置角色或不存在。
		role, gerr := r.GetRoleByID(ctx, id)
		if gerr != nil {
			return rbac.ErrRoleNotFound
		}
		if role.IsBuiltin {
			return rbac.ErrRoleBuiltin
		}
		return rbac.ErrRoleNotFound
	}
	return nil
}

// --- 角色-权限 ---

// GrantPermissions 批量授予角色权限（upsert）。
func (r *Repository) GrantPermissions(ctx context.Context, roleID int64, permIDs []int64, granted bool, actorID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, pid := range permIDs {
		_, err := tx.Exec(ctx,
			`INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by, created_at)
			 VALUES ($1,$2,$3,$4,now())
			 ON CONFLICT (role_id, permission_id) DO UPDATE SET granted=$3, created_by=$4`,
			roleID, pid, granted, nullableInt64(actorID))
		if err != nil {
			return fmt.Errorf("upsert role permission: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// RevokePermissions 批量撤销角色权限。
func (r *Repository) RevokePermissions(ctx context.Context, roleID int64, permIDs []int64) error {
	if len(permIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`DELETE FROM vo_role_permissions WHERE role_id=$1 AND permission_id = ANY($2)`,
		roleID, permIDs)
	if err != nil {
		return fmt.Errorf("delete role permissions: %w", err)
	}
	return nil
}

// ListPermissionsByRole 列出角色的权限。
func (r *Repository) ListPermissionsByRole(ctx context.Context, roleID int64) ([]*rbac.Permission, error) {
	q := `SELECT ` + permColumns + ` FROM vo_permissions p
		JOIN vo_role_permissions rp ON rp.permission_id = p.id
		WHERE rp.role_id=$1 AND rp.granted=true AND p.deleted=false
		ORDER BY p.sort_order ASC, p.id ASC`
	rows, err := r.pool.Query(ctx, q, roleID)
	if err != nil {
		return nil, fmt.Errorf("query permissions by role: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Permission
	for rows.Next() {
		p, err := scanPermission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// ListPermissionCodesByRole 列出角色的权限 code。
func (r *Repository) ListPermissionCodesByRole(ctx context.Context, roleID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.code FROM vo_permissions p
		 JOIN vo_role_permissions rp ON rp.permission_id = p.id
		 WHERE rp.role_id=$1 AND rp.granted=true AND p.deleted=false AND p.enabled=true`, roleID)
	if err != nil {
		return nil, fmt.Errorf("query permission codes: %w", err)
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// --- 平台角色绑定 ---

// BindPlatformRole 绑定平台角色。
func (r *Repository) BindPlatformRole(ctx context.Context, userID, roleID int64, expiresAt *time.Time, actorID int64) error {
	now := r.now()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO vo_platform_role_bindings (user_id, role_id, expires_at, version, created_at, created_by, updated_at, updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (user_id, role_id) DO UPDATE SET expires_at=$3, updated_at=$7, updated_by=$6, version=vo_platform_role_bindings.version+1`,
		userID, roleID, expiresAt, 1, now, nullableInt64(actorID), now, nullableInt64(actorID))
	if err != nil {
		return fmt.Errorf("upsert platform role binding: %w", err)
	}
	return nil
}

// UnbindPlatformRole 解绑平台角色。
func (r *Repository) UnbindPlatformRole(ctx context.Context, userID, roleID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_platform_role_bindings SET deleted=true, deleted_at=now(), version=version+1
		 WHERE user_id=$1 AND role_id=$2 AND deleted=false`,
		userID, roleID)
	if err != nil {
		return fmt.Errorf("delete platform role binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return rbac.ErrBindingNotFound
	}
	return nil
}

// ListPlatformRolesByUser 列出用户的平台角色。
func (r *Repository) ListPlatformRolesByUser(ctx context.Context, userID int64) ([]*rbac.Role, error) {
	q := `SELECT ` + roleColumns + ` FROM vo_roles r
		JOIN vo_platform_role_bindings b ON b.role_id = r.id
		WHERE b.user_id=$1 AND b.deleted=false AND r.deleted=false AND r.enabled=true
		  AND (b.expires_at IS NULL OR b.expires_at > now())`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("query platform roles by user: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, role)
	}
	return items, rows.Err()
}

// --- workspace 成员 ---

// AddWorkspaceMember 添加 workspace 成员。
func (r *Repository) AddWorkspaceMember(ctx context.Context, m *rbac.WorkspaceMember) error {
	now := r.now()
	if m.JoinedAt.IsZero() {
		m.JoinedAt = now
	}
	if m.Status == "" {
		m.Status = rbac.MemberActive
	}
	const q = `INSERT INTO vo_workspace_members (workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		m.WorkspaceID, m.UserID, m.RoleID, nullableInt64(m.InvitedBy), m.JoinedAt, m.Status, m.Version,
		now, nullableInt64(m.CreatedBy), now, nullableInt64(m.CreatedBy),
	).Scan(&m.ID, &m.Version, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return rbac.ErrMemberExists
		}
		return fmt.Errorf("insert workspace member: %w", err)
	}
	return nil
}

// UpdateWorkspaceMember 更新成员（角色/状态）。
func (r *Repository) UpdateWorkspaceMember(ctx context.Context, m *rbac.WorkspaceMember) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_members SET role_id=$1, status=$2, updated_at=now(), updated_by=$3, version=version+1
		 WHERE id=$4 AND version=$5 AND deleted=false`,
		m.RoleID, m.Status, nullableInt64(m.UpdatedBy), m.ID, m.Version)
	if err != nil {
		return fmt.Errorf("update workspace member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// RemoveWorkspaceMember 软删除成员。
func (r *Repository) RemoveWorkspaceMember(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_workspace_members SET deleted=true, deleted_at=now(), deleted_by=$1, status='removed', version=version+1
		 WHERE id=$2 AND deleted=false`,
		nullableInt64(actorID), id)
	if err != nil {
		return fmt.Errorf("delete workspace member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return rbac.ErrMemberNotFound
	}
	return nil
}

// GetWorkspaceMember 查询成员。
func (r *Repository) GetWorkspaceMember(ctx context.Context, workspaceID, userID int64) (*rbac.WorkspaceMember, error) {
	q := `SELECT id, workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
		FROM vo_workspace_members WHERE workspace_id=$1 AND user_id=$2 AND deleted=false`
	m := &rbac.WorkspaceMember{}
	var (
		invitedBy  *int64
		createdBy  *int64
		updatedBy  *int64
		deletedAt  *time.Time
		deletedBy  *int64
	)
	if err := r.pool.QueryRow(ctx, q, workspaceID, userID).Scan(
		&m.ID, &m.WorkspaceID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status, &m.Version,
		&m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, rbac.ErrMemberNotFound
		}
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

// ListWorkspaceMembers 列出 workspace 成员。
func (r *Repository) ListWorkspaceMembers(ctx context.Context, workspaceID int64) ([]*rbac.WorkspaceMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
		 FROM vo_workspace_members WHERE workspace_id=$1 AND deleted=false ORDER BY joined_at ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("query workspace members: %w", err)
	}
	defer rows.Close()
	var items []*rbac.WorkspaceMember
	for rows.Next() {
		m := &rbac.WorkspaceMember{}
		var (
			invitedBy *int64
			createdBy *int64
			updatedBy *int64
			deletedAt *time.Time
			deletedBy *int64
		)
		if err := rows.Scan(
			&m.ID, &m.WorkspaceID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status, &m.Version,
			&m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
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
		items = append(items, m)
	}
	return items, rows.Err()
}

// ListWorkspaceRolesByUser 列出用户加入的所有 workspace 成员记录。
func (r *Repository) ListWorkspaceRolesByUser(ctx context.Context, userID int64) ([]*rbac.WorkspaceMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, workspace_id, user_id, role_id, invited_by, joined_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
		 FROM vo_workspace_members WHERE user_id=$1 AND deleted=false AND status='active' ORDER BY joined_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("query workspace roles by user: %w", err)
	}
	defer rows.Close()
	var items []*rbac.WorkspaceMember
	for rows.Next() {
		m := &rbac.WorkspaceMember{}
		var (
			invitedBy *int64
			createdBy *int64
			updatedBy *int64
			deletedAt *time.Time
			deletedBy *int64
		)
		if err := rows.Scan(
			&m.ID, &m.WorkspaceID, &m.UserID, &m.RoleID, &invitedBy, &m.JoinedAt, &m.Status, &m.Version,
			&m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy, &m.Deleted, &deletedAt, &deletedBy,
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
		items = append(items, m)
	}
	return items, rows.Err()
}

// --- 权限解析 ---

// ListRoleIDsForUser 聚合用户在平台 + 指定 workspace 维度的角色 ID。
func (r *Repository) ListRoleIDsForUser(ctx context.Context, userID int64, workspaceID int64) ([]int64, error) {
	// 平台角色。
	platformRows, err := r.pool.Query(ctx,
		`SELECT role_id FROM vo_platform_role_bindings
		 WHERE user_id=$1 AND deleted=false AND (expires_at IS NULL OR expires_at > now())`, userID)
	if err != nil {
		return nil, fmt.Errorf("query platform role ids: %w", err)
	}
	defer platformRows.Close()
	var roleIDs []int64
	for platformRows.Next() {
		var rid int64
		if err := platformRows.Scan(&rid); err != nil {
			return nil, err
		}
		roleIDs = append(roleIDs, rid)
	}
	// workspace 角色：指定空间则只取该空间；workspaceID=0 时聚合用户加入的全部空间。
	// 后者供 /me/menus、/me/permissions 使用，避免「只有空间角色的开发者侧栏空白」。
	var wsRows pgx.Rows
	var errWS error
	if workspaceID != 0 {
		wsRows, errWS = r.pool.Query(ctx,
			`SELECT role_id FROM vo_workspace_members
			 WHERE user_id=$1 AND workspace_id=$2 AND deleted=false AND status='active'`, userID, workspaceID)
	} else {
		wsRows, errWS = r.pool.Query(ctx,
			`SELECT DISTINCT role_id FROM vo_workspace_members
			 WHERE user_id=$1 AND deleted=false AND status='active'`, userID)
	}
	if errWS != nil {
		return nil, fmt.Errorf("query workspace role ids: %w", errWS)
	}
	defer wsRows.Close()
	for wsRows.Next() {
		var rid int64
		if err := wsRows.Scan(&rid); err != nil {
			return nil, err
		}
		roleIDs = append(roleIDs, rid)
	}
	return roleIDs, nil
}

// ListPermissionCodesByRoles 根据角色 ID 列表聚合权限 code（去重）。
func (r *Repository) ListPermissionCodesByRoles(ctx context.Context, roleIDs []int64) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT p.code FROM vo_permissions p
		 JOIN vo_role_permissions rp ON rp.permission_id = p.id
		 WHERE rp.role_id = ANY($1) AND rp.granted=true AND p.deleted=false AND p.enabled=true`, roleIDs)
	if err != nil {
		return nil, fmt.Errorf("query permission codes by roles: %w", err)
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ListVisibleMenus 根据权限 code 集合与角色 ID 集合返回可见菜单（OR 关系）。
// 可见条件（任一满足）：
//   - permission_code 为空（分组目录，对所有人可见）；
//   - menu 直接绑定到 roleIDs 中任一角色（vo_role_menus）；
//   - menu 的 permission_code 命中 permCodes（支持 '*' 与 'prefix:*' 通配）。
func (r *Repository) ListVisibleMenus(ctx context.Context, scope rbac.PermissionScope, permCodes []string, roleIDs []int64) ([]*rbac.Menu, error) {
	hasWildcard := false
	permSet := make(map[string]struct{}, len(permCodes))
	prefixWildcards := make([]string, 0, len(permCodes))
	for _, c := range permCodes {
		permSet[c] = struct{}{}
		if c == "*" {
			hasWildcard = true
		} else if strings.HasSuffix(c, ":*") {
			prefixWildcards = append(prefixWildcards, strings.TrimSuffix(c, "*"))
		}
	}
	matchPerm := func(code string) bool {
		if _, ok := permSet[code]; ok {
			return true
		}
		if hasWildcard {
			return true
		}
		for _, p := range prefixWildcards {
			if strings.HasPrefix(code, p) {
				return true
			}
		}
		return false
	}

	// 预取角色直接绑定的 menu id 集合，避免循环内逐条查库。
	boundMenuIDs := make(map[int64]struct{})
	if len(roleIDs) > 0 {
		ids, err := r.listMenuIDsByRoles(ctx, roleIDs)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			boundMenuIDs[id] = struct{}{}
		}
	}

	allMenus, err := r.ListMenus(ctx, scope)
	if err != nil {
		return nil, err
	}
	var visible []*rbac.Menu
	for _, m := range allMenus {
		if !m.Visible {
			continue
		}
		if m.PermissionCode == "" {
			visible = append(visible, m)
			continue
		}
		if _, ok := boundMenuIDs[m.ID]; ok {
			visible = append(visible, m)
			continue
		}
		if matchPerm(m.PermissionCode) {
			visible = append(visible, m)
		}
	}
	return visible, nil
}

// listMenuIDsByRoles 返回任一角色直接绑定的 menu id 集合（去重）。
func (r *Repository) listMenuIDsByRoles(ctx context.Context, roleIDs []int64) ([]int64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	// 动态构造 IN 列表（roleIDs 已校验为内部生成的 int64，无注入风险）。
	args := make([]any, 0, len(roleIDs))
	placeholders := make([]string, 0, len(roleIDs))
	for i, id := range roleIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	q := "SELECT DISTINCT menu_id FROM vo_role_menus WHERE role_id IN (" + strings.Join(placeholders, ",") + ")"
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query role menus: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// BindMenusToRole 批量绑定菜单到角色（幂等，已存在则跳过）。
func (r *Repository) BindMenusToRole(ctx context.Context, roleID int64, menuIDs []int64, actorID int64) error {
	if len(menuIDs) == 0 {
		return nil
	}
	for _, mid := range menuIDs {
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO vo_role_menus (role_id, menu_id, created_by) VALUES ($1, $2, $3)
			 ON CONFLICT (role_id, menu_id) DO NOTHING`,
			roleID, mid, nullableInt64(actorID)); err != nil {
			return fmt.Errorf("bind menu to role: %w", err)
		}
	}
	return nil
}

// UnbindMenusFromRole 批量解绑菜单。
func (r *Repository) UnbindMenusFromRole(ctx context.Context, roleID int64, menuIDs []int64) error {
	if len(menuIDs) == 0 {
		return nil
	}
	for _, mid := range menuIDs {
		if _, err := r.pool.Exec(ctx,
			`DELETE FROM vo_role_menus WHERE role_id = $1 AND menu_id = $2`,
			roleID, mid); err != nil {
			return fmt.Errorf("unbind menu from role: %w", err)
		}
	}
	return nil
}

// ListMenuIDsByRole 返回角色直接绑定的 menu id 列表。
func (r *Repository) ListMenuIDsByRole(ctx context.Context, roleID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT menu_id FROM vo_role_menus WHERE role_id = $1 ORDER BY menu_id`, roleID)
	if err != nil {
		return nil, fmt.Errorf("list menu ids by role: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListMenusByRole 返回角色直接绑定的菜单（含完整字段）。
func (r *Repository) ListMenusByRole(ctx context.Context, roleID int64) ([]*rbac.Menu, error) {
	q := `SELECT ` + menuColumns + ` FROM vo_menus m
		WHERE m.deleted = false AND m.id IN (SELECT menu_id FROM vo_role_menus WHERE role_id = $1)
		ORDER BY m.sort_order ASC, m.id ASC`
	rows, err := r.pool.Query(ctx, q, roleID)
	if err != nil {
		return nil, fmt.Errorf("list menus by role: %w", err)
	}
	defer rows.Close()
	var items []*rbac.Menu
	for rows.Next() {
		m, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// --- helpers ---

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

func joinConds(conds []string) string {
	if len(conds) == 0 {
		return "true"
	}
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}
