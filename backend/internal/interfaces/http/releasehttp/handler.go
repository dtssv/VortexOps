// Package releasehttp 是发布领域的 HTTP handlers。
package releasehttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/releaseapp"
	"github.com/vortexops/vortexops/internal/domain/release"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理发布相关路由。
type Handler struct {
	svc *releaseapp.Service
}

// NewHandler 创建发布 handler。
func NewHandler(svc *releaseapp.Service) *Handler {
	return &Handler{svc: svc}
}

// TriggerRelease POST /api/v1/groups/{groupId}/releases
func (h *Handler) TriggerRelease(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	var req struct {
		ImageID               int64    `json:"image_id"`
		ConfigVersion         int      `json:"config_version"`
		ReleaseType           string   `json:"release_type"`
		Replicas              int      `json:"replicas"`
		Strategy              string   `json:"strategy"`
		MaxSurge              string   `json:"max_surge"`
		MaxUnavailable        string   `json:"max_unavailable"`
		BatchSize             int      `json:"batch_size"`
		BatchIntervalSec      int      `json:"batch_interval_sec"`
		AutoRollbackOnFailure bool     `json:"auto_rollback_on_failure"`
		TargetPercentage      int      `json:"target_percentage"`
		TargetPodNames        []string `json:"target_pod_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	rel, err := h.svc.TriggerRelease(r.Context(), releaseapp.TriggerReleaseInput{
		GroupID: groupID, ImageID: req.ImageID, ConfigVersion: req.ConfigVersion,
		ReleaseType: release.ReleaseType(req.ReleaseType), Replicas: req.Replicas,
		Strategy: release.Strategy(req.Strategy), MaxSurge: req.MaxSurge, MaxUnavailable: req.MaxUnavailable,
		BatchSize: req.BatchSize, BatchIntervalSec: req.BatchIntervalSec,
		TriggeredBy: uid, TriggerSource: release.TriggerManual,
		AutoRollbackOnFailure: req.AutoRollbackOnFailure,
		TargetPercentage: req.TargetPercentage, TargetPodNames: req.TargetPodNames,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toReleaseDTO(rel))
}

// Rollback POST /api/v1/groups/{groupId}/rollback
func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	rel, err := h.svc.Rollback(r.Context(), groupID, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toReleaseDTO(rel))
}

// GetRelease GET /api/v1/releases/{id}
func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.GetRelease(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseDTO(rel))
}

// ListReleases GET /api/v1/groups/{groupId}/releases?status=&page=&size=
func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, chi.URLParam(r, "groupId"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	status := release.Status(r.URL.Query().Get("status"))
	items, total, err := h.svc.ListReleases(r.Context(), groupID, status, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[releaseDTO]{
		Items: toReleaseDTOs(items), Total: total, Page: page, Size: size,
	})
}

// AbortRelease POST /api/v1/releases/{id}/abort
func (h *Handler) AbortRelease(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.AbortRelease(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseDTO(rel))
}

// PauseRelease POST /api/v1/releases/{id}/pause
func (h *Handler) PauseRelease(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.PauseRelease(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseDTO(rel))
}

// ResumeRelease POST /api/v1/releases/{id}/resume
func (h *Handler) ResumeRelease(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.ResumeRelease(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseDTO(rel))
}

// ListReleaseEvents GET /api/v1/releases/{id}/events
func (h *Handler) ListReleaseEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListReleaseEvents(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseEventDTOs(items))
}

// ListBatchRecords GET /api/v1/releases/{id}/batches
func (h *Handler) ListBatchRecords(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListBatchRecords(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBatchRecordDTOs(items))
}

// --- 预设 ---

// CreatePreset POST /api/v1/release-presets
func (h *Handler) CreatePreset(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Scope                 string `json:"scope"`
		ScopeID               int64  `json:"scope_id"`
		Name                  string `json:"name"`
		Description           string `json:"description"`
		Strategy              string `json:"strategy"`
		MaxSurge              string `json:"max_surge"`
		MaxUnavailable        string `json:"max_unavailable"`
		BatchSize             int    `json:"batch_size"`
		BatchIntervalSec      int    `json:"batch_interval_sec"`
		AutoRollbackOnFailure bool   `json:"auto_rollback_on_failure"`
		IsDefault             bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	p, err := h.svc.CreatePreset(r.Context(), releaseapp.CreatePresetInput{
		Scope: release.PresetScope(req.Scope), ScopeID: req.ScopeID, Name: req.Name, Description: req.Description,
		Strategy: release.Strategy(req.Strategy), MaxSurge: req.MaxSurge, MaxUnavailable: req.MaxUnavailable,
		BatchSize: req.BatchSize, BatchIntervalSec: req.BatchIntervalSec,
		AutoRollbackOnFailure: req.AutoRollbackOnFailure, IsDefault: req.IsDefault, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toPresetDTO(p))
}

// ListPresets GET /api/v1/release-presets?scope=&scope_id=&page=&size=
func (h *Handler) ListPresets(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scope := release.PresetScope(r.URL.Query().Get("scope"))
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	items, total, err := h.svc.ListPresets(r.Context(), scope, scopeID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[presetDTO]{
		Items: toPresetDTOs(items), Total: total, Page: page, Size: size,
	})
}

// DeletePreset DELETE /api/v1/release-presets/{id}
func (h *Handler) DeletePreset(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeletePreset(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 窗口 ---

// CreateWindow POST /api/v1/applications/{appId}/release-windows
func (h *Handler) CreateWindow(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	var req struct {
		Name            string `json:"name"`
		Timezone        string `json:"timezone"`
		Crontab         string `json:"crontab"`
		DurationMinutes int    `json:"duration_minutes"`
		IsActive        bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	win, err := h.svc.CreateWindow(r.Context(), releaseapp.CreateWindowInput{
		ApplicationID: appID, Name: req.Name, Timezone: req.Timezone, Crontab: req.Crontab,
		DurationMinutes: req.DurationMinutes, IsActive: req.IsActive, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toWindowDTO(win))
}

// ListWindows GET /api/v1/applications/{appId}/release-windows
func (h *Handler) ListWindows(w http.ResponseWriter, r *http.Request) {
	appID, ok := parseID(w, chi.URLParam(r, "appId"))
	if !ok {
		return
	}
	items, err := h.svc.ListWindows(r.Context(), appID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toWindowDTOs(items))
}

// DeleteWindow DELETE /api/v1/release-windows/{id}
func (h *Handler) DeleteWindow(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteWindow(r.Context(), id, uid); err != nil {
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

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return 0, false
	}
	return id, true
}
