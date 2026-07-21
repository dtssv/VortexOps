package rbachttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/application/rbacapp"
	"github.com/vortexops/vortexops/internal/domain/rbac"
)

type permissionDTO struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Scope       string `json:"scope"`
	Description string `json:"description,omitempty"`
	SortOrder   int    `json:"sort_order"`
	Enabled     bool   `json:"enabled"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

func toPermissionDTO(p *rbac.Permission) *permissionDTO {
	if p == nil {
		return nil
	}
	return &permissionDTO{
		ID: p.ID, Code: p.Code, Name: p.Name, Category: string(p.Category), Scope: string(p.Scope),
		Description: p.Description, SortOrder: p.SortOrder, Enabled: p.Enabled,
		Version: p.Version, CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}

func toPermissionDTOs(items []*rbac.Permission) []permissionDTO {
	out := make([]permissionDTO, 0, len(items))
	for _, p := range items {
		out = append(out, *toPermissionDTO(p))
	}
	return out
}

type menuDTO struct {
	ID             int64  `json:"id"`
	UUID           string `json:"uuid"`
	ParentID       int64  `json:"parent_id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	NameEN         string `json:"name_en,omitempty"`
	Path           string `json:"path,omitempty"`
	Icon           string `json:"icon,omitempty"`
	Component      string `json:"component,omitempty"`
	MenuType       string `json:"menu_type"`
	Scope          string `json:"scope"`
	PermissionCode string `json:"permission_code,omitempty"`
	Visible        bool   `json:"visible"`
	SortOrder      int    `json:"sort_order"`
	KeepAlive      bool   `json:"keep_alive"`
	ExternalLink   string `json:"external_link,omitempty"`
	Version        int    `json:"version"`
	CreatedAt      string `json:"created_at"`
}

func toMenuDTO(m *rbac.Menu) *menuDTO {
	if m == nil {
		return nil
	}
	return &menuDTO{
		ID: m.ID, UUID: m.UUID.String(), ParentID: m.ParentID, Code: m.Code, Name: m.Name, NameEN: m.NameEN,
		Path: m.Path, Icon: m.Icon, Component: m.Component, MenuType: string(m.MenuType), Scope: string(m.Scope),
		PermissionCode: m.PermissionCode, Visible: m.Visible, SortOrder: m.SortOrder, KeepAlive: m.KeepAlive,
		ExternalLink: m.ExternalLink, Version: m.Version, CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
}

func toMenuDTOs(items []*rbac.Menu) []menuDTO {
	out := make([]menuDTO, 0, len(items))
	for _, m := range items {
		out = append(out, *toMenuDTO(m))
	}
	return out
}

// menuTreeNodeDTO 是带 children 的菜单树节点，用于 /me/menus 接口。
// rbacapp.MenuNode 内嵌的 *rbac.Menu 无 JSON 标签，直接序列化会输出 Go 风格字段名
// （ID/ParentID/MenuType...），前端按 snake_case 解析会拿到 undefined，导致菜单渲染为空。
// 这里显式转换为带正确 JSON 标签的 DTO。
type menuTreeNodeDTO struct {
	ID             int64              `json:"id"`
	UUID           string             `json:"uuid"`
	ParentID       int64              `json:"parent_id"`
	Code           string             `json:"code"`
	Name           string             `json:"name"`
	NameEN         string             `json:"name_en,omitempty"`
	Path           string             `json:"path,omitempty"`
	Icon           string             `json:"icon,omitempty"`
	Component      string             `json:"component,omitempty"`
	MenuType       string             `json:"menu_type"`
	Scope          string             `json:"scope"`
	PermissionCode string             `json:"permission_code,omitempty"`
	Visible        bool               `json:"visible"`
	SortOrder      int                `json:"sort_order"`
	KeepAlive      bool               `json:"keep_alive"`
	ExternalLink   string             `json:"external_link,omitempty"`
	Version        int                `json:"version"`
	CreatedAt      string             `json:"created_at"`
	Children       []menuTreeNodeDTO  `json:"children"`
}

func toMenuTreeNodeDTO(n *rbacapp.MenuNode) menuTreeNodeDTO {
	dto := menuTreeNodeDTO{
		ID: n.ID, UUID: n.UUID.String(), ParentID: n.ParentID, Code: n.Code, Name: n.Name, NameEN: n.NameEN,
		Path: n.Path, Icon: n.Icon, Component: n.Component, MenuType: string(n.MenuType), Scope: string(n.Scope),
		PermissionCode: n.PermissionCode, Visible: n.Visible, SortOrder: n.SortOrder, KeepAlive: n.KeepAlive,
		ExternalLink: n.ExternalLink, Version: n.Version, CreatedAt: n.CreatedAt.Format(time.RFC3339),
		Children: []menuTreeNodeDTO{},
	}
	for _, c := range n.Children {
		dto.Children = append(dto.Children, toMenuTreeNodeDTO(c))
	}
	return dto
}

func toMenuTreeNodeDTOs(nodes []*rbacapp.MenuNode) []menuTreeNodeDTO {
	out := make([]menuTreeNodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toMenuTreeNodeDTO(n))
	}
	return out
}

type roleDTO struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	Scope       string `json:"scope"`
	ScopeID     int64  `json:"scope_id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsBuiltin   bool   `json:"is_builtin"`
	IsDefault   bool   `json:"is_default"`
	Enabled     bool   `json:"enabled"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

func toRoleDTO(r *rbac.Role) *roleDTO {
	if r == nil {
		return nil
	}
	return &roleDTO{
		ID: r.ID, UUID: r.UUID.String(), Scope: string(r.Scope), ScopeID: r.ScopeID, Code: r.Code, Name: r.Name,
		Description: r.Description, IsBuiltin: r.IsBuiltin, IsDefault: r.IsDefault, Enabled: r.Enabled,
		Version: r.Version, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

func toRoleDTOs(items []*rbac.Role) []roleDTO {
	out := make([]roleDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toRoleDTO(r))
	}
	return out
}

type memberDTO struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspace_id"`
	UserID      int64  `json:"user_id"`
	RoleID      int64  `json:"role_id"`
	InvitedBy   int64  `json:"invited_by,omitempty"`
	JoinedAt    string `json:"joined_at"`
	Status      string `json:"status"`
	Version     int    `json:"version"`
}

func toMemberDTO(m *rbac.WorkspaceMember) *memberDTO {
	if m == nil {
		return nil
	}
	return &memberDTO{
		ID: m.ID, WorkspaceID: m.WorkspaceID, UserID: m.UserID, RoleID: m.RoleID, InvitedBy: m.InvitedBy,
		JoinedAt: m.JoinedAt.Format(time.RFC3339), Status: string(m.Status), Version: m.Version,
	}
}

func toMemberDTOs(items []*rbac.WorkspaceMember) []memberDTO {
	out := make([]memberDTO, 0, len(items))
	for _, m := range items {
		out = append(out, *toMemberDTO(m))
	}
	return out
}
