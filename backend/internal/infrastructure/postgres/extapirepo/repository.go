// Package extapirepo 是对外 API 领域的 PostgreSQL 仓储实现。
package extapirepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/extapi"
)

// Repository PostgreSQL 仓储（Token、调用日志、建空间策略）。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const tokenColumns = `id, uuid, user_id, name, token_prefix, token_hash, scopes, allowed_workspaces, allowed_apps,
	rate_limit_per_min, ip_allowlist, webhook_url, token_type, expires_at, last_used_at, last_used_ip, status,
	version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanToken(row pgx.Row) (*extapi.ExternalToken, error) {
	t := &extapi.ExternalToken{Scopes: []string{}, IPAllowlist: []string{}}
	var (
		scopes            []byte
		allowedWS         []int64
		allowedApps       []int64
		rateLimit         *int
		ipAllowlist       []byte
		webhookURL        *string
		tokenType         string
		expiresAt         *time.Time
		lastUsedAt        *time.Time
		lastUsedIP        *string
		status            string
		createdBy         *int64
		updatedBy         *int64
		deletedAt         *time.Time
		deletedBy         *int64
	)
	if err := row.Scan(
		&t.ID, &t.UUID, &t.UserID, &t.Name, &t.TokenPrefix, &t.TokenHash, &scopes, &allowedWS, &allowedApps,
		&rateLimit, &ipAllowlist, &webhookURL, &tokenType, &expiresAt, &lastUsedAt, &lastUsedIP, &status,
		&t.Version, &t.CreatedAt, &createdBy, &t.UpdatedAt, &updatedBy, &t.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if scopes != nil {
		_ = json.Unmarshal(scopes, &t.Scopes)
	}
	t.AllowedWorkspaces = allowedWS
	t.AllowedApps = allowedApps
	t.RateLimitPerMin = rateLimit
	if ipAllowlist != nil {
		_ = json.Unmarshal(ipAllowlist, &t.IPAllowlist)
	}
	if webhookURL != nil {
		t.WebhookURL = *webhookURL
	}
	if expiresAt != nil {
		t.ExpiresAt = expiresAt
	}
	if lastUsedAt != nil {
		t.LastUsedAt = lastUsedAt
	}
	if lastUsedIP != nil {
		t.LastUsedIP = *lastUsedIP
	}
	t.Status = extapi.TokenStatus(status)
	if createdBy != nil {
		t.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		t.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		t.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		t.DeletedBy = *deletedBy
	}
	return t, nil
}

// CreateToken 创建 external Token 记录（明文由上层生成，此处仅存哈希）。
func (r *Repository) CreateToken(ctx context.Context, t *extapi.ExternalToken) error {
	if t.UUID == uuid.Nil {
		t.UUID = uuid.New()
	}
	scopes, _ := json.Marshal(t.Scopes)
	ipAllow, _ := json.Marshal(t.IPAllowlist)
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_api_tokens (uuid, user_id, name, token_prefix, token_hash, scopes, allowed_workspaces, allowed_apps,
  rate_limit_per_min, ip_allowlist, webhook_url, token_type, expires_at, status, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,$15,$15)
RETURNING id, created_at, updated_at`,
		t.UUID, t.UserID, t.Name, t.TokenPrefix, t.TokenHash, scopes, t.AllowedWorkspaces, t.AllowedApps,
		t.RateLimitPerMin, ipAllow, nullableStr(t.WebhookURL), extapi.TokenTypeExternal, t.ExpiresAt,
		string(t.Status), t.CreatedBy)
	return row.Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

// GetTokenByHash 按哈希查询 Token。
func (r *Repository) GetTokenByHash(ctx context.Context, hash string) (*extapi.ExternalToken, error) {
	q := `SELECT ` + tokenColumns + ` FROM vo_api_tokens WHERE token_hash=$1 AND token_type=$2 AND deleted=false`
	t, err := scanToken(r.pool.QueryRow(ctx, q, hash, extapi.TokenTypeExternal))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, extapi.ErrTokenNotFound
	}
	return t, err
}

// GetTokenByID 按 ID 查询 Token。
func (r *Repository) GetTokenByID(ctx context.Context, id int64) (*extapi.ExternalToken, error) {
	q := `SELECT ` + tokenColumns + ` FROM vo_api_tokens WHERE id=$1 AND token_type=$2 AND deleted=false`
	t, err := scanToken(r.pool.QueryRow(ctx, q, id, extapi.TokenTypeExternal))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, extapi.ErrTokenNotFound
	}
	return t, err
}

// ListTokensByUser 分页列出用户的 external Token。
func (r *Repository) ListTokensByUser(ctx context.Context, q extapi.TokenQuery) ([]*extapi.ExternalToken, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_api_tokens WHERE user_id=$1 AND token_type=$2 AND deleted=false`,
		q.UserID, extapi.TokenTypeExternal).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+tokenColumns+` FROM vo_api_tokens WHERE user_id=$1 AND token_type=$2 AND deleted=false
		 ORDER BY id DESC LIMIT $3 OFFSET $4`, q.UserID, extapi.TokenTypeExternal, q.Limit, q.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*extapi.ExternalToken, 0)
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// UpdateToken 更新 Token 元数据（不含哈希）。
func (r *Repository) UpdateToken(ctx context.Context, t *extapi.ExternalToken) error {
	scopes, _ := json.Marshal(t.Scopes)
	ipAllow, _ := json.Marshal(t.IPAllowlist)
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_api_tokens SET name=$1, scopes=$2, allowed_workspaces=$3, allowed_apps=$4, rate_limit_per_min=$5,
  ip_allowlist=$6, webhook_url=$7, expires_at=$8, version=version+1, updated_at=now(), updated_by=$9
WHERE id=$10 AND token_type=$11 AND deleted=false AND version=$12`,
		t.Name, scopes, t.AllowedWorkspaces, t.AllowedApps, t.RateLimitPerMin, ipAllow,
		nullableStr(t.WebhookURL), t.ExpiresAt, t.UpdatedBy, t.ID, extapi.TokenTypeExternal, t.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return extapi.ErrTokenNotFound
	}
	return nil
}

// RevokeToken 撤销 Token。
func (r *Repository) RevokeToken(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_api_tokens SET status=$1, updated_at=now(), updated_by=$2, version=version+1
WHERE id=$3 AND token_type=$4 AND deleted=false`,
		string(extapi.TokenStatusRevoked), actorID, id, extapi.TokenTypeExternal)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return extapi.ErrTokenNotFound
	}
	return nil
}

