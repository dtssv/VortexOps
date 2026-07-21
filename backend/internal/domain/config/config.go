// Package configdomain 是配置管理领域的核心实体与仓储接口。
// 覆盖：配置（env/file/mount/secret/configmap）、配置版本、ConfigSet（跨 group 共享）、Group-Config 绑定。
package configdomain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// --- 枚举 ---

type Scope string

const (
	ScopeWorkspace   Scope = "workspace"
	ScopeApplication Scope = "application"
	ScopeGroup       Scope = "group"
)

type ConfigType string

const (
	ConfigEnv       ConfigType = "env"
	ConfigFile      ConfigType = "file"
	ConfigMount     ConfigType = "mount"
	ConfigSecret    ConfigType = "secret"
	ConfigConfigMap ConfigType = "configmap"
)

type ConfigStatus string

const (
	ConfigActive   ConfigStatus = "active"
	ConfigArchived ConfigStatus = "archived"
)

// --- 实体 ---

// Config 配置版本（每次变更生成新版本，不可变）。
type Config struct {
	ID             int64
	UUID           uuid.UUID
	Scope          Scope
	ScopeID        int64
	GroupID        int64
	Name           string
	ConfigType     ConfigType
	ConfigVersion  int
	Description    string
	RenderedContent string
	DiffWithPrevious string
	Checksum       string
	Status         ConfigStatus
	domain.Audit
}

// ConfigSet 配置集（应用维度，一个应用可有多个配置集）。
// Content 为结构化 JSON：{files:[{path,content,mode,is_secret}], env:[{name,value,is_secret}], command:[...], args:[...]}。
// 历史上 ConfigSet 为 workspace 维度；迁移后以 application_id 为主，workspace_id 保留兼容。
type ConfigSet struct {
	ID           int64
	UUID         uuid.UUID
	WorkspaceID  int64
	ApplicationID int64 // 应用维度（新模型主键）；0 表示历史数据未关联应用
	Name         string
	Description  string
	Content      map[string]any
	domain.Audit
}

// GroupLocalConfig 分组本地配置（与配置集绑定互斥）。
// 当分组未绑定任何配置集时，可直接维护本地配置；绑定配置集后本地配置被覆盖且不可编辑。
// Content 结构与 ConfigSet.Content 同构：
//   { files:[{path,content,mode,is_secret}], env:[{name,value,is_secret}], command:[...], args:[...] }
// 每个分组至多一条未删除记录（uk_group_local_cfg）。
type GroupLocalConfig struct {
	ID          int64
	UUID        uuid.UUID
	GroupID     int64
	Name        string
	Description string
	Content     map[string]any
	domain.Audit
}

// SnapshotTargetType 配置内容快照归属类型。
type SnapshotTargetType string

const (
	SnapshotConfigSet  SnapshotTargetType = "config_set"
	SnapshotGroupLocal SnapshotTargetType = "group_local"
	SnapshotGroupBind  SnapshotTargetType = "group_bind" // 分组绑定/解绑时的生效配置快照
)

// ContentSnapshot 配置文件类型内容的历史快照（files 变更时生成）。
type ContentSnapshot struct {
	ID           int64
	TargetType   SnapshotTargetType
	TargetID     int64
	SnapshotNo   int
	Content      map[string]any
	ChangeReason string
	FilesHash    string
	CreatedBy    int64
	CreatedAt    time.Time
}

// GroupConfigBinding group 与配置集的绑定。
// 历史绑定走 ConfigID（指向 vo_configs）；新绑定走 ConfigSetID（指向 vo_config_sets）。
// Priority 决定多配置集合并顺序；PinnedVersion 锁定特定版本（nil=最新）。
// 一个分组至多一条未删除绑定（uk_group_single_binding）。
type GroupConfigBinding struct {
	ID           int64
	GroupID      int64
	ConfigID     int64 // 兼容历史（指向 vo_configs）
	ConfigSetID  int64 // 新模型（指向 vo_config_sets）
	Priority     int
	PinnedVersion *int
	MountPath    string
	SubPath      string
	domain.Audit
}

