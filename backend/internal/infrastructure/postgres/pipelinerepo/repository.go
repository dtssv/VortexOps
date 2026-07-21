// Package pipelinerepo 是 pipeline 领域的 PostgreSQL 仓储实现。
package pipelinerepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vortexops/vortexops/internal/domain/pipeline"
)

// Repository 实现 pipeline.Repository。
type Repository struct {
	pool Querier
}

// Querier pgx 兼容接口。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// New 创建仓储。
func New(pool Querier) *Repository { return &Repository{pool: pool} }

// --- pipeline ---

func (r *Repository) CreatePipeline(ctx context.Context, p *pipeline.Pipeline) error {
	if p.UUID == uuid.Nil {
		p.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_pipelines (uuid, workspace_id, scope, scope_id, name, description, trigger, trigger_config,
  trigger_on_pipeline, stages_config, enabled, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12,$12)
RETURNING id, version, created_at, updated_at`,
		p.UUID, p.WorkspaceID, string(p.Scope), nilIfZero(p.ScopeID), p.Name, p.Description, string(p.Trigger),
		p.TriggerConfig, nilIfZero(p.TriggerOnPipeline), p.StagesConfig, p.Enabled, p.CreatedBy)
	return row.Scan(&p.ID, &p.Audit.Version, &p.Audit.CreatedAt, &p.Audit.UpdatedAt)
}

func (r *Repository) GetPipelineByID(ctx context.Context, id int64) (*pipeline.Pipeline, error) {
	p := &pipeline.Pipeline{}
	var scope, trigger string
	var scopeID, triggerOn *int64
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, workspace_id, scope, scope_id, name, description, trigger, trigger_config, trigger_on_pipeline,
       stages_config, enabled, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_pipelines WHERE id=$1 AND deleted=false`, id).
		Scan(&p.ID, &p.UUID, &p.WorkspaceID, &scope, &scopeID, &p.Name, &p.Description, &trigger, &p.TriggerConfig,
			&triggerOn, &p.StagesConfig, &p.Enabled, &p.Audit.Version, &p.Audit.CreatedAt, &p.Audit.CreatedBy,
			&p.Audit.UpdatedAt, &p.Audit.UpdatedBy, &p.Audit.Deleted, &p.Audit.DeletedAt, &p.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pipeline.ErrPipelineNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Scope = pipeline.Scope(scope)
	p.Trigger = pipeline.Trigger(trigger)
	if scopeID != nil {
		p.ScopeID = *scopeID
	}
	if triggerOn != nil {
		p.TriggerOnPipeline = *triggerOn
	}
	return p, nil
}

func (r *Repository) ListPipelines(ctx context.Context, q pipeline.PipelineQuery) ([]*pipeline.Pipeline, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	where := "deleted=false"
	args := []any{}
	idx := 1
	add := func(clause string, val any) {
		where += " AND " + clause
		args = append(args, val)
		idx++
	}
	if q.WorkspaceID > 0 {
		add("workspace_id=$"+fmt.Sprint(idx), q.WorkspaceID)
	}
	if q.Scope != "" {
		add("scope=$"+fmt.Sprint(idx), string(q.Scope))
	}
	if q.ScopeID > 0 {
		add("scope_id=$"+fmt.Sprint(idx), q.ScopeID)
	}
	if q.Enabled != nil {
		add("enabled=$"+fmt.Sprint(idx), *q.Enabled)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_pipelines WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `SELECT id, uuid, workspace_id, scope, scope_id, name, description, trigger, trigger_config, trigger_on_pipeline,
       stages_config, enabled, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_pipelines WHERE ` + where + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*pipeline.Pipeline, 0)
	for rows.Next() {
		p := &pipeline.Pipeline{}
		var scope, trigger string
		var scopeID, triggerOn *int64
		if err := rows.Scan(&p.ID, &p.UUID, &p.WorkspaceID, &scope, &scopeID, &p.Name, &p.Description, &trigger,
			&p.TriggerConfig, &triggerOn, &p.StagesConfig, &p.Enabled, &p.Audit.Version, &p.Audit.CreatedAt,
			&p.Audit.CreatedBy, &p.Audit.UpdatedAt, &p.Audit.UpdatedBy, &p.Audit.Deleted, &p.Audit.DeletedAt,
			&p.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		p.Scope = pipeline.Scope(scope)
		p.Trigger = pipeline.Trigger(trigger)
		if scopeID != nil {
			p.ScopeID = *scopeID
		}
		if triggerOn != nil {
			p.TriggerOnPipeline = *triggerOn
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdatePipeline(ctx context.Context, p *pipeline.Pipeline) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_pipelines SET name=$2, description=$3, trigger=$4, trigger_config=$5, trigger_on_pipeline=$6,
  stages_config=$7, enabled=$8, version=version+1, updated_at=now(), updated_by=$9
WHERE id=$1 AND deleted=false AND version=$10`,
		p.ID, p.Name, p.Description, string(p.Trigger), p.TriggerConfig, nilIfZero(p.TriggerOnPipeline),
		p.StagesConfig, p.Enabled, p.Audit.UpdatedBy, p.Audit.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrPipelineNotFound
	}
	return nil
}

func (r *Repository) DeletePipeline(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_pipelines SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2, enabled=false
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrPipelineNotFound
	}
	return nil
}

// --- stages ---

func (r *Repository) CreateStages(ctx context.Context, stages []*pipeline.Stage) error {
	for _, s := range stages {
		if s.UUID == uuid.Nil {
			s.UUID = uuid.New()
		}
		_, err := r.pool.Exec(ctx, `
INSERT INTO vo_pipeline_stages (uuid, pipeline_id, seq, name, type, gate, on_failure, params, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9)`,
			s.UUID, s.PipelineID, s.Seq, s.Name, string(s.Type), s.Gate, string(s.OnFailure), s.Params, s.CreatedBy)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListStages(ctx context.Context, pipelineID int64) ([]*pipeline.Stage, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, pipeline_id, seq, name, type, gate, on_failure, params, version, created_at, created_by, updated_at, updated_by
FROM vo_pipeline_stages WHERE pipeline_id=$1 AND deleted=false ORDER BY seq ASC`, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*pipeline.Stage, 0)
	for rows.Next() {
		s := &pipeline.Stage{}
		var stype, onFail string
		if err := rows.Scan(&s.ID, &s.UUID, &s.PipelineID, &s.Seq, &s.Name, &stype, &s.Gate, &onFail, &s.Params,
			&s.Audit.Version, &s.Audit.CreatedAt, &s.Audit.CreatedBy, &s.Audit.UpdatedAt, &s.Audit.UpdatedBy); err != nil {
			return nil, err
		}
		s.Type = pipeline.StageType(stype)
		s.OnFailure = pipeline.OnFailure(onFail)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteStages(ctx context.Context, pipelineID, actorID int64) error {
	_, err := r.pool.Exec(ctx, `
UPDATE vo_pipeline_stages SET deleted=true, deleted_at=now(), deleted_by=$2
WHERE pipeline_id=$1 AND deleted=false`, pipelineID, actorID)
	return err
}

// --- runs ---

func (r *Repository) CreateRun(ctx context.Context, run *pipeline.Run) error {
	if run.UUID == uuid.Nil {
		run.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_pipeline_runs (uuid, pipeline_id, workspace_id, run_number, trigger, trigger_ref, trigger_commit_sha,
  trigger_by, status, current_stage_seq, started_at, artifacts_image_ids, metadata, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,1,$14,$14)
RETURNING id, version, created_at, updated_at`,
		run.UUID, run.PipelineID, run.WorkspaceID, run.RunNumber, string(run.Trigger), run.TriggerRef,
		run.TriggerCommitSHA, nilIfZero(run.TriggerBy), string(run.Status), run.CurrentStageSeq, run.StartedAt,
		run.ArtifactsImageIDs, run.Metadata, run.CreatedBy)
	return row.Scan(&run.ID, &run.Audit.Version, &run.Audit.CreatedAt, &run.Audit.UpdatedAt)
}

func (r *Repository) GetRunByID(ctx context.Context, id int64) (*pipeline.Run, error) {
	run := &pipeline.Run{}
	var trigger, status string
	var triggerBy *int64
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, pipeline_id, workspace_id, run_number, trigger, trigger_ref, trigger_commit_sha, trigger_by,
       status, current_stage_seq, started_at, finished_at, duration_ms, artifacts_image_ids, metadata,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_pipeline_runs WHERE id=$1 AND deleted=false`, id).
		Scan(&run.ID, &run.UUID, &run.PipelineID, &run.WorkspaceID, &run.RunNumber, &trigger, &run.TriggerRef,
			&run.TriggerCommitSHA, &triggerBy, &status, &run.CurrentStageSeq, &run.StartedAt, &run.FinishedAt,
			&run.DurationMs, &run.ArtifactsImageIDs, &run.Metadata, &run.Audit.Version, &run.Audit.CreatedAt,
			&run.Audit.CreatedBy, &run.Audit.UpdatedAt, &run.Audit.UpdatedBy, &run.Audit.Deleted, &run.Audit.DeletedAt,
			&run.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pipeline.ErrRunNotFound
	}
	if err != nil {
		return nil, err
	}
	run.Trigger = pipeline.Trigger(trigger)
	run.Status = pipeline.RunStatus(status)
	if triggerBy != nil {
		run.TriggerBy = *triggerBy
	}
	return run, nil
}

func (r *Repository) ListRuns(ctx context.Context, q pipeline.RunQuery) ([]*pipeline.Run, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	where := "deleted=false"
	args := []any{}
	idx := 1
	add := func(clause string, val any) {
		where += " AND " + clause
		args = append(args, val)
		idx++
	}
	if q.PipelineID > 0 {
		add("pipeline_id=$"+fmt.Sprint(idx), q.PipelineID)
	}
	if q.WorkspaceID > 0 {
		add("workspace_id=$"+fmt.Sprint(idx), q.WorkspaceID)
	}
	if q.Status != "" {
		add("status=$"+fmt.Sprint(idx), string(q.Status))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_pipeline_runs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `SELECT id, uuid, pipeline_id, workspace_id, run_number, trigger, trigger_ref, trigger_commit_sha, trigger_by,
       status, current_stage_seq, started_at, finished_at, duration_ms, artifacts_image_ids, metadata,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_pipeline_runs WHERE ` + where + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*pipeline.Run, 0)
	for rows.Next() {
		run := &pipeline.Run{}
		var trigger, status string
		var triggerBy *int64
		if err := rows.Scan(&run.ID, &run.UUID, &run.PipelineID, &run.WorkspaceID, &run.RunNumber, &trigger,
			&run.TriggerRef, &run.TriggerCommitSHA, &triggerBy, &status, &run.CurrentStageSeq, &run.StartedAt,
			&run.FinishedAt, &run.DurationMs, &run.ArtifactsImageIDs, &run.Metadata, &run.Audit.Version,
			&run.Audit.CreatedAt, &run.Audit.CreatedBy, &run.Audit.UpdatedAt, &run.Audit.UpdatedBy, &run.Audit.Deleted,
			&run.Audit.DeletedAt, &run.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		run.Trigger = pipeline.Trigger(trigger)
		run.Status = pipeline.RunStatus(status)
		if triggerBy != nil {
			run.TriggerBy = *triggerBy
		}
		out = append(out, run)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdateRun(ctx context.Context, run *pipeline.Run) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_pipeline_runs SET status=$2, current_stage_seq=$3, finished_at=$4, duration_ms=$5,
  artifacts_image_ids=$6, metadata=$7, version=version+1, updated_at=now(), updated_by=$8
WHERE id=$1 AND deleted=false AND version=$9`,
		run.ID, string(run.Status), run.CurrentStageSeq, run.FinishedAt, run.DurationMs, run.ArtifactsImageIDs,
		run.Metadata, run.Audit.UpdatedBy, run.Audit.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrRunNotFound
	}
	return nil
}

func (r *Repository) NextRunNumber(ctx context.Context, pipelineID int64) (int, error) {
	var maxNum int
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(run_number),0)+1 FROM vo_pipeline_runs WHERE pipeline_id=$1`, pipelineID).Scan(&maxNum)
	return maxNum, err
}

func (r *Repository) ListActiveRuns(ctx context.Context) ([]*pipeline.Run, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, pipeline_id, workspace_id, run_number, trigger, trigger_ref, trigger_commit_sha, trigger_by,
       status, current_stage_seq, started_at, finished_at, duration_ms, artifacts_image_ids, metadata,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_pipeline_runs WHERE deleted=false AND status IN ('running','paused') ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*pipeline.Run, 0)
	for rows.Next() {
		run := &pipeline.Run{}
		var trigger, status string
		var triggerBy *int64
		if err := rows.Scan(&run.ID, &run.UUID, &run.PipelineID, &run.WorkspaceID, &run.RunNumber, &trigger,
			&run.TriggerRef, &run.TriggerCommitSHA, &triggerBy, &status, &run.CurrentStageSeq, &run.StartedAt,
			&run.FinishedAt, &run.DurationMs, &run.ArtifactsImageIDs, &run.Metadata, &run.Audit.Version,
			&run.Audit.CreatedAt, &run.Audit.CreatedBy, &run.Audit.UpdatedAt, &run.Audit.UpdatedBy, &run.Audit.Deleted,
			&run.Audit.DeletedAt, &run.Audit.DeletedBy); err != nil {
			return nil, err
		}
		run.Trigger = pipeline.Trigger(trigger)
		run.Status = pipeline.RunStatus(status)
		if triggerBy != nil {
			run.TriggerBy = *triggerBy
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// --- stage runs ---

func (r *Repository) CreateStageRun(ctx context.Context, sr *pipeline.StageRun) error {
	if sr.UUID == uuid.Nil {
		sr.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_pipeline_stage_runs (uuid, pipeline_run_id, stage_id, seq, status, related_build_id, related_release_id,
  related_image_id, gate_result, started_at, finished_at, message, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$13)
RETURNING id, version, created_at, updated_at`,
		sr.UUID, sr.PipelineRunID, sr.StageID, sr.Seq, string(sr.Status), nilIfZero(sr.RelatedBuildID),
		nilIfZero(sr.RelatedReleaseID), nilIfZero(sr.RelatedImageID), sr.GateResult, sr.StartedAt, sr.FinishedAt,
		sr.Message, sr.CreatedBy)
	return row.Scan(&sr.ID, &sr.Audit.Version, &sr.Audit.CreatedAt, &sr.Audit.UpdatedAt)
}

func (r *Repository) GetStageRunByID(ctx context.Context, id int64) (*pipeline.StageRun, error) {
	sr := &pipeline.StageRun{}
	var status string
	var buildID, releaseID, imageID *int64
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, pipeline_run_id, stage_id, seq, status, related_build_id, related_release_id, related_image_id,
       gate_result, started_at, finished_at, message, version, created_at, created_by, updated_at, updated_by,
       deleted, deleted_at, deleted_by
FROM vo_pipeline_stage_runs WHERE id=$1 AND deleted=false`, id).
		Scan(&sr.ID, &sr.UUID, &sr.PipelineRunID, &sr.StageID, &sr.Seq, &status, &buildID, &releaseID, &imageID,
			&sr.GateResult, &sr.StartedAt, &sr.FinishedAt, &sr.Message, &sr.Audit.Version, &sr.Audit.CreatedAt,
			&sr.Audit.CreatedBy, &sr.Audit.UpdatedAt, &sr.Audit.UpdatedBy, &sr.Audit.Deleted, &sr.Audit.DeletedAt,
			&sr.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pipeline.ErrStageRunNotFound
	}
	if err != nil {
		return nil, err
	}
	sr.Status = pipeline.StageRunStatus(status)
	if buildID != nil {
		sr.RelatedBuildID = *buildID
	}
	if releaseID != nil {
		sr.RelatedReleaseID = *releaseID
	}
	if imageID != nil {
		sr.RelatedImageID = *imageID
	}
	return sr, nil
}

func (r *Repository) ListStageRuns(ctx context.Context, runID int64) ([]*pipeline.StageRun, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_pipeline_stage_runs WHERE pipeline_run_id=$1 AND deleted=false`, runID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, pipeline_run_id, stage_id, seq, status, related_build_id, related_release_id, related_image_id,
       gate_result, started_at, finished_at, message, version, created_at, created_by, updated_at, updated_by,
       deleted, deleted_at, deleted_by
FROM vo_pipeline_stage_runs WHERE pipeline_run_id=$1 AND deleted=false ORDER BY seq ASC`, runID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*pipeline.StageRun, 0)
	for rows.Next() {
		sr := &pipeline.StageRun{}
		var status string
		var buildID, releaseID, imageID *int64
		if err := rows.Scan(&sr.ID, &sr.UUID, &sr.PipelineRunID, &sr.StageID, &sr.Seq, &status, &buildID, &releaseID,
			&imageID, &sr.GateResult, &sr.StartedAt, &sr.FinishedAt, &sr.Message, &sr.Audit.Version, &sr.Audit.CreatedAt,
			&sr.Audit.CreatedBy, &sr.Audit.UpdatedAt, &sr.Audit.UpdatedBy, &sr.Audit.Deleted, &sr.Audit.DeletedAt,
			&sr.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		sr.Status = pipeline.StageRunStatus(status)
		if buildID != nil {
			sr.RelatedBuildID = *buildID
		}
		if releaseID != nil {
			sr.RelatedReleaseID = *releaseID
		}
		if imageID != nil {
			sr.RelatedImageID = *imageID
		}
		out = append(out, sr)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdateStageRun(ctx context.Context, sr *pipeline.StageRun) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_pipeline_stage_runs SET status=$2, related_build_id=$3, related_release_id=$4, related_image_id=$5,
  gate_result=$6, started_at=$7, finished_at=$8, message=$9, version=version+1, updated_at=now(), updated_by=$10
WHERE id=$1 AND deleted=false AND version=$11`,
		sr.ID, string(sr.Status), nilIfZero(sr.RelatedBuildID), nilIfZero(sr.RelatedReleaseID),
		nilIfZero(sr.RelatedImageID), sr.GateResult, sr.StartedAt, sr.FinishedAt, sr.Message, sr.Audit.UpdatedBy,
		sr.Audit.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrStageRunNotFound
	}
	return nil
}

// --- promotions ---

func (r *Repository) CreatePromotion(ctx context.Context, p *pipeline.Promotion) error {
	if p.UUID == uuid.Nil {
		p.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_promotions (uuid, workspace_id, application_id, source_env, target_env, artifact_image_id,
  artifact_config_version, target_group_ids, strategy, auto_promote_on_verify, status, pipeline_run_id,
  approval_instance_id, started_by, started_at, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,1,$16,$16)
RETURNING id, version, created_at, updated_at`,
		p.UUID, p.WorkspaceID, p.ApplicationID, p.SourceEnv, p.TargetEnv, p.ArtifactImageID, p.ArtifactConfigVersion,
		p.TargetGroupIDs, string(p.Strategy), p.AutoPromoteOnVerify, string(p.Status), nilIfZero(p.PipelineRunID),
		nilIfZero(p.ApprovalInstanceID), p.StartedBy, p.StartedAt, p.CreatedBy)
	return row.Scan(&p.ID, &p.Audit.Version, &p.Audit.CreatedAt, &p.Audit.UpdatedAt)
}

func (r *Repository) GetPromotionByID(ctx context.Context, id int64) (*pipeline.Promotion, error) {
	p := &pipeline.Promotion{}
	var strategy, status string
	var pipelineRunID, approvalID *int64
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, workspace_id, application_id, source_env, target_env, artifact_image_id, artifact_config_version,
       target_group_ids, strategy, auto_promote_on_verify, status, pipeline_run_id, approval_instance_id,
       started_by, started_at, finished_at, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_promotions WHERE id=$1 AND deleted=false`, id).
		Scan(&p.ID, &p.UUID, &p.WorkspaceID, &p.ApplicationID, &p.SourceEnv, &p.TargetEnv, &p.ArtifactImageID,
			&p.ArtifactConfigVersion, &p.TargetGroupIDs, &strategy, &p.AutoPromoteOnVerify, &status, &pipelineRunID,
			&approvalID, &p.StartedBy, &p.StartedAt, &p.FinishedAt, &p.Audit.Version, &p.Audit.CreatedAt,
			&p.Audit.CreatedBy, &p.Audit.UpdatedAt, &p.Audit.UpdatedBy, &p.Audit.Deleted, &p.Audit.DeletedAt,
			&p.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pipeline.ErrPromotionNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Strategy = pipeline.PromotionStrategy(strategy)
	p.Status = pipeline.PromotionStatus(status)
	if pipelineRunID != nil {
		p.PipelineRunID = *pipelineRunID
	}
	if approvalID != nil {
		p.ApprovalInstanceID = *approvalID
	}
	return p, nil
}

func (r *Repository) ListPromotions(ctx context.Context, q pipeline.PromotionQuery) ([]*pipeline.Promotion, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	where := "deleted=false"
	args := []any{}
	idx := 1
	add := func(clause string, val any) {
		where += " AND " + clause
		args = append(args, val)
		idx++
	}
	if q.WorkspaceID > 0 {
		add("workspace_id=$"+fmt.Sprint(idx), q.WorkspaceID)
	}
	if q.ApplicationID > 0 {
		add("application_id=$"+fmt.Sprint(idx), q.ApplicationID)
	}
	if q.Status != "" {
		add("status=$"+fmt.Sprint(idx), string(q.Status))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_promotions WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `SELECT id, uuid, workspace_id, application_id, source_env, target_env, artifact_image_id, artifact_config_version,
       target_group_ids, strategy, auto_promote_on_verify, status, pipeline_run_id, approval_instance_id,
       started_by, started_at, finished_at, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_promotions WHERE ` + where + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*pipeline.Promotion, 0)
	for rows.Next() {
		p := &pipeline.Promotion{}
		var strategy, status string
		var pipelineRunID, approvalID *int64
		if err := rows.Scan(&p.ID, &p.UUID, &p.WorkspaceID, &p.ApplicationID, &p.SourceEnv, &p.TargetEnv,
			&p.ArtifactImageID, &p.ArtifactConfigVersion, &p.TargetGroupIDs, &strategy, &p.AutoPromoteOnVerify, &status,
			&pipelineRunID, &approvalID, &p.StartedBy, &p.StartedAt, &p.FinishedAt, &p.Audit.Version, &p.Audit.CreatedAt,
			&p.Audit.CreatedBy, &p.Audit.UpdatedAt, &p.Audit.UpdatedBy, &p.Audit.Deleted, &p.Audit.DeletedAt,
			&p.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		p.Strategy = pipeline.PromotionStrategy(strategy)
		p.Status = pipeline.PromotionStatus(status)
		if pipelineRunID != nil {
			p.PipelineRunID = *pipelineRunID
		}
		if approvalID != nil {
			p.ApprovalInstanceID = *approvalID
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdatePromotion(ctx context.Context, p *pipeline.Promotion) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_promotions SET status=$2, pipeline_run_id=$3, approval_instance_id=$4, finished_at=$5,
  version=version+1, updated_at=now(), updated_by=$6
WHERE id=$1 AND deleted=false AND version=$7`,
		p.ID, string(p.Status), nilIfZero(p.PipelineRunID), nilIfZero(p.ApprovalInstanceID), p.FinishedAt,
		p.Audit.UpdatedBy, p.Audit.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrPromotionNotFound
	}
	return nil
}

// --- signatures ---

func (r *Repository) CreateSignature(ctx context.Context, s *pipeline.ArtifactSignature) error {
	if s.UUID == uuid.Nil {
		s.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_artifacts_signatures (uuid, image_id, signature_type, signature_payload, public_key_ref, signed_by,
  signed_at, sbom_storage_key, sbom_format, provenance, verification_status, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12,$12)
RETURNING id, version, created_at, updated_at`,
		s.UUID, s.ImageID, string(s.SignatureType), s.SignaturePayload, s.PublicKeyRef, s.SignedBy, s.SignedAt,
		s.SBOMStorageKey, stringOrNil(s.SBOMFormat), s.Provenance, string(s.VerificationStatus), s.CreatedBy)
	return row.Scan(&s.ID, &s.Audit.Version, &s.Audit.CreatedAt, &s.Audit.UpdatedAt)
}

func (r *Repository) GetSignatureByImageID(ctx context.Context, imageID int64) (*pipeline.ArtifactSignature, error) {
	s := &pipeline.ArtifactSignature{}
	var sigType, verifyStatus string
	var sbomFormat *string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, image_id, signature_type, signature_payload, public_key_ref, signed_by, signed_at,
       sbom_storage_key, sbom_format, provenance, verification_status, version, created_at, created_by, updated_at, updated_by,
       deleted, deleted_at, deleted_by
FROM vo_artifacts_signatures WHERE image_id=$1 AND deleted=false`, imageID).
		Scan(&s.ID, &s.UUID, &s.ImageID, &sigType, &s.SignaturePayload, &s.PublicKeyRef, &s.SignedBy, &s.SignedAt,
			&s.SBOMStorageKey, &sbomFormat, &s.Provenance, &verifyStatus, &s.Audit.Version, &s.Audit.CreatedAt,
			&s.Audit.CreatedBy, &s.Audit.UpdatedAt, &s.Audit.UpdatedBy, &s.Audit.Deleted, &s.Audit.DeletedAt,
			&s.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pipeline.ErrSignatureNotFound
	}
	if err != nil {
		return nil, err
	}
	s.SignatureType = pipeline.SignatureType(sigType)
	s.VerificationStatus = pipeline.VerificationStatus(verifyStatus)
	if sbomFormat != nil {
		s.SBOMFormat = pipeline.SBOMFormat(*sbomFormat)
	}
	return s, nil
}

func (r *Repository) UpdateSignature(ctx context.Context, s *pipeline.ArtifactSignature) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_artifacts_signatures SET signature_payload=$2, public_key_ref=$3, signed_by=$4, signed_at=$5,
  sbom_storage_key=$6, sbom_format=$7, provenance=$8, verification_status=$9,
  version=version+1, updated_at=now(), updated_by=$10
WHERE id=$1 AND deleted=false AND version=$11`,
		s.ID, s.SignaturePayload, s.PublicKeyRef, s.SignedBy, s.SignedAt, s.SBOMStorageKey, stringOrNil(s.SBOMFormat),
		s.Provenance, string(s.VerificationStatus), s.Audit.UpdatedBy, s.Audit.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrSignatureNotFound
	}
	return nil
}

// --- helpers ---

func nilIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func stringOrNil(v pipeline.SBOMFormat) any {
	if v == "" {
		return nil
	}
	return string(v)
}
