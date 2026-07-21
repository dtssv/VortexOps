package buildrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/build"
)

// --- 构建任务 ---

const buildColumns = `id, uuid, application_id, build_number, git_source_id, ref_type, ref_value, commit_sha,
	commit_message, build_template_id, build_strategy, build_command, context_path, artifact_path, dockerfile_path, base_image_id, build_tool, builder_image, dockerfile_source,
	dockerfile_content, build_args, target_registry_id, target_repository, target_tag, output_image_id,
	jenkins_instance_id, jenkins_queue_id, jenkins_build_number, jenkins_job_name, pipeline_run_name,
	status, progress_percent, current_step, duration_ms, started_at, finished_at, log_storage_key, log_excerpt,
	failure_reason, triggered_by, trigger_source, idempotency_key, metadata, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanBuild(row pgx.Row) (*build.Build, error) {
	b := &build.Build{
		BuildArgs: map[string]string{}, Metadata: map[string]any{},
	}
	var (
		commitSHA         *string
		commitMessage     *string
		buildTemplateID   *int64
		buildCommand      *string
		contextPath       *string
		artifactPath      *string
		dockerfilePath    *string
		baseImageID       *int64
		buildTool         *string
		builderImage      *string
		dockerfileSource  *string
		dockerfileContent *string
		buildArgs         []byte
		targetRegistryID  *int64
		targetRepository  *string
		targetTag         *string
		outputImageID     *int64
		jenkinsInstanceID *int64
		jenkinsQueueID    *string
		jenkinsBuildNum   *int
		jenkinsJobName    *string
		pipelineRunName   *string
		currentStep       *string
		durationMs        *int64
		startedAt         *time.Time
		finishedAt        *time.Time
		logStorageKey     *string
		logExcerpt        *string
		failureReason     *string
		idempotencyKey    *string
		metadata          []byte
		createdBy         *int64
		updatedBy         *int64
		deletedAt         *time.Time
		deletedBy         *int64
	)
	if err := row.Scan(
		&b.ID, &b.UUID, &b.ApplicationID, &b.BuildNumber, &b.GitSourceID, &b.RefType, &b.RefValue, &commitSHA,
		&commitMessage, &buildTemplateID, &b.BuildStrategy, &buildCommand, &contextPath, &artifactPath, &dockerfilePath, &baseImageID, &buildTool, &builderImage, &dockerfileSource,
		&dockerfileContent, &buildArgs, &targetRegistryID, &targetRepository, &targetTag, &outputImageID,
		&jenkinsInstanceID, &jenkinsQueueID, &jenkinsBuildNum, &jenkinsJobName, &pipelineRunName, &b.Status, &b.ProgressPercent,
		&currentStep, &durationMs, &startedAt, &finishedAt, &logStorageKey, &logExcerpt, &failureReason, &b.TriggeredBy,
		&b.TriggerSource, &idempotencyKey, &metadata, &b.Version, &b.CreatedAt, &createdBy, &b.UpdatedAt, &updatedBy,
		&b.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if commitSHA != nil {
		b.CommitSHA = *commitSHA
	}
	if commitMessage != nil {
		b.CommitMessage = *commitMessage
	}
	if buildTemplateID != nil {
		b.BuildTemplateID = *buildTemplateID
	}
	if buildCommand != nil {
		b.BuildCommand = *buildCommand
	}
	if contextPath != nil {
		b.ContextPath = *contextPath
	}
	if artifactPath != nil {
		b.ArtifactPath = *artifactPath
	}
	if dockerfilePath != nil {
		b.DockerfilePath = *dockerfilePath
	}
	if baseImageID != nil {
		b.BaseImageID = *baseImageID
	}
	if buildTool != nil {
		b.BuildTool = *buildTool
	}
	if builderImage != nil {
		b.BuilderImage = *builderImage
	}
	if dockerfileSource != nil {
		b.DockerfileSource = build.DockerfileSource(*dockerfileSource)
	}
	if dockerfileContent != nil {
		b.DockerfileContent = *dockerfileContent
	}
	if buildArgs != nil {
		_ = jsonUnmarshal(buildArgs, &b.BuildArgs)
	}
	if targetRegistryID != nil {
		b.TargetRegistryID = *targetRegistryID
	}
	if targetRepository != nil {
		b.TargetRepository = *targetRepository
	}
	if targetTag != nil {
		b.TargetTag = *targetTag
	}
	if outputImageID != nil {
		b.OutputImageID = *outputImageID
	}
	if jenkinsInstanceID != nil {
		b.JenkinsInstanceID = *jenkinsInstanceID
	}
	if jenkinsQueueID != nil {
		b.JenkinsQueueID = *jenkinsQueueID
	}
	if jenkinsBuildNum != nil {
		b.JenkinsBuildNumber = *jenkinsBuildNum
	}
	if jenkinsJobName != nil {
		b.JenkinsJobName = *jenkinsJobName
	}
	if pipelineRunName != nil {
		b.PipelineRunName = *pipelineRunName
	}
	if currentStep != nil {
		b.CurrentStep = *currentStep
	}
	if durationMs != nil {
		b.DurationMs = *durationMs
	}
	if startedAt != nil {
		b.StartedAt = startedAt
	}
	if finishedAt != nil {
		b.FinishedAt = finishedAt
	}
	if logStorageKey != nil {
		b.LogStorageKey = *logStorageKey
	}
	if logExcerpt != nil {
		b.LogExcerpt = *logExcerpt
	}
	if failureReason != nil {
		b.FailureReason = *failureReason
	}
	if idempotencyKey != nil {
		b.IdempotencyKey = *idempotencyKey
	}
	if metadata != nil {
		_ = jsonUnmarshal(metadata, &b.Metadata)
	}
	if createdBy != nil {
		b.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		b.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		b.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		b.DeletedBy = *deletedBy
	}
	return b, nil
}

// CreateBuild 创建构建任务。build_number 由 NextBuildNumber 预先获取。
func (r *Repository) CreateBuild(ctx context.Context, b *build.Build) error {
	if b.UUID == uuid.Nil {
		b.UUID = uuid.New()
	}
	now := r.now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
		b.UpdatedAt = now
	}
	if b.Status == "" {
		b.Status = build.BuildPending
	}
	if b.TriggerSource == "" {
		b.TriggerSource = build.TriggerManual
	}
	if b.BuildArgs == nil {
		b.BuildArgs = map[string]string{}
	}
	if b.Metadata == nil {
		b.Metadata = map[string]any{}
	}
	const q = `INSERT INTO vo_builds
		(uuid, application_id, build_number, git_source_id, ref_type, ref_value, commit_sha, commit_message,
		 build_template_id, build_strategy, build_command, context_path, artifact_path, dockerfile_path, base_image_id, build_tool, builder_image, dockerfile_source,
		 dockerfile_content, build_args, target_registry_id, target_repository, target_tag, output_image_id,
		 jenkins_instance_id, jenkins_queue_id, jenkins_build_number, jenkins_job_name, pipeline_run_name,
		 status, progress_percent, current_step, duration_ms, started_at, finished_at, log_storage_key, log_excerpt,
		 failure_reason, triggered_by, trigger_source, idempotency_key, metadata, version, created_at, created_by,
		 updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		b.UUID, b.ApplicationID, b.BuildNumber, b.GitSourceID, b.RefType, b.RefValue, nullableStr(b.CommitSHA),
		nullableStr(b.CommitMessage), nullableInt64(b.BuildTemplateID), b.BuildStrategy, nullableStr(b.BuildCommand),
		nullableStr(b.ContextPath), nullableStr(b.ArtifactPath), nullableStr(b.DockerfilePath), nullableInt64(b.BaseImageID), nullableStr(b.BuildTool), nullableStr(b.BuilderImage), nullableDockerfileSourceStr(b.DockerfileSource),
		nullableStr(b.DockerfileContent), jsonbPtrMarshal(b.BuildArgs), b.TargetRegistryID, b.TargetRepository,
		nullableStr(b.TargetTag), nullableInt64(b.OutputImageID), nullableInt64(b.JenkinsInstanceID),
		nullableStr(b.JenkinsQueueID), nullableInt(b.JenkinsBuildNumber), nullableStr(b.JenkinsJobName),
		nullableStr(b.PipelineRunName),
		b.Status, b.ProgressPercent, nullableStr(b.CurrentStep), nullableInt64(b.DurationMs), b.StartedAt, b.FinishedAt,
		nullableStr(b.LogStorageKey), nullableStr(b.LogExcerpt), nullableStr(b.FailureReason), b.TriggeredBy,
		b.TriggerSource, nullableStr(b.IdempotencyKey), jsonbPtrMarshal(b.Metadata), b.Version, b.CreatedAt,
		nullableInt64(b.CreatedBy), b.UpdatedAt, nullableInt64(b.CreatedBy),
	).Scan(&b.ID, &b.Version, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return build.ErrIdempotencyConflict
		}
		return fmt.Errorf("insert build: %w", err)
	}
	return nil
}

