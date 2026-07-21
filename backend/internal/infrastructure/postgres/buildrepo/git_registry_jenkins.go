// Package buildrepo 是构建领域的 PostgreSQL 仓储实现。
package buildrepo

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
	"golang.org/x/crypto/bcrypt"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/build"
)

const pgUniqueViolation = "23505"

// Repository 构建领域 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

// --- Git 源 ---

const gitColumns = `id, uuid, application_id, name, provider, repo_url, default_branch, credential_id,
	webhook_enabled, webhook_secret_hash, last_synced_at, version, created_at, created_by, updated_at, updated_by,
	deleted, deleted_at, deleted_by`

func scanGitSource(row pgx.Row) (*build.GitSource, error) {
	g := &build.GitSource{}
	var (
		defaultBranch     *string
		credentialID      *int64
		webhookSecretHash *string
		lastSyncedAt      *time.Time
		createdBy         *int64
		updatedBy         *int64
		deletedAt         *time.Time
		deletedBy         *int64
	)
	if err := row.Scan(
		&g.ID, &g.UUID, &g.ApplicationID, &g.Name, &g.Provider, &g.RepoURL, &defaultBranch, &credentialID,
		&g.WebhookEnabled, &webhookSecretHash, &lastSyncedAt, &g.Version, &g.CreatedAt, &createdBy, &g.UpdatedAt, &updatedBy,
		&g.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if defaultBranch != nil {
		g.DefaultBranch = *defaultBranch
	}
	if credentialID != nil {
		g.CredentialID = *credentialID
	}
	if webhookSecretHash != nil {
		g.WebhookSecretHash = *webhookSecretHash
	}
	if lastSyncedAt != nil {
		g.LastSyncedAt = lastSyncedAt
	}
	if createdBy != nil {
		g.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		g.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		g.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		g.DeletedBy = *deletedBy
	}
	return g, nil
}

// CreateGitSource 创建 Git 源。Webhook secret 以 bcrypt 哈希存储。
func (r *Repository) CreateGitSource(ctx context.Context, g *build.GitSource) error {
	if g.UUID == uuid.Nil {
		g.UUID = uuid.New()
	}
	now := r.now()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
		g.UpdatedAt = now
	}
	const q = `INSERT INTO vo_git_sources
		(uuid, application_id, name, provider, repo_url, default_branch, credential_id, webhook_enabled,
		 webhook_secret_hash, last_synced_at, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		g.UUID, g.ApplicationID, g.Name, g.Provider, g.RepoURL, nullableStr(g.DefaultBranch),
		nullableInt64(g.CredentialID), g.WebhookEnabled, nullableStr(g.WebhookSecretHash), g.LastSyncedAt,
		g.Version, g.CreatedAt, nullableInt64(g.CreatedBy), g.UpdatedAt, nullableInt64(g.CreatedBy),
	).Scan(&g.ID, &g.Version, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert git source: %w", err)
	}
	return nil
}

// GetGitSourceByID 按 ID 查询。
func (r *Repository) GetGitSourceByID(ctx context.Context, id int64) (*build.GitSource, error) {
	q := `SELECT ` + gitColumns + ` FROM vo_git_sources WHERE id=$1 AND deleted=false`
	g, err := scanGitSource(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrGitSourceNotFound
		}
		return nil, err
	}
	return g, nil
}

// GetGitSourceByName 按应用+名称查询。
func (r *Repository) GetGitSourceByName(ctx context.Context, appID int64, name string) (*build.GitSource, error) {
	q := `SELECT ` + gitColumns + ` FROM vo_git_sources WHERE application_id=$1 AND name=$2 AND deleted=false`
	g, err := scanGitSource(r.pool.QueryRow(ctx, q, appID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrGitSourceNotFound
		}
		return nil, err
	}
	return g, nil
}

// ListGitSources 列出应用的 Git 源。
func (r *Repository) ListGitSources(ctx context.Context, appID int64) ([]*build.GitSource, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+gitColumns+` FROM vo_git_sources WHERE application_id=$1 AND deleted=false ORDER BY created_at ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("query git sources: %w", err)
	}
	defer rows.Close()
	var items []*build.GitSource
	for rows.Next() {
		g, err := scanGitSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

// UpdateGitSource 更新 Git 源。
func (r *Repository) UpdateGitSource(ctx context.Context, g *build.GitSource) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_git_sources SET name=$1, provider=$2, repo_url=$3, default_branch=$4, credential_id=$5,
		 webhook_enabled=$6, webhook_secret_hash=$7, last_synced_at=$8, updated_at=$9, updated_by=$10, version=version+1
		 WHERE id=$11 AND version=$12 AND deleted=false`,
		g.Name, g.Provider, g.RepoURL, nullableStr(g.DefaultBranch), nullableInt64(g.CredentialID),
		g.WebhookEnabled, nullableStr(g.WebhookSecretHash), g.LastSyncedAt, now, nullableInt64(g.UpdatedBy), g.ID, g.Version)
	if err != nil {
		return fmt.Errorf("update git source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteGitSource 软删除 Git 源。
func (r *Repository) DeleteGitSource(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_git_sources SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete git source: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrGitSourceNotFound
	}
	return nil
}

// --- 镜像仓库 ---

const registryColumns = `id, uuid, name, type, url, credential_id, is_default, status, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanRegistry(row pgx.Row) (*build.Registry, error) {
	r := &build.Registry{}
	var (
		credentialID *int64
		createdBy    *int64
		updatedBy    *int64
		deletedAt    *time.Time
		deletedBy    *int64
	)
	if err := row.Scan(
		&r.ID, &r.UUID, &r.Name, &r.Type, &r.URL, &credentialID, &r.IsDefault, &r.Status, &r.Version,
		&r.CreatedAt, &createdBy, &r.UpdatedAt, &updatedBy, &r.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if credentialID != nil {
		r.CredentialID = *credentialID
	}
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		r.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		r.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		r.DeletedBy = *deletedBy
	}
	return r, nil
}

// CreateRegistry 创建镜像仓库。
func (r *Repository) CreateRegistry(ctx context.Context, reg *build.Registry) error {
	if reg.UUID == uuid.Nil {
		reg.UUID = uuid.New()
	}
	now := r.now()
	if reg.CreatedAt.IsZero() {
		reg.CreatedAt = now
		reg.UpdatedAt = now
	}
	if reg.Status == "" {
		reg.Status = build.RegistryActive
	}
	const q = `INSERT INTO vo_registries
		(uuid, name, type, url, credential_id, is_default, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		reg.UUID, reg.Name, reg.Type, reg.URL, nullableInt64(reg.CredentialID), reg.IsDefault, reg.Status,
		reg.Version, reg.CreatedAt, nullableInt64(reg.CreatedBy), reg.UpdatedAt, nullableInt64(reg.CreatedBy),
	).Scan(&reg.ID, &reg.Version, &reg.CreatedAt, &reg.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return build.ErrRegistryNameExists
		}
		return fmt.Errorf("insert registry: %w", err)
	}
	// 若设为默认，取消其他默认。
	if reg.IsDefault {
		_, _ = r.pool.Exec(ctx, `UPDATE vo_registries SET is_default=false WHERE id!=$1`, reg.ID)
	}
	return nil
}

// GetRegistryByID 按 ID 查询。
func (r *Repository) GetRegistryByID(ctx context.Context, id int64) (*build.Registry, error) {
	q := `SELECT ` + registryColumns + ` FROM vo_registries WHERE id=$1 AND deleted=false`
	reg, err := scanRegistry(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrRegistryNotFound
		}
		return nil, err
	}
	return reg, nil
}

// GetRegistryByName 按名称查询。
func (r *Repository) GetRegistryByName(ctx context.Context, name string) (*build.Registry, error) {
	q := `SELECT ` + registryColumns + ` FROM vo_registries WHERE name=$1 AND deleted=false`
	reg, err := scanRegistry(r.pool.QueryRow(ctx, q, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrRegistryNotFound
		}
		return nil, err
	}
	return reg, nil
}

// GetDefaultRegistry 取默认仓库。
func (r *Repository) GetDefaultRegistry(ctx context.Context) (*build.Registry, error) {
	q := `SELECT ` + registryColumns + ` FROM vo_registries WHERE is_default=true AND status='active' AND deleted=false LIMIT 1`
	reg, err := scanRegistry(r.pool.QueryRow(ctx, q))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrRegistryNotFound
		}
		return nil, err
	}
	return reg, nil
}

// ListRegistries 分页列出仓库。
func (r *Repository) ListRegistries(ctx context.Context, offset, limit int) ([]*build.Registry, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_registries WHERE deleted=false`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count registries: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+registryColumns+` FROM vo_registries WHERE deleted=false ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query registries: %w", err)
	}
	defer rows.Close()
	var items []*build.Registry
	for rows.Next() {
		reg, err := scanRegistry(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, reg)
	}
	return items, total, rows.Err()
}

// UpdateRegistry 更新仓库。
func (r *Repository) UpdateRegistry(ctx context.Context, reg *build.Registry) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_registries SET name=$1, type=$2, url=$3, credential_id=$4, is_default=$5, status=$6,
		 updated_at=$7, updated_by=$8, version=version+1
		 WHERE id=$9 AND version=$10 AND deleted=false`,
		reg.Name, reg.Type, reg.URL, nullableInt64(reg.CredentialID), reg.IsDefault, reg.Status,
		now, nullableInt64(reg.UpdatedBy), reg.ID, reg.Version)
	if err != nil {
		return fmt.Errorf("update registry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	if reg.IsDefault {
		_, _ = r.pool.Exec(ctx, `UPDATE vo_registries SET is_default=false WHERE id!=$1`, reg.ID)
	}
	return nil
}

// DeleteRegistry 软删除仓库。
func (r *Repository) DeleteRegistry(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_registries SET deleted=true, deleted_at=$1, deleted_by=$2, status='disabled', updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete registry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrRegistryNotFound
	}
	return nil
}

