package chathttp

import (
	"encoding/json"
	"time"

	"github.com/vortexops/vortexops/internal/domain/chat"
)

type sessionDTO struct {
	ID            int64     `json:"id"`
	UUID          string    `json:"uuid"`
	UserID        int64     `json:"user_id"`
	Title         string    `json:"title"`
	Scene         string    `json:"scene"`
	Summary       string    `json:"summary,omitempty"`
	Entities      any       `json:"entities,omitempty"`
	MessageCount  int       `json:"message_count"`
	LastIntent    string    `json:"last_intent,omitempty"`
	LastActiveAt  time.Time `json:"last_active_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func toSessionDTO(s *chat.Session) sessionDTO {
	return sessionDTO{
		ID: s.ID, UUID: s.UUID, UserID: s.UserID, Title: s.Title, Scene: s.Scene,
		Summary: s.Summary, Entities: s.Entities, MessageCount: s.MessageCount,
		LastIntent: s.LastIntent, LastActiveAt: s.LastActiveAt,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

func toSessionDTOs(items []*chat.Session) []sessionDTO {
	out := make([]sessionDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toSessionDTO(s))
	}
	return out
}

type messageDTO struct {
	ID         int64     `json:"id"`
	UUID       string    `json:"uuid"`
	SessionID  int64     `json:"session_id"`
	Role       string    `json:"role"`
	Content    string    `json:"content"`
	Intent     any       `json:"intent,omitempty"`
	Tools      any       `json:"tools,omitempty"`
	References any       `json:"references,omitempty"`
	LatencyMs  int       `json:"latency_ms,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func toMessageDTO(m *chat.Message) messageDTO {
	dto := messageDTO{
		ID: m.ID, UUID: m.UUID, SessionID: m.SessionID, Role: m.Role,
		Content: m.Content, LatencyMs: m.LatencyMs, CreatedAt: m.CreatedAt,
	}
	if m.Intent != nil {
		var v any
		if json.Unmarshal([]byte(*m.Intent), &v) == nil {
			dto.Intent = v
		}
	}
	if m.Tools != nil {
		var v any
		if json.Unmarshal([]byte(*m.Tools), &v) == nil {
			dto.Tools = v
		}
	}
	if m.References != nil {
		var v any
		if json.Unmarshal([]byte(*m.References), &v) == nil {
			dto.References = v
		}
	}
	return dto
}

func toMessageDTOs(items []*chat.Message) []messageDTO {
	out := make([]messageDTO, 0, len(items))
	for _, m := range items {
		out = append(out, toMessageDTO(m))
	}
	return out
}
