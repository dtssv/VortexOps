// Package pipeline 是 CI/CD 流水线领域的核心实体与仓储接口。
// 覆盖：Pipeline（定义）、PipelineStage（阶段定义）、PipelineRun（运行实例）、
// PipelineStageRun（阶段运行实例）、Promotion（环境晋升）、ArtifactSignature（制品签名/SBOM）。
package pipeline

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
	ScopeWorkspace    Scope = "workspace"
	ScopeApplication  Scope = "application"
)

type Trigger string

const (
	TriggerManual    Trigger = "manual"
	TriggerWebhook   Trigger = "webhook"
	TriggerSchedule  Trigger = "schedule"
	TriggerPromotion Trigger = "promotion"
)

type StageType string

const (
	StageSequential StageType = "sequential"
	StageParallel   StageType = "parallel"
)

type OnFailure string

const (
	FailureAbort         OnFailure = "abort"
	FailureManualRetry   OnFailure = "manual_retry"
	FailureContinue      OnFailure = "continue"
)

// RunStatus 流水线运行状态。
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunPaused    RunStatus = "paused"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunAborted   RunStatus = "aborted"
	RunCanceled  RunStatus = "canceled"
)

// IsTerminal 终态。
func (s RunStatus) IsTerminal() bool {
	switch s {
	case RunSucceeded, RunFailed, RunAborted, RunCanceled:
		return true
	}
	return false
}

// StageRunStatus 阶段运行状态。
type StageRunStatus string

const (
	StageRunPending   StageRunStatus = "pending"
	StageRunRunning   StageRunStatus = "running"
	StageRunPaused    StageRunStatus = "paused"
	StageRunSucceeded StageRunStatus = "succeeded"
	StageRunFailed    StageRunStatus = "failed"
	StageRunSkipped   StageRunStatus = "skipped"
)

// PromotionStrategy 晋升策略。
type PromotionStrategy string

const (
	PromoAuto    PromotionStrategy = "auto"
	PromoCanary  PromotionStrategy = "canary"
	PromoManual  PromotionStrategy = "manual"
)

// PromotionStatus 晋升状态。
type PromotionStatus string

const (
	PromoStatusPending    PromotionStatus = "pending"
	PromoStatusDeploying  PromotionStatus = "deploying"
	PromoStatusVerifying  PromotionStatus = "verifying"
	PromoStatusSucceeded  PromotionStatus = "succeeded"
	PromoStatusFailed     PromotionStatus = "failed"
	PromoStatusAborted    PromotionStatus = "aborted"
)

type SignatureType string

const (
	SigCosign  SignatureType = "cosign"
	SigNotation SignatureType = "notation"
)

type VerificationStatus string

const (
	VerifyPending  VerificationStatus = "pending"
	VerifyVerified VerificationStatus = "verified"
	VerifyFailed   VerificationStatus = "failed"
)

type SBOMFormat string

const (
	SBOMCycloneDX SBOMFormat = "cyclonedx"
	SBOMSPDX      SBOMFormat = "spdx"
)

// --- 实体 ---

// Pipeline 流水线定义。
type Pipeline struct {
	ID               int64
	UUID             uuid.UUID
	WorkspaceID      int64
	Scope            Scope
	ScopeID          int64
	Name             string
	Description      string
	Trigger          Trigger
	TriggerConfig    map[string]any
	TriggerOnPipeline int64
	StagesConfig     []map[string]any
	Enabled          bool
	domain.Audit
}

// Stage 流水线阶段定义。
type Stage struct {
	ID         int64
	UUID       uuid.UUID
	PipelineID int64
	Seq        int
	Name       string
	Type       StageType
	Gate       map[string]any
	OnFailure  OnFailure
	Params     map[string]any
	domain.Audit
}

// Run 流水线运行实例。
type Run struct {
	ID                int64
	UUID              uuid.UUID
	PipelineID        int64
	WorkspaceID       int64
	RunNumber         int
	Trigger           Trigger
	TriggerRef        string
	TriggerCommitSHA  string
	TriggerBy         int64
	Status            RunStatus
	CurrentStageSeq   int
	StartedAt         time.Time
	FinishedAt        *time.Time
	DurationMs        int64
	ArtifactsImageIDs []int64
	Metadata          map[string]any
	domain.Audit
}

// StageRun 阶段运行实例。
type StageRun struct {
	ID               int64
	UUID             uuid.UUID
	PipelineRunID    int64
	StageID          int64
	Seq              int
	Status           StageRunStatus
	RelatedBuildID   int64
	RelatedReleaseID int64
	RelatedImageID   int64
	GateResult       map[string]any
	StartedAt        *time.Time
	FinishedAt       *time.Time
	Message          string
	domain.Audit
}

