// Package opssessionrepo 是运维会话的 PostgreSQL 仓储实现。
package opssessionrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/opssession"
)

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const sessionColumns = `
	id, uuid, workspace_id, cluster_id, namespace, pod, container, type, status,
	user_id, user_name, client_ip, recording_key, started_at, ended_at,
	duration_ms, version, created_at, updated_at
`

func (r *Repository) Create(ctx context.Context, s *opssession.Session) error {
	now := time.Now()
	if s.UUID == uuid.Nil {
		s.UUID = uuid.New()
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = now
	}
	s.CreatedAt = now
	s.UpdatedAt = now
	s.Version = 1
	const q = `
		INSERT INTO vo_ops_sessions
			(uuid, workspace_id, cluster_id, namespace, pod, container, type, status,
			 user_id, user_name, client_ip, recording_key, started_at, ended_at, duration_ms, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		s.UUID, s.WorkspaceID, s.ClusterID, s.Namespace, s.Pod, s.Container, s.Type, s.Status,
		s.UserID, s.UserName, s.ClientIP, s.RecordingKey, s.StartedAt, nullableTime(s.EndedAt), s.DurationMs, s.Version, s.CreatedAt, s.UpdatedAt,
	).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("create ops session: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*opssession.Session, error) {
	const q = `SELECT ` + sessionColumns + ` FROM vo_ops_sessions WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, opssession.ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *Repository) List(ctx context.Context, q opssession.Query) ([]*opssession.Session, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	const baseTable = `FROM vo_ops_sessions WHERE 1=1`
	base := baseTable
	args := []any{}
	idx := 1
	if q.WorkspaceID != 0 {
		args = append(args, q.WorkspaceID)
		base += fmt.Sprintf(" AND workspace_id = $%d", idx)
		idx++
	}
	if q.ClusterID != 0 {
		args = append(args, q.ClusterID)
		base += fmt.Sprintf(" AND cluster_id = $%d", idx)
		idx++
	}
	if q.UserID != 0 {
		args = append(args, q.UserID)
		base += fmt.Sprintf(" AND user_id = $%d", idx)
		idx++
	}
	if q.Type != "" {
		args = append(args, q.Type)
		base += fmt.Sprintf(" AND type = $%d", idx)
		idx++
	}
	if q.Status != "" {
		args = append(args, q.Status)
		base += fmt.Sprintf(" AND status = $%d", idx)
		idx++
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) "+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, "SELECT "+sessionColumns+" "+base+" ORDER BY started_at DESC LIMIT $"+itoa(idx)+" OFFSET $"+itoa(idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*opssession.Session, 0)
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, nil
}

func (r *Repository) Update(ctx context.Context, s *opssession.Session) error {
	s.UpdatedAt = time.Now()
	s.Version++
	const q = `
		UPDATE vo_ops_sessions SET
			status = $2, recording_key = $3, ended_at = $4, duration_ms = $5, version = $6, updated_at = $7
		WHERE id = $1`
	_, err := r.pool.Exec(ctx, q, s.ID, s.Status, s.RecordingKey, nullableTime(s.EndedAt), s.DurationMs, s.Version, s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update ops session: %w", err)
	}
	return nil
}

// --- scan helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(s scanner) (*opssession.Session, error) {
	var sess opssession.Session
	var endedAt sql.NullTime
	var recordingKey sql.NullString
	var container sql.NullString
	err := s.Scan(
		&sess.ID, &sess.UUID, &sess.WorkspaceID, &sess.ClusterID, &sess.Namespace, &sess.Pod, &container,
		&sess.Type, &sess.Status, &sess.UserID, &sess.UserName, &sess.ClientIP, &recordingKey,
		&sess.StartedAt, &endedAt, &sess.DurationMs, &sess.Version, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	sess.Container = container.String
	sess.RecordingKey = recordingKey.String
	if endedAt.Valid {
		t := endedAt.Time
		sess.EndedAt = &t
	}
	return &sess, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