// --- Jenkins 实例 ---

const jenkinsColumns = `id, uuid, name, url, credential_id, default_job_folder, is_default, status, last_checked_at,
	version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanJenkins(row pgx.Row) (*build.JenkinsInstance, error) {
	j := &build.JenkinsInstance{}
	var (
		credentialID     *int64
		defaultJobFolder *string
		lastCheckedAt    *time.Time
		createdBy        *int64
		updatedBy        *int64
		deletedAt        *time.Time
		deletedBy        *int64
	)
	if err := row.Scan(
		&j.ID, &j.UUID, &j.Name, &j.URL, &credentialID, &defaultJobFolder, &j.IsDefault, &j.Status, &lastCheckedAt,
		&j.Version, &j.CreatedAt, &createdBy, &j.UpdatedAt, &updatedBy, &j.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if credentialID != nil {
		j.CredentialID = *credentialID
	}
	if defaultJobFolder != nil {
		j.DefaultJobFolder = *defaultJobFolder
	}
	if lastCheckedAt != nil {
		j.LastCheckedAt = lastCheckedAt
	}
	if createdBy != nil {
		j.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		j.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		j.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		j.DeletedBy = *deletedBy
	}
	return j, nil
}

// CreateJenkins 创建 Jenkins 实例。
func (r *Repository) CreateJenkins(ctx context.Context, j *build.JenkinsInstance) error {
	if j.UUID == uuid.Nil {
		j.UUID = uuid.New()
	}
	now := r.now()
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
		j.UpdatedAt = now
	}
	if j.Status == "" {
		j.Status = build.JenkinsActive
	}
	const q = `INSERT INTO vo_jenkins_instances
		(uuid, name, url, credential_id, default_job_folder, is_default, status, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		j.UUID, j.Name, j.URL, nullableInt64(j.CredentialID), nullableStr(j.DefaultJobFolder), j.IsDefault, j.Status,
		j.Version, j.CreatedAt, nullableInt64(j.CreatedBy), j.UpdatedAt, nullableInt64(j.CreatedBy),
	).Scan(&j.ID, &j.Version, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return build.ErrJenkinsNameExists
		}
		return fmt.Errorf("insert jenkins: %w", err)
	}
	if j.IsDefault {
		_, _ = r.pool.Exec(ctx, `UPDATE vo_jenkins_instances SET is_default=false WHERE id!=$1`, j.ID)
	}
	return nil
}

