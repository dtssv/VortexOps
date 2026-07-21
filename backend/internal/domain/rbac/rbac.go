// Package rbac 是权限领域的核心实体与仓储接口。
// 覆盖：权限、菜单、角色、角色-权限、平台角色绑定、workspace 成员（含 role）、数据范围。
package rbac

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// --- 枚举 ---

type PermissionCategory string

const (
	PermCategoryMenu   PermissionCategory = "menu"
	PermCategoryAction PermissionCategory = "action"
	PermCategoryData   PermissionCategory = "data"
)

type PermissionScope string

const (
	PermScopePlatform    PermissionScope = "platform"
	PermScopeWorkspace   PermissionScope = "workspace"
	PermScopeApplication PermissionScope = "application"
)

type RoleScope string

const (
	RoleScopePlatform    RoleScope = "platform"
	RoleScopeWorkspace   RoleScope = "workspace"
	RoleScopeApplication RoleScope = "application"
)

type MenuType string

const (
	MenuTypeDirectory MenuType = "directory"
	MenuTypeMenu      MenuType = "menu"
	MenuTypeButton    MenuType = "button"
)

type MemberStatus string

const (
	MemberActive  MemberStatus = "active"
	MemberPending MemberStatus = "pending"
	MemberRemoved MemberStatus = "removed"
)

// DataScope 数据范围（控制用户能看哪些 workspace/app 的数据）。
type DataScope string

const (
	DataScopeAll        DataScope = "all"
	DataScopeWorkspace  DataScope = "workspace"
	DataScopeApp        DataScope = "application"
	DataScopeSelf       DataScope = "self"
)

// --- 实体 ---

// Permission 权限项。
type Permission struct {
	ID          int64
	Code        string
	Name        string
	Category    PermissionCategory
	Scope       PermissionScope
	Description string
	SortOrder   int
	Enabled     bool
	domain.Audit
}

// Menu 菜单项（树形）。
type Menu struct {
	ID             int64
	UUID           uuid.UUID
	ParentID       int64
	Code           string
	Name           string
	NameEN         string
	Path           string
	Icon           string
	Component      string
	MenuType       MenuType
	Scope          PermissionScope
	PermissionCode string
	Visible        bool
	SortOrder      int
	KeepAlive      bool
	ExternalLink   string
	Metadata       map[string]any
	domain.Audit
}

// Role 角色。
type Role struct {
	ID          int64
	UUID        uuid.UUID
	Scope       RoleScope
	ScopeID     int64
	Code        string
	Name        string
	Description string
	IsBuiltin   bool
	IsDefault   bool
	Enabled     bool
	Metadata    map[string]any
	domain.Audit
}

// RolePermission 角色-权限关联。
type RolePermission struct {
	ID           int64
	RoleID       int64
	PermissionID int64
	Granted      bool
	CreatedBy    int64
	CreatedAt    time.Time
}

// PlatformRoleBinding 平台级用户-角色绑定。
type PlatformRoleBinding struct {
	ID        int64
	UserID    int64
	RoleID    int64
	ExpiresAt *time.Time
	domain.Audit
}

// WorkspaceMember workspace 成员（含角色）。
type WorkspaceMember struct {
	ID          int64
	WorkspaceID int64
	UserID      int64
	RoleID      int64
	InvitedBy   int64
	JoinedAt    time.Time
	Status      MemberStatus
	domain.Audit
}

// 领域错误。
var (
	ErrPermissionNotFound  = errors.New("permission not found")
	ErrPermissionCodeExists = errors.New("permission code already exists")
	ErrMenuNotFound        = errors.New("menu not found")
	ErrMenuCodeExists      = errors.New("menu code already exists")
	ErrRoleNotFound        = errors.New("role not found")
	ErrRoleCodeExists      = errors.New("role code already exists")
	ErrRoleBuiltin         = errors.New("builtin role cannot be modified")
	ErrBindingNotFound     = errors.New("role binding not found")
	ErrBindingExists       = errors.New("role binding already exists")
	ErrMemberNotFound      = errors.New("workspace member not found")
	ErrMemberExists        = errors.New("workspace member already exists")
)

// PermissionQuery 权限查询。
type PermissionQuery struct {
	Category PermissionCategory
	Scope    PermissionScope
	Enabled  *bool
	Offset   int
	Limit    int
}

