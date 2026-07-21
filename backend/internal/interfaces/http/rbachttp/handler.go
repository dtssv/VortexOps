// Package rbachttp 是权限领域的 HTTP handlers。
package rbachttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/rbacapp"
	"github.com/vortexops/vortexops/internal/domain/rbac"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理权限相关路由。
type Handler struct {
	svc *rbacapp.Service
}

// NewHandler 创建权限 handler。
func NewHandler(svc *rbacapp.Service) *Handler {
	return &Handler{svc: svc}
}

// --- 权限 ---

// CreatePermission POST /api/v1/permissions
func (h *Handler) CreatePermission(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Category    string `json:"category"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	p, err := h.svc.CreatePermission(r.Context(), rbacapp.CreatePermissionInput{
		Code: req.Code, Name: req.Name, Category: rbac.PermissionCategory(req.Category),
		Scope: rbac.PermissionScope(req.Scope), Description: req.Description, SortOrder: req.SortOrder, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toPermissionDTO(p))
}

// ListPermissions GET /api/v1/permissions?category=&scope=&page=&size=
func (h *Handler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	items, total, err := h.svc.ListPermissions(r.Context(),
		rbac.PermissionCategory(r.URL.Query().Get("category")),
		rbac.PermissionScope(r.URL.Query().Get("scope")), page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[permissionDTO]{
		Items: toPermissionDTOs(items), Total: total, Page: page, Size: size,
	})
}

// DeletePermission DELETE /api/v1/permissions/{id}
func (h *Handler) DeletePermission(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeletePermission(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 菜单 ---

// CreateMenu POST /api/v1/menus
func (h *Handler) CreateMenu(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		ParentID       int64  `json:"parent_id"`
		Code           string `json:"code"`
		Name           string `json:"name"`
		NameEN         string `json:"name_en"`
		Path           string `json:"path"`
		Icon           string `json:"icon"`
		Component      string `json:"component"`
		MenuType       string `json:"menu_type"`
		Scope          string `json:"scope"`
		PermissionCode string `json:"permission_code"`
		Visible        bool   `json:"visible"`
		SortOrder      int    `json:"sort_order"`
		KeepAlive      bool   `json:"keep_alive"`
		ExternalLink   string `json:"external_link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	m, err := h.svc.CreateMenu(r.Context(), rbacapp.CreateMenuInput{
		ParentID: req.ParentID, Code: req.Code, Name: req.Name, NameEN: req.NameEN, Path: req.Path,
		Icon: req.Icon, Component: req.Component, MenuType: rbac.MenuType(req.MenuType),
		Scope: rbac.PermissionScope(req.Scope), PermissionCode: req.PermissionCode, Visible: req.Visible,
		SortOrder: req.SortOrder, KeepAlive: req.KeepAlive, ExternalLink: req.ExternalLink, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toMenuDTO(m))
}

// ListMenus GET /api/v1/menus?scope=
func (h *Handler) ListMenus(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListMenus(r.Context(), rbac.PermissionScope(r.URL.Query().Get("scope")))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toMenuDTOs(items))
}

// GetMyMenuTree GET /api/v1/me/menus?scope=
func (h *Handler) GetMyMenuTree(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	tree, err := h.svc.GetCurrentUserMenuTree(r.Context(), uid, 0, rbac.PermissionScope(r.URL.Query().Get("scope")))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toMenuTreeNodeDTOs(tree))
}

