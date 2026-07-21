// Package pipelineapp 是 CI/CD 流水线的应用服务：pipeline 定义 CRUD、触发运行、晋升、签名记录。
// 阶段执行由 pipeline-worker（cmd/pipeline-worker）通过 executor.Engine 异步驱动，apiserver 仅写期望态。
package pipelineapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
	"github.com/vortexops/vortexops/internal/infrastructure/kafka"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Repository 流水线仓储接口（由 pipelinerepo 实现）。
type Repository interface {
	pipeline.Repository
}

// StageInput 阶段定义输入（创建 pipeline 时一并提交）。
type StageInput struct {
	Seq       int                  `json:"seq"`
	Name      string               `json:"name"`
	Type      pipeline.StageType   `json:"type"`
	Gate      map[string]any       `json:"gate"`
	OnFailure pipeline.OnFailure   `json:"on_failure"`
	Params    map[string]any       `json:"params"`
}

// CreatePipelineInput 创建流水线输入。
type CreatePipelineInput struct {
	WorkspaceID       int64
	Scope             pipeline.Scope
	ScopeID           int64
	Name              string
	Description       string
	Trigger           pipeline.Trigger
	TriggerConfig     map[string]any
	TriggerOnPipeline int64
	Stages            []StageInput
	Enabled           bool
	CreatedBy         int64
}

// Service 流水线应用服务。
type Service struct {
	repo      Repository
	producer  *kafka.Producer
	brokers   []string
	topicKey  string
	topicName string
}

// New 创建服务。producer 可为 nil（未启用 Kafka）。
func New(repo Repository, producer *kafka.Producer, brokers []string, topicKey, topicName string) *Service {
	return &Service{repo: repo, producer: producer, brokers: brokers, topicKey: topicKey, topicName: topicName}
}

// --- pipeline CRUD ---

// CreatePipeline 创建流水线定义 + 阶段。
func (s *Service) CreatePipeline(ctx context.Context, in CreatePipelineInput) (*pipeline.Pipeline, error) {
	p := &pipeline.Pipeline{
		WorkspaceID: in.WorkspaceID, Scope: in.Scope, ScopeID: in.ScopeID, Name: in.Name,
		Description: in.Description, Trigger: in.Trigger, TriggerConfig: in.TriggerConfig,
		TriggerOnPipeline: in.TriggerOnPipeline, Enabled: in.Enabled,
	}
	if p.Scope == "" {
		p.Scope = pipeline.ScopeWorkspace
	}
	if p.Trigger == "" {
		p.Trigger = pipeline.TriggerManual
	}
	p.Audit.CreatedBy = in.CreatedBy
	p.Audit.UpdatedBy = in.CreatedBy
	if err := s.repo.CreatePipeline(ctx, p); err != nil {
		return nil, apperr.Internal("create pipeline", err)
	}
	if len(in.Stages) > 0 {
		stages := make([]*pipeline.Stage, 0, len(in.Stages))
		for _, st := range in.Stages {
			s := &pipeline.Stage{
				PipelineID: p.ID, Seq: st.Seq, Name: st.Name, Type: st.Type, Gate: st.Gate,
				OnFailure: st.OnFailure, Params: st.Params,
			}
			if s.Type == "" {
				s.Type = pipeline.StageSequential
			}
			if s.OnFailure == "" {
				s.OnFailure = pipeline.FailureAbort
			}
			s.Audit.CreatedBy = in.CreatedBy
			s.Audit.UpdatedBy = in.CreatedBy
			stages = append(stages, s)
		}
		if err := s.repo.CreateStages(ctx, stages); err != nil {
			return nil, apperr.Internal("create stages", err)
		}
	}
	return p, nil
}

