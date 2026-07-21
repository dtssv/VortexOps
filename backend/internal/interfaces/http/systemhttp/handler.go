// Package systemhttp 是系统设置领域的 HTTP handlers。
package systemhttp

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/systemapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/system-settings 路由。
type Handler struct {
	svc *systemapp.Service
}

// NewHandler 创建系统设置 handler。
func NewHandler(svc *systemapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListPublic GET /api/v1/system-settings
// 返回 is_public=true 的设置项（所有登录用户可读）。
func (h *Handler) ListPublic(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	items, err := h.svc.List(r.Context(), true, search)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSettingDTOs(items))
}

// ListAll GET /api/v1/system-settings/all
// 管理员接口：返回所有设置项（含 is_public=false）。
func (h *Handler) ListAll(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	items, err := h.svc.List(r.Context(), false, search)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSettingDTOs(items))
}

// Get GET /api/v1/system-settings/{key}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		httpx.WriteError(w, apperr.Validation("key is required", nil))
		return
	}
	s, err := h.svc.Get(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSettingDTO(s))
}

// Update PUT /api/v1/system-settings/{key}
// 请求体：{"value": <any>, "description": "...", "is_public": false}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		httpx.WriteError(w, apperr.Validation("key is required", nil))
		return
	}
	var req struct {
		Value       any    `json:"value"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	s, err := h.svc.Set(r.Context(), systemapp.SetInput{
		Key: key, Value: req.Value, Description: req.Description, IsPublic: req.IsPublic, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toSettingDTO(s))
}
