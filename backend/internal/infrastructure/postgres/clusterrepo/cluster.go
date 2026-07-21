// Package clusterrepo 是集群领域的 PostgreSQL 仓储实现。
package clusterrepo

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
	"github.com/vortexops/vortexops/internal/domain/cluster"
)

const pgUniqueViolation = "23505"

// Repository 集群仓储的 PostgreSQL 实现。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建集群仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const clusterColumns = `id, uuid, name, display_name, description, api_server, kubeconfig_encrypted, ca_cert_encrypted,
	default_namespace_prefix, insecure_skip_tls, region, environment, k8s_version, node_count, status,
	last_checked_at, last_error, allocatable_cpu_m, allocatable_memory_bytes, allocatable_gpu, capacity_synced_at,
	labels, metadata, version, created_at, created_by, updated_at, updated_by,
	deleted, deleted_at, deleted_by`

func scanCluster(row pgx.Row) (*cluster.Cluster, error) {
	c := &cluster.Cluster{Labels: map[string]string{}, Metadata: map[string]any{}}
	var (
		displayName   *string
		description   *string
		kubeconfigEnc []byte
		caCertEnc     []byte
		nsPrefix      *string
		region        *string
		environment   *string
		k8sVersion    *string
		nodeCount     *int
		lastChecked   *time.Time
		lastError     *string
		allocCPUM     *int
		allocMem      *int64
		allocGPU      *int
		capSyncedAt   *time.Time
		labels        []byte
		metadata      []byte
		createdBy     *int64
		updatedBy     *int64
		deletedAt     *time.Time
		deletedBy     *int64
	)
	if err := row.Scan(
		&c.ID, &c.UUID, &c.Name, &displayName, &description, &c.APIServer, &kubeconfigEnc, &caCertEnc,
		&nsPrefix, &c.InsecureSkipTLS, &region, &environment, &k8sVersion, &nodeCount, &c.Status,
		&lastChecked, &lastError, &allocCPUM, &allocMem, &allocGPU, &capSyncedAt,
		&labels, &metadata, &c.Version, &c.CreatedAt, &createdBy, &c.UpdatedAt, &updatedBy,
		&c.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if displayName != nil {
		c.DisplayName = *displayName
	}
	if description != nil {
		c.Description = *description
	}
	c.KubeconfigEncrypted = kubeconfigEnc
	c.CACertEncrypted = caCertEnc
	if nsPrefix != nil {
		c.DefaultNamespacePrefix = *nsPrefix
	}
	if region != nil {
		c.Region = *region
	}
	if environment != nil {
		c.Environment = *environment
	}
	if k8sVersion != nil {
		c.K8sVersion = *k8sVersion
	}
	if nodeCount != nil {
		c.NodeCount = *nodeCount
	}
	if lastChecked != nil {
		c.LastCheckedAt = lastChecked
	}
	if lastError != nil {
		c.LastError = *lastError
	}
	if allocCPUM != nil {
		c.AllocatableCPUM = *allocCPUM
	}
	if allocMem != nil {
		c.AllocatableMemoryBytes = *allocMem
	}
	if allocGPU != nil {
		c.AllocatableGPU = *allocGPU
	}
	c.CapacitySyncedAt = capSyncedAt
	if labels != nil {
		_ = json.Unmarshal(labels, &c.Labels)
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &c.Metadata)
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

// CreateCluster 创建集群。
func (r *Repository) CreateCluster(ctx context.Context, c *cluster.Cluster) error {
	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}
	now := r.now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
		c.UpdatedAt = now
	}
	if c.Status == "" {
		c.Status = cluster.StatusHealthy
	}
	if c.Labels == nil {
		c.Labels = map[string]string{}
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	labels, _ := json.Marshal(c.Labels)
	metadata, _ := json.Marshal(c.Metadata)
	const q = `INSERT INTO vo_clusters
		(uuid, name, display_name, description, api_server, kubeconfig_encrypted, ca_cert_encrypted,
		 default_namespace_prefix, insecure_skip_tls, region, environment, k8s_version, node_count, status,
		 labels, metadata, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		c.UUID, c.Name, nullableStr(c.DisplayName), nullableStr(c.Description), c.APIServer,
		nullableBytes(c.KubeconfigEncrypted), nullableBytes(c.CACertEncrypted),
		nullableStr(c.DefaultNamespacePrefix), c.InsecureSkipTLS, nullableStr(c.Region), nullableStr(c.Environment),
		nullableStr(c.K8sVersion), nullableInt(c.NodeCount), c.Status,
		labels, metadata, c.Version, c.CreatedAt, nullableInt64(c.CreatedBy), c.UpdatedAt, nullableInt64(c.CreatedBy),
	).Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return cluster.ErrClusterNameExists
		}
		return fmt.Errorf("insert cluster: %w", err)
	}
	return nil
}

