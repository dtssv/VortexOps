package clusterrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/cluster"
)

// --- IP 池 ---

const ipPoolColumns = `id, uuid, cluster_id, name, cidr, gateway, provider, total_count, allocated_count, reserved_ips, metadata,
	version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanIPPool(row pgx.Row) (*cluster.IPPool, error) {
	p := &cluster.IPPool{}
	var (
		gateway        *string
		totalCount     *int
		reservedIPs    []byte
		metadata       []byte
		createdBy      *int64
		updatedBy      *int64
		deletedAt      *time.Time
		deletedBy      *int64
	)
	if err := row.Scan(
		&p.ID, &p.UUID, &p.ClusterID, &p.Name, &p.CIDR, &gateway, &p.Provider, &totalCount, &p.AllocatedCount,
		&reservedIPs, &metadata, &p.Version, &p.CreatedAt, &createdBy, &p.UpdatedAt, &updatedBy, &p.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if gateway != nil {
		p.Gateway = *gateway
	}
	if totalCount != nil {
		p.TotalCount = *totalCount
	}
	if reservedIPs != nil {
		_ = json.Unmarshal(reservedIPs, &p.ReservedIPs)
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &p.Metadata)
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

// CreateIPPool 创建 IP 池。
func (r *Repository) CreateIPPool(ctx context.Context, p *cluster.IPPool) error {
	if p.UUID == uuid.Nil {
		p.UUID = uuid.New()
	}
	now := r.now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
		p.UpdatedAt = now
	}
	if p.Provider == "" {
		p.Provider = cluster.IPPoolMetalLB
	}
	var reservedIPs any
	if len(p.ReservedIPs) > 0 {
		b, _ := json.Marshal(p.ReservedIPs)
		reservedIPs = b
	}
	metadataJSON, _ := json.Marshal(p.Metadata)
	if p.Metadata == nil {
		metadataJSON = []byte("{}")
	}
	const q = `INSERT INTO vo_cluster_ip_pools
		(uuid, cluster_id, name, cidr, gateway, provider, total_count, allocated_count, reserved_ips, metadata, version,
		 created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.UUID, p.ClusterID, p.Name, p.CIDR, nullableStr(p.Gateway), p.Provider, nullableInt(p.TotalCount),
		p.AllocatedCount, reservedIPs, metadataJSON, p.Version, p.CreatedAt, nullableInt64(p.CreatedBy), p.UpdatedAt, nullableInt64(p.CreatedBy),
	).Scan(&p.ID, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert ip pool: %w", err)
	}
	// Phase 1: 建池后预生成 IP 条目（10万+规模 IPAM 地基）。
	// 条目生成失败不回滚池创建（池行已提交），但返回错误让上层感知并可重试 PreGenerateEntries。
	if p.CIDR != "" {
		if _, gerr := r.PreGenerateEntries(ctx, p.ID, p.CIDR, p.ReservedIPs); gerr != nil {
			return fmt.Errorf("ip pool created (id=%d) but pre-generate entries failed: %w", p.ID, gerr)
		}
	}
	return nil
}

// GetIPPoolByID 按 ID 查询 IP 池。
func (r *Repository) GetIPPoolByID(ctx context.Context, id int64) (*cluster.IPPool, error) {
	q := `SELECT ` + ipPoolColumns + ` FROM vo_cluster_ip_pools WHERE id=$1 AND deleted=false`
	p, err := scanIPPool(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrIPPoolNotFound
		}
		return nil, err
	}
	return p, nil
}