// 领域错误。
var (
	ErrConfigNotFound      = errors.New("config not found")
	ErrConfigSetNotFound   = errors.New("config set not found")
	ErrConfigSetExists     = errors.New("config set name already exists")
	ErrBindingNotFound     = errors.New("config binding not found")
	ErrBindingExists       = errors.New("config binding already exists")
	ErrNoPreviousVersion   = errors.New("no previous config version to diff")
	// 一个分组至多绑定一个配置集。
	ErrGroupAlreadyBound = errors.New("group already has a config set binding")
	// 分组已绑定配置集时禁止编辑本地配置。
	ErrGroupBoundCannotEditLocal = errors.New("group has config set binding, cannot edit local config")
	// 分组存在本地配置时仍允许绑定，绑定后本地配置被覆盖（只读）。
	ErrLocalConfigNotFound = errors.New("group local config not found")
	// 配置集仍被分组绑定时禁止删除。
	ErrConfigSetInUse = errors.New("config set is still bound by groups")
	ErrSnapshotNotFound = errors.New("config snapshot not found")
)

// CreateConfigInput 创建配置输入。
type CreateConfigInput struct {
	Scope           Scope
	ScopeID         int64
	GroupID         int64
	Name            string
	ConfigType      ConfigType
	Description     string
	RenderedContent string
	CreatedBy       int64
}

// ConfigQuery 配置查询。
type ConfigQuery struct {
	Scope    Scope
	ScopeID  int64
	GroupID  int64
	Name     string
	Offset   int
	Limit    int
}

// Repository 配置领域仓储接口。
type Repository interface {
	// 配置
	CreateConfig(ctx context.Context, c *Config) error
	GetConfigByID(ctx context.Context, id int64) (*Config, error)
	GetLatestConfig(ctx context.Context, scope Scope, scopeID int64, groupID int64, name string) (*Config, error)
	GetConfigByVersion(ctx context.Context, scope Scope, scopeID int64, name string, version int) (*Config, error)
	ListConfigs(ctx context.Context, q ConfigQuery) ([]*Config, int64, error)
	ArchiveConfig(ctx context.Context, id int64) error
	NextConfigVersion(ctx context.Context, scope Scope, scopeID int64, name string) (int, error)
	UpdateGroupCurrentConfig(ctx context.Context, groupID, configID int64) error

	// ConfigSet
	CreateConfigSet(ctx context.Context, cs *ConfigSet) error
	GetConfigSetByID(ctx context.Context, id int64) (*ConfigSet, error)
	GetConfigSetByName(ctx context.Context, workspaceID int64, name string) (*ConfigSet, error)
	ListConfigSets(ctx context.Context, workspaceID int64, offset, limit int) ([]*ConfigSet, int64, error)
	ListConfigSetsByApplication(ctx context.Context, applicationID int64) ([]*ConfigSet, error)
	UpdateConfigSet(ctx context.Context, cs *ConfigSet) error
	DeleteConfigSet(ctx context.Context, id, actorID int64) error

	// 绑定
	CreateBinding(ctx context.Context, b *GroupConfigBinding) error
	GetBindingByID(ctx context.Context, id int64) (*GroupConfigBinding, error)
	GetBinding(ctx context.Context, groupID, configID int64) (*GroupConfigBinding, error)
	ListBindingsByGroup(ctx context.Context, groupID int64) ([]*GroupConfigBinding, error)
	// ListActiveBindingsByConfigSet 列出某配置集当前未删除的所有绑定（用于删除前校验）。
	ListActiveBindingsByConfigSet(ctx context.Context, configSetID int64) ([]*GroupConfigBinding, error)
	UpdateBinding(ctx context.Context, b *GroupConfigBinding) error
	DeleteBinding(ctx context.Context, id, actorID int64) error
	// CountActiveBindingsByGroup 统计分组当前未删除的绑定数（用于单绑定校验）。
	CountActiveBindingsByGroup(ctx context.Context, groupID int64) (int64, error)

	// 分组本地配置（与配置集绑定互斥）
	UpsertLocalConfig(ctx context.Context, c *GroupLocalConfig) error
	GetLocalConfigByGroup(ctx context.Context, groupID int64) (*GroupLocalConfig, error)
	DeleteLocalConfig(ctx context.Context, groupID, actorID int64) error

	// 配置内容快照
	CreateContentSnapshot(ctx context.Context, s *ContentSnapshot) error
	ListContentSnapshots(ctx context.Context, targetType SnapshotTargetType, targetID int64) ([]*ContentSnapshot, error)
	GetContentSnapshot(ctx context.Context, id int64) (*ContentSnapshot, error)
	NextSnapshotNo(ctx context.Context, targetType SnapshotTargetType, targetID int64) (int, error)
}
