// Package application 是应用与分组领域。
// 层次：Workspace → Application → Group。
// Group 是对应 K8s 工作负载（Deployment/StatefulSet/CronJob/Job）的逻辑分组。
package application

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// Lifecycle 应用生命周期。
type Lifecycle string

const (
	LifecycleActive   Lifecycle = "active"
	LifecycleFrozen   Lifecycle = "frozen"
	LifecycleArchived Lifecycle = "archived"
)

// Environment 环境标识。
type Environment string

const (
	EnvDev     Environment = "dev"
	EnvTest    Environment = "test"
	EnvStaging Environment = "staging"
	EnvProd    Environment = "prod"
)

// WorkloadType 工作负载类型。
type WorkloadType string

const (
	WorkloadDeployment  WorkloadType = "deployment"
	WorkloadStatefulSet WorkloadType = "statefulset"
	WorkloadCronJob     WorkloadType = "cronjob"
	WorkloadJob         WorkloadType = "job"
)

// Strategy 发布策略。
type Strategy string

const (
	StrategyRolling  Strategy = "rolling"
	StrategyRecreate Strategy = "recreate"
)

// MemberStatus 应用成员状态。
const (
	MemberStatusActive  = "active"
	MemberStatusPending = "pending"
	MemberStatusRemoved = "removed"
)

// Application 应用实体。
type Application struct {
	ID                int64
	UUID              uuid.UUID
	WorkspaceID       int64
	Name              string
	Code              string
	DisplayName       string
	Description       string
	Icon              string
	DefaultGitSourceID   int64
	DefaultRegistryID    int64
	Lifecycle         Lifecycle
	OwnerID           int64
	Labels            map[string]string
	Metadata          map[string]any
	domain.Audit
}

// AppType 应用类型常量（同时存于 Application.Code 派生与 Group.AppType 列）。
const (
	AppTypeWeb        = "web"
	AppTypeWorker     = "worker"
	AppTypeJob        = "job"
	AppTypeInference  = "inference"
)

// IsActive 应用是否处于 active 状态。
func (a *Application) IsActive() bool { return a.Lifecycle == LifecycleActive }

// HealthCheck 健康检查配置。
type HealthCheck struct {
	LivenessProbe  map[string]any `json:"liveness_probe,omitempty"`
	ReadinessProbe map[string]any `json:"readiness_probe,omitempty"`
	StartupProbe   map[string]any `json:"startup_probe,omitempty"`
}

// Resources 资源配置。
type Resources struct {
	CPUm                int    `json:"cpu_m"`
	CPULimitM           int    `json:"cpu_limit_m,omitempty"`
	MemoryBytes         int64  `json:"memory_bytes"`
	MemoryLimitBytes    int64  `json:"memory_limit_bytes,omitempty"`
	GPU                 int    `json:"gpu,omitempty"`
	GPUType             string `json:"gpu_type,omitempty"`
	GPUResourceName     string `json:"gpu_resource_name,omitempty"`
}

// Storage 存储配置。
type Storage struct {
	StorageSizeBytes           int64  `json:"storage_size_bytes,omitempty"`
	StorageClass               string `json:"storage_class,omitempty"`
	EphemeralStorageRequestBytes int64 `json:"ephemeral_storage_request_bytes,omitempty"`
	EphemeralStorageLimitBytes   int64 `json:"ephemeral_storage_limit_bytes,omitempty"`
	ResourceTemplateID         int64  `json:"resource_template_id,omitempty"`
}

// Scheduling 调度配置。
type Scheduling struct {
	NodeSelector   map[string]string `json:"node_selector,omitempty"`
	NodeAffinity   map[string]any    `json:"node_affinity,omitempty"`
	Tolerations    []map[string]any  `json:"tolerations,omitempty"`
	PriorityClass  string            `json:"priority_class,omitempty"`
}

// Workload 工作负载相关配置。
type Workload struct {
	Type         WorkloadType `json:"type"`
	CronSchedule string       `json:"cron_schedule,omitempty"`
	JobPolicy    map[string]any `json:"job_policy,omitempty"`
	Strategy     Strategy      `json:"strategy"`
	MaxSurge     string        `json:"max_surge"`
	MaxUnavailable string      `json:"max_unavailable"`
}

// Autoscaling 自动伸缩配置。
type Autoscaling struct {
	Enabled       bool              `json:"enabled"`
	MinReplicas   int               `json:"min_replicas,omitempty"`
	MaxReplicas   int               `json:"max_replicas,omitempty"`
	Metrics       []map[string]any  `json:"metrics,omitempty"`
	Behavior      map[string]any    `json:"behavior,omitempty"`
}

// Group 分组实体（对应 K8s 工作负载）。
type Group struct {
	ID                int64
	UUID              uuid.UUID
	ApplicationID     int64
	Name              string
	DisplayName       string
	Description       string
	AppType           string
	Environment       Environment
	ClusterID         int64
	Namespace         string
	DeploymentName    string
	ServiceName       string
	Replicas          int
	CurrentImageID    int64
	CurrentConfigID   int64
	CurrentReleaseID  int64
	// 多版本共存（candidate Deployment 模式）：发布进行中时记录候选版本，
	// 与 CurrentImageID 并存最多两版本。发布完成后晋升为 Current，清空候选。
	CandidateImageID   int64
	CandidateReleaseID int64
	CandidateReplicas  int // 候选 Deployment 当前副本数（分批推进时递增）
	Resources         Resources
	Storage           Storage
	MeshEnabled       bool // 分组维度是否启用 Mesh（Cilium L7 治理），默认 false；Phase 5 生效
	Scheduling        Scheduling
	Workload          Workload
	HealthCheck       *HealthCheck
	Autoscaling       *Autoscaling
	ReleaseRequiresApproval bool
	Labels            map[string]string
	Metadata          map[string]any
	domain.Audit
}