// GetClusterByID 按 ID 查询。
func (r *Repository) GetClusterByID(ctx context.Context, id int64) (*cluster.Cluster, error) {
	q := `SELECT ` + clusterColumns + ` FROM vo_clusters WHERE id=$1 AND deleted=false`
	c, err := scanCluster(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrClusterNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetClusterByUUID 按 UUID 查询。
func (r *Repository) GetClusterByUUID(ctx context.Context, id uuid.UUID) (*cluster.Cluster, error) {
	q := `SELECT ` + clusterColumns + ` FROM vo_clusters WHERE uuid=$1 AND deleted=false`
	c, err := scanCluster(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrClusterNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetClusterByName 按名称查询。
func (r *Repository) GetClusterByName(ctx context.Context, name string) (*cluster.Cluster, error) {
	q := `SELECT ` + clusterColumns + ` FROM vo_clusters WHERE name=$1 AND deleted=false`
	c, err := scanCluster(r.pool.QueryRow(ctx, q, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrClusterNotFound
		}
		return nil, err
	}
	return c, nil
}

// UpdateCluster 更新集群（乐观锁）。
func (r *Repository) UpdateCluster(ctx context.Context, in cluster.UpdateClusterInput) (*cluster.Cluster, error) {
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
	if in.KubeconfigEncrypted != nil {
		addSet("kubeconfig_encrypted", in.KubeconfigEncrypted)
	}
	if in.CACertEncrypted != nil {
		addSet("ca_cert_encrypted", in.CACertEncrypted)
	}
	if in.DefaultNamespacePrefix != nil {
		addSet("default_namespace_prefix", nullableStr(*in.DefaultNamespacePrefix))
	}
	if in.InsecureSkipTLS != nil {
		addSet("insecure_skip_tls", *in.InsecureSkipTLS)
	}
	if in.Region != nil {
		addSet("region", nullableStr(*in.Region))
	}
	if in.Environment != nil {
		addSet("environment", nullableStr(*in.Environment))
	}
	if in.Status != nil {
		addSet("status", *in.Status)
	}
	if in.K8sVersion != nil {
		addSet("k8s_version", nullableStr(*in.K8sVersion))
	}
	if in.NodeCount != nil {
		addSet("node_count", nullableInt(*in.NodeCount))
	}
	if in.LastCheckedAt != nil {
		addSet("last_checked_at", *in.LastCheckedAt)
	}
	if in.LastError != nil {
		addSet("last_error", nullableStr(*in.LastError))
	}
	if in.AllocatableCPUM != nil {
		addSet("allocatable_cpu_m", nullableInt(*in.AllocatableCPUM))
	}
	if in.AllocatableMemoryBytes != nil {
		addSet("allocatable_memory_bytes", nullableInt64(*in.AllocatableMemoryBytes))
	}
	if in.AllocatableGPU != nil {
		addSet("allocatable_gpu", *in.AllocatableGPU)
	}
	if in.CapacitySyncedAt != nil {
		addSet("capacity_synced_at", *in.CapacitySyncedAt)
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
		return r.GetClusterByID(ctx, in.ID)
	}
	addSet("updated_at", now)
	addSet("updated_by", nullableInt64(in.UpdatedBy))
	addSet("version", in.Version+1)

	args = append(args, in.ID, in.Version)
	q := fmt.Sprintf(`UPDATE vo_clusters SET %s WHERE id=$%d AND version=$%d AND deleted=false`,
		strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("update cluster: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, gerr := r.GetClusterByID(ctx, in.ID)
		if gerr != nil {
			return nil, cluster.ErrClusterNotFound
		}
		if existing.Version != in.Version {
			return nil, domain.ErrConflict
		}
		return nil, cluster.ErrClusterNotFound
	}
	return r.GetClusterByID(ctx, in.ID)
}

// ListClusters 分页查询集群。
func (r *Repository) ListClusters(ctx context.Context, q cluster.ClusterQuery) ([]*cluster.Cluster, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	conds = append(conds, "deleted = false")
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, q.Status)
		argIdx++
	}
	if q.Region != "" {
		conds = append(conds, fmt.Sprintf("region = $%d", argIdx))
		args = append(args, q.Region)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR display_name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_clusters WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count clusters: %w", err)
	}

	listQ := fmt.Sprintf("SELECT %s FROM vo_clusters WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		clusterColumns, where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query clusters: %w", err)
	}
	defer rows.Close()
	var items []*cluster.Cluster
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
}

// ListActiveClusters 列出所有未禁用集群（供 syncer 加载分片）。
func (r *Repository) ListActiveClusters(ctx context.Context) ([]*cluster.Cluster, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+clusterColumns+` FROM vo_clusters WHERE deleted=false AND status!='disabled' ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query active clusters: %w", err)
	}
	defer rows.Close()
	var items []*cluster.Cluster
	for rows.Next() {
		c, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// ListNamespacesByCluster 列出某集群在所有 workspace 中绑定的 namespace（去重）。
func (r *Repository) ListNamespacesByCluster(ctx context.Context, clusterID int64) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT namespace FROM vo_workspace_clusters WHERE cluster_id=$1 AND deleted=false`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query namespaces by cluster: %w", err)
	}
	defer rows.Close()
	var nss []string
	for rows.Next() {
		var ns string
		if err := rows.Scan(&ns); err != nil {
			return nil, err
		}
		nss = append(nss, ns)
	}
	return nss, rows.Err()
}

// CountWorkspaceBindings 统计某集群在所有 workspace 中的绑定数（删除前关联校验）。
func (r *Repository) CountWorkspaceBindings(ctx context.Context, clusterID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_workspace_clusters WHERE cluster_id=$1 AND deleted=false`, clusterID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count workspace bindings: %w", err)
	}
	return n, nil
}

// CountGroupsByCluster 统计某集群下未删除的分组数（删除前关联校验）。
func (r *Repository) CountGroupsByCluster(ctx context.Context, clusterID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_groups WHERE cluster_id=$1 AND deleted=false`, clusterID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count groups by cluster: %w", err)
	}
	return n, nil
}

