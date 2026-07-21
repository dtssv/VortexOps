// Package clusteropsrepo 是集群运维领域的 PostgreSQL 仓储实现。
package clusteropsrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/clusterops"
)

// Repository 集群运维 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const operationColumns = `id, uuid, cluster_id, node_name, operation_type, scheduled_at, status,
	executed_at, completed_at, error_message, notify_affected, notified_user_ids, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

const nodeStatusColumns = `id, uuid, cluster_id, node_name, status, unschedulable, kubelet_version,
	allocatable_cpu_m, allocatable_memory_bytes, allocatable_gpu,
	used_cpu_m, used_memory_bytes, used_gpu, pod_count, abnormal_pod_count,
	roles, taints, addresses, last_synced_at, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanOperation(row pgx.Row) (*clusterops.Operation, error) {
	o := &clusterops.Operation{}
	var (
		nodeName      *string
		errorMessage  *string
		executedAt    *time.Time
		completedAt   *time.Time
		createdBy     *int64
		updatedBy     *int64
		deletedAt     *time.Time
		deletedBy     *int64
	)
	if err := row.Scan(
		&o.ID, &o.UUID, &o.ClusterID, &nodeName, &o.OperationType, &o.ScheduledAt, &o.Status,
		&executedAt, &completedAt, &errorMessage, &o.NotifyAffected, &o.NotifiedUserIDs, &o.Version,
		&o.CreatedAt, &createdBy, &o.UpdatedAt, &updatedBy, &o.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if nodeName != nil {
		o.NodeName = *nodeName
	}
	if errorMessage != nil {
		o.ErrorMessage = *errorMessage
	}
	o.ExecutedAt = executedAt
	o.CompletedAt = completedAt
	if createdBy != nil {
		o.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		o.UpdatedBy = *updatedBy
	}
	o.DeletedAt = deletedAt
	if deletedBy != nil {
		o.DeletedBy = *deletedBy
	}
	if o.NotifiedUserIDs == nil {
		o.NotifiedUserIDs = []int64{}
	}
	return o, nil
}

func scanNodeStatus(row pgx.Row) (*clusterops.NodeStatus, error) {
	ns := &clusterops.NodeStatus{}
	var (
		kubeletVersion *string
		lastSyncedAt   *time.Time
		rolesRaw       []byte
		taintsRaw      []byte
		addressesRaw   []byte
		createdBy      *int64
		updatedBy      *int64
		deletedAt      *time.Time
		deletedBy      *int64
	)
	if err := row.Scan(
		&ns.ID, &ns.UUID, &ns.ClusterID, &ns.NodeName, &ns.Status, &ns.Unschedulable, &kubeletVersion,
		&ns.AllocatableCPUM, &ns.AllocatableMemoryBytes, &ns.AllocatableGPU,
		&ns.UsedCPUM, &ns.UsedMemoryBytes, &ns.UsedGPU, &ns.PodCount, &ns.AbnormalPodCount,
		&rolesRaw, &taintsRaw, &addressesRaw, &lastSyncedAt, &ns.Version,
		&ns.CreatedAt, &createdBy, &ns.UpdatedAt, &updatedBy, &ns.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if kubeletVersion != nil {
		ns.KubeletVersion = *kubeletVersion
	}
	ns.LastSyncedAt = lastSyncedAt
	if createdBy != nil {
		ns.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		ns.UpdatedBy = *updatedBy
	}
	ns.DeletedAt = deletedAt
	if deletedBy != nil {
		ns.DeletedBy = *deletedBy
	}
	ns.Roles = decodeStringSlice(rolesRaw)
	ns.Taints = decodeMapSlice(taintsRaw)
	ns.Addresses = decodeMapSlice(addressesRaw)
	return ns, nil
}

// ============================================================================
// Operation
// ============================================================================

// CreateOperation 创建运维任务。
func (r *Repository) CreateOperation(ctx context.Context, op *clusterops.Operation) error {
	if op.UUID == uuid.Nil {
		op.UUID = uuid.New()
	}
	now := r.now()
	if op.CreatedAt.IsZero() {
		op.CreatedAt = now
	}
	op.UpdatedAt = now
	if op.NotifiedUserIDs == nil {
		op.NotifiedUserIDs = []int64{}
	}
	const q = `INSERT INTO vo_cluster_operations
		(uuid, cluster_id, node_name, operation_type, scheduled_at, status, error_message,
		 notify_affected, notified_user_ids, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		op.UUID, op.ClusterID, nullableStr(op.NodeName), op.OperationType, op.ScheduledAt,
		op.Status, nullableStr(op.ErrorMessage), op.NotifyAffected, op.NotifiedUserIDs,
		op.Version, op.CreatedAt, nullableInt64(op.CreatedBy), op.UpdatedAt, nullableInt64(op.UpdatedBy),
	).Scan(&op.ID)
	if err != nil {
		return fmt.Errorf("insert cluster operation: %w", err)
	}
	return nil
}