// RoleQuery 角色查询。
type RoleQuery struct {
	Scope   RoleScope
	ScopeID int64
	Enabled *bool
	Offset  int
	Limit   int
}

// AuditLogQuery 审计日志查询。
type AuditLogQuery struct {
	UserID       int64
	WorkspaceID  int64
	ResourceType string
	Action       string
	StartTime    time.Time
	EndTime      time.Time
	Offset       int
	Limit        int
}

// Repository 权限领域仓储接口。
type Repository interface {
	// 权限
	CreatePermission(ctx context.Context, p *Permission) error
	GetPermissionByID(ctx context.Context, id int64) (*Permission, error)
	GetPermissionByCode(ctx context.Context, code string) (*Permission, error)
	ListPermissions(ctx context.Context, q PermissionQuery) ([]*Permission, int64, error)
	UpdatePermission(ctx context.Context, p *Permission) error
	DeletePermission(ctx context.Context, id, actorID int64) error

	// 菜单
	CreateMenu(ctx context.Context, m *Menu) error
	GetMenuByID(ctx context.Context, id int64) (*Menu, error)
	GetMenuByCode(ctx context.Context, code string) (*Menu, error)
	ListMenus(ctx context.Context, scope PermissionScope) ([]*Menu, error)
	UpdateMenu(ctx context.Context, m *Menu) error
	DeleteMenu(ctx context.Context, id, actorID int64) error

	// 角色
	CreateRole(ctx context.Context, r *Role) error
	GetRoleByID(ctx context.Context, id int64) (*Role, error)
	GetRoleByCode(ctx context.Context, scope RoleScope, scopeID int64, code string) (*Role, error)
	ListRoles(ctx context.Context, q RoleQuery) ([]*Role, int64, error)
	UpdateRole(ctx context.Context, r *Role) error
	DeleteRole(ctx context.Context, id, actorID int64) error

	// 角色-权限
	GrantPermissions(ctx context.Context, roleID int64, permIDs []int64, granted bool, actorID int64) error
	RevokePermissions(ctx context.Context, roleID int64, permIDs []int64) error
	ListPermissionsByRole(ctx context.Context, roleID int64) ([]*Permission, error)
	ListPermissionCodesByRole(ctx context.Context, roleID int64) ([]string, error)

	// 平台角色绑定
	BindPlatformRole(ctx context.Context, userID, roleID int64, expiresAt *time.Time, actorID int64) error
	UnbindPlatformRole(ctx context.Context, userID, roleID int64) error
	ListPlatformRolesByUser(ctx context.Context, userID int64) ([]*Role, error)

	// workspace 成员
	AddWorkspaceMember(ctx context.Context, m *WorkspaceMember) error
	UpdateWorkspaceMember(ctx context.Context, m *WorkspaceMember) error
	RemoveWorkspaceMember(ctx context.Context, id, actorID int64) error
	GetWorkspaceMember(ctx context.Context, workspaceID, userID int64) (*WorkspaceMember, error)
	ListWorkspaceMembers(ctx context.Context, workspaceID int64) ([]*WorkspaceMember, error)
	ListWorkspaceRolesByUser(ctx context.Context, userID int64) ([]*WorkspaceMember, error)

	// 权限解析：聚合用户在平台 + workspace 维度的所有角色 ID。
	ListRoleIDsForUser(ctx context.Context, userID int64, workspaceID int64) ([]int64, error)
	// 权限解析：根据角色 ID 列表聚合所有权限 code（去重）。
	ListPermissionCodesByRoles(ctx context.Context, roleIDs []int64) ([]string, error)
	// 菜单解析：根据权限 code 集合与角色 ID 集合返回可见菜单（OR 关系）。
	// permission_code 为空的菜单对所有人可见；menu 直接绑定到 roleIDs 中任一角色 → 可见；
	// menu 的 permission_code 命中 permCodes → 可见。
	ListVisibleMenus(ctx context.Context, scope PermissionScope, permCodes []string, roleIDs []int64) ([]*Menu, error)

	// 角色-菜单直接绑定
	BindMenusToRole(ctx context.Context, roleID int64, menuIDs []int64, actorID int64) error
	UnbindMenusFromRole(ctx context.Context, roleID int64, menuIDs []int64) error
	ListMenuIDsByRole(ctx context.Context, roleID int64) ([]int64, error)
	ListMenusByRole(ctx context.Context, roleID int64) ([]*Menu, error)
}
