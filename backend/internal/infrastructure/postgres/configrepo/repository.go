// Package configrepo 是配置管理领域的 PostgreSQL 仓储实现。
package configrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	configdomain "github.com/vortexops/vortexops/internal/domain/config"
)

const pgUniqueViolation = "23505"

// Repository 配置领域 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

// --- 配置 ---

const configColumns = `id, uuid, scope, scope_id, group_id, name, config_type, config_version, description,
	rendered_content, diff_with_previous, checksum, status, version, created_at, created_by, updated_at, updated_by,
	deleted, deleted_at, deleted_by`

func scanConfig(row pgx.Row) (*configdomain.Config, error) {
	c := &configdomain.Config{}
	var (
		groupID          *int64
		desc             *string
		renderedContent  *string
		diffWithPrev     *string
		createdBy        *int64
		updatedBy        *int64
		deletedAt        *time.Time
		deletedBy        *int64
	)
	if err := row.Scan(
		&c.ID, &c.UUID, &c.Scope, &c.ScopeID, &groupID, &c.Name, &c.ConfigType, &c.ConfigVersion, &desc,
		&renderedContent, &diffWithPrev, &c.Checksum, &c.Status, &c.Version, &c.CreatedAt, &createdBy, &c.UpdatedAt, &updatedBy,
		&c.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if groupID != nil {
		c.GroupID = *groupID
	}
	if desc != nil {
		c.Description = *desc
	}
	if renderedContent != nil {
		c.RenderedContent = *renderedContent
	}
	if diffWithPrev != nil {
		c.DiffWithPrevious = *diffWithPrev
	}
	if createdBy != nil {
		c.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		c.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		c.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		c.DeletedBy = *deletedBy
	}
	return c, nil
}

// CreateConfig 创建配置版本（自动计算 checksum 与 version）。
func (r *Repository) CreateConfig(ctx context.Context, c *configdomain.Config) error {
	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}
	now := r.now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
		c.UpdatedAt = now
	}
	if c.Status == "" {
		c.Status = configdomain.ConfigActive
	}
	// 计算版本号。
	ver, err := r.NextConfigVersion(ctx, c.Scope, c.ScopeID, c.Name)
	if err != nil {
		return err
	}
	c.ConfigVersion = ver
	// 计算 checksum（内容 SHA-256）。
	c.Checksum = checksum(c.RenderedContent)
	// 计算 diff_with_previous（与上一版本对比，简化为内容差异摘要）。
	if c.ConfigVersion > 1 {
		prev, perr := r.GetConfigByVersion(ctx, c.Scope, c.ScopeID, c.Name, c.ConfigVersion-1)
		if perr == nil && prev != nil {
			c.DiffWithPrevious = computeDiff(prev.RenderedContent, c.RenderedContent)
		}
	}
	const q = `INSERT INTO vo_configs
		(uuid, scope, scope_id, group_id, name, config_type, config_version, description, rendered_content,
		 diff_with_previous, checksum, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, version, created_at, updated_at`
	err = r.pool.QueryRow(ctx, q,
		c.UUID, c.Scope, nullableInt64(c.ScopeID), nullableInt64(c.GroupID), c.Name, c.ConfigType, c.ConfigVersion,
		nullableStr(c.Description), nullableStr(c.RenderedContent), nullableStr(c.DiffWithPrevious), c.Checksum,
		c.Status, c.Version, c.CreatedAt, nullableInt64(c.CreatedBy), c.UpdatedAt, nullableInt64(c.CreatedBy),
	).Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("insert config: %w", err)
	}
	return nil
}

