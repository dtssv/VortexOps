// Package collabhttp 是协作领域的 HTTP handlers（通知）。
package collabhttp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vortexops/vortexops/internal/application/collabapp"
	"github.com/vortexops/vortexops/internal/domain/collab"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理通知路由。
type Handler struct {
	svc *collabapp.Service
}

// NewHandler 创建协作 handler。
func NewHandler(svc *collabapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListNotifications GET /api/v1/notifications?unread=true&page=&size=
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	page, size, _ := httpx.Pagination(r)
	unread := r.URL.Query().Get("unread") == "true"
	items, total, err := h.svc.ListNotifications(r.Context(), uid, unread, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[notificationDTO]{
		Items: toNotificationDTOs(items), Total: total, Page: page, Size: size,
	})
}

// CountUnread GET /api/v1/notifications/unread-count
func (h *Handler) CountUnread(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	count, err := h.svc.CountUnread(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]int64{"count": count})
}

// MarkRead POST /api/v1/notifications/{id}/read
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	_ = err
	// 从路径取 id（chi 路由未注入此处用 query 兼容）
	if id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", nil))
		return
	}
	if err := h.svc.MarkRead(r.Context(), id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// MarkAllRead POST /api/v1/notifications/read-all
func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	if err := h.svc.MarkAllRead(r.Context(), uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- helpers ---

func mustAuth(w http.ResponseWriter, r *http.Request) int64 {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return 0
	}
	return uid
}

type notificationDTO struct {
	ID           int64  `json:"id"`
	UUID         string `json:"uuid"`
	UserID       int64  `json:"user_id"`
	Channel      string `json:"channel"`
	TemplateCode string `json:"template_code,omitempty"`
	Recipient    string `json:"recipient,omitempty"`
	Subject      string `json:"subject,omitempty"`
	Body         string `json:"body,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
	Status       string `json:"status"`
	SentAt       string `json:"sent_at,omitempty"`
	ReadAt       string `json:"read_at,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	CreatedAt    string `json:"created_at"`
}

func toNotificationDTO(n *collab.Notification) *notificationDTO {
	if n == nil {
		return nil
	}
	dto := &notificationDTO{
		ID: n.ID, UUID: n.UUID.String(), UserID: n.UserID, Channel: string(n.Channel),
		TemplateCode: n.TemplateCode, Recipient: n.Recipient, Subject: n.Subject, Body: n.Body,
		Payload: n.Payload, Status: string(n.Status), ErrorMessage: n.ErrorMessage,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
	if n.SentAt != nil {
		dto.SentAt = n.SentAt.Format(time.RFC3339)
	}
	if n.ReadAt != nil {
		dto.ReadAt = n.ReadAt.Format(time.RFC3339)
	}
	return dto
}

func toNotificationDTOs(items []*collab.Notification) []notificationDTO {
	out := make([]notificationDTO, 0, len(items))
	for _, n := range items {
		out = append(out, *toNotificationDTO(n))
	}
	return out
}
