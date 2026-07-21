// Package chatrepo 是 AI 助手对话会话的 Postgres 仓储实现。
package chatrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/chat"
)

// Repository 对话会话仓储。
type Repository struct {
	pool *pgxpool.Pool
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const sessionColumns = "id, uuid, user_id, title, scene, summary, entities, message_count, last_intent, last_active_at, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by"

const messageColumns = `id, uuid, session_id, user_id, role, content, intent, tools, "references", latency_ms, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

// ==================== 会话 ====================

func (r *Repository) CreateSession(ctx context.Context, s *chat.Session) (*chat.Session, error) {
	entities, _ := json.Marshal(s.Entities)
	if string(entities) == "null" || string(entities) == "" {
		entities = []byte("{}")
	}
	query := `
INSERT INTO vo_chat_sessions (user_id, title, scene, summary, entities, message_count, last_intent, last_active_at, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, now(), 1, $8, $8)
RETURNING ` + sessionColumns
	row := r.pool.QueryRow(ctx, query,
		s.UserID, s.Title, s.Scene, s.Summary, entities, s.MessageCount, s.LastIntent, s.CreatedBy)
	return scanSession(row)
}

func (r *Repository) GetSession(ctx context.Context, id int64) (*chat.Session, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_chat_sessions WHERE id = $1 AND deleted = false", sessionColumns)
	row := r.pool.QueryRow(ctx, query, id)
	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, chat.ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *Repository) GetSessionByUUID(ctx context.Context, uuid string) (*chat.Session, error) {
	query := fmt.Sprintf("SELECT %s FROM vo_chat_sessions WHERE uuid = $1 AND deleted = false", sessionColumns)
	row := r.pool.QueryRow(ctx, query, uuid)
	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, chat.ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *Repository) ListSessions(ctx context.Context, userID int64, limit int) ([]*chat.Session, error) {
	if limit <= 0 {
		limit = 20
	}
	query := fmt.Sprintf("SELECT %s FROM vo_chat_sessions WHERE user_id = $1 AND deleted = false ORDER BY last_active_at DESC LIMIT $2", sessionColumns)
	rows, err := r.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat sessions: %w", err)
	}
	defer rows.Close()
	out := make([]*chat.Session, 0)
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateSession(ctx context.Context, s *chat.Session) (*chat.Session, error) {
	entities, _ := json.Marshal(s.Entities)
	if string(entities) == "null" || string(entities) == "" {
		entities = []byte("{}")
	}
	query := `
UPDATE vo_chat_sessions SET
  title = $1, scene = $2, summary = $3, entities = $4, message_count = $5, last_intent = $6,
  last_active_at = now(), version = version + 1, updated_by = $7
WHERE id = $8 AND version = $9 AND deleted = false
RETURNING ` + sessionColumns
	row := r.pool.QueryRow(ctx, query,
		s.Title, s.Scene, s.Summary, entities, s.MessageCount, s.LastIntent, s.UpdatedBy, s.ID, s.Version)
	s2, err := scanSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrConflict
		}
		return nil, err
	}
	return s2, nil
}

func (r *Repository) DeleteSession(ctx context.Context, id int64, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_chat_sessions SET deleted = true, deleted_at = now(), deleted_by = $1, version = version + 1 WHERE id = $2 AND deleted = false`,
		deletedBy, id)
	if err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return chat.ErrSessionNotFound
	}
	// 关联消息软删除。
	_, _ = r.pool.Exec(ctx,
		`UPDATE vo_chat_messages SET deleted = true, deleted_at = now(), deleted_by = $1 WHERE session_id = $2 AND deleted = false`,
		deletedBy, id)
	return nil
}

func scanSession(row pgx.Row) (*chat.Session, error) {
	s := &chat.Session{Entities: map[string]any{}}
	var (
		title          *string
		scene          *string
		summary        *string
		entitiesBytes  []byte
		lastIntent     *string
		createdBy      *int64
		updatedBy      *int64
		deletedBy      *int64
	)
	if err := row.Scan(
		&s.ID, &s.UUID, &s.UserID, &title, &scene, &summary, &entitiesBytes,
		&s.MessageCount, &lastIntent, &s.LastActiveAt,
		&s.Version, &s.CreatedAt, &createdBy, &s.UpdatedAt, &updatedBy,
		&s.Deleted, &s.DeletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if title != nil {
		s.Title = *title
	}
	if scene != nil {
		s.Scene = *scene
	}
	if summary != nil {
		s.Summary = *summary
	}
	if lastIntent != nil {
		s.LastIntent = *lastIntent
	}
	if createdBy != nil {
		s.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		s.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		s.DeletedBy = *deletedBy
	}
	if len(entitiesBytes) > 0 {
		_ = json.Unmarshal(entitiesBytes, &s.Entities)
	}
	if s.Entities == nil {
		s.Entities = map[string]any{}
	}
	return s, nil
}

// ==================== 消息 ====================

func (r *Repository) AppendMessage(ctx context.Context, m *chat.Message) (*chat.Message, error) {
	query := `
INSERT INTO vo_chat_messages (session_id, user_id, role, content, intent, tools, "references", latency_ms, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, $9)
RETURNING ` + messageColumns
	row := r.pool.QueryRow(ctx, query,
		m.SessionID, m.UserID, m.Role, m.Content, m.Intent, m.Tools, m.References, m.LatencyMs, m.CreatedBy)
	m2, err := scanMessage(row)
	if err != nil {
		return nil, err
	}
	// 更新会话的 message_count 与 last_active_at。
	_, _ = r.pool.Exec(ctx,
		`UPDATE vo_chat_sessions SET message_count = message_count + 1, last_active_at = now() WHERE id = $1`,
		m.SessionID)
	return m2, nil
}

func (r *Repository) ListMessages(ctx context.Context, sessionID int64, limit int) ([]*chat.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf("SELECT %s FROM vo_chat_messages WHERE session_id = $1 AND deleted = false ORDER BY id ASC LIMIT $2", messageColumns)
	rows, err := r.pool.Query(ctx, query, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()
	out := make([]*chat.Message, 0)
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMessage(row pgx.Row) (*chat.Message, error) {
	m := &chat.Message{}
	var (
		latencyMs *int
		createdBy *int64
		updatedBy *int64
		deletedBy *int64
	)
	if err := row.Scan(
		&m.ID, &m.UUID, &m.SessionID, &m.UserID, &m.Role, &m.Content, &m.Intent, &m.Tools, &m.References, &latencyMs,
		&m.Version, &m.CreatedAt, &createdBy, &m.UpdatedAt, &updatedBy,
		&m.Deleted, &m.DeletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if latencyMs != nil {
		m.LatencyMs = *latencyMs
	}
	if createdBy != nil {
		m.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		m.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		m.DeletedBy = *deletedBy
	}
	return m, nil
}
