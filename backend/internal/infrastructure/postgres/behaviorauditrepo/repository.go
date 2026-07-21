// Package behaviorauditrepo 是行为审计的 PostgreSQL 仓储实现。
package behaviorauditrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/behavioraudit"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Append(ctx context.Context, l *behavioraudit.Log) error {
	if l.UUID == uuid.Nil {
		l.UUID = uuid.New()
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now()
	}
	if l.RiskLevel == "" {
		l.RiskLevel = behavioraudit.RiskInfo
	}
	const q = `
		INSERT INTO vo_behavior_audit_logs
			(uuid, workspace_id, session_id, cluster_id, namespace, pod, user_id, user_name, command, risk_level, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		l.UUID, l.WorkspaceID, nullableInt64(l.SessionID), l.ClusterID, l.Namespace, l.Pod,
		l.UserID, l.UserName, l.Command, l.RiskLevel, l.CreatedAt,
	).Scan(&l.ID)
	if err != nil {
		return fmt.Errorf("append behavior audit: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context, q behavioraudit.Query) ([]*behavioraudit.Log, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	const baseTable = `FROM vo_behavior_audit_logs WHERE 1=1`
	base := baseTable
	args := []any{}
	idx := 1
	if q.WorkspaceID != 0 {
		args = append(args, q.WorkspaceID)
		base += fmt.Sprintf(" AND workspace_id = $%d", idx)
		idx++
	}
	if q.SessionID != 0 {
		args = append(args, q.SessionID)
		base += fmt.Sprintf(" AND session_id = $%d", idx)
		idx++
	}
	if q.UserID != 0 {
		args = append(args, q.UserID)
		base += fmt.Sprintf(" AND user_id = $%d", idx)
		idx++
	}
	if q.ClusterID != 0 {
		args = append(args, q.ClusterID)
		base += fmt.Sprintf(" AND cluster_id = $%d", idx)
		idx++
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx,
		"SELECT id, uuid, workspace_id, session_id, cluster_id, namespace, pod, user_id, user_name, command, risk_level, created_at "+base+
			" ORDER BY created_at DESC LIMIT $"+itoa(idx)+" OFFSET $"+itoa(idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*behavioraudit.Log, 0)
	for rows.Next() {
		var l behavioraudit.Log
		var sessionID *int64
		if err := rows.Scan(&l.ID, &l.UUID, &l.WorkspaceID, &sessionID, &l.ClusterID, &l.Namespace, &l.Pod,
			&l.UserID, &l.UserName, &l.Command, &l.RiskLevel, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		if sessionID != nil {
			l.SessionID = *sessionID
		}
		out = append(out, &l)
	}
	return out, total, nil
}

func nullableInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
