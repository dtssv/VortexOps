package releaserepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/release"
)

// --- 编排 ---

const orchColumns = `id, uuid, workspace_id, application_id, name, strategy, status, progress_percent,
	image_id, config_version, replicas, max_surge, max_unavailable, batch_size, batch_interval_sec,
	failure_reason, started_at, finished_at, duration_ms, triggered_by, trigger_source,
	version, created_at, created_by, updated_at, updated_by`

const orchTargetColumns = `id, orchestration_id, group_id, cluster_id, image_id, config_version, replicas, seq,
	batch_size, batch_interval_sec, release_id, status, failure_reason, started_at, finished_at,
	version, created_at, updated_at`

// CreateOrchestration 创建编排及目标（单事务）。
func (r *Repository) CreateOrchestration(ctx context.Context, o *release.Orchestration, targets []release.OrchestrationTarget) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := r.now()
	o.Status = release.OrchStatusPending
	o.Version = 1
	o.CreatedAt = now
	o.UpdatedAt = now
	if o.TriggerSource == "" {
		o.TriggerSource = release.TriggerManual
	}
	if o.Strategy == "" {
		o.Strategy = release.OrchSequential
	}

	const q = `INSERT INTO vo_release_orchestrations (workspace_id, application_id, name, strategy, status,
		image_id, config_version, replicas, max_surge, max_unavailable, batch_size, batch_interval_sec,
		triggered_by, trigger_source, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id, uuid`
	if err := tx.QueryRow(ctx, q,
		o.WorkspaceID, o.ApplicationID, o.Name, o.Strategy, o.Status,
		nullableInt64(o.ImageID), nullableInt(o.ConfigVersion), nullableInt(o.Replicas), o.MaxSurge, o.MaxUnavailable,
		nullableInt(o.BatchSize), nullableInt(o.BatchIntervalSec),
		o.TriggeredBy, o.TriggerSource, o.Version, now, nullableInt64(o.CreatedBy), now, nullableInt64(o.UpdatedBy),
	).Scan(&o.ID, &o.UUID); err != nil {
		return fmt.Errorf("insert orchestration: %w", err)
	}

	for i := range targets {
		t := &targets[i]
		t.OrchestrationID = o.ID
		t.Status = release.TargetPending
		t.Version = 1
		t.CreatedAt = now
		t.UpdatedAt = now
		const tq = `INSERT INTO vo_release_orchestration_targets (orchestration_id, group_id, cluster_id, image_id,
			config_version, replicas, seq, batch_size, batch_interval_sec, status, version, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`
		if err := tx.QueryRow(ctx, tq,
			t.OrchestrationID, t.GroupID, t.ClusterID, nullableInt64(t.ImageID),
			nullableInt(t.ConfigVersion), nullableInt(t.Replicas), t.Seq,
			nullableInt(t.BatchSize), nullableInt(t.BatchIntervalSec), t.Status, t.Version, now, now,
		).Scan(&t.ID); err != nil {
			return fmt.Errorf("insert orchestration target: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// GetOrchestration 按 ID 查询编排。
func (r *Repository) GetOrchestration(ctx context.Context, id int64) (*release.Orchestration, error) {
	o := &release.Orchestration{}
	var (
		imageID, createdBy, updatedBy       *int64
		configVersion, replicas             *int
		batchSize, batchIntervalSec         *int
		failureReason                       *string
		startedAt, finishedAt               *time.Time
	)
	const q = `SELECT ` + orchColumns + ` FROM vo_release_orchestrations WHERE id=$1 AND deleted=false`
	if err := r.pool.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.UUID, &o.WorkspaceID, &o.ApplicationID, &o.Name, &o.Strategy, &o.Status, &o.ProgressPercent,
		&imageID, &configVersion, &replicas, &o.MaxSurge, &o.MaxUnavailable, &batchSize, &batchIntervalSec,
		&failureReason, &startedAt, &finishedAt, &o.DurationMs, &o.TriggeredBy, &o.TriggerSource,
		&o.Version, &o.CreatedAt, &createdBy, &o.UpdatedAt, &updatedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrOrchestrationNotFound
		}
		return nil, fmt.Errorf("get orchestration: %w", err)
	}
	assignIntPtr(configVersion, &o.ConfigVersion)
	assignIntPtr(replicas, &o.Replicas)
	assignIntBatch(batchSize, &o.BatchSize)
	assignIntBatch(batchIntervalSec, &o.BatchIntervalSec)
	assignInt64Ptr(imageID, &o.ImageID)
	assignInt64Ptr(createdBy, &o.CreatedBy)
	assignInt64Ptr(updatedBy, &o.UpdatedBy)
	if failureReason != nil {
		o.FailureReason = *failureReason
	}
	o.StartedAt = startedAt
	o.FinishedAt = finishedAt
	return o, nil
}

// ListOrchestrations 分页查询编排。
func (r *Repository) ListOrchestrations(ctx context.Context, appID int64, offset, limit int) ([]*release.Orchestration, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM vo_release_orchestrations WHERE application_id=$1 AND deleted=false`, appID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count orchestrations: %w", err)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+orchColumns+` FROM vo_release_orchestrations WHERE application_id=$1 AND deleted=false
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, appID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list orchestrations: %w", err)
	}
	defer rows.Close()
	var out []*release.Orchestration
	for rows.Next() {
		o := &release.Orchestration{}
		var (
			imageID, createdBy, updatedBy *int64
			configVersion, replicas       *int
			batchSize, batchIntervalSec   *int
			failureReason                 *string
			startedAt, finishedAt         *time.Time
		)
		if err := rows.Scan(
			&o.ID, &o.UUID, &o.WorkspaceID, &o.ApplicationID, &o.Name, &o.Strategy, &o.Status, &o.ProgressPercent,
			&imageID, &configVersion, &replicas, &o.MaxSurge, &o.MaxUnavailable, &batchSize, &batchIntervalSec,
			&failureReason, &startedAt, &finishedAt, &o.DurationMs, &o.TriggeredBy, &o.TriggerSource,
			&o.Version, &o.CreatedAt, &createdBy, &o.UpdatedAt, &updatedBy,
		); err != nil {
			return nil, 0, fmt.Errorf("scan orchestration: %w", err)
		}
		assignIntPtr(configVersion, &o.ConfigVersion)
		assignIntPtr(replicas, &o.Replicas)
		assignIntBatch(batchSize, &o.BatchSize)
		assignIntBatch(batchIntervalSec, &o.BatchIntervalSec)
		assignInt64Ptr(imageID, &o.ImageID)
		assignInt64Ptr(createdBy, &o.CreatedBy)
		assignInt64Ptr(updatedBy, &o.UpdatedBy)
		if failureReason != nil {
			o.FailureReason = *failureReason
		}
		o.StartedAt = startedAt
		o.FinishedAt = finishedAt
		out = append(out, o)
	}
	return out, total, nil
}