// Promotion 环境晋升。
type Promotion struct {
	ID                    int64
	UUID                  uuid.UUID
	WorkspaceID           int64
	ApplicationID         int64
	SourceEnv             string
	TargetEnv             string
	ArtifactImageID       int64
	ArtifactConfigVersion int
	TargetGroupIDs        []int64
	Strategy              PromotionStrategy
	AutoPromoteOnVerify   bool
	Status                PromotionStatus
	PipelineRunID         int64
	ApprovalInstanceID    int64
	StartedBy             int64
	StartedAt             time.Time
	FinishedAt            *time.Time
	domain.Audit
}

// ArtifactSignature 制品签名 + SBOM。
type ArtifactSignature struct {
	ID                 int64
	UUID               uuid.UUID
	ImageID            int64
	SignatureType      SignatureType
	SignaturePayload   string
	PublicKeyRef       string
	SignedBy           string
	SignedAt           time.Time
	SBOMStorageKey     string
	SBOMFormat         SBOMFormat
	Provenance         map[string]any
	VerificationStatus VerificationStatus
	domain.Audit
}

// 领域错误。
var (
	ErrPipelineNotFound  = errors.New("pipeline not found")
	ErrPipelineNameUsed  = errors.New("pipeline name already used in workspace")
	ErrStageNotFound     = errors.New("pipeline stage not found")
	ErrRunNotFound       = errors.New("pipeline run not found")
	ErrStageRunNotFound  = errors.New("pipeline stage run not found")
	ErrRunNotCancellable = errors.New("pipeline run not cancellable")
	ErrPromotionNotFound = errors.New("promotion not found")
	ErrSignatureNotFound = errors.New("artifact signature not found")
)

// PipelineQuery 流水线查询。
type PipelineQuery struct {
	WorkspaceID int64
	Scope       Scope
	ScopeID     int64
	Enabled     *bool
	Offset      int
	Limit       int
}

// RunQuery 运行查询。
type RunQuery struct {
	PipelineID  int64
	WorkspaceID int64
	Status      RunStatus
	Offset      int
	Limit       int
}

// PromotionQuery 晋升查询。
type PromotionQuery struct {
	WorkspaceID   int64
	ApplicationID int64
	Status        PromotionStatus
	Offset        int
	Limit         int
}

// Repository 流水线领域仓储接口。
type Repository interface {
	// pipeline
	CreatePipeline(ctx context.Context, p *Pipeline) error
	GetPipelineByID(ctx context.Context, id int64) (*Pipeline, error)
	ListPipelines(ctx context.Context, q PipelineQuery) ([]*Pipeline, int64, error)
	UpdatePipeline(ctx context.Context, p *Pipeline) error
	DeletePipeline(ctx context.Context, id, actorID int64) error
	// stages
	CreateStages(ctx context.Context, stages []*Stage) error
	ListStages(ctx context.Context, pipelineID int64) ([]*Stage, error)
	DeleteStages(ctx context.Context, pipelineID, actorID int64) error
	// runs
	CreateRun(ctx context.Context, r *Run) error
	GetRunByID(ctx context.Context, id int64) (*Run, error)
	ListRuns(ctx context.Context, q RunQuery) ([]*Run, int64, error)
	UpdateRun(ctx context.Context, r *Run) error
	NextRunNumber(ctx context.Context, pipelineID int64) (int, error)
	ListActiveRuns(ctx context.Context) ([]*Run, error)
	// stage runs
	CreateStageRun(ctx context.Context, sr *StageRun) error
	GetStageRunByID(ctx context.Context, id int64) (*StageRun, error)
	ListStageRuns(ctx context.Context, runID int64) ([]*StageRun, int64, error)
	UpdateStageRun(ctx context.Context, sr *StageRun) error
	// promotions
	CreatePromotion(ctx context.Context, p *Promotion) error
	GetPromotionByID(ctx context.Context, id int64) (*Promotion, error)
	ListPromotions(ctx context.Context, q PromotionQuery) ([]*Promotion, int64, error)
	UpdatePromotion(ctx context.Context, p *Promotion) error
	// signatures
	CreateSignature(ctx context.Context, s *ArtifactSignature) error
	GetSignatureByImageID(ctx context.Context, imageID int64) (*ArtifactSignature, error)
	UpdateSignature(ctx context.Context, s *ArtifactSignature) error
}