// GetJenkinsByID 按 ID 查询。
func (r *Repository) GetJenkinsByID(ctx context.Context, id int64) (*build.JenkinsInstance, error) {
	q := `SELECT ` + jenkinsColumns + ` FROM vo_jenkins_instances WHERE id=$1 AND deleted=false`
	j, err := scanJenkins(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrJenkinsNotFound
		}
		return nil, err
	}
	return j, nil
}

// GetJenkinsByName 按名称查询。
func (r *Repository) GetJenkinsByName(ctx context.Context, name string) (*build.JenkinsInstance, error) {
	q := `SELECT ` + jenkinsColumns + ` FROM vo_jenkins_instances WHERE name=$1 AND deleted=false`
	j, err := scanJenkins(r.pool.QueryRow(ctx, q, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrJenkinsNotFound
		}
		return nil, err
	}
	return j, nil
}

// GetDefaultJenkins 取默认 Jenkins。
func (r *Repository) GetDefaultJenkins(ctx context.Context) (*build.JenkinsInstance, error) {
	q := `SELECT ` + jenkinsColumns + ` FROM vo_jenkins_instances WHERE is_default=true AND status='active' AND deleted=false LIMIT 1`
	j, err := scanJenkins(r.pool.QueryRow(ctx, q))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrJenkinsNotFound
		}
		return nil, err
	}
	return j, nil
}

