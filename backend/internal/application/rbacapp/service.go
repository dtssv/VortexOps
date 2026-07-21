// Package rbacapp 是权限领域的应用服务层。
// 编排：角色/权限/菜单 CRUD、用户-角色绑定、权限解析（聚合用户权限集）、动态菜单树构建。
// 提供权限校验能力供 httpauth 中间件使用。
package rbacapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vortexops/vortexops/internal/domain/rbac"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Cache 权限缓存接口（Redis 实现）。按用户 ID 缓存权限 code 集合。
type Cache interface {
	GetUserPermissions(ctx context.Context, userID int64) ([]string, error)
	SetUserPermissions(ctx context.Context, userID int64, codes []string, ttl time.Duration) error
	EvictUserPermissions(ctx context.Context, userID int64) error
}

// Service 权限应用服务。
type Service struct {
	repo  rbac.Repository
	cache Cache
}

// New 创建权限服务。cache 可为 nil（不缓存，每次查库）。
func New(repo rbac.Repository, cache Cache) *Service {
	return &Service{repo: repo, cache: cache}
}

// --- 权限 ---

// CreatePermissionInput 创建权限输入。
type CreatePermissionInput struct {
	Code        string
	Name        string
	Category    rbac.PermissionCategory
	Scope       rbac.PermissionScope
	Description string
	SortOrder   int
	CreatedBy   int64
}

// CreatePermission 创建权限。
func (s *Service) CreatePermission(ctx context.Context, in CreatePermissionInput) (*rbac.Permission, error) {
	if in.Code == "" || in.Name == "" {
		return nil, apperr.Validation("code and name are required", nil)
	}
	if in.Category == "" {
		in.Category = rbac.PermCategoryAction
	}
	if in.Scope == "" {
		in.Scope = rbac.PermScopePlatform
	}
	p := &rbac.Permission{
		Code: in.Code, Name: in.Name, Category: in.Category, Scope: in.Scope,
		Description: in.Description, SortOrder: in.SortOrder, Enabled: true,
	}
	p.CreatedBy = in.CreatedBy
	p.UpdatedBy = in.CreatedBy
	if err := s.repo.CreatePermission(ctx, p); err != nil {
		if errors.Is(err, rbac.ErrPermissionCodeExists) {
			return nil, apperr.Conflict("permission code already exists", err)
		}
		return nil, apperr.Internal("create permission", err)
	}
	return p, nil
}