// GetBuildByID 按 ID 查询构建。
func (r *Repository) GetBuildByID(ctx context.Context, id int64) (*build.Build, error) {
	q := `SELECT ` + buildColumns + ` FROM vo_builds WHERE id=$1 AND deleted=false`
	b, err := scanBuild(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBuildNotFound
		}
		return nil, err
	}
	return b, nil
}

// GetBuildByUUID 按 UUID 查询构建。
func (r *Repository) GetBuildByUUID(ctx context.Context, id uuid.UUID) (*build.Build, error) {
	q := `SELECT ` + buildColumns + ` FROM vo_builds WHERE uuid=$1 AND deleted=false`
	b, err := scanBuild(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBuildNotFound
		}
		return nil, err
	}
	return b, nil
}

// GetBuildByIdempotencyKey 按 idempotency key 查询（幂等去重）。
func (r *Repository) GetBuildByIdempotencyKey(ctx context.Context, appID int64, key string) (*build.Build, error) {
	q := `SELECT ` + buildColumns + ` FROM vo_builds WHERE application_id=$1 AND idempotency_key=$2 AND deleted=false`
	b, err := scanBuild(r.pool.QueryRow(ctx, q, appID, key))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrBuildNotFound
		}
		return nil, err
	}
	return b, nil
}

