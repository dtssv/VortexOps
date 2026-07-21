// Package approvalrepo 是审批领域的 PostgreSQL 仓储实现。
package approvalrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/approval"
)

// Repository 审批 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const approvalColumns = `id, uuid, workspace_id, resource_type, resource_id, operation, requested_by, requested_at,
	approver_role, status, approver_id, approved_at, comment, expires_at, version,
	created_at, created_by, updated_at, updated_by`

func scanApproval(row pgx.Row) (*approval.Approval, error) {
	a := &approval.Approval{}
	var (
		approverID    *int64
		approvedAt    *time.Time
		comment       *string
		expiresAt     *time.Time
		approverRole  *string
		createdBy     *int64
		updatedBy     *int64
	)
	if err := row.Scan(
		&a.ID, &a.UUID, &a.WorkspaceID, &a.ResourceType, &a.ResourceID, &a.Operation,
		&a.RequestedBy, &a.RequestedAt, &approverRole, &a.Status, &approverID, &approvedAt,
		&comment, &expiresAt, &a.Version, &a.CreatedAt, &createdBy, &a.UpdatedAt, &updatedBy,
	); err != nil {
		return nil, err
	}
	if approverID != nil {
		a.ApproverID = *approverID
	}
	a.ApprovedAt = approvedAt
	if comment != nil {
		a.Comment = *comment
	}
	a.ExpiresAt = expiresAt
	if approverRole != nil {
		a.ApproverRole = *approverRole
	}
	if createdBy != nil {
		a.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		a.UpdatedBy = *updatedBy
	}
	return a, nil
}

// Create 创建审批。
func (r *Repository) Create(ctx context.Context, a *approval.Approval) error {
	now := time.Now()
	a.UUID = uuid.New()
	a.Status = approval.StatusPending
	a.RequestedAt = now
	a.Version = 1
	a.CreatedAt = now
	a.UpdatedAt = now
	const q = `INSERT INTO vo_approvals (uuid, workspace_id, resource_type, resource_id, operation,
		requested_by, requested_at, approver_role, status, comment, expires_at, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`
	if err := r.pool.QueryRow(ctx, q,
		a.UUID, a.WorkspaceID, a.ResourceType, a.ResourceID, a.Operation,
		a.RequestedBy, a.RequestedAt, nullableStr(a.ApproverRole), a.Status, nullableStr(a.Comment), a.ExpiresAt,
		a.Version, now, nullableInt64(a.CreatedBy), now, nullableInt64(a.UpdatedBy),
	).Scan(&a.ID); err != nil {
		return fmt.Errorf("insert approval: %w", err)
	}
	return nil
}

// GetByID 按 ID 查询审批。
func (r *Repository) GetByID(ctx context.Context, id int64) (*approval.Approval, error) {
	const q = `SELECT ` + approvalColumns + ` FROM vo_approvals WHERE id=$1 AND deleted=false`
	a, err := scanApproval(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, approval.ErrApprovalNotFound
		}
		return nil, fmt.Errorf("get approval: %w", err)
	}
	return a, nil
}

// List 分页查询审批。
func (r *Repository) List(ctx context.Context, q approval.Query) ([]*approval.Approval, int64, error) {
	conds := []string{"deleted=false"}
	args := []any{}
	if q.WorkspaceID > 0 {
		args = append(args, q.WorkspaceID)
		conds = append(conds, fmt.Sprintf("workspace_id=$%d", len(args)))
	}
	if q.ResourceType != "" {
		args = append(args, q.ResourceType)
		conds = append(conds, fmt.Sprintf("resource_type=$%d", len(args)))
	}
	if q.Status != "" {
		args = append(args, q.Status)
		conds = append(conds, fmt.Sprintf("status=$%d", len(args)))
	}
	where := joinConds(conds)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_approvals WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count approvals: %w", err)
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		`SELECT `+approvalColumns+` FROM vo_approvals WHERE `+where+` ORDER BY requested_at DESC LIMIT $`+itoa(len(args)-1)+` OFFSET $`+itoa(len(args)),
		args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()
	var out []*approval.Approval
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan approval: %w", err)
		}
		out = append(out, a)
	}
	return out, total, nil
}

// Update 更新审批（乐观锁）。
func (r *Repository) Update(ctx context.Context, a *approval.Approval) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_approvals SET status=$1, approver_id=$2, approved_at=$3, comment=$4, version=version+1, updated_at=now(), updated_by=$5
		 WHERE id=$6 AND version=$7 AND deleted=false`,
		a.Status, nullableInt64(a.ApproverID), a.ApprovedAt, nullableStr(a.Comment), nullableInt64(a.UpdatedBy), a.ID, a.Version)
	if err != nil {
		return fmt.Errorf("update approval: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	a.Version++
	return nil
}

// GetPendingByResource 查询资源的待审批记录。
func (r *Repository) GetPendingByResource(ctx context.Context, rt approval.ResourceType, resourceID int64) (*approval.Approval, error) {
	const q = `SELECT ` + approvalColumns + ` FROM vo_approvals
		WHERE resource_type=$1 AND resource_id=$2 AND status='pending' AND deleted=false LIMIT 1`
	a, err := scanApproval(r.pool.QueryRow(ctx, q, rt, resourceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pending approval: %w", err)
	}
	return a, nil
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

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