// DeleteCluster 软删除集群。
func (r *Repository) DeleteCluster(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_clusters SET deleted=true, deleted_at=$1, deleted_by=$2, status='disabled', updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return cluster.ErrClusterNotFound
	}
	return nil
}

// --- 凭证 ---

const credColumns = `id, uuid, name, kind, scope, scope_id, payload_encrypted, expires_at, last_rotated_at, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanCredential(row pgx.Row) (*cluster.Credential, error) {
	c := &cluster.Credential{}
	var (
		scopeID       *int64
		expiresAt     *time.Time
		lastRotatedAt *time.Time
		createdBy     *int64
		updatedBy     *int64
		deletedAt     *time.Time
		deletedBy     *int64
	)
	if err := row.Scan(
		&c.ID, &c.UUID, &c.Name, &c.Kind, &c.Scope, &scopeID, &c.PayloadEncrypted, &expiresAt, &lastRotatedAt,
		&c.Version, &c.CreatedAt, &createdBy, &c.UpdatedAt, &updatedBy, &c.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if scopeID != nil {
		c.ScopeID = *scopeID
	}
	if expiresAt != nil {
		c.ExpiresAt = expiresAt
	}
	if lastRotatedAt != nil {
		c.LastRotatedAt = lastRotatedAt
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

// CreateCredential 创建凭证。
func (r *Repository) CreateCredential(ctx context.Context, c *cluster.Credential) error {
	if c.UUID == uuid.Nil {
		c.UUID = uuid.New()
	}
	now := r.now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
		c.UpdatedAt = now
	}
	if c.Scope == "" {
		c.Scope = cluster.CredScopePlatform
	}
	const q = `INSERT INTO vo_credentials
		(uuid, name, kind, scope, scope_id, payload_encrypted, expires_at, last_rotated_at, version,
		 created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		c.UUID, c.Name, c.Kind, c.Scope, nullableInt64(c.ScopeID), c.PayloadEncrypted, c.ExpiresAt, c.LastRotatedAt,
		c.Version, c.CreatedAt, nullableInt64(c.CreatedBy), c.UpdatedAt, nullableInt64(c.CreatedBy),
	).Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return cluster.ErrCredentialNameExists
		}
		return fmt.Errorf("insert credential: %w", err)
	}
	return nil
}