// DeleteMenu DELETE /api/v1/menus/{id}
func (h *Handler) DeleteMenu(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteMenu(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 角色 ---

// CreateRole POST /api/v1/roles
func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	var req struct {
		Scope       string `json:"scope"`
		ScopeID     int64  `json:"scope_id"`
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		IsDefault   bool   `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	role, err := h.svc.CreateRole(r.Context(), rbacapp.CreateRoleInput{
		Scope: rbac.RoleScope(req.Scope), ScopeID: req.ScopeID, Code: req.Code, Name: req.Name,
		Description: req.Description, IsDefault: req.IsDefault, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRoleDTO(role))
}

// ListRoles GET /api/v1/roles?scope=&scope_id=&page=&size=
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	scopeID, _ := strconv.ParseInt(r.URL.Query().Get("scope_id"), 10, 64)
	items, total, err := h.svc.ListRoles(r.Context(), rbac.RoleScope(r.URL.Query().Get("scope")), scopeID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[roleDTO]{
		Items: toRoleDTOs(items), Total: total, Page: page, Size: size,
	})
}

// DeleteRole DELETE /api/v1/roles/{id}
func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteRole(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// GrantPermissions POST /api/v1/roles/{id}/permissions
func (h *Handler) GrantPermissions(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		PermissionIDs []int64 `json:"permission_ids"`
		Granted       bool    `json:"granted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if req.Granted {
		if err := h.svc.GrantPermissions(r.Context(), id, req.PermissionIDs, uid); err != nil {
			httpx.WriteError(w, err)
			return
		}
	} else {
		if err := h.svc.RevokePermissions(r.Context(), id, req.PermissionIDs); err != nil {
			httpx.WriteError(w, err)
			return
		}
	}
	httpx.NoContent(w)
}

// ListPermissionsByRole GET /api/v1/roles/{id}/permissions
func (h *Handler) ListPermissionsByRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListPermissionsByRole(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toPermissionDTOs(items))
}

// ListMenusByRole GET /api/v1/roles/{id}/menus （需 rbac:manage）
// 返回角色直接绑定的菜单列表（全量替换模式下供前端回显已选菜单）。
func (h *Handler) ListMenusByRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListMenusByRole(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toMenuDTOs(items))
}

// BindRoleMenus POST /api/v1/roles/{id}/menus （需 rbac:manage）
// 绑定菜单到角色。clear=true 时先清空现有绑定再绑定 menu_ids（全量替换）。
func (h *Handler) BindRoleMenus(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		MenuIDs []int64 `json:"menu_ids"`
		Clear   bool    `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	if err := h.svc.BindMenusToRole(r.Context(), rbacapp.BindMenusInput{
		RoleID: id, MenuIDs: req.MenuIDs, Clear: req.Clear, ActorID: uid,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- 用户-角色绑定 ---

// BindPlatformRole POST /api/v1/users/{userId}/platform-roles
func (h *Handler) BindPlatformRole(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	var req struct {
		RoleID    int64  `json:"role_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid request body", err))
		return
	}
	var expiresAt *string
	if req.ExpiresAt != "" {
		expiresAt = &req.ExpiresAt
	}
	if err := h.svc.BindPlatformRole(r.Context(), userID, req.RoleID, parseTimePtr(expiresAt), uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// ListPlatformRolesByUser GET /api/v1/users/{userId}/platform-roles
func (h *Handler) ListPlatformRolesByUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseID(w, chi.URLParam(r, "userId"))
	if !ok {
		return
	}
	items, err := h.svc.ListPlatformRolesByUser(r.Context(), userID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRoleDTOs(items))
}

// --- workspace 成员 ---

// AddWorkspaceMember POST /api/v1/workspaces/{wsId}/members
func (h *Handler) AddWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	wsID, ok := parseID(w, chi.URLParam(r, "wsId"))
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
	m, err := h.svc.AddWorkspaceMember(r.Context(), rbacapp.AddWorkspaceMemberInput{
		WorkspaceID: wsID, UserID: req.UserID, RoleID: req.RoleID, InvitedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toMemberDTO(m))
}

// ListWorkspaceMembers GET /api/v1/workspaces/{wsId}/members
func (h *Handler) ListWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	wsID, ok := parseID(w, chi.URLParam(r, "wsId"))
	if !ok {
		return
	}
	items, err := h.svc.ListWorkspaceMembers(r.Context(), wsID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toMemberDTOs(items))
}

// RemoveWorkspaceMember DELETE /api/v1/workspace-members/{id}
func (h *Handler) RemoveWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	if uid == 0 {
		return
	}
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.RemoveWorkspaceMember(r.Context(), id, 0, uid); err != nil {
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

func parseTimePtr(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}