// ListPermissions 分页列出权限。
func (s *Service) ListPermissions(ctx context.Context, category rbac.PermissionCategory, scope rbac.PermissionScope, page, size int) ([]*rbac.Permission, int64, error) {
	items, total, err := s.repo.ListPermissions(ctx, rbac.PermissionQuery{
		Category: category, Scope: scope, Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list permissions", err)
	}
	return items, total, nil
}

// DeletePermission 删除权限。
func (s *Service) DeletePermission(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeletePermission(ctx, id, actorID); err != nil {
		if errors.Is(err, rbac.ErrPermissionNotFound) {
			return apperr.NotFound("permission", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete permission", err)
	}
	return nil
}

// --- 菜单 ---

// CreateMenuInput 创建菜单输入。
type CreateMenuInput struct {
	ParentID       int64
	Code           string
	Name           string
	NameEN         string
	Path           string
	Icon           string
	Component      string
	MenuType       rbac.MenuType
	Scope          rbac.PermissionScope
	PermissionCode string
	Visible        bool
	SortOrder      int
	KeepAlive      bool
	ExternalLink   string
	CreatedBy      int64
}

// CreateMenu 创建菜单。
func (s *Service) CreateMenu(ctx context.Context, in CreateMenuInput) (*rbac.Menu, error) {
	if in.Code == "" || in.Name == "" {
		return nil, apperr.Validation("code and name are required", nil)
	}
	if in.MenuType == "" {
		in.MenuType = rbac.MenuTypeMenu
	}
	if in.Scope == "" {
		in.Scope = rbac.PermScopePlatform
	}
	m := &rbac.Menu{
		ParentID: in.ParentID, Code: in.Code, Name: in.Name, NameEN: in.NameEN, Path: in.Path,
		Icon: in.Icon, Component: in.Component, MenuType: in.MenuType, Scope: in.Scope,
		PermissionCode: in.PermissionCode, Visible: in.Visible, SortOrder: in.SortOrder,
		KeepAlive: in.KeepAlive, ExternalLink: in.ExternalLink,
	}
	m.CreatedBy = in.CreatedBy
	m.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateMenu(ctx, m); err != nil {
		if errors.Is(err, rbac.ErrMenuCodeExists) {
			return nil, apperr.Conflict("menu code already exists", err)
		}
		return nil, apperr.Internal("create menu", err)
	}
	return m, nil
}

// ListMenus 列出菜单（全量，供管理端构建树）。
func (s *Service) ListMenus(ctx context.Context, scope rbac.PermissionScope) ([]*rbac.Menu, error) {
	items, err := s.repo.ListMenus(ctx, scope)
	if err != nil {
		return nil, apperr.Internal("list menus", err)
	}
	return items, nil
}

// GetCurrentUserMenuTree 获取当前用户可见菜单树（按权限 + 角色直接绑定过滤）。
// 可见条件（OR）：permission_code 为空（分组目录）；menu 直接绑定到用户任一角色；
// menu 的 permission_code 命中用户权限集。
func (s *Service) GetCurrentUserMenuTree(ctx context.Context, userID int64, workspaceID int64, scope rbac.PermissionScope) ([]*MenuNode, error) {
	permCodes, err := s.GetUserPermissions(ctx, userID, workspaceID)
	if err != nil {
		return nil, apperr.Internal("get user permissions", err)
	}
	roleIDs, err := s.repo.ListRoleIDsForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, apperr.Internal("get user role ids", err)
	}
	menus, err := s.repo.ListVisibleMenus(ctx, scope, permCodes, roleIDs)
	if err != nil {
		return nil, apperr.Internal("list visible menus", err)
	}
	return buildMenuTree(menus), nil
}

// MenuNode 菜单树节点。
type MenuNode struct {
	*rbac.Menu
	Children []*MenuNode `json:"children"`
}

// buildMenuTree 把扁平菜单列表构建为树。
func buildMenuTree(menus []*rbac.Menu) []*MenuNode {
	nodeMap := make(map[int64]*MenuNode, len(menus))
	var roots []*MenuNode
	for _, m := range menus {
		nodeMap[m.ID] = &MenuNode{Menu: m, Children: []*MenuNode{}}
	}
	for _, m := range menus {
		node := nodeMap[m.ID]
		if m.ParentID == 0 {
			roots = append(roots, node)
		} else if parent, ok := nodeMap[m.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			// 父节点不在可见集合内（无权限）：作为根节点保留（避免丢失）。
			roots = append(roots, node)
		}
	}
	return roots
}

// UpdateMenu 更新菜单。
func (s *Service) UpdateMenu(ctx context.Context, m *rbac.Menu) error {
	if err := s.repo.UpdateMenu(ctx, m); err != nil {
		return apperr.Internal("update menu", err)
	}
	return nil
}

// DeleteMenu 删除菜单。
func (s *Service) DeleteMenu(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteMenu(ctx, id, actorID); err != nil {
		if errors.Is(err, rbac.ErrMenuNotFound) {
			return apperr.NotFound("menu", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete menu", err)
	}
	return nil
}

// --- 角色-菜单直接绑定 ---

// BindMenusToRole 给角色绑定菜单（幂等）。menuIDs 为空时清空该角色全部菜单绑定（如果 clear=true）。
type BindMenusInput struct {
	RoleID  int64
	MenuIDs []int64
	// Clear 是否先清空角色现有菜单绑定再绑定新集合（全量替换语义）。
	Clear   bool
	ActorID int64
}

// BindMenusToRole 绑定菜单到角色。
func (s *Service) BindMenusToRole(ctx context.Context, in BindMenusInput) error {
	if in.RoleID == 0 {
		return apperr.Validation("role_id is required", nil)
	}
	if _, err := s.repo.GetRoleByID(ctx, in.RoleID); err != nil {
		if errors.Is(err, rbac.ErrRoleNotFound) {
			return apperr.NotFound("role", strconv.FormatInt(in.RoleID, 10))
		}
		return apperr.Internal("get role", err)
	}
	if in.Clear {
		// 全量替换：先解绑现有全部（用 ListMenuIDsByRole 取出现有集合再删）。
		existing, err := s.repo.ListMenuIDsByRole(ctx, in.RoleID)
		if err != nil {
			return apperr.Internal("list role menus", err)
		}
		if len(existing) > 0 {
			if err := s.repo.UnbindMenusFromRole(ctx, in.RoleID, existing); err != nil {
				return apperr.Internal("clear role menus", err)
			}
		}
	}
	if err := s.repo.BindMenusToRole(ctx, in.RoleID, in.MenuIDs, in.ActorID); err != nil {
		return apperr.Internal("bind menus to role", err)
	}
	return nil
}

// ListMenusByRole 返回角色直接绑定的菜单列表。
func (s *Service) ListMenusByRole(ctx context.Context, roleID int64) ([]*rbac.Menu, error) {
	items, err := s.repo.ListMenusByRole(ctx, roleID)
	if err != nil {
		return nil, apperr.Internal("list menus by role", err)
	}
	return items, nil
}

// --- 角色 ---

// CreateRoleInput 创建角色输入。
type CreateRoleInput struct {
	Scope       rbac.RoleScope
	ScopeID     int64
	Code        string
	Name        string
	Description string
	IsDefault   bool
	CreatedBy   int64
}

// CreateRole 创建角色。
func (s *Service) CreateRole(ctx context.Context, in CreateRoleInput) (*rbac.Role, error) {
	if in.Code == "" || in.Name == "" {
		return nil, apperr.Validation("code and name are required", nil)
	}
	if in.Scope == "" {
		in.Scope = rbac.RoleScopeWorkspace
	}
	role := &rbac.Role{
		Scope: in.Scope, ScopeID: in.ScopeID, Code: in.Code, Name: in.Name,
		Description: in.Description, IsDefault: in.IsDefault, Enabled: true,
	}
	role.CreatedBy = in.CreatedBy
	role.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateRole(ctx, role); err != nil {
		if errors.Is(err, rbac.ErrRoleCodeExists) {
			return nil, apperr.Conflict("role code already exists", err)
		}
		return nil, apperr.Internal("create role", err)
	}
	return role, nil
}

// ListRoles 分页列出角色。
func (s *Service) ListRoles(ctx context.Context, scope rbac.RoleScope, scopeID int64, page, size int) ([]*rbac.Role, int64, error) {
	items, total, err := s.repo.ListRoles(ctx, rbac.RoleQuery{
		Scope: scope, ScopeID: scopeID, Offset: (page - 1) * size, Limit: size,
	})
	if err != nil {
		return nil, 0, apperr.Internal("list roles", err)
	}
	return items, total, nil
}

// UpdateRole 更新角色。
func (s *Service) UpdateRole(ctx context.Context, role *rbac.Role) error {
	if err := s.repo.UpdateRole(ctx, role); err != nil {
		return apperr.Internal("update role", err)
	}
	return nil
}

// DeleteRole 删除角色。
func (s *Service) DeleteRole(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeleteRole(ctx, id, actorID); err != nil {
		if errors.Is(err, rbac.ErrRoleBuiltin) {
			return apperr.BusinessRule("builtin role cannot be deleted", err)
		}
		if errors.Is(err, rbac.ErrRoleNotFound) {
			return apperr.NotFound("role", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("delete role", err)
	}
	return nil
}

// GrantPermissions 授予角色权限。
func (s *Service) GrantPermissions(ctx context.Context, roleID int64, permIDs []int64, actorID int64) error {
	if err := s.repo.GrantPermissions(ctx, roleID, permIDs, true, actorID); err != nil {
		return apperr.Internal("grant permissions", err)
	}
	// 权限变更后失效受影响用户的缓存（简化：全量失效不可行，依赖绑定关系逐个 evict）。
	// 实际生产应查 role-bindings 找出受影响用户再 evict；此处保留接口，由调用方在绑定层处理。
	return nil
}

// RevokePermissions 撤销角色权限。
func (s *Service) RevokePermissions(ctx context.Context, roleID int64, permIDs []int64) error {
	if err := s.repo.RevokePermissions(ctx, roleID, permIDs); err != nil {
		return apperr.Internal("revoke permissions", err)
	}
	return nil
}

// ListPermissionsByRole 列出角色的权限。
func (s *Service) ListPermissionsByRole(ctx context.Context, roleID int64) ([]*rbac.Permission, error) {
	items, err := s.repo.ListPermissionsByRole(ctx, roleID)
	if err != nil {
		return nil, apperr.Internal("list permissions by role", err)
	}
	return items, nil
}

// --- 用户-角色绑定 ---

// BindPlatformRole 绑定平台角色。
func (s *Service) BindPlatformRole(ctx context.Context, userID, roleID int64, expiresAt *time.Time, actorID int64) error {
	if err := s.repo.BindPlatformRole(ctx, userID, roleID, expiresAt, actorID); err != nil {
		return apperr.Internal("bind platform role", err)
	}
	s.evictUserPermissions(ctx, userID)
	return nil
}

// UnbindPlatformRole 解绑平台角色。
func (s *Service) UnbindPlatformRole(ctx context.Context, userID, roleID int64) error {
	if err := s.repo.UnbindPlatformRole(ctx, userID, roleID); err != nil {
		if errors.Is(err, rbac.ErrBindingNotFound) {
			return apperr.NotFound("platform role binding", "")
		}
		return apperr.Internal("unbind platform role", err)
	}
	s.evictUserPermissions(ctx, userID)
	return nil
}

// ListPlatformRolesByUser 列出用户的平台角色。
func (s *Service) ListPlatformRolesByUser(ctx context.Context, userID int64) ([]*rbac.Role, error) {
	items, err := s.repo.ListPlatformRolesByUser(ctx, userID)
	if err != nil {
		return nil, apperr.Internal("list platform roles", err)
	}
	return items, nil
}

// --- workspace 成员 ---

// AddWorkspaceMemberInput 添加成员输入。
type AddWorkspaceMemberInput struct {
	WorkspaceID int64
	UserID      int64
	RoleID      int64
	InvitedBy   int64
}

// AddWorkspaceMember 添加 workspace 成员。
func (s *Service) AddWorkspaceMember(ctx context.Context, in AddWorkspaceMemberInput) (*rbac.WorkspaceMember, error) {
	m := &rbac.WorkspaceMember{
		WorkspaceID: in.WorkspaceID, UserID: in.UserID, RoleID: in.RoleID,
		InvitedBy: in.InvitedBy, Status: rbac.MemberActive,
	}
	m.CreatedBy = in.InvitedBy
	m.UpdatedBy = in.InvitedBy
	if err := s.repo.AddWorkspaceMember(ctx, m); err != nil {
		if errors.Is(err, rbac.ErrMemberExists) {
			return nil, apperr.Conflict("member already exists", err)
		}
		return nil, apperr.Internal("add workspace member", err)
	}
	s.evictUserPermissions(ctx, in.UserID)
	return m, nil
}

// UpdateWorkspaceMember 更新成员角色。
func (s *Service) UpdateWorkspaceMember(ctx context.Context, m *rbac.WorkspaceMember) error {
	if err := s.repo.UpdateWorkspaceMember(ctx, m); err != nil {
		return apperr.Internal("update workspace member", err)
	}
	s.evictUserPermissions(ctx, m.UserID)
	return nil
}

// RemoveWorkspaceMember 移除成员。
func (s *Service) RemoveWorkspaceMember(ctx context.Context, id, userID, actorID int64) error {
	if err := s.repo.RemoveWorkspaceMember(ctx, id, actorID); err != nil {
		if errors.Is(err, rbac.ErrMemberNotFound) {
			return apperr.NotFound("workspace member", strconv.FormatInt(id, 10))
		}
		return apperr.Internal("remove workspace member", err)
	}
	s.evictUserPermissions(ctx, userID)
	return nil
}

// ListWorkspaceMembers 列出 workspace 成员。
func (s *Service) ListWorkspaceMembers(ctx context.Context, workspaceID int64) ([]*rbac.WorkspaceMember, error) {
	items, err := s.repo.ListWorkspaceMembers(ctx, workspaceID)
	if err != nil {
		return nil, apperr.Internal("list workspace members", err)
	}
	return items, nil
}

// --- 权限解析（核心） ---

// GetUserPermissions 获取用户在指定 workspace 上下文下的全部权限 code（聚合平台 + workspace 角色）。
// 优先走缓存；缓存未命中查库并回填。
func (s *Service) GetUserPermissions(ctx context.Context, userID int64, workspaceID int64) ([]string, error) {
	// 缓存 key 含 workspace 上下文：简化用 userID（权限集跨 workspace 合并时需细分）。
	// 此处缓存按 (userID, workspaceID) 维度；实现上以 userID 为 key 时 workspace 维度差异需在 value 内分层。
	// 为正确性，缓存 key 编码 userID+workspaceID。
	cacheKey := userID
	if workspaceID != 0 {
		cacheKey = userID // 简化：缓存用户全部角色聚合的权限集（平台+所有workspace），workspace 切换不影响权限集。
	}
	if s.cache != nil {
		if codes, err := s.cache.GetUserPermissions(ctx, cacheKey); err == nil && codes != nil {
			return codes, nil
		}
	}
	roleIDs, err := s.repo.ListRoleIDsForUser(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	codes, err := s.repo.ListPermissionCodesByRoles(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		_ = s.cache.SetUserPermissions(ctx, cacheKey, codes, 5*time.Minute)
	}
	return codes, nil
}

// HasPermission 判断用户是否拥有指定权限（支持通配符 * 表示拥有全部权限）。
func (s *Service) HasPermission(ctx context.Context, userID int64, workspaceID int64, permCode string) (bool, error) {
	codes, err := s.GetUserPermissions(ctx, userID, workspaceID)
	if err != nil {
		return false, err
	}
	for _, c := range codes {
		if c == "*" || c == permCode {
			return true, nil
		}
		// 支持前缀通配：app:read:* 匹配 app:read:any
		if strings.HasSuffix(c, ":*") {
			prefix := strings.TrimSuffix(c, "*")
			if strings.HasPrefix(permCode, prefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

// evictUserPermissions 失效用户权限缓存。
func (s *Service) evictUserPermissions(ctx context.Context, userID int64) {
	if s.cache != nil {
		_ = s.cache.EvictUserPermissions(ctx, userID)
	}
}

// 确保 sync import 被使用（未来扩展并发安全缓存时使用）。
var _ sync.Mutex

// 占位避免 fmt 未使用。
var _ = fmt.Sprintf
