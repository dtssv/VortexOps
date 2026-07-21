// Package release 是发布领域的核心实体与仓储接口。
// 覆盖：发布记录、发布事件、批次记录、发布预设、发布窗口。
// 发布把 group 的期望态（image/config/replicas）应用到 K8s 工作负载。
package release

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/vortexops/vortexops/internal/domain"
)

// --- 枚举 ---

type ReleaseType string

const (
	ReleaseInitial ReleaseType = "initial"
	ReleaseRolling ReleaseType = "rolling"
	ReleaseRollback ReleaseType = "rollback"
	ReleasePause   ReleaseType = "pause"
	ReleaseResume  ReleaseType = "resume"
	ReleaseConfig  ReleaseType = "config"
	ReleaseScale   ReleaseType = "scale"
	ReleaseRestart ReleaseType = "restart"
)

type Strategy string

const (
	StrategyRolling     Strategy = "rolling"
	StrategyRecreate    Strategy = "recreate"
	StrategyBlueGreen   Strategy = "blue_green"
	StrategyCanary      Strategy = "canary"
	StrategyPercentage  Strategy = "percentage"   // 按分组副本数百分比分批（candidate 逐步晋升）
	StrategyMachineCount Strategy = "machine_count" // 按指定 Pod 名分批（candidate 钉到指定 Pod）
)

type Status string

const (
	StatusPending        Status = "pending"
	StatusPendingApproval Status = "pending_approval"
	StatusRunning        Status = "running"
	StatusPaused         Status = "paused"
	StatusSucceeded      Status = "succeeded"
	StatusFailed         Status = "failed"
	StatusAborted        Status = "aborted"
	StatusInterrupted    Status = "interrupted" // 被同分组新发布抢占而中断
	StatusRolledBack     Status = "rolled_back"
)

type TriggerSource string

const (
	TriggerManual   TriggerSource = "manual"
	TriggerWebhook  TriggerSource = "webhook"
	TriggerAPI      TriggerSource = "api"
	TriggerSchedule TriggerSource = "schedule"
)

type PresetScope string

const (
	PresetScopePlatform    PresetScope = "platform"
	PresetScopeWorkspace   PresetScope = "workspace"
	PresetScopeApplication PresetScope = "application"
)

// --- 实体 ---

// Release 发布记录。
type Release struct {
	ID                    int64
	UUID                  uuid.UUID
	GroupID               int64
	ReleaseNumber         int
	PreviousReleaseID     int64
	ImageID               int64
	ConfigVersion         int
	ReleaseType           ReleaseType
	Replicas              int
	Strategy              Strategy
	MaxSurge              string
	MaxUnavailable        string
	BatchSize             int
	BatchIntervalSec      int
	// 多版本分批发布参数：
	// TargetPercentage：percentage 策略目标百分比（1-100），候选副本数=ceil(group.replicas*pct/100)。
	// TargetPodNames：machine_count 策略目标 Pod 名列表，candidate Deployment 钉到这些 Pod（通过 podName 约束）。
	TargetPercentage  int
	TargetPodNames    []string
	Paused                bool
	Status                Status
	ProgressPercent       int
	FailureReason         string
	StartedAt             time.Time
	FinishedAt            *time.Time
	DurationMs            int64
	TriggeredBy           int64
	TriggerSource         TriggerSource
	AutoRollbackOnFailure bool
	RollbackOfReleaseID   int64
	domain.Audit
}

// ReleaseEvent 发布事件（进度/状态变更日志）。
type ReleaseEvent struct {
	ID          int64
	ReleaseID   int64
	Seq         int
	EventType   string
	Message     string
	OperatorID  int64
	OperatorName string
	OccurredAt  time.Time
}

// ReleaseBatchRecord 发布批次记录（分批发布时每批的状态）。
type ReleaseBatchRecord struct {
	ID         int64
	ReleaseID  int64
	BatchIndex int
	Status     Status
	StartedAt  *time.Time
	FinishedAt *time.Time
	Message    string
}

// ReleasePreset 发布预设（常用发布配置模板）。
type ReleasePreset struct {
	ID                    int64
	UUID                  uuid.UUID
	Scope                 PresetScope
	ScopeID               int64
	Name                  string
	Description           string
	Strategy              Strategy
	MaxSurge              string
	MaxUnavailable        string
	BatchSize             int
	BatchIntervalSec      int
	AutoRollbackOnFailure bool
	IsDefault             bool
	domain.Audit
}

