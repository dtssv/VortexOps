// Package chatapp 是 AI 助手对话会话的应用服务层。
// 提供会话持久化、上下文摘要与实体记忆。
package chatapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/chat"
	"github.com/vortexops/vortexops/internal/platform/llm"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Service 对话会话应用服务。
type Service struct {
	repo chat.Repository
	llm  llm.ChatClient
}

// New 创建服务。
func New(repo chat.Repository, llm llm.ChatClient) *Service {
	return &Service{repo: repo, llm: llm}
}

// CreateSessionInput 创建会话输入。
type CreateSessionInput struct {
	UserID  int64
	Title   string
	Scene   string
	ActorID int64
}

// CreateSession 创建会话。
func (s *Service) CreateSession(ctx context.Context, in CreateSessionInput) (*chat.Session, error) {
	if in.Title == "" {
		in.Title = "新对话"
	}
	sess := &chat.Session{
		UserID: in.UserID, Title: in.Title, Scene: in.Scene,
		Entities: map[string]any{}, Audit: domain.Audit{CreatedBy: in.ActorID, UpdatedBy: in.ActorID},
	}
	out, err := s.repo.CreateSession(ctx, sess)
	if err != nil {
		return nil, apperr.Internal("create chat session", err)
	}
	return out, nil
}

// GetSession 获取会话。
func (s *Service) GetSession(ctx context.Context, id int64) (*chat.Session, error) {
	sess, err := s.repo.GetSession(ctx, id)
	if err != nil {
		if errors.Is(err, chat.ErrSessionNotFound) {
			return nil, apperr.NotFound("chat session", fmt.Sprintf("%d", id))
		}
		return nil, apperr.Internal("get chat session", err)
	}
	return sess, nil
}

// ListSessions 列出用户的会话。
func (s *Service) ListSessions(ctx context.Context, userID int64, limit int) ([]*chat.Session, error) {
	items, err := s.repo.ListSessions(ctx, userID, limit)
	if err != nil {
		return nil, apperr.Internal("list chat sessions", err)
	}
	return items, nil
}

// DeleteSession 删除会话。
func (s *Service) DeleteSession(ctx context.Context, id int64, actorID int64) error {
	if err := s.repo.DeleteSession(ctx, id, actorID); err != nil {
		if errors.Is(err, chat.ErrSessionNotFound) {
			return apperr.NotFound("chat session", fmt.Sprintf("%d", id))
		}
		return apperr.Internal("delete chat session", err)
	}
	return nil
}

// ListMessages 列出会话消息。
func (s *Service) ListMessages(ctx context.Context, sessionID int64, limit int) ([]*chat.Message, error) {
	items, err := s.repo.ListMessages(ctx, sessionID, limit)
	if err != nil {
		return nil, apperr.Internal("list chat messages", err)
	}
	return items, nil
}

// AppendMessageInput 追加消息输入。
type AppendMessageInput struct {
	SessionID  int64
	UserID     int64
	Role       string
	Content    string
	Intent     any  // 原始对象，序列化为 JSON 存储
	Tools      any
	References any
	LatencyMs  int
	ActorID    int64
}

// AppendMessage 追加一条消息并更新会话元信息。
func (s *Service) AppendMessage(ctx context.Context, in AppendMessageInput) (*chat.Message, error) {
	m := &chat.Message{
		SessionID: in.SessionID, UserID: in.UserID, Role: in.Role, Content: in.Content,
		LatencyMs: in.LatencyMs, Audit: domain.Audit{CreatedBy: in.ActorID, UpdatedBy: in.ActorID},
	}
	if in.Intent != nil {
		if b, err := json.Marshal(in.Intent); err == nil {
			s := string(b)
			m.Intent = &s
		}
	}
	if in.Tools != nil {
		if b, err := json.Marshal(in.Tools); err == nil {
			s := string(b)
			m.Tools = &s
		}
	}
	if in.References != nil {
		if b, err := json.Marshal(in.References); err == nil {
			s := string(b)
			m.References = &s
		}
	}
	out, err := s.repo.AppendMessage(ctx, m)
	if err != nil {
		return nil, apperr.Internal("append chat message", err)
	}
	// 若是 user 消息且会话标题为默认值，则用消息内容生成标题。
	if in.Role == "user" {
		go func(sessionID int64, content string, userID int64) {
			bgCtx := context.Background()
			s.maybeUpdateTitle(bgCtx, sessionID, content, userID)
		}(in.SessionID, in.Content, in.UserID)
	}
	return out, nil
}

