// Package workspacehttp 是空间领域的 HTTP handlers。
package workspacehttp

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/workspaceapp"
	"github.com/vortexops/vortexops/internal/domain/workspace"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理 /api/v1/workspaces 路由。
type Handler struct {
	svc *workspaceapp.Service
}

// NewHandler 创建空间 handler。
func NewHandler(svc *workspaceapp.Service) *Handler {
	return &Handler{svc: svc}
}

// Create POST /api/v1/workspaces
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Name              string            `json:"name"`
		DisplayName       string            `json:"display_name"`
		Description       string            `json:"description"`
		LogoURL           string            `json:"logo_url"`
		DefaultRegistryID int64             `json:"default_registry_id"`
		DefaultJenkinsID  int64             `json:"default_jenkins_id"`
		Labels            map[string]string `json:"labels"`
		Metadata          map[string]any    `json:"metadata"`
		MaxApplications   int               `json:"max_applications"`
		MaxGroups         int               `json:"max_groups"`
		MaxMembers        int               `json:"max_members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	ws, err := h.svc.Create(r.Context(), workspaceapp.CreateInput{
		Name: req.Name, DisplayName: req.DisplayName, Description: req.Description, LogoURL: req.LogoURL,
		DefaultRegistryID: req.DefaultRegistryID, DefaultJenkinsID: req.DefaultJenkinsID,
		Labels: req.Labels, Metadata: req.Metadata,
		MaxApplications: req.MaxApplications, MaxGroups: req.MaxGroups, MaxMembers: req.MaxMembers,
		OwnerID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toWorkspaceDTO(ws))
}

// Get GET /api/v1/workspaces/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ws, err := h.svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toWorkspaceDTO(ws))
}

// List GET /api/v1/workspaces?page=&size=&owner_id=&status=&search=
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size, offset := httpx.Pagination(r)
	ownerID := int64(httpx.QueryInt(r, "owner_id", 0))
	status := workspace.Status(r.URL.Query().Get("status"))
	search := r.URL.Query().Get("search")
	items, total, err := h.svc.List(r.Context(), ownerID, status, search, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	_ = offset
	httpx.OK(w, httpx.Paged[workspaceDTO]{
		Items: toWorkspaceDTOs(items), Total: total, Page: page, Size: size,
	})
}

// Update PUT /api/v1/workspaces/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		DisplayName       *string            `json:"display_name"`
		Description       *string            `json:"description"`
		LogoURL           *string            `json:"logo_url"`
		Status            *string            `json:"status"`
		DefaultRegistryID *int64             `json:"default_registry_id"`
		DefaultJenkinsID  *int64             `json:"default_jenkins_id"`
		Labels            *map[string]string `json:"labels"`
		Metadata          *map[string]any    `json:"metadata"`
		Version           int                `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var status *workspace.Status
	if req.Status != nil {
		s := workspace.Status(*req.Status)
		status = &s
	}
	ws, err := h.svc.Update(r.Context(), workspaceapp.UpdateInput{
		ID: id, DisplayName: req.DisplayName, Description: req.Description, LogoURL: req.LogoURL,
		Status: status, DefaultRegistryID: req.DefaultRegistryID, DefaultJenkinsID: req.DefaultJenkinsID,
		Labels: req.Labels, Metadata: req.Metadata, Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toWorkspaceDTO(ws))
}

// Delete DELETE /api/v1/workspaces/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 配额 ---

// GetQuota GET /api/v1/workspaces/{id}/quota
func (h *Handler) GetQuota(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	q, err := h.svc.GetQuota(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toQuotaDTO(q))
}