// GetOperation 按 ID 查询运维任务。
func (r *Repository) GetOperation(ctx context.Context, id int64) (*clusterops.Operation, error) {
	q := `SELECT ` + operationColumns + ` FROM vo_cluster_operations WHERE id=$1 AND deleted=false`
	op, err := scanOperation(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, clusterops.ErrOperationNotFound
		}
		return nil, err
	}
	return op, nil
}

// UpdateOperation 更新运维任务（状态/时间/错误/已通知用户）。
func (r *Repository) UpdateOperation(ctx context.Context, op *clusterops.Operation) error {
	op.UpdatedAt = r.now()
	op.Version++
	const q = `UPDATE vo_cluster_operations SET
		node_name=$2, status=$3, executed_at=$4, completed_at=$5, error_message=$6,
		notify_affected=$7, notified_user_ids=$8, version=$9, updated_at=$10, updated_by=$11
		WHERE id=$1 AND deleted=false`
	ct, err := r.pool.Exec(ctx, q,
		op.ID, nullableStr(op.NodeName), op.Status, op.ExecutedAt, op.CompletedAt,
		nullableStr(op.ErrorMessage), op.NotifyAffected, op.NotifiedUserIDs,
		op.Version, op.UpdatedAt, nullableInt64(op.UpdatedBy),
	)
	if err != nil {
		return fmt.Errorf("update cluster operation: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return clusterops.ErrOperationNotFound
	}
	return nil
}

// ListOperations 分页查询运维任务。
func (r *Repository) ListOperations(ctx context.Context, q clusterops.OperationQuery) ([]*clusterops.Operation, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var conds []string
	args := []any{}
	conds = append(conds, "deleted=false")
	if q.ClusterID != 0 {
		conds = append(conds, fmt.Sprintf("cluster_id = $%d", len(args)+1))
		args = append(args, q.ClusterID)
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, q.Status)
	}
	if q.OperationType != "" {
		conds = append(conds, fmt.Sprintf("operation_type = $%d", len(args)+1))
		args = append(args, q.OperationType)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_cluster_operations WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+operationColumns+` FROM vo_cluster_operations WHERE `+where+
			` ORDER BY scheduled_at DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var items []*clusterops.Operation
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, op)
	}
	return items, total, rows.Err()
}

// ListDueOperations 列出到期待执行的 pending 任务。
func (r *Repository) ListDueOperations(ctx context.Context, before time.Time, limit int) ([]*clusterops.Operation, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+operationColumns+` FROM vo_cluster_operations
		 WHERE deleted=false AND status='pending' AND scheduled_at <= $1
		 ORDER BY scheduled_at ASC LIMIT $2`,
		before, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*clusterops.Operation
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, op)
	}
	return items, rows.Err()
}

// ============================================================================
// NodeStatus
// ============================================================================

// UpsertNodeStatus 插入或更新节点状态（按 cluster_id+node_name 唯一）。
func (r *Repository) UpsertNodeStatus(ctx context.Context, in clusterops.UpsertNodeStatusInput) (*clusterops.NodeStatus, error) {
	now := r.now()
	if in.Roles == nil {
		in.Roles = []string{}
	}
	if in.Taints == nil {
		in.Taints = []map[string]any{}
	}
	if in.Addresses == nil {
		in.Addresses = []map[string]any{}
	}
	rolesJSON, err := json.Marshal(in.Roles)
	if err != nil {
		return nil, fmt.Errorf("marshal roles: %w", err)
	}
	taintsJSON, err := json.Marshal(in.Taints)
	if err != nil {
		return nil, fmt.Errorf("marshal taints: %w", err)
	}
	addressesJSON, err := json.Marshal(in.Addresses)
	if err != nil {
		return nil, fmt.Errorf("marshal addresses: %w", err)
	}
	const q = `INSERT INTO vo_cluster_node_status
		(uuid, cluster_id, node_name, status, unschedulable, kubelet_version,
		 allocatable_cpu_m, allocatable_memory_bytes, allocatable_gpu,
		 used_cpu_m, used_memory_bytes, used_gpu, pod_count, abnormal_pod_count,
		 roles, taints, addresses, last_synced_at, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (cluster_id, node_name) WHERE deleted=false DO UPDATE SET
		 status=EXCLUDED.status, unschedulable=EXCLUDED.unschedulable, kubelet_version=EXCLUDED.kubelet_version,
		 allocatable_cpu_m=EXCLUDED.allocatable_cpu_m, allocatable_memory_bytes=EXCLUDED.allocatable_memory_bytes,
		 allocatable_gpu=EXCLUDED.allocatable_gpu,
		 used_cpu_m=EXCLUDED.used_cpu_m, used_memory_bytes=EXCLUDED.used_memory_bytes, used_gpu=EXCLUDED.used_gpu,
		 pod_count=EXCLUDED.pod_count, abnormal_pod_count=EXCLUDED.abnormal_pod_count,
		 roles=EXCLUDED.roles, taints=EXCLUDED.taints, addresses=EXCLUDED.addresses,
		 last_synced_at=EXCLUDED.last_synced_at, version=vo_cluster_node_status.version+1,
		 updated_at=EXCLUDED.updated_at
		RETURNING ` + nodeStatusColumns
	row := r.pool.QueryRow(ctx, q,
		uuid.New(), in.ClusterID, in.NodeName, in.Status, in.Unschedulable, nullableStr(in.KubeletVersion),
		in.AllocatableCPUM, in.AllocatableMemoryBytes, in.AllocatableGPU,
		in.UsedCPUM, in.UsedMemoryBytes, in.UsedGPU, in.PodCount, in.AbnormalPodCount,
		rolesJSON, taintsJSON, addressesJSON, now, 1, now, now,
	)
	ns, err := scanNodeStatus(row)
	if err != nil {
		return nil, fmt.Errorf("upsert cluster node status: %w", err)
	}
	return ns, nil
}

// ListNodeStatuses 列出集群下所有节点状态。
func (r *Repository) ListNodeStatuses(ctx context.Context, clusterID int64) ([]*clusterops.NodeStatus, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+nodeStatusColumns+` FROM vo_cluster_node_status
		 WHERE cluster_id=$1 AND deleted=false ORDER BY node_name ASC`,
		clusterID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*clusterops.NodeStatus
	for rows.Next() {
		ns, err := scanNodeStatus(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, ns)
	}
	return items, rows.Err()
}

// DeleteNodeStatuses 软删除不在 keep 集合中的节点状态（节点已下线时清理）。
func (r *Repository) DeleteNodeStatuses(ctx context.Context, clusterID int64, keep map[string]struct{}) error {
	if len(keep) == 0 {
		return nil
	}
	now := r.now()
	// 简化实现：先查所有，再逐个软删除不在 keep 中的。
	rows, err := r.pool.Query(ctx,
		`SELECT id, node_name FROM vo_cluster_node_status WHERE cluster_id=$1 AND deleted=false`,
		clusterID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rec struct {
		id    int64
		nodeN string
	}
	var toDelete []int64
	for rows.Next() {
		var r2 rec
		if err := rows.Scan(&r2.id, &r2.nodeN); err != nil {
			return err
		}
		if _, ok := keep[r2.nodeN]; !ok {
			toDelete = append(toDelete, r2.id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range toDelete {
		_, _ = r.pool.Exec(ctx,
			`UPDATE vo_cluster_node_status SET deleted=true, deleted_at=$2, updated_at=$2 WHERE id=$1`,
			id, now,
		)
	}
	return nil
}

// ============================================================================
// 辅助函数
// ============================================================================

func joinConds(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

func nullableInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func nullableStr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func decodeStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return []string{}
	}
	return out
}

func decodeMapSlice(raw []byte) []map[string]any {
	if len(raw) == 0 {
		return []map[string]any{}
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return []map[string]any{}
	}
	return out
}