// maybeUpdateTitle 若会话标题仍为默认值，则根据首条 user 消息生成标题。
func (s *Service) maybeUpdateTitle(ctx context.Context, sessionID int64, content string, userID int64) {
	sess, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	if sess.Title != "新对话" && sess.Title != "" {
		return
	}
	title := generateTitle(content)
	if title == "" {
		return
	}
	sess.Title = title
	sess.UpdatedBy = userID
	_, _ = s.repo.UpdateSession(ctx, sess)
}

// generateTitle 从用户消息生成会话标题。取首行前 30 字符。
func generateTitle(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	// 取首行。
	if idx := strings.IndexAny(content, "\n\r"); idx > 0 {
		content = content[:idx]
	}
	content = strings.TrimSpace(content)
	runeCount := 0
	for i := range content {
		if runeCount >= 30 {
			content = content[:i] + "..."
			break
		}
		runeCount++
	}
	return content
}

// UpdateSessionMeta 更新会话元信息（场景、实体记忆、最近意图）。
func (s *Service) UpdateSessionMeta(ctx context.Context, sessionID int64, scene string, entities map[string]any, lastIntent string, actorID int64) error {
	sess, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, chat.ErrSessionNotFound) {
			return apperr.NotFound("chat session", fmt.Sprintf("%d", sessionID))
		}
		return apperr.Internal("get chat session", err)
	}
	if scene != "" {
		sess.Scene = scene
	}
	if entities != nil {
		// 合并而非覆盖：保留已有实体。
		if sess.Entities == nil {
			sess.Entities = map[string]any{}
		}
		for k, v := range entities {
			sess.Entities[k] = v
		}
	}
	if lastIntent != "" {
		sess.LastIntent = lastIntent
	}
	sess.UpdatedBy = actorID
	if _, err := s.repo.UpdateSession(ctx, sess); err != nil {
		return apperr.Internal("update chat session", err)
	}
	return nil
}

// SummarizeIfNeeded 当会话消息数超过阈值时，由 LLM 生成上下文摘要。
// 摘要会替换 session.summary，便于长会话压缩上下文。
func (s *Service) SummarizeIfNeeded(ctx context.Context, sessionID int64, threshold int) error {
	if s.llm == nil || threshold <= 0 {
		return nil
	}
	sess, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.MessageCount < threshold {
		return nil
	}
	msgs, err := s.repo.ListMessages(ctx, sessionID, threshold*2)
	if err != nil {
		return err
	}
	if len(msgs) < threshold {
		return nil
	}
	// 拼接最近 threshold*2 条消息作为摘要输入。
	var sb strings.Builder
	for _, m := range msgs {
		role := "用户"
		if m.Role == "assistant" {
			role = "助手"
		}
		fmt.Fprintf(&sb, "%s: %s\n", role, m.Content)
	}
	prompt := fmt.Sprintf(`请将以下对话压缩为不超过 300 字的摘要，保留关键问题、结论、涉及实体（如应用名/构建 ID/Pod 名）：
%s`, sb.String())
	summary, err := s.llm.Chat(ctx, "你是对话摘要助手。", prompt)
	if err != nil {
		return err
	}
	sess.Summary = summary
	sess.UpdatedBy = sess.UserID
	_, err = s.repo.UpdateSession(ctx, sess)
	return err
}

// BuildContext 构造对话上下文，用于注入到 LLM prompt。
// 包含：会话摘要、实体记忆、最近 N 条消息。
func (s *Service) BuildContext(ctx context.Context, sessionID int64, recentN int) (string, error) {
	sess, err := s.repo.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	var parts []string
	if sess.Summary != "" {
		parts = append(parts, "对话摘要："+sess.Summary)
	}
	if len(sess.Entities) > 0 {
		if b, err := json.Marshal(sess.Entities); err == nil && string(b) != "{}" {
			parts = append(parts, "已知实体："+string(b))
		}
	}
	if recentN > 0 {
		msgs, err := s.repo.ListMessages(ctx, sessionID, recentN)
		if err != nil {
			return "", err
		}
		// 取最近 N 条。
		if len(msgs) > recentN {
			msgs = msgs[len(msgs)-recentN:]
		}
		var sb strings.Builder
		for _, m := range msgs {
			role := "用户"
			if m.Role == "assistant" {
				role = "助手"
			}
			fmt.Fprintf(&sb, "%s: %s\n", role, m.Content)
		}
		if sb.Len() > 0 {
			parts = append(parts, "近期对话：\n"+sb.String())
		}
	}
	return strings.Join(parts, "\n\n"), nil
}
