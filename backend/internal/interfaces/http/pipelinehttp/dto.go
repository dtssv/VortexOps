package pipelinehttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
)

type pipelineDTO struct {
	ID                int64          `json:"id"`
	UUID              string         `json:"uuid"`
	WorkspaceID       int64          `json:"workspace_id"`
	Scope             string         `json:"scope"`
	ScopeID           int64          `json:"scope_id,omitempty"`
	Name              string         `json:"name"`
	Description       string         `json:"description,omitempty"`
	Trigger           string         `json:"trigger"`
	TriggerConfig     map[string]any `json:"trigger_config,omitempty"`
	TriggerOnPipeline int64          `json:"trigger_on_pipeline,omitempty"`
	StagesConfig      []map[string]any `json:"stages_config,omitempty"`
	Enabled           bool           `json:"enabled"`
	Version           int            `json:"version_col"`
	CreatedAt         string         `json:"created_at"`
}

func toPipelineDTO(p *pipeline.Pipeline) *pipelineDTO {
	if p == nil {
		return nil
	}
	return &pipelineDTO{
		ID: p.ID, UUID: p.UUID.String(), WorkspaceID: p.WorkspaceID, Scope: string(p.Scope), ScopeID: p.ScopeID,
		Name: p.Name, Description: p.Description, Trigger: string(p.Trigger), TriggerConfig: p.TriggerConfig,
		TriggerOnPipeline: p.TriggerOnPipeline, StagesConfig: p.StagesConfig, Enabled: p.Enabled,
		Version: p.Audit.Version, CreatedAt: p.Audit.CreatedAt.Format(time.RFC3339),
	}
}

func toPipelineDTOs(items []*pipeline.Pipeline) []pipelineDTO {
	out := make([]pipelineDTO, 0, len(items))
	for _, p := range items {
		out = append(out, *toPipelineDTO(p))
	}
	return out
}

type stageDTO struct {
	ID         int64          `json:"id"`
	UUID       string         `json:"uuid"`
	PipelineID int64          `json:"pipeline_id"`
	Seq        int            `json:"seq"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Gate       map[string]any `json:"gate,omitempty"`
	OnFailure  string         `json:"on_failure"`
	Params     map[string]any `json:"params,omitempty"`
}

func toStageDTO(s *pipeline.Stage) *stageDTO {
	if s == nil {
		return nil
	}
	return &stageDTO{
		ID: s.ID, UUID: s.UUID.String(), PipelineID: s.PipelineID, Seq: s.Seq, Name: s.Name,
		Type: string(s.Type), Gate: s.Gate, OnFailure: string(s.OnFailure), Params: s.Params,
	}
}

func toStageDTOs(items []*pipeline.Stage) []stageDTO {
	out := make([]stageDTO, 0, len(items))
	for _, s := range items {
		out = append(out, *toStageDTO(s))
	}
	return out
}

type runDTO struct {
	ID                int64    `json:"id"`
	UUID              string   `json:"uuid"`
	PipelineID        int64    `json:"pipeline_id"`
	WorkspaceID       int64    `json:"workspace_id"`
	RunNumber         int      `json:"run_number"`
	Trigger           string   `json:"trigger"`
	TriggerRef        string   `json:"trigger_ref,omitempty"`
	TriggerCommitSHA  string   `json:"trigger_commit_sha,omitempty"`
	TriggerBy         int64    `json:"trigger_by,omitempty"`
	Status            string   `json:"status"`
	CurrentStageSeq   int      `json:"current_stage_seq"`
	StartedAt         string   `json:"started_at"`
	FinishedAt        string   `json:"finished_at,omitempty"`
	DurationMs        int64    `json:"duration_ms,omitempty"`
	ArtifactsImageIDs []int64  `json:"artifacts_image_ids,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Version           int      `json:"version_col"`
}