// UpdateQuota PUT /api/v1/workspaces/{id}/quota
func (h *Handler) UpdateQuota(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		MaxApplications     int `json:"max_applications"`
		MaxGroups           int `json:"max_groups"`
		MaxConcurrentBuilds int `json:"max_concurrent_builds"`
		MaxImagesRetained   int `json:"max_images_retained"`
		MaxMembers          int `json:"max_members"`
		Version             int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	err := h.svc.UpdateQuota(r.Context(), workspaceapp.UpdateQuotaInput{
		WorkspaceID: id,
		Quota: workspace.Quota{
			MaxApplications: req.MaxApplications, MaxGroups: req.MaxGroups,
			MaxConcurrentBuilds: req.MaxConcurrentBuilds, MaxImagesRetained: req.MaxImagesRetained,
			MaxMembers: req.MaxMembers,
		},
		Version: req.Version, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 成员 ---

// AddMember POST /api/v1/workspaces/{id}/members
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		UserID int64 `json:"user_id"`
		RoleID int64 `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	m, err := h.svc.AddMember(r.Context(), workspaceapp.AddMemberInput{
		WorkspaceID: id, UserID: req.UserID, RoleID: req.RoleID, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toMemberDTO(m))
}

// ListMembers GET /api/v1/workspaces/{id}/members
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListMembers(r.Context(), id, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[memberDTO]{
		Items: toMemberDTOs(items), Total: total, Page: page, Size: size,
	})
}

// UpdateMemberRole PUT /api/v1/workspaces/{id}/members/{userId}
func (h *Handler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	var req struct {
		RoleID int64 `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.UpdateMemberRole(r.Context(), id, userID, req.RoleID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// RemoveMember DELETE /api/v1/workspaces/{id}/members/{userId}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	if err := h.svc.RemoveMember(r.Context(), id, userID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 集群绑定 ---

// AddClusterBinding POST /api/v1/workspaces/{id}/clusters
func (h *Handler) AddClusterBinding(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		ClusterID    int64          `json:"cluster_id"`
		Namespace    string         `json:"namespace"`
		Role         string         `json:"role"`
		AutoCreateNS bool           `json:"auto_create_namespace"`
		ResourceQuota map[string]any `json:"resource_quota"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	b, err := h.svc.AddClusterBinding(r.Context(), workspaceapp.AddClusterBindingInput{
		WorkspaceID: id, ClusterID: req.ClusterID, Namespace: req.Namespace,
		Role: workspace.ClusterRole(req.Role), AutoCreateNS: req.AutoCreateNS,
		ResourceQuota: req.ResourceQuota, ActorID: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toBindingDTO(b))
}

// ListClusterBindings GET /api/v1/workspaces/{id}/clusters
func (h *Handler) ListClusterBindings(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListClusterBindings(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toBindingDTOs(items))
}

// RemoveClusterBinding DELETE /api/v1/workspaces/{id}/clusters/{clusterId}
func (h *Handler) RemoveClusterBinding(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	clusterID, ok := parseID(w, chi.URLParam(r, "clusterId"))
	if !ok {
		return
	}
	if err := h.svc.RemoveClusterBinding(r.Context(), id, clusterID, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- helpers & DTOs ---

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

type workspaceDTO struct {
	ID                int64          `json:"id"`
	UUID              string         `json:"uuid"`
	Name              string         `json:"name"`
	DisplayName       string         `json:"display_name"`
	Description       string         `json:"description"`
	LogoURL           string         `json:"logo_url"`
	Status            string         `json:"status"`
	OwnerID           int64          `json:"owner_id"`
	DefaultRegistryID int64          `json:"default_registry_id"`
	DefaultJenkinsID  int64          `json:"default_jenkins_id"`
	Labels            map[string]string `json:"labels"`
	Metadata          map[string]any `json:"metadata"`
	Version           int            `json:"version"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

func toWorkspaceDTO(w *workspace.Workspace) *workspaceDTO {
	if w == nil {
		return nil
	}
	return &workspaceDTO{
		ID: w.ID, UUID: w.UUID.String(), Name: w.Name, DisplayName: w.DisplayName,
		Description: w.Description, LogoURL: w.LogoURL, Status: string(w.Status),
		OwnerID: w.OwnerID, DefaultRegistryID: w.DefaultRegistryID, DefaultJenkinsID: w.DefaultJenkinsID,
		Labels: w.Labels, Metadata: w.Metadata, Version: w.Version,
		CreatedAt: w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: w.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toWorkspaceDTOs(items []*workspace.Workspace) []workspaceDTO {
	out := make([]workspaceDTO, 0, len(items))
	for _, w := range items {
		out = append(out, *toWorkspaceDTO(w))
	}
	return out
}

type quotaDTO struct {
	MaxApplications     int `json:"max_applications"`
	MaxGroups           int `json:"max_groups"`
	MaxConcurrentBuilds int `json:"max_concurrent_builds"`
	MaxImagesRetained   int `json:"max_images_retained"`
	MaxMembers          int `json:"max_members"`
}

func toQuotaDTO(q *workspace.Quota) *quotaDTO {
	if q == nil {
		return nil
	}
	return &quotaDTO{
		MaxApplications: q.MaxApplications, MaxGroups: q.MaxGroups,
		MaxConcurrentBuilds: q.MaxConcurrentBuilds, MaxImagesRetained: q.MaxImagesRetained,
		MaxMembers: q.MaxMembers,
	}
}

type memberDTO struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	RoleID      int64  `json:"role_id"`
	RoleCode    string `json:"role_code"`
	RoleName    string `json:"role_name"`
	InvitedBy   int64  `json:"invited_by"`
	JoinedAt    string `json:"joined_at"`
	Status      string `json:"status"`
	Version     int    `json:"version"`
}

func toMemberDTO(m *workspace.Member) *memberDTO {
	if m == nil {
		return nil
	}
	return &memberDTO{
		ID:          m.ID,
		WorkspaceID: m.WorkspaceID,
		UserID:      m.UserID,
		Username:    m.Username,
		DisplayName: m.DisplayName,
		AvatarURL:   m.AvatarURL,
		RoleID:      m.RoleID,
		RoleCode:    m.RoleCode,
		RoleName:    m.RoleName,
		InvitedBy:   m.InvitedBy,
		JoinedAt:    m.JoinedAt.Format("2006-01-02T15:04:05Z07:00"),
		Status:      m.Status,
		Version:     m.Version,
	}
}

func toMemberDTOs(items []*workspace.Member) []memberDTO {
	out := make([]memberDTO, 0, len(items))
	for _, m := range items {
		out = append(out, *toMemberDTO(m))
	}
	return out
}

type clusterBindingDTO struct {
	ID            int64          `json:"id"`
	UUID          string         `json:"uuid"`
	WorkspaceID   int64          `json:"workspace_id"`
	ClusterID     int64          `json:"cluster_id"`
	Namespace     string         `json:"namespace"`
	Role          string         `json:"role"`
	AutoCreateNS  bool           `json:"auto_create_namespace"`
	ResourceQuota map[string]any `json:"resource_quota"`
	Version       int            `json:"version"`
	CreatedAt     string         `json:"created_at"`
}

func toBindingDTO(b *workspace.ClusterBinding) *clusterBindingDTO {
	if b == nil {
		return nil
	}
	return &clusterBindingDTO{
		ID: b.ID, UUID: b.UUID.String(), WorkspaceID: b.WorkspaceID, ClusterID: b.ClusterID,
		Namespace: b.Namespace, Role: string(b.Role), AutoCreateNS: b.AutoCreateNS,
		ResourceQuota: b.ResourceQuota, Version: b.Version,
		CreatedAt: b.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toBindingDTOs(items []*workspace.ClusterBinding) []clusterBindingDTO {
	out := make([]clusterBindingDTO, 0, len(items))
	for _, b := range items {
		out = append(out, *toBindingDTO(b))
	}
	return out
}