// Member 应用成员。
type Member struct {
	ID            int64
	ApplicationID int64
	UserID        int64
	RoleID        int64
	InvitedBy     int64
	JoinedAt      time.Time
	Status        string
	// 关联展示字段（join vo_users / vo_roles 填充，仅查询场景）。
	UserName    string
	DisplayName string
	Email       string
	RoleName    string
	domain.Audit
}

// 领域错误。
var (
	ErrApplicationNotFound   = errors.New("application not found")
	ErrApplicationNameExists = errors.New("application name already exists in workspace")
	ErrApplicationCodeExists = errors.New("application code already exists in workspace")
	ErrApplicationArchived   = errors.New("application is archived")
	ErrApplicationFrozen     = errors.New("application is frozen")
	ErrApplicationNotEmpty   = errors.New("application not empty")
	ErrGroupNotFound         = errors.New("group not found")
	ErrGroupNameExists       = errors.New("group name already exists in application")
	ErrAppMemberExists       = errors.New("member already exists in application")
	ErrAppMemberNotFound     = errors.New("application member not found")
	ErrNotAppOwner           = errors.New("user is not application owner")
)

// CreateApplicationInput 创建应用输入。
type CreateApplicationInput struct {
	WorkspaceID       int64
	Name              string
	Code              string
	DisplayName       string
	Description       string
	Icon              string
	DefaultRegistryID int64
	OwnerID           int64
	Labels            map[string]string
	Metadata          map[string]any
	CreatedBy         int64
}

// UpdateApplicationInput 更新应用输入。
type UpdateApplicationInput struct {
	ID                   int64
	DisplayName          *string
	Description          *string
	Icon                 *string
	Lifecycle            *Lifecycle
	DefaultGitSourceID   *int64
	DefaultRegistryID    *int64
	Labels               *map[string]string
	Metadata             *map[string]any
	Version              int
	UpdatedBy            int64
}

// ApplicationQuery 应用查询。
type ApplicationQuery struct {
	WorkspaceID int64
	OwnerID     int64
	Lifecycle   Lifecycle
	AppType     string
	Search      string
	Offset      int
	Limit       int
}

// CreateGroupInput 创建分组输入。
type CreateGroupInput struct {
	ApplicationID int64
	Name          string
	DisplayName   string
	Description   string
	Environment   Environment
	ClusterID     int64
	Namespace     string
	Replicas      int
	Resources     Resources
	Storage       Storage
	MeshEnabled   bool
	Scheduling    Scheduling
	Workload      Workload
	HealthCheck   *HealthCheck
	Autoscaling   *Autoscaling
	ReleaseRequiresApproval bool
	Labels        map[string]string
	Metadata      map[string]any
	CreatedBy     int64
}

// UpdateGroupInput 更新分组输入。
type UpdateGroupInput struct {
	ID                  int64
	DisplayName         *string
	Description         *string
	Replicas            *int
	Resources           *Resources
	Storage             *Storage
	MeshEnabled         *bool
	Scheduling          *Scheduling
	Workload            *Workload
	HealthCheck         *HealthCheck
	Autoscaling         *Autoscaling
	ReleaseRequiresApproval *bool
	Labels              *map[string]string
	Metadata            *map[string]any
	Version             int
	UpdatedBy           int64
}

// GroupQuery 分组查询。
type GroupQuery struct {
	ApplicationID int64
	Environment   Environment
	ClusterID     int64
	AppType       string
	Search        string
	Offset        int
	Limit         int
}

// Repository 应用与分组仓储接口。
type Repository interface {
	// 应用
	CreateApplication(ctx context.Context, a *Application) error
	GetApplicationByID(ctx context.Context, id int64) (*Application, error)
	GetApplicationByUUID(ctx context.Context, id uuid.UUID) (*Application, error)
	GetApplicationByName(ctx context.Context, workspaceID int64, name string) (*Application, error)
	GetApplicationByCode(ctx context.Context, workspaceID int64, code string) (*Application, error)
	UpdateApplication(ctx context.Context, in UpdateApplicationInput) (*Application, error)
	ListApplications(ctx context.Context, q ApplicationQuery) ([]*Application, int64, error)
	DeleteApplication(ctx context.Context, id, deletedBy int64) error

	// 应用成员
	AddAppMember(ctx context.Context, m *Member) error
	GetAppMember(ctx context.Context, applicationID, userID int64) (*Member, error)
	ListAppMembers(ctx context.Context, applicationID int64, offset, limit int) ([]*Member, int64, error)
	UpdateAppMemberRole(ctx context.Context, applicationID, userID, roleID, updatedBy int64) error
	RemoveAppMember(ctx context.Context, applicationID, userID, removedBy int64) error

	// 分组
	CreateGroup(ctx context.Context, g *Group) error
	GetGroupByID(ctx context.Context, id int64) (*Group, error)
	GetGroupByUUID(ctx context.Context, id uuid.UUID) (*Group, error)
	GetGroupByName(ctx context.Context, applicationID int64, name string) (*Group, error)
	UpdateGroup(ctx context.Context, in UpdateGroupInput) (*Group, error)
	ListGroups(ctx context.Context, q GroupQuery) ([]*Group, int64, error)
	DeleteGroup(ctx context.Context, id, deletedBy int64) error
}