// ReleaseWindow 发布窗口（限定可发布时间）。
type ReleaseWindow struct {
	ID              int64
	UUID            uuid.UUID
	ApplicationID   int64
	Name            string
	Timezone        string
	Crontab         string
	DurationMinutes int
	IsActive        bool
	domain.Audit
}

// 领域错误。
var (
	ErrReleaseNotFound     = errors.New("release not found")
	ErrPresetNotFound      = errors.New("release preset not found")
	ErrWindowNotFound      = errors.New("release window not found")
	ErrReleaseNotCancellable = errors.New("release cannot be cancelled in current state")
	ErrReleaseNotRollable  = errors.New("release cannot be rolled back")
	ErrNoPreviousRelease   = errors.New("no previous release to roll back to")
	ErrEventNotFound       = errors.New("release event not found")
)

// CreateReleaseInput 创建发布输入。
type CreateReleaseInput struct {
	GroupID               int64
	PreviousReleaseID     int64
	ImageID               int64
	ConfigVersion         int
	ReleaseType           ReleaseType
	Replicas              int
	Strategy              Strategy
	MaxSurge              string
	MaxUnavailable        string
	BatchSize             int
	BatchIntervalSec      int
	TriggeredBy           int64
	TriggerSource         TriggerSource
	AutoRollbackOnFailure bool
}

// ReleaseQuery 发布查询。
type ReleaseQuery struct {
	GroupID  int64
	Status   Status
	Offset   int
	Limit    int
}

// Repository 发布领域仓储接口。
type Repository interface {
	CreateRelease(ctx context.Context, r *Release) error
	GetReleaseByID(ctx context.Context, id int64) (*Release, error)
	GetReleaseByUUID(ctx context.Context, id uuid.UUID) (*Release, error)
	NextReleaseNumber(ctx context.Context, groupID int64) (int, error)
	ListReleases(ctx context.Context, q ReleaseQuery) ([]*Release, int64, error)
	UpdateReleaseStatus(ctx context.Context, id int64, status Status, progress int, failureReason string, version int) (*Release, error)
	CompleteRelease(ctx context.Context, id int64, status Status, durationMs int64, finishedAt time.Time, version int) (*Release, error)
	GetLastSuccessfulRelease(ctx context.Context, groupID int64) (*Release, error)
	GetCurrentRelease(ctx context.Context, groupID int64) (*Release, error)
	// GetReleasesByStatus 返回指定分组下处于指定状态的发布（按 release_number 降序）。
	GetReleasesByStatus(ctx context.Context, groupID int64, status Status) ([]*Release, error)

	// 发布事件
	AppendEvent(ctx context.Context, e *ReleaseEvent) error
	ListEvents(ctx context.Context, releaseID int64) ([]*ReleaseEvent, error)

	// 批次记录
	CreateBatchRecord(ctx context.Context, b *ReleaseBatchRecord) error
	UpdateBatchRecord(ctx context.Context, b *ReleaseBatchRecord) error
	ListBatchRecords(ctx context.Context, releaseID int64) ([]*ReleaseBatchRecord, error)

	// 预设
	CreatePreset(ctx context.Context, p *ReleasePreset) error
	GetPresetByID(ctx context.Context, id int64) (*ReleasePreset, error)
	ListPresets(ctx context.Context, scope PresetScope, scopeID int64, offset, limit int) ([]*ReleasePreset, int64, error)
	UpdatePreset(ctx context.Context, p *ReleasePreset) error
	DeletePreset(ctx context.Context, id, actorID int64) error

	// 窗口
	CreateWindow(ctx context.Context, w *ReleaseWindow) error
	GetWindowByID(ctx context.Context, id int64) (*ReleaseWindow, error)
	ListWindows(ctx context.Context, appID int64) ([]*ReleaseWindow, error)
	UpdateWindow(ctx context.Context, w *ReleaseWindow) error
	DeleteWindow(ctx context.Context, id, actorID int64) error

	// group 期望态更新（发布成功后回写 group 的 current_release_id/current_image_id/current_config_id）
	UpdateGroupCurrentRelease(ctx context.Context, groupID, releaseID, imageID int64, configVersion int) error

	// 多版本共存：分批发布推进中更新候选版本（candidate Deployment）。
	UpdateGroupCandidate(ctx context.Context, groupID, releaseID, imageID int64, candidateReplicas int) error
	// 多版本共存：发布晋升/回滚后清空候选版本。
	ClearGroupCandidate(ctx context.Context, groupID int64) error
}
