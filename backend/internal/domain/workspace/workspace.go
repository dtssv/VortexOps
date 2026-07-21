// Package workspace 是空间领域。空间是资源层次的最顶层：Workspace → Application → Group。
package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// Status 空间状态。
type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
	StatusFrozen   Status = "frozen"
)

// ClusterRole 空间绑定集群的角色。
type ClusterRole string

const (
	ClusterRolePrimary   ClusterRole = "primary"
	ClusterRoleSecondary ClusterRole = "secondary"
)

// Type 空间类型。用于区分通用应用空间与按类型自动建的专用空间（中间件/推理）。
type Type string

const (
	TypeApp        Type = "app"
	TypeInference  Type = "inference"
)

// Quota 空间配额。
type Quota struct {
	MaxApplications      int
	MaxGroups            int
	MaxConcurrentBuilds  int
	MaxImagesRetained    int
	MaxMembers           int
}

// Workspace 空间实体。
type Workspace struct {
	ID                int64
	UUID              uuid.UUID
	Name              string
	DisplayName       string
	Description       string
	LogoURL           string
	Status            Status
	Type              Type
	OwnerID           int64
	DefaultRegistryID int64
	DefaultJenkinsID  int64
	Labels            map[string]string
	Metadata          map[string]any
	domain.Audit
}

// IsActive 是否处于 active 状态。
func (w *Workspace) IsActive() bool { return w.Status == StatusActive }

// IsOwner 是否是指定用户的所有者。
func (w *Workspace) IsOwner(userID int64) bool { return w.OwnerID == userID }

// Member 空间成员。
type Member struct {
	ID          int64
	WorkspaceID int64
	UserID      int64
	RoleID      int64
	InvitedBy   int64
	JoinedAt    time.Time
	Status      string
	domain.Audit

	// 以下字段仅在 ListMembers 时由仓储 JOIN 填充（展示用），非持久化字段。
	Username    string
	DisplayName string
	AvatarURL   string
	RoleCode    string
	RoleName    string
}

// MemberStatus 成员状态。
const (
	MemberStatusActive  = "active"
	MemberStatusPending = "pending"
	MemberStatusRemoved = "removed"
)

// ClusterBinding 空间与集群的绑定。
type ClusterBinding struct {
	ID                 int64
	UUID               uuid.UUID
	WorkspaceID        int64
	ClusterID          int64
	Namespace          string
	Role               ClusterRole
	AutoCreateNS       bool
	ResourceQuota      map[string]any
	domain.Audit
}

// 领域错误。
var (
	ErrWorkspaceNotFound   = errors.New("workspace not found")
	ErrWorkspaceNameExists = errors.New("workspace name already exists")
	ErrWorkspaceArchived   = errors.New("workspace is archived")
	ErrWorkspaceFrozen     = errors.New("workspace is frozen")
	ErrWorkspaceNotEmpty   = errors.New("workspace not empty")
	ErrMemberExists        = errors.New("member already exists in workspace")
	ErrMemberNotFound      = errors.New("member not found")
	ErrClusterBindingNotFound = errors.New("cluster binding not found")
	ErrQuotaExceeded       = errors.New("quota exceeded")
	ErrNotOwner            = errors.New("user is not workspace owner")
)

// CreateInput 创建空间输入。
type CreateInput struct {
	Name              string
	DisplayName       string
	Description       string
	LogoURL           string
	Type              Type
	OwnerID           int64
	DefaultRegistryID int64
	DefaultJenkinsID  int64
	Labels            map[string]string
	Metadata          map[string]any
	// DefaultQuota 创建时应用的默认配额，nil 则用系统默认。
	DefaultQuota *Quota
	CreatedBy    int64
}

// UpdateInput 更新空间输入。
type UpdateInput struct {
	ID                int64
	DisplayName       *string
	Description       *string
	LogoURL           *string
	Status            *Status
	DefaultRegistryID *int64
	DefaultJenkinsID  *int64
	Labels            *map[string]string
	Metadata          *map[string]any
	Version           int
	UpdatedBy         int64
}

// Query 空间查询。
type Query struct {
	OwnerID int64
	Status  Status
	Type    Type
	Search  string
	Offset  int
	Limit   int
}

// MemberQuery 成员查询。
type MemberQuery struct {
	WorkspaceID int64
	UserID      int64
	RoleID      int64
	Status      string
	Offset      int
	Limit       int
}

// Repository 空间仓储接口。
type Repository interface {
	Create(ctx context.Context, w *Workspace, quota *Quota) error
	GetByID(ctx context.Context, id int64) (*Workspace, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (*Workspace, error)
	GetByName(ctx context.Context, name string) (*Workspace, error)
	GetByTypeAndName(ctx context.Context, t Type, name string) (*Workspace, error)
	Update(ctx context.Context, in UpdateInput) (*Workspace, error)
	List(ctx context.Context, q Query) ([]*Workspace, int64, error)
	Delete(ctx context.Context, id, deletedBy int64) error

	// 配额
	GetQuota(ctx context.Context, workspaceID int64) (*Quota, error)
	UpdateQuota(ctx context.Context, workspaceID int64, q Quota, version int, updatedBy int64) error

	// 成员
	AddMember(ctx context.Context, m *Member) error
	GetMember(ctx context.Context, workspaceID, userID int64) (*Member, error)
	ListMembers(ctx context.Context, q MemberQuery) ([]*Member, int64, error)
	UpdateMemberRole(ctx context.Context, workspaceID, userID, roleID, updatedBy int64) error
	RemoveMember(ctx context.Context, workspaceID, userID, removedBy int64) error
	CountMembers(ctx context.Context, workspaceID int64) (int64, error)

	// 集群绑定
	AddClusterBinding(ctx context.Context, b *ClusterBinding) error
	ListClusterBindings(ctx context.Context, workspaceID int64) ([]*ClusterBinding, error)
	RemoveClusterBinding(ctx context.Context, workspaceID, clusterID, removedBy int64) error

	// 计数（用于配额校验）
	CountApplications(ctx context.Context, workspaceID int64) (int64, error)
	CountGroups(ctx context.Context, workspaceID int64) (int64, error)
}