// ListIPPools 列出集群的 IP 池。
func (r *Repository) ListIPPools(ctx context.Context, clusterID int64) ([]*cluster.IPPool, error) {
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if clusterID != 0 {
		conds = append(conds, "cluster_id = $1")
		args = append(args, clusterID)
	}
	rows, err := r.pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM vo_cluster_ip_pools WHERE %s ORDER BY created_at ASC", ipPoolColumns, strings.Join(conds, " AND ")),
		args...)
	if err != nil {
		return nil, fmt.Errorf("query ip pools: %w", err)
	}
	defer rows.Close()
	var items []*cluster.IPPool
	for rows.Next() {
		p, err := scanIPPool(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// UpdateIPPool 更新 IP 池（乐观锁）。
func (r *Repository) UpdateIPPool(ctx context.Context, p *cluster.IPPool) error {
	now := r.now()
	var reservedIPs any
	if len(p.ReservedIPs) > 0 {
		b, _ := json.Marshal(p.ReservedIPs)
		reservedIPs = b
	}
	metadataJSON, _ := json.Marshal(p.Metadata)
	if p.Metadata == nil {
		metadataJSON = []byte("{}")
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_cluster_ip_pools SET name=$1, cidr=$2, gateway=$3, provider=$4, total_count=$5,
		 reserved_ips=$6, metadata=$7, updated_at=$8, updated_by=$9, version=version+1
		 WHERE id=$10 AND version=$11 AND deleted=false`,
		p.Name, p.CIDR, nullableStr(p.Gateway), p.Provider, nullableInt(p.TotalCount),
		reservedIPs, metadataJSON, now, nullableInt64(p.UpdatedBy), p.ID, p.Version)
	if err != nil {
		return fmt.Errorf("update ip pool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteIPPool 软删除 IP 池。
func (r *Repository) DeleteIPPool(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_cluster_ip_pools SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete ip pool: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return cluster.ErrIPPoolNotFound
	}
	return nil
}

// --- IP 分配 ---

const ipAllocColumns = `id, ip_pool_id, cluster_id, ip_address, resource_type, resource_id, replica_index, status,
	allocated_at, released_at`

func scanIPAllocation(row pgx.Row) (*cluster.IPAllocation, error) {
	a := &cluster.IPAllocation{}
	var (
		replicaIndex *int
		releasedAt   *time.Time
	)
	if err := row.Scan(
		&a.ID, &a.IPPoolID, &a.ClusterID, &a.IPAddress, &a.ResourceType, &a.ResourceID, &replicaIndex, &a.Status,
		&a.AllocatedAt, &releasedAt,
	); err != nil {
		return nil, err
	}
	if replicaIndex != nil {
		a.ReplicaIndex = *replicaIndex
	}
	if releasedAt != nil {
		a.ReleasedAt = releasedAt
	}
	return a, nil
}

// ListAvailableIPs 列出池中未分配的 IP（按地址排序，最多 limit 个）。
// Phase 1: 改为查 vo_cluster_ip_pool_entries 表，不再内存枚举 CIDR。
// 若条目表为空（老池未预生成），降级到 enumerateCIDR 兼容历史数据。
func (r *Repository) ListAvailableIPs(ctx context.Context, poolID int64, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT ip_address FROM vo_cluster_ip_pool_entries
		 WHERE ip_pool_id=$1 AND status='free'
		 ORDER BY ip_address LIMIT $2`, poolID, limit)
	if err != nil {
		return nil, fmt.Errorf("query free entries: %w", err)
	}
	defer rows.Close()
	var ips []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 条目表有数据直接返回。
	if len(ips) > 0 {
		return ips, nil
	}
	// 降级：条目表为空（老池或迁移期），回退到内存枚举。
	return r.listAvailableIPsLegacy(ctx, poolID, limit)
}

// listAvailableIPsLegacy 内存枚举兜底（老池无 entries 条目时）。
func (r *Repository) listAvailableIPsLegacy(ctx context.Context, poolID int64, limit int) ([]string, error) {
	pool, err := r.GetIPPoolByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	allIPs, err := enumerateCIDR(pool.CIDR, pool.ReservedIPs)
	if err != nil {
		return nil, fmt.Errorf("enumerate cidr %s: %w", pool.CIDR, err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT ip_address FROM vo_cluster_ip_allocations WHERE ip_pool_id=$1 AND status='allocated'`, poolID)
	if err != nil {
		return nil, fmt.Errorf("query allocated ips: %w", err)
	}
	defer rows.Close()
	allocated := make(map[string]struct{})
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		allocated[ip] = struct{}{}
	}
	var available []string
	for _, ip := range allIPs {
		if _, ok := allocated[ip]; ok {
			continue
		}
		available = append(available, ip)
		if len(available) >= limit {
			break
		}
	}
	return available, rows.Err()
}

// PreGenerateEntries 建池时预生成所有可用 IP 条目到 vo_cluster_ip_pool_entries。
// 分批 INSERT（每批 1000 行）避免单事务过大。网络地址、广播地址排除；
// 保留 IP 标记为 reserved（不在 free 集合中，不会被分配，但计入 total_count）。
// 幂等：已存在条目（UNIQUE 冲突）跳过。
func (r *Repository) PreGenerateEntries(ctx context.Context, poolID int64, cidr string, reserved []string) (int, error) {
	allIPs, err := enumerateCIDRAll(cidr)
	if err != nil {
		return 0, fmt.Errorf("enumerate cidr %s: %w", cidr, err)
	}
	reservedSet := make(map[string]struct{}, len(reserved))
	for _, ip := range reserved {
		reservedSet[ip] = struct{}{}
	}
	const batchSize = 1000
	total := 0
	for start := 0; start < len(allIPs); start += batchSize {
		end := start + batchSize
		if end > len(allIPs) {
			end = len(allIPs)
		}
		var sb strings.Builder
		sb.WriteString(`INSERT INTO vo_cluster_ip_pool_entries (ip_pool_id, ip_address, status, updated_at) VALUES `)
		args := make([]any, 0, (end-start)*2+1)
		args = append(args, poolID)
		for i, ip := range allIPs[start:end] {
			if i > 0 {
				sb.WriteByte(',')
			}
			status := "free"
			if _, ok := reservedSet[ip]; ok {
				status = "reserved"
			}
			// poolID is $1; each IP is a sequential param starting at $2.
			sb.WriteString(fmt.Sprintf("($1,$%d,'%s',now())", i+2, status))
			args = append(args, ip)
		}
		sb.WriteString(` ON CONFLICT (ip_pool_id, ip_address) DO NOTHING`)
		tag, err := r.pool.Exec(ctx, sb.String(), args...)
		if err != nil {
			return total, fmt.Errorf("batch insert entries: %w", err)
		}
		total += int(tag.RowsAffected())
	}
	// 更新池的 total_count（可用 IP 数 = free + reserved，即枚举总数）。
	if total > 0 {
		_, _ = r.pool.Exec(ctx,
			`UPDATE vo_cluster_ip_pools SET total_count = GREATEST(COALESCE(total_count,0), $2) WHERE id=$1`,
			poolID, len(allIPs))
	}
	return total, nil
}

// AllocateIPsBatch 单事务批量分配 N 个 IP。
// 核心查询：SELECT ... FOR UPDATE SKIP LOCKED LIMIT n，并发安全、无冲突重试。
// 同时写入 entries 表（权威状态）和 allocations 表（审计记录）。
func (r *Repository) AllocateIPsBatch(ctx context.Context, poolID, clusterID int64, n int, rtype cluster.IPAllocResourceType, resourceID int64) ([]*cluster.IPAllocation, error) {
	if n <= 0 {
		return nil, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := r.now()
	// 1) SELECT FOR UPDATE SKIP LOCKED 批量预占 N 个 free 条目。
	rows, err := tx.Query(ctx,
		`SELECT id, ip_address FROM vo_cluster_ip_pool_entries
		 WHERE ip_pool_id=$1 AND status='free'
		 ORDER BY ip_address
		 FOR UPDATE SKIP LOCKED LIMIT $2`, poolID, n)
	if err != nil {
		return nil, fmt.Errorf("select free entries: %w", err)
	}
	type entry struct {
		id  int64
		ip  string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.ip); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, cluster.ErrIPExhausted
	}

	allocs := make([]*cluster.IPAllocation, 0, len(entries))
	for i, e := range entries {
		replicaIdx := int64(i)
		// 2) 更新 entries 表为 allocated（已持有行锁）。
		if _, err := tx.Exec(ctx,
			`UPDATE vo_cluster_ip_pool_entries
			 SET status='allocated', resource_type=$1, resource_id=$2, replica_index=$3, allocated_at=$4, updated_at=$5
			 WHERE id=$6`,
			rtype, resourceID, replicaIdx, now, now, e.id); err != nil {
			return nil, fmt.Errorf("update entry %s: %w", e.ip, err)
		}
		// 3) 写 allocations 审计表（幂等：已存在则跳过）。
		var allocID int64
		err := tx.QueryRow(ctx,
			`INSERT INTO vo_cluster_ip_allocations
			 (ip_pool_id, cluster_id, ip_address, resource_type, resource_id, replica_index, status, allocated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,'allocated',$7)
			 ON CONFLICT (ip_pool_id, ip_address) DO UPDATE
			   SET status='allocated', resource_type=$4, resource_id=$5, replica_index=$6, allocated_at=$7, released_at=NULL
			 RETURNING id`,
			poolID, clusterID, e.ip, rtype, resourceID, replicaIdx, now).Scan(&allocID)
		if err != nil {
			return nil, fmt.Errorf("upsert allocation %s: %w", e.ip, err)
		}
		allocs = append(allocs, &cluster.IPAllocation{
			ID: allocID, IPPoolID: poolID, ClusterID: clusterID, IPAddress: e.ip,
			ResourceType: rtype, ResourceID: resourceID, ReplicaIndex: int(replicaIdx),
			Status: cluster.IPAllocAllocated, AllocatedAt: now,
		})
	}
	// 4) 更新池计数（单次 UPDATE，避免 N 次串行化）。
	if _, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pools SET allocated_count = allocated_count + $2 WHERE id=$1`,
		poolID, len(entries)); err != nil {
		return nil, fmt.Errorf("increment pool count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return allocs, nil
}

// ReleaseByResource 释放某资源的所有 IP 分配（group 删除时调用）。
// 同步 entries 表（free）+ allocations 表（released）+ 池计数。
func (r *Repository) ReleaseByResource(ctx context.Context, poolID int64, rtype cluster.IPAllocResourceType, resourceID int64) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := r.now()
	// 1) entries 表标记 free。
	tag, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pool_entries
		 SET status='free', resource_type=NULL, resource_id=NULL, replica_index=NULL, allocated_at=NULL, updated_at=$1
		 WHERE ip_pool_id=$2 AND resource_type=$3 AND resource_id=$4 AND status='allocated'`,
		now, poolID, rtype, resourceID)
	if err != nil {
		return 0, fmt.Errorf("release entries: %w", err)
	}
	released := int(tag.RowsAffected())
	// 2) allocations 表标记 released。
	if _, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_allocations SET status='released', released_at=$1
		 WHERE ip_pool_id=$2 AND resource_type=$3 AND resource_id=$4 AND status='allocated'`,
		now, poolID, rtype, resourceID); err != nil {
		return 0, fmt.Errorf("release allocations: %w", err)
	}
	// 3) 池计数递减。
	if released > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE vo_cluster_ip_pools SET allocated_count = GREATEST(allocated_count - $2, 0) WHERE id=$1`,
			poolID, released); err != nil {
			return 0, fmt.Errorf("decrement pool count: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return released, nil
}

// AllocateIP 原子分配单个 IP（事务：更新 entries 表 + allocations 表 + 池计数）。
// 用于指定 IP 的分配（如 webhook 注入特定 IP）。Phase 1 起同步 entries 表。
func (r *Repository) AllocateIP(ctx context.Context, poolID, clusterID int64, ip string, rtype cluster.IPAllocResourceType, resourceID, replicaIndex int64) (*cluster.IPAllocation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := r.now()
	var replicaIdxArg any
	if replicaIndex != 0 {
		replicaIdxArg = replicaIndex
	}
	// 1) entries 表：标记为 allocated（条件：当前 free/reserved）。
	etag, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pool_entries
		 SET status='allocated', resource_type=$1, resource_id=$2, replica_index=$3, allocated_at=$4, updated_at=$5
		 WHERE ip_pool_id=$6 AND ip_address=$7 AND status IN ('free','reserved')`,
		rtype, resourceID, replicaIdxArg, now, now, poolID, ip)
	if err != nil {
		return nil, fmt.Errorf("update entry: %w", err)
	}
	if etag.RowsAffected() == 0 {
		// 条目不存在（老池）或已 allocated。检查 allocations 表判断是否已分配。
		return nil, cluster.ErrIPAlreadyAllocated
	}
	// 2) allocations 审计表（幂等 upsert）。
	const q = `INSERT INTO vo_cluster_ip_allocations
		(ip_pool_id, cluster_id, ip_address, resource_type, resource_id, replica_index, status, allocated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (ip_pool_id, ip_address) DO UPDATE
		  SET status='allocated', resource_type=$4, resource_id=$5, replica_index=$6, allocated_at=$8, released_at=NULL
		RETURNING id`
	a := &cluster.IPAllocation{
		IPPoolID: poolID, ClusterID: clusterID, IPAddress: ip, ResourceType: rtype,
		ResourceID: resourceID, ReplicaIndex: int(replicaIndex),
		Status: cluster.IPAllocAllocated, AllocatedAt: now,
	}
	err = tx.QueryRow(ctx, q, poolID, clusterID, ip, rtype, resourceID, replicaIdxArg, cluster.IPAllocAllocated, now).Scan(&a.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return nil, cluster.ErrIPAlreadyAllocated
		}
		return nil, fmt.Errorf("upsert ip allocation: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pools SET allocated_count = allocated_count + 1 WHERE id=$1`, poolID); err != nil {
		return nil, fmt.Errorf("increment pool count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return a, nil
}

// ReleaseIP 释放 IP（事务：entries 表标 free + allocations 表标 released + 减计数）。
func (r *Repository) ReleaseIP(ctx context.Context, poolID int64, ip string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := r.now()
	// 1) allocations 表标记 released。
	tag, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_allocations SET status=$1, released_at=$2
		 WHERE ip_pool_id=$3 AND ip_address=$4 AND status=$5`,
		cluster.IPAllocReleased, now, poolID, ip, cluster.IPAllocAllocated)
	if err != nil {
		return fmt.Errorf("release ip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return cluster.ErrIPAllocationNotFound
	}
	// 2) entries 表标记 free（best-effort，老池无条目则无影响）。
	if _, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pool_entries
		 SET status='free', resource_type=NULL, resource_id=NULL, replica_index=NULL, allocated_at=NULL, updated_at=$1
		 WHERE ip_pool_id=$2 AND ip_address=$3 AND status='allocated'`,
		now, poolID, ip); err != nil {
		return fmt.Errorf("release entry: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE vo_cluster_ip_pools SET allocated_count = GREATEST(allocated_count - 1, 0) WHERE id=$1`, poolID); err != nil {
		return fmt.Errorf("decrement pool count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// GetAllocation 查询单个分配。
func (r *Repository) GetAllocation(ctx context.Context, poolID int64, ip string) (*cluster.IPAllocation, error) {
	q := `SELECT ` + ipAllocColumns + ` FROM vo_cluster_ip_allocations WHERE ip_pool_id=$1 AND ip_address=$2`
	a, err := scanIPAllocation(r.pool.QueryRow(ctx, q, poolID, ip))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cluster.ErrIPAllocationNotFound
		}
		return nil, err
	}
	return a, nil
}

// ListAllocationsByResource 按资源查询分配记录（用于 keep_pod_ip 注入）。
func (r *Repository) ListAllocationsByResource(ctx context.Context, rtype cluster.IPAllocResourceType, resourceID int64) ([]*cluster.IPAllocation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+ipAllocColumns+` FROM vo_cluster_ip_allocations WHERE resource_type=$1 AND resource_id=$2 AND status=$3 ORDER BY replica_index ASC`,
		rtype, resourceID, cluster.IPAllocAllocated)
	if err != nil {
		return nil, fmt.Errorf("query allocations: %w", err)
	}
	defer rows.Close()
	var items []*cluster.IPAllocation
	for rows.Next() {
		a, err := scanIPAllocation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// enumerateCIDR 枚举 CIDR 内所有可用主机 IP（排除网络地址、广播地址、保留 IP）。
func enumerateCIDR(cidr string, reserved []string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	reservedSet := make(map[string]struct{}, len(reserved))
	for _, r := range reserved {
		reservedSet[r] = struct{}{}
	}
	var ips []string
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); incIP(ip) {
		addr := ip.String()
		if _, ok := reservedSet[addr]; ok {
			continue
		}
		ips = append(ips, addr)
	}
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}
	return ips, nil
}

// enumerateCIDRAll 枚举 CIDR 内所有主机 IP（仅排除网络地址与广播地址，
// 保留 IP 也包含在内，由调用方标记 reserved）。供 PreGenerateEntries 用。
func enumerateCIDRAll(cidr string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	var ips []string
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); incIP(ip) {
		ips = append(ips, ip.String())
	}
	if len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}
	return ips, nil
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