// NextBuildNumber 获取下一个 build_number（按应用自增）。
func (r *Repository) NextBuildNumber(ctx context.Context, appID int64) (int, error) {
	var maxNum int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(build_number), 0) FROM vo_builds WHERE application_id=$1`, appID).Scan(&maxNum)
	if err != nil {
		return 0, fmt.Errorf("get max build number: %w", err)
	}
	return maxNum + 1, nil
}

// ListBuilds 分页查询构建。
func (r *Repository) ListBuilds(ctx context.Context, q build.BuildQuery) ([]*build.Build, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if q.ApplicationID != 0 {
		conds = append(conds, fmt.Sprintf("application_id = $%d", len(args)+1))
		args = append(args, q.ApplicationID)
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, q.Status)
	}
	if q.TriggeredBy != 0 {
		conds = append(conds, fmt.Sprintf("triggered_by = $%d", len(args)+1))
		args = append(args, q.TriggeredBy)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_builds WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count builds: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_builds WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		buildColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query builds: %w", err)
	}
	defer rows.Close()
	var items []*build.Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, b)
	}
	return items, total, rows.Err()
}

// UpdateBuildStatus 更新构建状态/进度/当前步骤（乐观锁）。
func (r *Repository) UpdateBuildStatus(ctx context.Context, id int64, status build.BuildStatus, progress int, currentStep string, version int) (*build.Build, error) {
	now := r.now()
	startedExpr := "started_at"
	if status == build.BuildRunning {
		startedExpr = "COALESCE(started_at, $7)"
	}
	q := fmt.Sprintf(`UPDATE vo_builds SET status=$1, progress_percent=$2, current_step=$3, updated_at=$4, version=version+1,
		%s = %s
		WHERE id=$5 AND version=$6 AND deleted=false`, startedExpr, startedExpr)
	tag, err := r.pool.Exec(ctx, q, status, progress, nullableStr(currentStep), now, id, version, now)
	if err != nil {
		return nil, fmt.Errorf("update build status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetBuildByID(ctx, id)
}

// CompleteBuild 完成构建（终态）。
func (r *Repository) CompleteBuild(ctx context.Context, id int64, status build.BuildStatus, outputImageID int64, durationMs int64, logKey, logExcerpt, failureReason string, finishedAt time.Time, version int) (*build.Build, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET status=$1, output_image_id=$2, duration_ms=$3, log_storage_key=$4, log_excerpt=$5,
		 failure_reason=$6, finished_at=$7, progress_percent=100, updated_at=$8, version=version+1
		 WHERE id=$9 AND version=$10 AND deleted=false`,
		status, nullableInt64(outputImageID), durationMs, nullableStr(logKey), nullableStr(logExcerpt),
		nullableStr(failureReason), finishedAt, r.now(), id, version)
	if err != nil {
		return nil, fmt.Errorf("complete build: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetBuildByID(ctx, id)
}

// SetJenkinsInfo 设置 Jenkins 触发返回的队列项与构建号。
func (r *Repository) SetJenkinsInfo(ctx context.Context, id int64, queueID string, buildNum int, jobName string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET jenkins_queue_id=$1, jenkins_build_number=$2, jenkins_job_name=$3, status='running',
		 started_at=COALESCE(started_at, now()), updated_at=now(), version=version+1
		 WHERE id=$4 AND deleted=false`,
		nullableStr(queueID), nullableInt(buildNum), nullableStr(jobName), id)
	if err != nil {
		return fmt.Errorf("set jenkins info: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBuildNotFound
	}
	return nil
}

// SetBuildTargetTag 回填/更新构建的 target_tag（rebuild 重新生成 tag 后持久化）。
// 仅写 target_tag 与 updated_at，不影响 status / 运行态字段。
func (r *Repository) SetBuildTargetTag(ctx context.Context, id int64, tag string) error {
	if tag == "" {
		return nil
	}
	if _, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET target_tag=$1, updated_at=now() WHERE id=$2 AND deleted=false`,
		tag, id); err != nil {
		return fmt.Errorf("set build target tag: %w", err)
	}
	return nil
}