// UpdateOrchestrationStatus 更新编排状态与进度（乐观锁）。
func (r *Repository) UpdateOrchestrationStatus(ctx context.Context, id int64, status release.OrchestrationStatus, progress int, failureReason string, version int) (*release.Orchestration, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_orchestrations SET status=$1, progress_percent=$2, failure_reason=$3, version=version+1, updated_at=now()
		 WHERE id=$4 AND version=$5 AND deleted=false`,
		status, progress, nullableStr(failureReason), id, version)
	if err != nil {
		return nil, fmt.Errorf("update orchestration status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetOrchestration(ctx, id)
}

// CompleteOrchestration 完成编排（写 finished_at/duration）。
func (r *Repository) CompleteOrchestration(ctx context.Context, id int64, status release.OrchestrationStatus, durationMs int64, finishedAt time.Time, version int) (*release.Orchestration, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_orchestrations SET status=$1, finished_at=$2, duration_ms=$3, version=version+1, updated_at=now()
		 WHERE id=$4 AND version=$5 AND deleted=false`,
		status, finishedAt, durationMs, id, version)
	if err != nil {
		return nil, fmt.Errorf("complete orchestration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetOrchestration(ctx, id)
}

// ListTargets 列出编排目标。
func (r *Repository) ListTargets(ctx context.Context, orchestrationID int64) ([]*release.OrchestrationTarget, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orchTargetColumns+` FROM vo_release_orchestration_targets WHERE orchestration_id=$1 ORDER BY seq, id`, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	defer rows.Close()
	var out []*release.OrchestrationTarget
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// UpdateTargetStatus 更新目标状态与关联 release_id（乐观锁）。
func (r *Repository) UpdateTargetStatus(ctx context.Context, t *release.OrchestrationTarget) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_orchestration_targets SET status=$1, release_id=$2, failure_reason=$3, started_at=$4, finished_at=$5,
		 version=version+1, updated_at=now() WHERE id=$6 AND version=$7`,
		t.Status, nullableInt64(t.ReleaseID), nullableStr(t.FailureReason), t.StartedAt, t.FinishedAt, t.ID, t.Version)
	if err != nil {
		return fmt.Errorf("update target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	t.Version++
	return nil
}

func scanTarget(row pgx.Row) (*release.OrchestrationTarget, error) {
	t := &release.OrchestrationTarget{}
	var (
		imageID, releaseID          *int64
		configVersion, replicas     *int
		batchSize, batchIntervalSec *int
		failureReason               *string
		startedAt, finishedAt       *time.Time
	)
	if err := row.Scan(
		&t.ID, &t.OrchestrationID, &t.GroupID, &t.ClusterID, &imageID, &configVersion, &replicas, &t.Seq,
		&batchSize, &batchIntervalSec, &releaseID, &t.Status, &failureReason, &startedAt, &finishedAt,
		&t.Version, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	assignIntPtr(configVersion, &t.ConfigVersion)
	assignIntPtr(replicas, &t.Replicas)
	assignIntBatch(batchSize, &t.BatchSize)
	assignIntBatch(batchIntervalSec, &t.BatchIntervalSec)
	assignInt64Ptr(imageID, &t.ImageID)
	assignInt64Ptr(releaseID, &t.ReleaseID)
	if failureReason != nil {
		t.FailureReason = *failureReason
	}
	t.StartedAt = startedAt
	t.FinishedAt = finishedAt
	return t, nil
}

// --- helpers ---

func assignIntPtr(src *int, dst *int) {
	if src != nil {
		*dst = *src
	}
}

func assignIntBatch(src *int, dst *int) {
	if src != nil {
		*dst = *src
	}
}

func assignInt64Ptr(src *int64, dst *int64) {
	if src != nil {
		*dst = *src
	}
}
