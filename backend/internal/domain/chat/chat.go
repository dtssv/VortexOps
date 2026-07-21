// Package chat 定义 AI 助手对话会话领域模型。
// 会话持久化多轮对话、上下文摘要与实体记忆，支持跨会话保留。
package chat

import (
	"context"
	"errors"
	"time"

	"github.com/vortexops/vortexops/internal/domain"
)

// Session 对话会话。
type Session struct {
	ID             int64
	UUID           string
	UserID         int64
	Title          string
	Scene          string
	Summary        string                 // 长会话的上下文摘要（LLM 生成）
	Entities       map[string]any         // 实体记忆：{build_id:123, app_id:456, ...}
	MessageCount   int
	LastIntent     string
	LastActiveAt   time.Time
	domain.Audit
}

// Message 对话消息。
type Message struct {
	ID         int64
	UUID       string
	SessionID  int64
	UserID     int64
	Role       string // user / assistant / system
	Content    string
	Intent     *string // JSON 字符串（assistant 消息的意图识别结果）
	Tools      *string // JSON 字符串（assistant 消息的工具调用记录）
	References *string // JSON 字符串（assistant 消息的 RAG 引用）
	LatencyMs  int
	domain.Audit
}

// Repository 对话会话仓储接口。
type Repository interface {
	// 会话
	CreateSession(ctx context.Context, s *Session) (*Session, error)
	GetSession(ctx context.Context, id int64) (*Session, error)
	GetSessionByUUID(ctx context.Context, uuid string) (*Session, error)
	ListSessions(ctx context.Context, userID int64, limit int) ([]*Session, error)
	UpdateSession(ctx context.Context, s *Session) (*Session, error)
	DeleteSession(ctx context.Context, id int64, deletedBy int64) error

	// 消息
	AppendMessage(ctx context.Context, m *Message) (*Message, error)
	ListMessages(ctx context.Context, sessionID int64, limit int) ([]*Message, error)
}

// 领域错误。
var (
	ErrSessionNotFound = errors.New("chat session not found")
)