// SetJenkinsBuildNumber 在队列项被调度后回填 jenkins_build_number。
// 仅当当前值为 0/NULL 时写入，避免覆盖已解析的构建号；不改变 status 与其它字段。
func (r *Repository) SetJenkinsBuildNumber(ctx context.Context, id int64, buildNumber int) error {
	if buildNumber <= 0 {
		return nil
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET jenkins_build_number=$1, updated_at=now(), version=version+1
		 WHERE id=$2 AND deleted=false AND (jenkins_build_number IS NULL OR jenkins_build_number = 0)`,
		buildNumber, id)
	if err != nil {
		return fmt.Errorf("set jenkins build number: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// 行不存在或已有构建号：视为幂等成功。
		return nil
	}
	return nil
}

// SetPipelineRunName 记录 Tekton PipelineRun 名称并标记构建为 running。
func (r *Repository) SetPipelineRunName(ctx context.Context, id int64, pipelineRunName string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET pipeline_run_name=$1, status='running',
		 started_at=COALESCE(started_at, now()), updated_at=now(), version=version+1
		 WHERE id=$2 AND deleted=false`,
		nullableStr(pipelineRunName), id)
	if err != nil {
		return fmt.Errorf("set pipeline_run_name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBuildNotFound
	}
	return nil
}

// UpdateBuild 更新构建可编辑元信息（commit_message/target_tag/metadata），乐观锁。
// 返回更新后的构建。
func (r *Repository) UpdateBuild(ctx context.Context, b *build.Build) (*build.Build, error) {
	if b.Metadata == nil {
		b.Metadata = map[string]any{}
	}
	if b.BuildArgs == nil {
		b.BuildArgs = map[string]string{}
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET
		   commit_message=$1, target_tag=$2, metadata=$3,
		   ref_type=$4, ref_value=$5, git_source_id=$6,
		   build_command=$7, context_path=$8, artifact_path=$9, dockerfile_path=$10, base_image_id=$11,
		   build_tool=$12, builder_image=$13,
		   dockerfile_source=$14, dockerfile_content=$15, build_args=$16,
		   target_repository=$17,
		   updated_at=$18, version=version+1
		 WHERE id=$19 AND version=$20 AND deleted=false`,
		nullableStr(b.CommitMessage), nullableStr(b.TargetTag), jsonbPtrMarshal(b.Metadata),
		nullableStr(string(b.RefType)), nullableStr(b.RefValue), nullableInt64(b.GitSourceID),
		nullableStr(b.BuildCommand), nullableStr(b.ContextPath), nullableStr(b.ArtifactPath), nullableStr(b.DockerfilePath), nullableInt64(b.BaseImageID),
		nullableStr(b.BuildTool), nullableStr(b.BuilderImage),
		nullableDockerfileSourceStr(b.DockerfileSource), nullableStr(b.DockerfileContent), jsonbPtrMarshal(b.BuildArgs),
		nullableStr(b.TargetRepository),
		r.now(), b.ID, b.Version)
	if err != nil {
		return nil, fmt.Errorf("update build: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetBuildByID(ctx, b.ID)
}

// DeleteBuild 软删除构建。
func (r *Repository) DeleteBuild(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete build: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBuildNotFound
	}
	return nil
}

// ResetBuildForRebuild 重置构建为 pending 以便重新拉取代码并构建（同一条记录）。
// 清空运行态字段并写入新的 commit 信息，乐观锁。
func (r *Repository) ResetBuildForRebuild(ctx context.Context, id int64, commitSHA, commitMessage string, version int) (*build.Build, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_builds SET status='pending', progress_percent=0, current_step=NULL,
		 duration_ms=NULL, started_at=NULL, finished_at=NULL,
		 log_storage_key=NULL, log_excerpt=NULL, failure_reason=NULL,
		 output_image_id=NULL, jenkins_queue_id=NULL, jenkins_build_number=NULL, pipeline_run_name=NULL,
		 commit_sha=$1, commit_message=$2,
		 updated_at=$3, version=version+1
		 WHERE id=$4 AND version=$5 AND deleted=false`,
		nullableStr(commitSHA), nullableStr(commitMessage), r.now(), id, version)
	if err != nil {
		return nil, fmt.Errorf("reset build for rebuild: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetBuildByID(ctx, id)
}

// --- 构建步骤 ---

const stepColumns = `id, build_id, seq, name, status, started_at, finished_at, duration_ms, message,
	log_offset_start, log_offset_end, log_storage_key, log_size_bytes, error_line`

func scanStep(row pgx.Row) (*build.BuildStep, error) {
	s := &build.BuildStep{}
	var (
		startedAt    *time.Time
		finishedAt   *time.Time
		durationMs   *int64
		message      *string
		logStorageKey *string
		errorLine    *string
	)
	if err := row.Scan(
		&s.ID, &s.BuildID, &s.Seq, &s.Name, &s.Status, &startedAt, &finishedAt, &durationMs, &message,
		&s.LogOffsetStart, &s.LogOffsetEnd, &logStorageKey, &s.LogSizeBytes, &errorLine,
	); err != nil {
		return nil, err
	}
	if startedAt != nil {
		s.StartedAt = startedAt
	}
	if finishedAt != nil {
		s.FinishedAt = finishedAt
	}
	if durationMs != nil {
		s.DurationMs = *durationMs
	}
	if message != nil {
		s.Message = *message
	}
	if logStorageKey != nil {
		s.LogStorageKey = *logStorageKey
	}
	if errorLine != nil {
		s.ErrorLine = *errorLine
	}
	return s, nil
}

// CreateStep 创建构建步骤。
func (r *Repository) CreateStep(ctx context.Context, s *build.BuildStep) error {
	const q = `INSERT INTO vo_build_steps
		(build_id, seq, name, status, started_at, finished_at, duration_ms, message, log_offset_start, log_offset_end,
		 log_storage_key, log_size_bytes, error_line)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		s.BuildID, s.Seq, s.Name, s.Status, s.StartedAt, s.FinishedAt, nullableInt64(s.DurationMs),
		nullableStr(s.Message), nullableInt64(s.LogOffsetStart), nullableInt64(s.LogOffsetEnd),
		nullableStr(s.LogStorageKey), s.LogSizeBytes, nullableStr(s.ErrorLine),
	).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("insert build step: %w", err)
	}
	return nil
}

// UpdateStep 更新构建步骤。
func (r *Repository) UpdateStep(ctx context.Context, s *build.BuildStep) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_build_steps SET status=$1, started_at=$2, finished_at=$3, duration_ms=$4, message=$5,
		 log_offset_start=$6, log_offset_end=$7, log_storage_key=$8, log_size_bytes=$9, error_line=$10
		 WHERE id=$11`,
		s.Status, s.StartedAt, s.FinishedAt, nullableInt64(s.DurationMs), nullableStr(s.Message),
		nullableInt64(s.LogOffsetStart), nullableInt64(s.LogOffsetEnd), nullableStr(s.LogStorageKey),
		s.LogSizeBytes, nullableStr(s.ErrorLine), s.ID)
	if err != nil {
		return fmt.Errorf("update build step: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrBuildNotFound
	}
	return nil
}

// ListSteps 列出构建的步骤。
func (r *Repository) ListSteps(ctx context.Context, buildID int64) ([]*build.BuildStep, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+stepColumns+` FROM vo_build_steps WHERE build_id=$1 ORDER BY seq ASC`, buildID)
	if err != nil {
		return nil, fmt.Errorf("query build steps: %w", err)
	}
	defer rows.Close()
	var items []*build.BuildStep
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// --- 制品版本 ---

const imageColumns = `id, uuid, application_id, registry_id, repository, tag, digest, full_reference, version_number,
	version_label, source, build_id, git_commit_sha, git_branch, git_commit_message, git_author, size_bytes,
	scan_status, scan_result, status, is_rollback_target, labels, version_col, created_at, created_by, updated_at,
	updated_by, deleted, deleted_at, deleted_by`

func scanImage(row pgx.Row) (*build.Image, error) {
	img := &build.Image{
		Labels: map[string]string{}, ScanResult: map[string]any{},
	}
	var (
		versionLabel      *string
		buildID           *int64
		gitCommitSHA      *string
		gitBranch         *string
		gitCommitMessage  *string
		gitAuthor         *string
		sizeBytes         *int64
		scanResult        []byte
		labels            []byte
		createdBy         *int64
		updatedBy         *int64
		deletedAt         *time.Time
		deletedBy         *int64
	)
	if err := row.Scan(
		&img.ID, &img.UUID, &img.ApplicationID, &img.RegistryID, &img.Repository, &img.Tag, &img.Digest,
		&img.FullReference, &img.VersionNumber, &versionLabel, &img.Source, &buildID, &gitCommitSHA, &gitBranch,
		&gitCommitMessage, &gitAuthor, &sizeBytes, &img.ScanStatus, &scanResult, &img.Status, &img.IsRollbackTarget,
		&labels, &img.Version, &img.CreatedAt, &createdBy, &img.UpdatedAt, &updatedBy, &img.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if versionLabel != nil {
		img.VersionLabel = *versionLabel
	}
	if buildID != nil {
		img.BuildID = *buildID
	}
	if gitCommitSHA != nil {
		img.GitCommitSHA = *gitCommitSHA
	}
	if gitBranch != nil {
		img.GitBranch = *gitBranch
	}
	if gitCommitMessage != nil {
		img.GitCommitMessage = *gitCommitMessage
	}
	if gitAuthor != nil {
		img.GitAuthor = *gitAuthor
	}
	if sizeBytes != nil {
		img.SizeBytes = *sizeBytes
	}
	if scanResult != nil {
		_ = jsonUnmarshal(scanResult, &img.ScanResult)
	}
	if labels != nil {
		_ = jsonUnmarshal(labels, &img.Labels)
	}
	if createdBy != nil {
		img.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		img.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		img.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		img.DeletedBy = *deletedBy
	}
	return img, nil
}

// CreateImage 创建制品版本。
func (r *Repository) CreateImage(ctx context.Context, img *build.Image) error {
	if img.UUID == uuid.Nil {
		img.UUID = uuid.New()
	}
	now := r.now()
	if img.CreatedAt.IsZero() {
		img.CreatedAt = now
		img.UpdatedAt = now
	}
	if img.Source == "" {
		img.Source = build.ImgSourceBuild
	}
	if img.ScanStatus == "" {
		img.ScanStatus = build.ImgScanPending
	}
	if img.Status == "" {
		img.Status = build.ImgStatusAvailable
	}
	if img.Labels == nil {
		img.Labels = map[string]string{}
	}
	const q = `INSERT INTO vo_images
		(uuid, application_id, registry_id, repository, tag, digest, full_reference, version_number, version_label,
		 source, build_id, git_commit_sha, git_branch, git_commit_message, git_author, size_bytes, scan_status,
		 scan_result, status, is_rollback_target, labels, version_col, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
		RETURNING id, version_col, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		img.UUID, img.ApplicationID, img.RegistryID, img.Repository, img.Tag, img.Digest, img.FullReference,
		img.VersionNumber, nullableStr(img.VersionLabel), img.Source, nullableInt64(img.BuildID),
		nullableStr(img.GitCommitSHA), nullableStr(img.GitBranch), nullableStr(img.GitCommitMessage),
		nullableStr(img.GitAuthor), nullableInt64(img.SizeBytes), img.ScanStatus, jsonbPtrMarshal(img.ScanResult),
		img.Status, img.IsRollbackTarget, jsonbObjNonNil(img.Labels), img.Version, img.CreatedAt,
		nullableInt64(img.CreatedBy), img.UpdatedAt, nullableInt64(img.CreatedBy),
	).Scan(&img.ID, &img.Version, &img.CreatedAt, &img.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert image: %w", err)
	}
	return nil
}

// GetImageByID 按 ID 查询镜像。
func (r *Repository) GetImageByID(ctx context.Context, id int64) (*build.Image, error) {
	q := `SELECT ` + imageColumns + ` FROM vo_images WHERE id=$1 AND deleted=false`
	img, err := scanImage(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrImageNotFound
		}
		return nil, err
	}
	return img, nil
}

// ListImages 分页列出镜像。
func (r *Repository) ListImages(ctx context.Context, appID int64, offset, limit int) ([]*build.Image, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_images WHERE application_id=$1 AND deleted=false`, appID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count images: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+imageColumns+` FROM vo_images WHERE application_id=$1 AND deleted=false ORDER BY version_number DESC LIMIT $2 OFFSET $3`,
		appID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query images: %w", err)
	}
	defer rows.Close()
	var items []*build.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, img)
	}
	return items, total, rows.Err()
}

// UpdateImageScan 更新镜像扫描结果。
func (r *Repository) UpdateImageScan(ctx context.Context, id int64, status build.ImageScanStatus, result map[string]any) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_images SET scan_status=$1, scan_result=$2, updated_at=now(), version_col=version_col+1 WHERE id=$3`,
		status, jsonbPtrMarshal(result), id)
	if err != nil {
		return fmt.Errorf("update image scan: %w", err)
	}
	return nil
}

// RetireImage 标记镜像为 retired（不再用于新发布）。
func (r *Repository) RetireImage(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_images SET status='retired', updated_at=now(), version_col=version_col+1 WHERE id=$1 AND deleted=false`,
		id)
	if err != nil {
		return fmt.Errorf("retire image: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrImageNotFound
	}
	return nil
}

