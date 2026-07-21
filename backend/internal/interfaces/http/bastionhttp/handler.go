// Package bastionhttp 是堡垒机领域的 HTTP handlers。
package bastionhttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/bastionapp"
	"github.com/vortexops/vortexops/internal/domain/bastion"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理堡垒机相关路由。
type Handler struct {
	svc *bastionapp.Service
}

// NewHandler 创建堡垒机 handler。
func NewHandler(svc *bastionapp.Service) *Handler {
	return &Handler{svc: svc}
}

// ListAssets GET /api/v1/bastion/assets?workspace_id=&protocol=&search=&page=&size=
func (h *Handler) ListAssets(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	items, total, err := h.svc.ListAssets(r.Context(), bastion.AssetQuery{
		WorkspaceID: wsID, Protocol: bastion.Protocol(r.URL.Query().Get("protocol")),
		Search: r.URL.Query().Get("search"),
	}, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[assetDTO]{
		Items: toAssetDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetAsset GET /api/v1/bastion/assets/{id}
func (h *Handler) GetAsset(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	a, err := h.svc.GetAsset(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toAssetDTO(a))
}

// CreateAsset POST /api/v1/bastion/assets
func (h *Handler) CreateAsset(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		WorkspaceID int64    `json:"workspace_id"`
		Name        string   `json:"name"`
		Host        string   `json:"host"`
		Port        int      `json:"port"`
		Protocol    string   `json:"protocol"`
		Platform    string   `json:"platform"`
		Username    string   `json:"username"`
		CredentialID int64   `json:"credential_id"`
		Tags        []string `json:"tags"`
		Comment     string   `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	a, err := h.svc.CreateAsset(r.Context(), bastion.CreateAssetInput{
		WorkspaceID: req.WorkspaceID, Name: req.Name, Host: req.Host, Port: req.Port,
		Protocol: bastion.Protocol(req.Protocol), Platform: req.Platform, Username: req.Username,
		CredentialID: req.CredentialID, Tags: req.Tags, Comment: req.Comment, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toAssetDTO(a))
}

// UpdateAsset PUT /api/v1/bastion/assets/{id}
func (h *Handler) UpdateAsset(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name         string   `json:"name"`
		Host         string   `json:"host"`
		Port         int      `json:"port"`
		Protocol     string   `json:"protocol"`
		Platform     string   `json:"platform"`
		Username     string   `json:"username"`
		CredentialID int64    `json:"credential_id"`
		Tags         []string `json:"tags"`
		Comment      string   `json:"comment"`
		IsActive     bool     `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	a, err := h.svc.UpdateAsset(r.Context(), id, bastionapp.UpdateAssetInput{
		Name: req.Name, Host: req.Host, Port: req.Port, Protocol: bastion.Protocol(req.Protocol),
		Platform: req.Platform, Username: req.Username, CredentialID: req.CredentialID,
		Tags: req.Tags, Comment: req.Comment, IsActive: req.IsActive, UpdatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toAssetDTO(a))
}

// DeleteAsset DELETE /api/v1/bastion/assets/{id}
func (h *Handler) DeleteAsset(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteAsset(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// Connect POST /api/v1/bastion/assets/{id}/connect
func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	username := httpauth.Username(r.Context())
	loginURL, err := h.svc.Connect(r.Context(), id, uid, username)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"login_url": loginURL})
}

// SyncAssets POST /api/v1/bastion/sync?workspace_id=
func (h *Handler) SyncAssets(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	count, err := h.svc.SyncAssets(r.Context(), wsID, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]int{"synced": count})
}

// ListSessions GET /api/v1/bastion/sessions?workspace_id=&asset_id=&status=&page=&size=
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	assetID, _ := strconv.ParseInt(r.URL.Query().Get("asset_id"), 10, 64)
	items, total, err := h.svc.ListSessions(r.Context(), bastion.SessionQuery{
		WorkspaceID: wsID, AssetID: assetID, Status: bastion.AssetStatus(r.URL.Query().Get("status")),
	}, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[sessionDTO]{
		Items: toSessionDTOs(items), Total: total, Page: page, Size: size,
	})
}

// GetReplay GET /api/v1/bastion/sessions/{id}/replay
func (h *Handler) GetReplay(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	url, err := h.svc.GetReplayURL(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, map[string]string{"replay_url": url})
}

// --- DTO ---

type assetDTO struct {
	ID           int64    `json:"id"`
	UUID         string   `json:"uuid"`
	WorkspaceID  int64    `json:"workspace_id"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Protocol     string   `json:"protocol"`
	Platform     string   `json:"platform"`
	Username     string   `json:"username"`
	CredentialID int64    `json:"credential_id"`
	JMSAssetID   string   `json:"jms_asset_id"`
	Tags         []string `json:"tags"`
	Comment      string   `json:"comment"`
	IsActive     bool     `json:"is_active"`
	CreatedAt    string   `json:"created_at"`
}

type sessionDTO struct {
	ID           int64  `json:"id"`
	UUID         string `json:"uuid"`
	WorkspaceID  int64  `json:"workspace_id"`
	AssetID      int64  `json:"asset_id"`
	Username     string `json:"username"`
	AssetName    string `json:"asset_name"`
	Protocol     string `json:"protocol"`
	RemoteAddr   string `json:"remote_addr"`
	LoginFrom    string `json:"login_from"`
	Status       string `json:"status"`
	StartedAt    *string `json:"started_at"`
	EndedAt      *string `json:"ended_at"`
	DurationMs   int64  `json:"duration_ms"`
	CommandCount int    `json:"command_count"`
}

func toAssetDTO(a *bastion.Asset) assetDTO {
	return assetDTO{
		ID: a.ID, UUID: a.UUID.String(), WorkspaceID: a.WorkspaceID, Name: a.Name, Host: a.Host,
		Port: a.Port, Protocol: string(a.Protocol), Platform: a.Platform, Username: a.Username,
		CredentialID: a.CredentialID, JMSAssetID: a.JMSAssetID, Tags: a.Tags, Comment: a.Comment,
		IsActive: a.IsActive, CreatedAt: a.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func toAssetDTOs(items []*bastion.Asset) []assetDTO {
	out := make([]assetDTO, 0, len(items))
	for _, a := range items {
		out = append(out, toAssetDTO(a))
	}
	return out
}

func toSessionDTO(s *bastion.Session) sessionDTO {
	dto := sessionDTO{
		ID: s.ID, UUID: s.UUID.String(), WorkspaceID: s.WorkspaceID, AssetID: s.AssetID,
		Username: s.Username, AssetName: s.AssetName, Protocol: string(s.Protocol),
		RemoteAddr: s.RemoteAddr, LoginFrom: s.LoginFrom, Status: string(s.Status),
		DurationMs: s.DurationMs, CommandCount: s.CommandCount,
	}
	if s.StartedAt != nil {
		t := s.StartedAt.Format("2006-01-02T15:04:05Z")
		dto.StartedAt = &t
	}
	if s.EndedAt != nil {
		t := s.EndedAt.Format("2006-01-02T15:04:05Z")
		dto.EndedAt = &t
	}
	return dto
}

func toSessionDTOs(items []*bastion.Session) []sessionDTO {
	out := make([]sessionDTO, 0, len(items))
	for _, s := range items {
		out = append(out, toSessionDTO(s))
	}
	return out
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
