// Package auditrepo 是审计日志领域的 PostgreSQL 仓储实现。
package auditrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/audit"
)

// Repository 审计日志 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

const logColumns = `id, uuid, user_id, user_name, workspace_id, resource_type, resource_id, resource_name, action,
	operation, request_id, method, path, status_code, client_ip, user_agent, request_body, response_summary,
	duration_ms, error_message, created_at`

func scanLog(row pgx.Row) (*audit.Log, error) {
	l := &audit.Log{RequestBody: map[string]any{}, ResponseSummary: map[string]any{}}
	var (
		userID          *int64
		userName        *string
		workspaceID     *int64
		resourceID      *int64
		resourceName    *string
		operation       *string
		requestID       *string
		method          *string
		path            *string
		statusCode      *int
		clientIP        *string
		userAgent       *string
		requestBody     []byte
		responseSummary []byte
		durationMs      *int
		errorMessage    *string
	)
	if err := row.Scan(
		&l.ID, &l.UUID, &userID, &userName, &workspaceID, &l.ResourceType, &resourceID, &resourceName, &l.Action,
		&operation, &requestID, &method, &path, &statusCode, &clientIP, &userAgent, &requestBody, &responseSummary,
		&durationMs, &errorMessage, &l.CreatedAt,
	); err != nil {
		return nil, err
	}
	if userID != nil {
		l.UserID = *userID
	}
	if userName != nil {
		l.UserName = *userName
	}
	if workspaceID != nil {
		l.WorkspaceID = *workspaceID
	}
	if resourceID != nil {
		l.ResourceID = *resourceID
	}
	if resourceName != nil {
		l.ResourceName = *resourceName
	}
	if operation != nil {
		l.Operation = *operation
	}
	if requestID != nil {
		l.RequestID = *requestID
	}
	if method != nil {
		l.Method = *method
	}
	if path != nil {
		l.Path = *path
	}
	if statusCode != nil {
		l.StatusCode = *statusCode
	}
	if clientIP != nil {
		l.ClientIP = *clientIP
	}
	if userAgent != nil {
		l.UserAgent = *userAgent
	}
	if requestBody != nil {
		_ = json.Unmarshal(requestBody, &l.RequestBody)
	}
	if responseSummary != nil {
		_ = json.Unmarshal(responseSummary, &l.ResponseSummary)
	}
	if durationMs != nil {
		l.DurationMs = *durationMs
	}
	if errorMessage != nil {
		l.ErrorMessage = *errorMessage
	}
	return l, nil
}

// Append 追加审计日志。
func (r *Repository) Append(ctx context.Context, log *audit.Log) error {
	if log.UUID == uuid.Nil {
		log.UUID = uuid.New()
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = r.now()
	}
	if log.RequestBody == nil {
		log.RequestBody = map[string]any{}
	}
	if log.ResponseSummary == nil {
		log.ResponseSummary = map[string]any{}
	}
	reqBody, _ := json.Marshal(log.RequestBody)
	respSummary, _ := json.Marshal(log.ResponseSummary)
	const q = `INSERT INTO vo_audit_logs
		(uuid, user_id, user_name, workspace_id, resource_type, resource_id, resource_name, action, operation,
		 request_id, method, path, status_code, client_ip, user_agent, request_body, response_summary, duration_ms,
		 error_message, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		log.UUID, nullableInt64(log.UserID), nullableStr(log.UserName), nullableInt64(log.WorkspaceID),
		log.ResourceType, nullableInt64(log.ResourceID), nullableStr(log.ResourceName), log.Action,
		nullableStr(log.Operation), nullableStr(log.RequestID), nullableStr(log.Method), nullableStr(log.Path),
		nullableInt(log.StatusCode), nullableStr(log.ClientIP), nullableStr(log.UserAgent), reqBody, respSummary,
		nullableInt(log.DurationMs), nullableStr(log.ErrorMessage), log.CreatedAt,
	).Scan(&log.ID)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

// GetByID 按 ID 查询审计日志。
func (r *Repository) GetByID(ctx context.Context, id int64) (*audit.Log, error) {
	q := `SELECT ` + logColumns + ` FROM vo_audit_logs WHERE id=$1`
	l, err := scanLog(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, audit.ErrLogNotFound
		}
		return nil, err
	}
	return l, nil
}

// List 分页查询审计日志。
func (r *Repository) List(ctx context.Context, q audit.Query) ([]*audit.Log, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	var (
		conds []string
		args  []any
	)
	if q.UserID != 0 {
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, q.UserID)
	}
	if q.WorkspaceID != 0 {
		conds = append(conds, fmt.Sprintf("workspace_id = $%d", len(args)+1))
		args = append(args, q.WorkspaceID)
	}
	if q.ResourceType != "" {
		conds = append(conds, fmt.Sprintf("resource_type = $%d", len(args)+1))
		args = append(args, q.ResourceType)
	}
	if q.Action != "" {
		conds = append(conds, fmt.Sprintf("action = $%d", len(args)+1))
		args = append(args, q.Action)
	}
	if !q.StartTime.IsZero() {
		conds = append(conds, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, q.StartTime)
	}
	if !q.EndTime.IsZero() {
		conds = append(conds, fmt.Sprintf("created_at < $%d", len(args)+1))
		args = append(args, q.EndTime)
	}
	where := "true"
	if len(conds) > 0 {
		where = ""
		for i, c := range conds {
			if i > 0 {
				where += " AND "
			}
			where += c
		}
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_audit_logs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit logs: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_audit_logs WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		logColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()
	var items []*audit.Log
	for rows.Next() {
		l, err := scanLog(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, l)
	}
	return items, total, rows.Err()
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