// NextImageVersion 获取下一个 image version_number（按应用自增）。
func (r *Repository) NextImageVersion(ctx context.Context, appID int64) (int, error) {
	var maxVer int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_number), 0) FROM vo_images WHERE application_id=$1 AND deleted=false`, appID).Scan(&maxVer)
	if err != nil {
		return 0, fmt.Errorf("get max image version: %w", err)
	}
	return maxVer + 1, nil
}

// --- 制品别名 ---

const imageTagColumns = `id, uuid, application_id, name, image_id, description, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanImageTag(row pgx.Row) (*build.ImageVersionTag, error) {
	t := &build.ImageVersionTag{}
	var (
		desc      *string
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&t.ID, &t.UUID, &t.ApplicationID, &t.Name, &t.ImageID, &desc, &t.Version, &t.CreatedAt, &createdBy,
		&t.UpdatedAt, &updatedBy, &t.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		t.Description = *desc
	}
	if createdBy != nil {
		t.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		t.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		t.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		t.DeletedBy = *deletedBy
	}
	return t, nil
}

// CreateImageTag 创建制品别名。
func (r *Repository) CreateImageTag(ctx context.Context, t *build.ImageVersionTag) error {
	if t.UUID == uuid.Nil {
		t.UUID = uuid.New()
	}
	now := r.now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
		t.UpdatedAt = now
	}
	const q = `INSERT INTO vo_image_version_tags
		(uuid, application_id, name, image_id, description, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		t.UUID, t.ApplicationID, t.Name, t.ImageID, nullableStr(t.Description), t.Version, t.CreatedAt,
		nullableInt64(t.CreatedBy), t.UpdatedAt, nullableInt64(t.CreatedBy),
	).Scan(&t.ID, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return build.ErrImageTagExists
		}
		return fmt.Errorf("insert image tag: %w", err)
	}
	return nil
}

// GetImageTagByName 按应用+名称查询别名。
func (r *Repository) GetImageTagByName(ctx context.Context, appID int64, name string) (*build.ImageVersionTag, error) {
	q := `SELECT ` + imageTagColumns + ` FROM vo_image_version_tags WHERE application_id=$1 AND name=$2 AND deleted=false`
	t, err := scanImageTag(r.pool.QueryRow(ctx, q, appID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, build.ErrImageTagNotFound
		}
		return nil, err
	}
	return t, nil
}

// ListImageTags 列出应用的别名。
func (r *Repository) ListImageTags(ctx context.Context, appID int64) ([]*build.ImageVersionTag, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+imageTagColumns+` FROM vo_image_version_tags WHERE application_id=$1 AND deleted=false ORDER BY name ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("query image tags: %w", err)
	}
	defer rows.Close()
	var items []*build.ImageVersionTag
	for rows.Next() {
		t, err := scanImageTag(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// UpdateImageTag 更新别名（指向新镜像）。
func (r *Repository) UpdateImageTag(ctx context.Context, t *build.ImageVersionTag) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_image_version_tags SET image_id=$1, description=$2, updated_at=now(), updated_by=$3, version=version+1
		 WHERE id=$4 AND version=$5 AND deleted=false`,
		t.ImageID, nullableStr(t.Description), nullableInt64(t.UpdatedBy), t.ID, t.Version)
	if err != nil {
		return fmt.Errorf("update image tag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteImageTag 软删除别名。
func (r *Repository) DeleteImageTag(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_image_version_tags SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete image tag: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return build.ErrImageTagNotFound
	}
	return nil
}

// nullableDockerfileSourceStr 把空 DockerfileSource 转 nil（DB 列可空）。
func nullableDockerfileSourceStr(s build.DockerfileSource) any {
	if s == "" {
		return nil
	}
	return s
}