// GetCredentialByID 按 ID 查询凭证。
func (r *Repository) GetCredentialByID(ctx context.Context, id int64) (*cluster.Credential, error) {
	q := `SELECT ` + credColumns + ` FROM vo_credentials WHERE id=$1 AND deleted=false`
	c, err := scanCredential(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrCredentialNotFound
		}
		return nil, err
	}
	return c, nil
}

// GetCredentialByName 按名称+作用域查询凭证。
func (r *Repository) GetCredentialByName(ctx context.Context, name string, scope cluster.CredentialScope, scopeID int64) (*cluster.Credential, error) {
	q := `SELECT ` + credColumns + ` FROM vo_credentials WHERE name=$1 AND scope=$2 AND COALESCE(scope_id,0)=$3 AND deleted=false`
	c, err := scanCredential(r.pool.QueryRow(ctx, q, name, scope, scopeID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrCredentialNotFound
		}
		return nil, err
	}
	return c, nil
}

// ListCredentials 分页查询凭证。
func (r *Repository) ListCredentials(ctx context.Context, scope cluster.CredentialScope, scopeID int64, kind cluster.CredentialKind, offset, limit int) ([]*cluster.Credential, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	conds = append(conds, "deleted = false")
	if scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", argIdx))
		args = append(args, scope)
		argIdx++
	}
	if scopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", argIdx))
		args = append(args, scopeID)
		argIdx++
	}
	if kind != "" {
		conds = append(conds, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, kind)
		argIdx++
	}
	where := strings.Join(conds, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_credentials WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count credentials: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_credentials WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		credColumns, where, argIdx, argIdx+1)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query credentials: %w", err)
	}
	defer rows.Close()
	var items []*cluster.Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
}

// UpdateCredential 更新凭证（轮换密钥）。
func (r *Repository) UpdateCredential(ctx context.Context, c *cluster.Credential) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_credentials SET name=$1, payload_encrypted=$2, expires_at=$3, last_rotated_at=$4,
		 version=version+1, updated_at=$5, updated_by=$6
		 WHERE id=$7 AND version=$8 AND deleted=false`,
		c.Name, c.PayloadEncrypted, c.ExpiresAt, now, now, nullableInt64(c.UpdatedBy), c.ID, c.Version)
	if err != nil {
		return fmt.Errorf("update credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteCredential 软删除凭证。
func (r *Repository) DeleteCredential(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_credentials SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return cluster.ErrCredentialNotFound
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
