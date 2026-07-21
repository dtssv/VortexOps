// Package collabrepo 是协作领域的 PostgreSQL 仓储实现（通知、收藏）。
package collabrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain/collab"
)

const pgUniqueViolation = "23505"

// Repository 协作领域 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

// --- 通知 ---

const notifColumns = `id, uuid, user_id, channel, template_code, recipient, subject, body, payload, status, sent_at,
	read_at, error_message, created_at`

func scanNotification(row pgx.Row) (*collab.Notification, error) {
	n := &collab.Notification{Payload: map[string]any{}}
	var (
		userID      *int64
		templateCode *string
		recipient   *string
		subject     *string
		body        *string
		payload     []byte
		sentAt      *time.Time
		readAt      *time.Time
		errorMessage *string
	)
	if err := row.Scan(
		&n.ID, &n.UUID, &userID, &n.Channel, &templateCode, &recipient, &subject, &body, &payload, &n.Status,
		&sentAt, &readAt, &errorMessage, &n.CreatedAt,
	); err != nil {
		return nil, err
	}
	if userID != nil {
		n.UserID = *userID
	}
	if templateCode != nil {
		n.TemplateCode = *templateCode
	}
	if recipient != nil {
		n.Recipient = *recipient
	}
	if subject != nil {
		n.Subject = *subject
	}
	if body != nil {
		n.Body = *body
	}
	if payload != nil {
		_ = json.Unmarshal(payload, &n.Payload)
	}
	if sentAt != nil {
		n.SentAt = sentAt
	}
	if readAt != nil {
		n.ReadAt = readAt
	}
	if errorMessage != nil {
		n.ErrorMessage = *errorMessage
	}
	return n, nil
}

// CreateNotification 创建通知。
func (r *Repository) CreateNotification(ctx context.Context, n *collab.Notification) error {
	if n.UUID == uuid.Nil {
		n.UUID = uuid.New()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = r.now()
	}
	if n.Status == "" {
		n.Status = collab.NotifPending
	}
	if n.Payload == nil {
		n.Payload = map[string]any{}
	}
	payloadBytes, _ := json.Marshal(n.Payload)
	const q = `INSERT INTO vo_notifications
		(uuid, user_id, channel, template_code, recipient, subject, body, payload, status, sent_at, read_at, error_message, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		n.UUID, nullableInt64(n.UserID), n.Channel, nullableStr(n.TemplateCode), nullableStr(n.Recipient),
		nullableStr(n.Subject), nullableStr(n.Body), payloadBytes, n.Status, n.SentAt, n.ReadAt,
		nullableStr(n.ErrorMessage), n.CreatedAt,
	).Scan(&n.ID)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}
	return nil
}

// GetNotification 按 ID 查询通知。
func (r *Repository) GetNotification(ctx context.Context, id int64) (*collab.Notification, error) {
	q := `SELECT ` + notifColumns + ` FROM vo_notifications WHERE id=$1`
	n, err := scanNotification(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, collab.ErrNotificationNotFound
		}
		return nil, err
	}
	return n, nil
}

// ListNotifications 分页查询通知。
func (r *Repository) ListNotifications(ctx context.Context, q collab.NotificationQuery) ([]*collab.Notification, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds []string
		args  []any
	)
	if q.UserID != 0 {
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, q.UserID)
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, q.Status)
	}
	if q.UnreadOnly {
		conds = append(conds, "read_at IS NULL")
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
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_notifications WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_notifications WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		notifColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()
	var items []*collab.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, n)
	}
	return items, total, rows.Err()
}

// MarkRead 标记单条通知已读。
func (r *Repository) MarkRead(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_notifications SET read_at=$1, status='read' WHERE id=$2 AND read_at IS NULL`,
		r.now(), id)
	if err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return collab.ErrNotificationNotFound
	}
	return nil
}

// MarkAllRead 标记用户所有未读通知已读。
func (r *Repository) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_notifications SET read_at=$1, status='read' WHERE user_id=$2 AND read_at IS NULL`,
		r.now(), userID)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	return nil
}

// CountUnread 统计用户未读通知数。
func (r *Repository) CountUnread(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_notifications WHERE user_id=$1 AND read_at IS NULL`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}
	return count, nil
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
