// Package chathttp 是 AI 助手对话会话的 HTTP handlers。
package chathttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/chatapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 对话会话 handler。
type Handler struct {
	svc *chatapp.Service
}

// NewHandler 创建对话会话 handler。
func NewHandler(svc *chatapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListSessions GET /api/v1/chat/sessions?limit=
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	limit := httpx.QueryInt(r, "limit", 20)
	items, err := h.svc.ListSessions(r.Context(), uid, limit)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSessionDTOs(items))
}

// CreateSession POST /api/v1/chat/sessions
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	var req struct {
		Title string `json:"title"`
		Scene string `json:"scene"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	sess, err := h.svc.CreateSession(r.Context(), chatapp.CreateSessionInput{
		UserID: uid, Title: req.Title, Scene: req.Scene, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toSessionDTO(sess))
}

// GetSession GET /api/v1/chat/sessions/{id}
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	sess, err := h.svc.GetSession(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSessionDTO(sess))
}

// DeleteSession DELETE /api/v1/chat/sessions/{id}
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	uid := httpauth.UserID(r.Context())
	if err := h.svc.DeleteSession(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListMessages GET /api/v1/chat/sessions/{id}/messages?limit=
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return
	}
	limit := httpx.QueryInt(r, "limit", 100)
	items, err := h.svc.ListMessages(r.Context(), id, limit)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toMessageDTOs(items))
}