// UpdateTokenLastUsed 更新最后使用时间/IP。
func (r *Repository) UpdateTokenLastUsed(ctx context.Context, id int64, ip string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
UPDATE vo_api_tokens SET last_used_at=$1, last_used_ip=$2 WHERE id=$3 AND deleted=false`, at, nullableStr(ip), id)
	return err
}

// AppendCallLog 追加对外 API 调用日志。
func (r *Repository) AppendCallLog(ctx context.Context, log *extapi.ExternalCallLog) error {
	if log.UUID == uuid.Nil {
		log.UUID = uuid.New()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = r.now()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_external_api_call_logs (uuid, token_id, token_prefix, method, path, operation, workspace_id,
  resource_type, resource_uuid, request_id, status_code, duration_ms, client_ip, user_agent, error_message, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
RETURNING id`, log.UUID, nullableInt64(log.TokenID), nullableStr(log.TokenPrefix), log.Method, log.Path,
		nullableStr(log.Operation), nullableInt64(log.WorkspaceID), nullableStr(log.ResourceType),
		nullableStr(log.ResourceUUID), nullableStr(log.RequestID), nullableInt(log.StatusCode),
		nullableInt(log.DurationMs), nullableStr(log.ClientIP), nullableStr(log.UserAgent),
		nullableStr(log.ErrorMessage), log.CreatedAt)
	return row.Scan(&log.ID)
}

// GetIdempotency 由 Redis 实现；Postgres 仓储不支持。
func (r *Repository) GetIdempotency(_ context.Context, _ string) (*extapi.IdempotencyRecord, error) {
	return nil, fmt.Errorf("idempotency: use redis store")
}

// SetIdempotency 由 Redis 实现；Postgres 仓储不支持。
func (r *Repository) SetIdempotency(_ context.Context, _ *extapi.IdempotencyRecord, _ time.Duration) error {
	return fmt.Errorf("idempotency: use redis store")
}

func scanPolicy(row pgx.Row) (*extapi.WorkspaceCreationPolicy, error) {
	p := &extapi.WorkspaceCreationPolicy{DefaultQuota: map[string]any{}}
	var roles, quota []byte
	var approverRole *string
	var createdBy, updatedBy, deletedBy *int64
	var deletedAt *time.Time
	if err := row.Scan(
		&p.ID, &p.UUID, &p.Name, &roles, &p.AllowSelfCreate, &p.MaxWorkspacesPerUser, &quota,
		&p.DefaultClusters, &p.RequireApproval, &approverRole, &p.AutoBindCatalog,
		&p.Version, &p.CreatedAt, &createdBy, &p.UpdatedAt, &updatedBy, &p.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if roles != nil {
		_ = json.Unmarshal(roles, &p.AppliesToRoles)
	}
	if quota != nil {
		_ = json.Unmarshal(quota, &p.DefaultQuota)
	}
	if approverRole != nil {
		p.ApproverRole = *approverRole
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

const policyColumns = `id, uuid, name, applies_to_roles, allow_self_create, max_workspaces_per_user, default_quota,
	default_clusters, require_approval, approver_role, auto_bind_catalog, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

// GetWorkspaceCreationPolicy 按 ID 查询策略。
func (r *Repository) GetWorkspaceCreationPolicy(ctx context.Context, id int64) (*extapi.WorkspaceCreationPolicy, error) {
	q := `SELECT ` + policyColumns + ` FROM vo_workspace_creation_policies WHERE id=$1 AND deleted=false`
	p, err := scanPolicy(r.pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, extapi.ErrPolicyNotFound
	}
	return p, err
}

// ListWorkspaceCreationPolicies 列出全部有效策略。
func (r *Repository) ListWorkspaceCreationPolicies(ctx context.Context) ([]*extapi.WorkspaceCreationPolicy, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+policyColumns+` FROM vo_workspace_creation_policies WHERE deleted=false ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*extapi.WorkspaceCreationPolicy, 0)
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CountUserWorkspaces 统计用户拥有的空间数。
func (r *Repository) CountUserWorkspaces(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_workspaces WHERE owner_id=$1 AND deleted=false`, userID).Scan(&n)
	return n, err
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

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