func toRunDTO(r *pipeline.Run) *runDTO {
	if r == nil {
		return nil
	}
	dto := &runDTO{
		ID: r.ID, UUID: r.UUID.String(), PipelineID: r.PipelineID, WorkspaceID: r.WorkspaceID,
		RunNumber: r.RunNumber, Trigger: string(r.Trigger), TriggerRef: r.TriggerRef,
		TriggerCommitSHA: r.TriggerCommitSHA, TriggerBy: r.TriggerBy, Status: string(r.Status),
		CurrentStageSeq: r.CurrentStageSeq, StartedAt: r.StartedAt.Format(time.RFC3339),
		DurationMs: r.DurationMs, ArtifactsImageIDs: r.ArtifactsImageIDs, Metadata: r.Metadata,
		Version: r.Audit.Version,
	}
	if r.FinishedAt != nil {
		dto.FinishedAt = r.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toRunDTOs(items []*pipeline.Run) []runDTO {
	out := make([]runDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toRunDTO(r))
	}
	return out
}

type stageRunDTO struct {
	ID               int64          `json:"id"`
	UUID             string         `json:"uuid"`
	PipelineRunID    int64          `json:"pipeline_run_id"`
	StageID          int64          `json:"stage_id"`
	Seq              int            `json:"seq"`
	Status           string         `json:"status"`
	RelatedBuildID   int64          `json:"related_build_id,omitempty"`
	RelatedReleaseID int64          `json:"related_release_id,omitempty"`
	RelatedImageID   int64          `json:"related_image_id,omitempty"`
	GateResult       map[string]any `json:"gate_result,omitempty"`
	StartedAt        string         `json:"started_at,omitempty"`
	FinishedAt       string         `json:"finished_at,omitempty"`
	Message          string         `json:"message,omitempty"`
}

func toStageRunDTO(sr *pipeline.StageRun) *stageRunDTO {
	if sr == nil {
		return nil
	}
	dto := &stageRunDTO{
		ID: sr.ID, UUID: sr.UUID.String(), PipelineRunID: sr.PipelineRunID, StageID: sr.StageID, Seq: sr.Seq,
		Status: string(sr.Status), RelatedBuildID: sr.RelatedBuildID, RelatedReleaseID: sr.RelatedReleaseID,
		RelatedImageID: sr.RelatedImageID, GateResult: sr.GateResult, Message: sr.Message,
	}
	if sr.StartedAt != nil {
		dto.StartedAt = sr.StartedAt.Format(time.RFC3339)
	}
	if sr.FinishedAt != nil {
		dto.FinishedAt = sr.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toStageRunDTOs(items []*pipeline.StageRun) []stageRunDTO {
	out := make([]stageRunDTO, 0, len(items))
	for _, sr := range items {
		out = append(out, *toStageRunDTO(sr))
	}
	return out
}

type promotionDTO struct {
	ID                    int64    `json:"id"`
	UUID                  string   `json:"uuid"`
	WorkspaceID           int64    `json:"workspace_id"`
	ApplicationID         int64    `json:"application_id"`
	SourceEnv             string   `json:"source_env"`
	TargetEnv             string   `json:"target_env"`
	ArtifactImageID       int64    `json:"artifact_image_id"`
	ArtifactConfigVersion int      `json:"artifact_config_version,omitempty"`
	TargetGroupIDs        []int64  `json:"target_group_ids,omitempty"`
	Strategy              string   `json:"strategy"`
	AutoPromoteOnVerify   bool     `json:"auto_promote_on_verify"`
	Status                string   `json:"status"`
	PipelineRunID         int64    `json:"pipeline_run_id,omitempty"`
	ApprovalInstanceID    int64    `json:"approval_instance_id,omitempty"`
	StartedBy             int64    `json:"started_by"`
	StartedAt             string   `json:"started_at"`
	FinishedAt            string   `json:"finished_at,omitempty"`
}

func toPromotionDTO(p *pipeline.Promotion) *promotionDTO {
	if p == nil {
		return nil
	}
	dto := &promotionDTO{
		ID: p.ID, UUID: p.UUID.String(), WorkspaceID: p.WorkspaceID, ApplicationID: p.ApplicationID,
		SourceEnv: p.SourceEnv, TargetEnv: p.TargetEnv, ArtifactImageID: p.ArtifactImageID,
		ArtifactConfigVersion: p.ArtifactConfigVersion, TargetGroupIDs: p.TargetGroupIDs,
		Strategy: string(p.Strategy), AutoPromoteOnVerify: p.AutoPromoteOnVerify, Status: string(p.Status),
		PipelineRunID: p.PipelineRunID, ApprovalInstanceID: p.ApprovalInstanceID, StartedBy: p.StartedBy,
		StartedAt: p.StartedAt.Format(time.RFC3339),
	}
	if p.FinishedAt != nil {
		dto.FinishedAt = p.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toPromotionDTOs(items []*pipeline.Promotion) []promotionDTO {
	out := make([]promotionDTO, 0, len(items))
	for _, p := range items {
		out = append(out, *toPromotionDTO(p))
	}
	return out
}

type signatureDTO struct {
	ID                 int64          `json:"id"`
	UUID               string         `json:"uuid"`
	ImageID            int64          `json:"image_id"`
	SignatureType      string         `json:"signature_type"`
	PublicKeyRef       string         `json:"public_key_ref,omitempty"`
	SignedBy           string         `json:"signed_by,omitempty"`
	SignedAt           string         `json:"signed_at"`
	SBOMStorageKey     string         `json:"sbom_storage_key,omitempty"`
	SBOMFormat         string         `json:"sbom_format,omitempty"`
	Provenance         map[string]any `json:"provenance,omitempty"`
	VerificationStatus string         `json:"verification_status"`
}

func toSignatureDTO(s *pipeline.ArtifactSignature) *signatureDTO {
	if s == nil {
		return nil
	}
	return &signatureDTO{
		ID: s.ID, UUID: s.UUID.String(), ImageID: s.ImageID, SignatureType: string(s.SignatureType),
		PublicKeyRef: s.PublicKeyRef, SignedBy: s.SignedBy, SignedAt: s.SignedAt.Format(time.RFC3339),
		SBOMStorageKey: s.SBOMStorageKey, SBOMFormat: string(s.SBOMFormat), Provenance: s.Provenance,
		VerificationStatus: string(s.VerificationStatus),
	}
}
