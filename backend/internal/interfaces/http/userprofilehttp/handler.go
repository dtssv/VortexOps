// Package userprofilehttp 是用户画像的 HTTP handlers。
package userprofilehttp

import (
	"encoding/json"
	"net/http"

	"github.com/vortexops/vortexops/internal/application/userprofileapp"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 用户画像 handler。
type Handler struct {
	svc *userprofileapp.Service
}

// NewHandler 创建用户画像 handler。
func NewHandler(svc *userprofileapp.Service) *Handler {
	return &Handler{svc: svc}
}

// GetProfile GET /api/v1/user-profile
// 返回当前登录用户的画像。
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	p, err := h.svc.GetByUserID(r.Context(), uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toProfileDTO(p))
}

// UpdateProfile PUT /api/v1/user-profile
// 手动更新当前用户的画像（管理员可代为调整）。
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return
	}
	var req struct {
		ExpertiseLevel     *string   `json:"expertise_level"`
		Roles              *[]string `json:"roles"`
		Domains            *[]string `json:"domains"`
		CommunicationStyle *string   `json:"communication_style"`
		PreferredLanguage  *string   `json:"preferred_language"`
		Summary            *string   `json:"summary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	p, err := h.svc.UpdateProfile(r.Context(), userprofileapp.UpdateProfileInput{
		UserID: uid, ExpertiseLevel: req.ExpertiseLevel, Roles: req.Roles,
		Domains: req.Domains, CommunicationStyle: req.CommunicationStyle,
		PreferredLanguage: req.PreferredLanguage, Summary: req.Summary, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toProfileDTO(p))
}