// GetPipeline 取流水线定义（含阶段）。
func (s *Service) GetPipeline(ctx context.Context, id int64) (*pipeline.Pipeline, error) {
	p, err := s.repo.GetPipelineByID(ctx, id)
	if err != nil {
		if errors.Is(err, pipeline.ErrPipelineNotFound) {
			return nil, apperr.NotFound("pipeline", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get pipeline", err)
	}
	return p, nil
}

// ListPipelines 列流水线。
func (s *Service) ListPipelines(ctx context.Context, q pipeline.PipelineQuery, page, size int) ([]*pipeline.Pipeline, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListPipelines(ctx, q)
}

// ListStages 列阶段。
func (s *Service) ListStages(ctx context.Context, pipelineID int64) ([]*pipeline.Stage, error) {
	return s.repo.ListStages(ctx, pipelineID)
}

// DeletePipeline 删除流水线（软删除）。
func (s *Service) DeletePipeline(ctx context.Context, id, actorID int64) error {
	if err := s.repo.DeletePipeline(ctx, id, actorID); err != nil {
		if errors.Is(err, pipeline.ErrPipelineNotFound) {
			return apperr.NotFound("pipeline", fmt.Sprint(id))
		}
		return apperr.Internal("delete pipeline", err)
	}
	_ = s.repo.DeleteStages(ctx, id, actorID)
	return nil
}

// --- runs ---

// TriggerRunInput 触发运行输入。
type TriggerRunInput struct {
	PipelineID       int64
	TriggerRef       string
	TriggerCommitSHA string
	TriggerBy        int64
}

// TriggerRun 创建运行实例（pending），发布 Kafka 事件由 worker 拉起执行。
func (s *Service) TriggerRun(ctx context.Context, in TriggerRunInput) (*pipeline.Run, error) {
	p, err := s.repo.GetPipelineByID(ctx, in.PipelineID)
	if err != nil {
		return nil, apperr.NotFound("pipeline", fmt.Sprint(in.PipelineID))
	}
	if !p.Enabled {
		return nil, apperr.BusinessRule("pipeline disabled", errors.New("pipeline not enabled"))
	}
	num, err := s.repo.NextRunNumber(ctx, in.PipelineID)
	if err != nil {
		return nil, apperr.Internal("alloc run number", err)
	}
	run := &pipeline.Run{
		PipelineID: p.ID, WorkspaceID: p.WorkspaceID, RunNumber: num, Trigger: p.Trigger,
		TriggerRef: in.TriggerRef, TriggerCommitSHA: in.TriggerCommitSHA, TriggerBy: in.TriggerBy,
		Status: pipeline.RunPending, StartedAt: time.Now(),
	}
	run.Audit.CreatedBy = in.TriggerBy
	run.Audit.UpdatedBy = in.TriggerBy
	if err := s.repo.CreateRun(ctx, run); err != nil {
		return nil, apperr.Internal("create run", err)
	}
	// 发布 Kafka 事件，pipeline-worker 消费后执行。
	if s.producer != nil && s.producer.Enabled() {
		_ = s.producer.Publish(ctx, s.brokers, s.topicKey, s.topicName, fmt.Sprintf("run-%d", run.ID),
			kafka.NewEvent("pipeline.run.triggered", "apiserver", map[string]any{
				"run_id": run.ID, "pipeline_id": p.ID, "workspace_id": p.WorkspaceID,
			}))
	}
	return run, nil
}

// GetRun 取运行。
func (s *Service) GetRun(ctx context.Context, id int64) (*pipeline.Run, error) {
	run, err := s.repo.GetRunByID(ctx, id)
	if err != nil {
		if errors.Is(err, pipeline.ErrRunNotFound) {
			return nil, apperr.NotFound("pipeline run", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get run", err)
	}
	return run, nil
}

// ListRuns 列运行。
func (s *Service) ListRuns(ctx context.Context, q pipeline.RunQuery, page, size int) ([]*pipeline.Run, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListRuns(ctx, q)
}

// ListStageRuns 列阶段运行。
func (s *Service) ListStageRuns(ctx context.Context, runID int64) ([]*pipeline.StageRun, int64, error) {
	return s.repo.ListStageRuns(ctx, runID)
}

// CancelRun 取消运行（仅 pending/running/paused 可取消）。
func (s *Service) CancelRun(ctx context.Context, id, actorID int64) (*pipeline.Run, error) {
	run, err := s.repo.GetRunByID(ctx, id)
	if err != nil {
		return nil, apperr.NotFound("pipeline run", fmt.Sprint(id))
	}
	if run.Status.IsTerminal() {
		return nil, apperr.BusinessRule("run already terminal", pipeline.ErrRunNotCancellable)
	}
	run.Status = pipeline.RunCanceled
	now := time.Now()
	run.FinishedAt = &now
	run.DurationMs = now.Sub(run.StartedAt).Milliseconds()
	run.Audit.UpdatedBy = actorID
	if err := s.repo.UpdateRun(ctx, run); err != nil {
		return nil, apperr.Internal("cancel run", err)
	}
	return run, nil
}

// --- promotions ---

// CreatePromotionInput 创建晋升输入。
type CreatePromotionInput struct {
	WorkspaceID           int64
	ApplicationID         int64
	SourceEnv             string
	TargetEnv             string
	ArtifactImageID       int64
	ArtifactConfigVersion int
	TargetGroupIDs        []int64
	Strategy              pipeline.PromotionStrategy
	AutoPromoteOnVerify   bool
	StartedBy             int64
}

// CreatePromotion 创建晋升记录（pending）。
func (s *Service) CreatePromotion(ctx context.Context, in CreatePromotionInput) (*pipeline.Promotion, error) {
	p := &pipeline.Promotion{
		WorkspaceID: in.WorkspaceID, ApplicationID: in.ApplicationID, SourceEnv: in.SourceEnv,
		TargetEnv: in.TargetEnv, ArtifactImageID: in.ArtifactImageID,
		ArtifactConfigVersion: in.ArtifactConfigVersion, TargetGroupIDs: in.TargetGroupIDs,
		Strategy: in.Strategy, AutoPromoteOnVerify: in.AutoPromoteOnVerify,
		Status: pipeline.PromoStatusPending, StartedBy: in.StartedBy, StartedAt: time.Now(),
	}
	if p.Strategy == "" {
		p.Strategy = pipeline.PromoAuto
	}
	p.Audit.CreatedBy = in.StartedBy
	p.Audit.UpdatedBy = in.StartedBy
	if err := s.repo.CreatePromotion(ctx, p); err != nil {
		return nil, apperr.Internal("create promotion", err)
	}
	return p, nil
}

// GetPromotion 取晋升。
func (s *Service) GetPromotion(ctx context.Context, id int64) (*pipeline.Promotion, error) {
	p, err := s.repo.GetPromotionByID(ctx, id)
	if err != nil {
		if errors.Is(err, pipeline.ErrPromotionNotFound) {
			return nil, apperr.NotFound("promotion", fmt.Sprint(id))
		}
		return nil, apperr.Internal("get promotion", err)
	}
	return p, nil
}

// ListPromotions 列晋升。
func (s *Service) ListPromotions(ctx context.Context, q pipeline.PromotionQuery, page, size int) ([]*pipeline.Promotion, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	q.Offset = (page - 1) * size
	q.Limit = size
	return s.repo.ListPromotions(ctx, q)
}

// --- signatures ---

// RecordSignatureInput 记录签名/SBOM 输入。
type RecordSignatureInput struct {
	ImageID            int64
	SignatureType      pipeline.SignatureType
	SignaturePayload   string
	PublicKeyRef       string
	SignedBy           string
	SBOMStorageKey     string
	SBOMFormat         pipeline.SBOMFormat
	Provenance         map[string]any
	VerificationStatus pipeline.VerificationStatus
	CreatedBy          int64
}

// RecordSignature 记录或更新制品签名。
func (s *Service) RecordSignature(ctx context.Context, in RecordSignatureInput) (*pipeline.ArtifactSignature, error) {
	existing, err := s.repo.GetSignatureByImageID(ctx, in.ImageID)
	if err != nil && !errors.Is(err, pipeline.ErrSignatureNotFound) {
		return nil, apperr.Internal("get signature", err)
	}
	if existing != nil {
		existing.SignatureType = in.SignatureType
		existing.SignaturePayload = in.SignaturePayload
		existing.PublicKeyRef = in.PublicKeyRef
		existing.SignedBy = in.SignedBy
		existing.SBOMStorageKey = in.SBOMStorageKey
		existing.SBOMFormat = in.SBOMFormat
		existing.Provenance = in.Provenance
		existing.VerificationStatus = in.VerificationStatus
		existing.Audit.UpdatedBy = in.CreatedBy
		if err := s.repo.UpdateSignature(ctx, existing); err != nil {
			return nil, apperr.Internal("update signature", err)
		}
		return existing, nil
	}
	sig := &pipeline.ArtifactSignature{
		ImageID: in.ImageID, SignatureType: in.SignatureType, SignaturePayload: in.SignaturePayload,
		PublicKeyRef: in.PublicKeyRef, SignedBy: in.SignedBy, SignedAt: time.Now(),
		SBOMStorageKey: in.SBOMStorageKey, SBOMFormat: in.SBOMFormat, Provenance: in.Provenance,
		VerificationStatus: in.VerificationStatus,
	}
	if sig.VerificationStatus == "" {
		sig.VerificationStatus = pipeline.VerifyPending
	}
	sig.Audit.CreatedBy = in.CreatedBy
	sig.Audit.UpdatedBy = in.CreatedBy
	if err := s.repo.CreateSignature(ctx, sig); err != nil {
		return nil, apperr.Internal("create signature", err)
	}
	return sig, nil
}

// GetSignature 取镜像签名。
func (s *Service) GetSignature(ctx context.Context, imageID int64) (*pipeline.ArtifactSignature, error) {
	sig, err := s.repo.GetSignatureByImageID(ctx, imageID)
	if err != nil {
		if errors.Is(err, pipeline.ErrSignatureNotFound) {
			return nil, apperr.NotFound("signature", fmt.Sprint(imageID))
		}
		return nil, apperr.Internal("get signature", err)
	}
	return sig, nil
}