// GetConfigByID 按 ID 查询配置。
func (r *Repository) GetConfigByID(ctx context.Context, id int64) (*configdomain.Config, error) {
	q := `SELECT ` + configColumns + ` FROM vo_configs WHERE id=$1 AND deleted=false`
	c, err := scanConfig(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetLatestConfig 取最新版本配置。
func (r *Repository) GetLatestConfig(ctx context.Context, scope configdomain.Scope, scopeID int64, groupID int64, name string) (*configdomain.Config, error) {
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "scope = $1", "name = $2", "deleted = false")
	args = append(args, scope, name)
	if scopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, scopeID)
	}
	if groupID != 0 {
		conds = append(conds, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, groupID)
	}
	q := `SELECT ` + configColumns + ` FROM vo_configs WHERE ` + joinConds(conds) + ` ORDER BY config_version DESC LIMIT 1`
	c, err := scanConfig(r.pool.QueryRow(ctx, q, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetConfigByVersion 按版本号查询配置。
func (r *Repository) GetConfigByVersion(ctx context.Context, scope configdomain.Scope, scopeID int64, name string, version int) (*configdomain.Config, error) {
	q := `SELECT ` + configColumns + ` FROM vo_configs WHERE scope=$1 AND name=$2 AND config_version=$3 AND deleted=false`
	c, err := scanConfig(r.pool.QueryRow(ctx, q, scope, name, version))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// ListConfigs 分页查询配置。
func (r *Repository) ListConfigs(ctx context.Context, q configdomain.ConfigQuery) ([]*configdomain.Config, int64, error) {
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
	if q.GroupID != 0 {
		conds = append(conds, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, q.GroupID)
	}
	if q.Name != "" {
		conds = append(conds, fmt.Sprintf("name = $%d", len(args)+1))
		args = append(args, q.Name)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_configs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count configs: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_configs WHERE %s ORDER BY config_version DESC LIMIT $%d OFFSET $%d",
		configColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query configs: %w", err)
	}
	defer rows.Close()
	var items []*configdomain.Config
	for rows.Next() {
		c, err := scanConfig(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
}

// ArchiveConfig 归档配置版本。
func (r *Repository) ArchiveConfig(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_configs SET status='archived', updated_at=now(), version=version+1 WHERE id=$1 AND deleted=false`, id)
	if err != nil {
		return fmt.Errorf("archive config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return configdomain.ErrConfigNotFound
	}
	return nil
}

// NextConfigVersion 获取下一个 config_version（按 scope+scope_id+name 自增）。
func (r *Repository) NextConfigVersion(ctx context.Context, scope configdomain.Scope, scopeID int64, name string) (int, error) {
	var maxVer int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(config_version), 0) FROM vo_configs WHERE scope=$1 AND COALESCE(scope_id,0)=COALESCE($2,0) AND name=$3 AND deleted=false`,
		scope, nullableInt64(scopeID), name).Scan(&maxVer)
	if err != nil {
		return 0, fmt.Errorf("get max config version: %w", err)
	}
	return maxVer + 1, nil
}

// UpdateGroupCurrentConfig 回写 group 的 current_config_id。
func (r *Repository) UpdateGroupCurrentConfig(ctx context.Context, groupID, configID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_groups SET current_config_id=$1, updated_at=now(), version=version+1 WHERE id=$2`,
		nullableInt64(configID), groupID)
	if err != nil {
		return fmt.Errorf("update group current config: %w", err)
	}
	return nil
}

// --- ConfigSet ---

const cfgSetColumns = `id, uuid, workspace_id, application_id, name, description, content, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanConfigSet(row pgx.Row) (*configdomain.ConfigSet, error) {
	cs := &configdomain.ConfigSet{Content: map[string]any{}}
	var (
		wsID       *int64
		appID      *int64
		desc       *string
		content    []byte
		createdBy  *int64
		updatedBy  *int64
		deletedAt  *time.Time
		deletedBy  *int64
	)
	if err := row.Scan(
		&cs.ID, &cs.UUID, &wsID, &appID, &cs.Name, &desc, &content, &cs.Version, &cs.CreatedAt, &createdBy,
		&cs.UpdatedAt, &updatedBy, &cs.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if wsID != nil {
		cs.WorkspaceID = *wsID
	}
	if appID != nil {
		cs.ApplicationID = *appID
	}
	if desc != nil {
		cs.Description = *desc
	}
	if content != nil {
		_ = json.Unmarshal(content, &cs.Content)
	}
	if createdBy != nil {
		cs.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		cs.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		cs.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		cs.DeletedBy = *deletedBy
	}
	return cs, nil
}

// CreateConfigSet 创建 ConfigSet。
func (r *Repository) CreateConfigSet(ctx context.Context, cs *configdomain.ConfigSet) error {
	if cs.UUID == uuid.Nil {
		cs.UUID = uuid.New()
	}
	now := r.now()
	if cs.CreatedAt.IsZero() {
		cs.CreatedAt = now
		cs.UpdatedAt = now
	}
	if cs.Content == nil {
		cs.Content = map[string]any{}
	}
	contentBytes, _ := json.Marshal(cs.Content)
	const q = `INSERT INTO vo_config_sets
		(uuid, workspace_id, application_id, name, description, content, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		cs.UUID, nullableInt64(cs.WorkspaceID), nullableInt64(cs.ApplicationID), cs.Name, nullableStr(cs.Description), contentBytes, cs.Version,
		cs.CreatedAt, nullableInt64(cs.CreatedBy), cs.UpdatedAt, nullableInt64(cs.CreatedBy),
	).Scan(&cs.ID, &cs.Version, &cs.CreatedAt, &cs.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return configdomain.ErrConfigSetExists
		}
		return fmt.Errorf("insert config set: %w", err)
	}
	return nil
}

// GetConfigSetByID 按 ID 查询 ConfigSet。
func (r *Repository) GetConfigSetByID(ctx context.Context, id int64) (*configdomain.ConfigSet, error) {
	q := `SELECT ` + cfgSetColumns + ` FROM vo_config_sets WHERE id=$1 AND deleted=false`
	cs, err := scanConfigSet(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrConfigSetNotFound
		}
		return nil, err
	}
	return cs, nil
}

// GetConfigSetByName 按名称查询 ConfigSet。
func (r *Repository) GetConfigSetByName(ctx context.Context, workspaceID int64, name string) (*configdomain.ConfigSet, error) {
	q := `SELECT ` + cfgSetColumns + ` FROM vo_config_sets WHERE workspace_id=$1 AND name=$2 AND deleted=false`
	cs, err := scanConfigSet(r.pool.QueryRow(ctx, q, workspaceID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrConfigSetNotFound
		}
		return nil, err
	}
	return cs, nil
}

// ListConfigSets 分页列出 ConfigSet。
func (r *Repository) ListConfigSets(ctx context.Context, workspaceID int64, offset, limit int) ([]*configdomain.ConfigSet, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_config_sets WHERE workspace_id=$1 AND deleted=false`, workspaceID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count config sets: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+cfgSetColumns+` FROM vo_config_sets WHERE workspace_id=$1 AND deleted=false ORDER BY name ASC LIMIT $2 OFFSET $3`,
		workspaceID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query config sets: %w", err)
	}
	defer rows.Close()
	var items []*configdomain.ConfigSet
	for rows.Next() {
		cs, err := scanConfigSet(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, cs)
	}
	return items, total, rows.Err()
}

// ListConfigSetsByApplication 列出应用下的所有配置集（不分页，供分组绑定下拉用）。
func (r *Repository) ListConfigSetsByApplication(ctx context.Context, applicationID int64) ([]*configdomain.ConfigSet, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+cfgSetColumns+` FROM vo_config_sets WHERE application_id=$1 AND deleted=false ORDER BY name ASC`,
		applicationID)
	if err != nil {
		return nil, fmt.Errorf("query config sets by app: %w", err)
	}
	defer rows.Close()
	var items []*configdomain.ConfigSet
	for rows.Next() {
		cs, err := scanConfigSet(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, cs)
	}
	return items, rows.Err()
}

// UpdateConfigSet 更新 ConfigSet。
func (r *Repository) UpdateConfigSet(ctx context.Context, cs *configdomain.ConfigSet) error {
	now := r.now()
	contentBytes, _ := json.Marshal(cs.Content)
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_config_sets SET name=$1, description=$2, content=$3, updated_at=$4, updated_by=$5, version=version+1
		 WHERE id=$6 AND version=$7 AND deleted=false`,
		cs.Name, nullableStr(cs.Description), contentBytes, now, nullableInt64(cs.UpdatedBy), cs.ID, cs.Version)
	if err != nil {
		return fmt.Errorf("update config set: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteConfigSet 软删除 ConfigSet。
func (r *Repository) DeleteConfigSet(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_config_sets SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete config set: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return configdomain.ErrConfigSetNotFound
	}
	return nil
}

// --- 绑定 ---

const bindingColumns = `id, group_id, config_id, config_set_id, priority, pinned_version, mount_path, sub_path,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanBinding(row pgx.Row) (*configdomain.GroupConfigBinding, error) {
	b := &configdomain.GroupConfigBinding{}
	var (
		configID     *int64
		configSetID  *int64
		pinnedVer    *int
		mountPath    *string
		subPath      *string
		createdBy    *int64
		updatedBy    *int64
		deletedAt    *time.Time
		deletedBy    *int64
	)
	if err := row.Scan(
		&b.ID, &b.GroupID, &configID, &configSetID, &b.Priority, &pinnedVer, &mountPath, &subPath,
		&b.CreatedAt, &createdBy, &b.UpdatedAt, &updatedBy,
		&b.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if configID != nil {
		b.ConfigID = *configID
	}
	if configSetID != nil {
		b.ConfigSetID = *configSetID
	}
	if pinnedVer != nil {
		b.PinnedVersion = pinnedVer
	}
	if mountPath != nil {
		b.MountPath = *mountPath
	}
	if subPath != nil {
		b.SubPath = *subPath
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

// CreateBinding 创建绑定。
func (r *Repository) CreateBinding(ctx context.Context, b *configdomain.GroupConfigBinding) error {
	now := r.now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
		b.UpdatedAt = now
	}
	const q = `INSERT INTO vo_group_config_bindings
		(group_id, config_id, config_set_id, priority, pinned_version, mount_path, sub_path, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		b.GroupID, nullableInt64(b.ConfigID), nullableInt64(b.ConfigSetID), b.Priority, nullableIntPtr(b.PinnedVersion),
		nullableStr(b.MountPath), nullableStr(b.SubPath),
		b.CreatedAt, nullableInt64(b.CreatedBy), b.UpdatedAt, nullableInt64(b.CreatedBy),
	).Scan(&b.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return configdomain.ErrBindingExists
		}
		return fmt.Errorf("insert binding: %w", err)
	}
	return nil
}

// GetBindingByID 按 ID 查询绑定。
func (r *Repository) GetBindingByID(ctx context.Context, id int64) (*configdomain.GroupConfigBinding, error) {
	q := `SELECT ` + bindingColumns + ` FROM vo_group_config_bindings WHERE id=$1 AND deleted=false`
	b, err := scanBinding(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrBindingNotFound
		}
		return nil, err
	}
	return b, nil
}

// GetBinding 查询绑定。
func (r *Repository) GetBinding(ctx context.Context, groupID, configID int64) (*configdomain.GroupConfigBinding, error) {
	q := `SELECT ` + bindingColumns + ` FROM vo_group_config_bindings WHERE group_id=$1 AND config_id=$2 AND deleted=false`
	b, err := scanBinding(r.pool.QueryRow(ctx, q, groupID, configID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrBindingNotFound
		}
		return nil, err
	}
	return b, nil
}

// ListBindingsByGroup 列出 group 的所有绑定。
func (r *Repository) ListBindingsByGroup(ctx context.Context, groupID int64) ([]*configdomain.GroupConfigBinding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+bindingColumns+` FROM vo_group_config_bindings WHERE group_id=$1 AND deleted=false ORDER BY created_at ASC`, groupID)
	if err != nil {
		return nil, fmt.Errorf("query bindings: %w", err)
	}
	defer rows.Close()
	var items []*configdomain.GroupConfigBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// ListActiveBindingsByConfigSet 列出某配置集当前未删除的所有绑定（用于删除前校验）。
// 历史兼容 config_id 指向 vo_configs 的绑定，需同时统计。
func (r *Repository) ListActiveBindingsByConfigSet(ctx context.Context, configSetID int64) ([]*configdomain.GroupConfigBinding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+bindingColumns+` FROM vo_group_config_bindings WHERE config_set_id=$1 AND deleted=false ORDER BY created_at ASC`, configSetID)
	if err != nil {
		return nil, fmt.Errorf("query bindings by config set: %w", err)
	}
	defer rows.Close()
	var items []*configdomain.GroupConfigBinding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// UpdateBinding 更新绑定（挂载路径）。
func (r *Repository) UpdateBinding(ctx context.Context, b *configdomain.GroupConfigBinding) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_group_config_bindings SET mount_path=$1, sub_path=$2, updated_at=now(), updated_by=$3 WHERE id=$4 AND deleted=false`,
		nullableStr(b.MountPath), nullableStr(b.SubPath), nullableInt64(b.UpdatedBy), b.ID)
	if err != nil {
		return fmt.Errorf("update binding: %w", err)
	}
	return nil
}

// DeleteBinding 软删除绑定。
func (r *Repository) DeleteBinding(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_group_config_bindings SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete binding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return configdomain.ErrBindingNotFound
	}
	return nil
}

// CountActiveBindingsByGroup 统计分组当前未删除的绑定数（单绑定校验）。
func (r *Repository) CountActiveBindingsByGroup(ctx context.Context, groupID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_group_config_bindings WHERE group_id=$1 AND deleted=false`, groupID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count group bindings: %w", err)
	}
	return count, nil
}

// --- 分组本地配置 ---

const localCfgColumns = `id, uuid, group_id, name, description, content, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanLocalConfig(row pgx.Row) (*configdomain.GroupLocalConfig, error) {
	c := &configdomain.GroupLocalConfig{Content: map[string]any{}}
	var (
		desc      *string
		content   []byte
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&c.ID, &c.UUID, &c.GroupID, &c.Name, &desc, &content, &c.Version, &c.CreatedAt, &createdBy,
		&c.UpdatedAt, &updatedBy, &c.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		c.Description = *desc
	}
	if content != nil {
		_ = json.Unmarshal(content, &c.Content)
	}
	if createdBy != nil {
		c.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		c.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		c.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		c.DeletedBy = *deletedBy
	}
	return c, nil
}

// UpsertLocalConfig 按 group_id 创建或更新本地配置（单行）。
// 重建模式：先软删旧行再插入新行，简化版本号与审计；前端以返回值为准。
func (r *Repository) UpsertLocalConfig(ctx context.Context, c *configdomain.GroupLocalConfig) error {
	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}
	now := r.now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
		c.UpdatedAt = now
	}
	if c.Name == "" {
		c.Name = "local"
	}
	if c.Content == nil {
		c.Content = map[string]any{}
	}
	contentBytes, _ := json.Marshal(c.Content)

	// 1. 软删已存在的未删除行（如有）。
	if _, err := r.pool.Exec(ctx,
		`UPDATE vo_group_local_configs SET deleted=true, deleted_at=$1, updated_at=$2 WHERE group_id=$3 AND deleted=false`,
		now, now, c.GroupID); err != nil {
		return fmt.Errorf("soft delete old local config: %w", err)
	}

	// 2. 插入新行。
	const q = `INSERT INTO vo_group_local_configs
		(uuid, group_id, name, description, content, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		c.UUID, c.GroupID, c.Name, nullableStr(c.Description), contentBytes, c.Version,
		c.CreatedAt, nullableInt64(c.UpdatedBy), c.UpdatedAt, nullableInt64(c.UpdatedBy),
	).Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert local config: %w", err)
	}
	return nil
}

// GetLocalConfigByGroup 取分组的本地配置（未删除）。无记录返回 ErrLocalConfigNotFound。
func (r *Repository) GetLocalConfigByGroup(ctx context.Context, groupID int64) (*configdomain.GroupLocalConfig, error) {
	q := `SELECT ` + localCfgColumns + ` FROM vo_group_local_configs WHERE group_id=$1 AND deleted=false LIMIT 1`
	c, err := scanLocalConfig(r.pool.QueryRow(ctx, q, groupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, configdomain.ErrLocalConfigNotFound
		}
		return nil, err
	}
	return c, nil
}

// DeleteLocalConfig 软删除分组的本地配置。
func (r *Repository) DeleteLocalConfig(ctx context.Context, groupID, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_group_local_configs SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3 WHERE group_id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), groupID)
	if err != nil {
		return fmt.Errorf("delete local config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return configdomain.ErrLocalConfigNotFound
	}
	return nil
}

// --- helpers ---

func checksum(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// computeDiff 简化 diff：返回前后内容长度差异与首行差异摘要。
// 完整 line-diff 在 application 层用更丰富实现；仓储层只存摘要。
func computeDiff(prev, curr string) string {
	if prev == curr {
		return "no changes"
	}
	return fmt.Sprintf("changed (prev=%dB, curr=%dB)", len(prev), len(curr))
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

func nullableIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
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
