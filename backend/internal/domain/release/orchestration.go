package release

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// --- 编排策略 ---

type OrchestrationStrategy string

const (
	OrchSequential OrchestrationStrategy = "sequential"
	OrchParallel   OrchestrationStrategy = "parallel"
	OrchCanary     OrchestrationStrategy = "canary"
)

type OrchestrationStatus string

const (
	OrchStatusPending   OrchestrationStatus = "pending"
	OrchStatusRunning   OrchestrationStatus = "running"
	OrchStatusSucceeded OrchestrationStatus = "succeeded"
	OrchStatusFailed    OrchestrationStatus = "failed"
	OrchStatusAborted   OrchestrationStatus = "aborted"
	OrchStatusPaused    OrchestrationStatus = "paused"
)

type TargetStatus string

const (
	TargetPending   TargetStatus = "pending"
	TargetRunning   TargetStatus = "running"
	TargetSucceeded TargetStatus = "succeeded"
	TargetFailed    TargetStatus = "failed"
	TargetSkipped   TargetStatus = "skipped"
)

// --- 实体 ---

// Orchestration 多集群发布编排：一次触发对多个 (group, image) 按策略执行发布。
type Orchestration struct {
	ID               int64
	UUID             uuid.UUID
	WorkspaceID      int64
	ApplicationID    int64
	Name             string
	Strategy         OrchestrationStrategy
	Status           OrchestrationStatus
	ProgressPercent  int
	ImageID          int64
	ConfigVersion    int
	Replicas         int
	MaxSurge         string
	MaxUnavailable   string
	BatchSize        int
	BatchIntervalSec int
	FailureReason    string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	DurationMs       int64
	TriggeredBy      int64
	TriggerSource    TriggerSource
	Version          int
	CreatedAt        time.Time
	CreatedBy        int64
	UpdatedAt        time.Time
	UpdatedBy        int64
}

// OrchestrationTarget 编排目标（每个 group 一行）。
type OrchestrationTarget struct {
	ID               int64
	OrchestrationID  int64
	GroupID          int64
	ClusterID        int64
	ImageID          int64
	ConfigVersion    int
	Replicas         int
	Seq              int
	BatchSize        int
	BatchIntervalSec int
	ReleaseID        int64
	Status           TargetStatus
	FailureReason    string
	StartedAt        *time.Time
	FinishedAt       *time.Time
	Version          int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// 领域错误。
var (
	ErrOrchestrationNotFound = errors.New("orchestration not found")
	ErrOrchestrationNotCancellable = errors.New("orchestration cannot be cancelled in current state")
)

// CreateOrchestrationInput 创建编排输入。
type CreateOrchestrationInput struct {
	WorkspaceID      int64
	ApplicationID    int64
	Name             string
	Strategy         OrchestrationStrategy
	ImageID          int64
	ConfigVersion    int
	Replicas         int
	MaxSurge         string
	MaxUnavailable   string
	BatchSize        int
	BatchIntervalSec int
	TriggeredBy      int64
	TriggerSource    TriggerSource
	Targets          []OrchestrationTargetInput
}

// OrchestrationTargetInput 目标输入。
type OrchestrationTargetInput struct {
	GroupID          int64
	ClusterID        int64
	ImageID          int64
	ConfigVersion    int
	Replicas         int
	Seq              int
	BatchSize        int
	BatchIntervalSec int
}

// OrchestrationRepository 编排仓储接口。
type OrchestrationRepository interface {
	CreateOrchestration(ctx context.Context, o *Orchestration, targets []OrchestrationTarget) error
	GetOrchestration(ctx context.Context, id int64) (*Orchestration, error)
	ListOrchestrations(ctx context.Context, appID int64, offset, limit int) ([]*Orchestration, int64, error)
	UpdateOrchestrationStatus(ctx context.Context, id int64, status OrchestrationStatus, progress int, failureReason string, version int) (*Orchestration, error)
	CompleteOrchestration(ctx context.Context, id int64, status OrchestrationStatus, durationMs int64, finishedAt time.Time, version int) (*Orchestration, error)
	ListTargets(ctx context.Context, orchestrationID int64) ([]*OrchestrationTarget, error)
	UpdateTargetStatus(ctx context.Context, t *OrchestrationTarget) error
}