// ListJenkins 分页列出 Jenkins。
func (r *Repository) ListJenkins(ctx context.Context, offset, limit int) ([]*build.JenkinsInstance, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_jenkins_instances WHERE deleted=false`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jenkins: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+jenkinsColumns+` FROM vo_jenkins_instances WHERE deleted=false ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query jenkins: %w", err)
	}
	defer rows.Close()
	var items []*build.JenkinsInstance
	for rows.Next() {
		j, err := scanJenkins(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, j)
	}
	return items, total, rows.Err()
}

// UpdateJenkins 更新 Jenkins。
func (r *Repository) UpdateJenkins(ctx context.Context, j *build.JenkinsInstance) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_jenkins_instances SET name=$1, url=$2, credential_id=$3, default_job_folder=$4, is_default=$5, status=$6,
		 last_checked_at=$7, updated_at=$8, updated_by=$9, version=version+1
		 WHERE id=$10 AND version=$11 AND deleted=false`,
		j.Name, j.URL, nullableInt64(j.CredentialID), nullableStr(j.DefaultJobFolder), j.IsDefault, j.Status,
		j.LastCheckedAt, now, nullableInt64(j.UpdatedBy), j.ID, j.Version)
	if err != nil {
		return fmt.Errorf("update jenkins: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	if j.IsDefault {
		_, _ = r.pool.Exec(ctx, `UPDATE vo_jenkins_instances SET is_default=false WHERE id!=$1`, j.ID)
	}
	return nil
}

// DeleteJenkins 软删除 Jenkins。
func (r *Repository) DeleteJenkins(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_jenkins_instances SET deleted=true, deleted_at=$1, deleted_by=$2, status='disabled', updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete jenkins: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrJenkinsNotFound
	}
	return nil
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

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// hashWebhookSecret 用 bcrypt 哈希 webhook secret。
func hashWebhookSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash webhook secret: %w", err)
	}
	return string(b), nil
}

// VerifyWebhookSecret 校验 webhook secret。
func VerifyWebhookSecret(hashed, plain string) bool {
	if hashed == "" {
		return plain == ""
	}
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain)) == nil
}

// jsonbMarshal 序列化为 JSONB 兼容字节（nil map → null）。
func jsonbMarshal(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	// 空 map/数组返回 nil 以使用 DB 默认值。
	s := string(b)
	if s == "{}" || s == "[]" || s == "null" {
		return nil
	}
	return b
}

// jsonbPtrMarshal 同上但返回 []byte（用于可空 JSONB 列）。
func jsonbPtrMarshal(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	if string(b) == "{}" || string(b) == "[]" || string(b) == "null" {
		return nil
	}
	return b
}

// jsonbObjNonNil 与 jsonbPtrMarshal 类似，但空 map/slice 序列化为 "{}" 而非 nil，
// 用于 vo_images.labels 等 NOT NULL jsonb 列，避免空 labels 触发 NOT NULL 违约。
func jsonbObjNonNil(v any) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	if string(b) == "null" {
		return []byte("{}")
	}
	if (string(b) == "{}" || string(b) == "[]") {
		return []byte("{}")
	}
	return b
}

// 避免未使用 import。
var _ = strings.TrimSpace
