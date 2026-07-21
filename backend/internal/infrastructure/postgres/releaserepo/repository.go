// Package releaserepo 是发布领域的 PostgreSQL 仓储实现。
package releaserepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/release"
)

const pgUniqueViolation = "23505"

// Repository 发布领域 PostgreSQL 仓储。
type Repository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New 创建仓储。
func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, now: time.Now}
}

// --- 发布记录 ---

const releaseColumns = `id, uuid, group_id, release_number, previous_release_id, image_id, config_version,
	release_type, replicas, strategy, max_surge, max_unavailable, batch_size, batch_interval_sec, target_percentage, target_pod_names, paused, status,
	progress_percent, failure_reason, started_at, finished_at, duration_ms, triggered_by, trigger_source,
	auto_rollback_on_failure, rollback_of_release_id, version, created_at, created_by, updated_at, updated_by,
	deleted, deleted_at, deleted_by`

func scanRelease(row pgx.Row) (*release.Release, error) {
	r := &release.Release{}
	var (
		previousReleaseID   *int64
		imageID             *int64
		configVersion       *int
		maxSurge            *string
		maxUnavailable      *string
		batchSize           *int
		batchIntervalSec    *int
		targetPercentage    *int
		targetPodNames      []byte
		failureReason       *string
		finishedAt          *time.Time
		durationMs          *int64
		triggerSource       *string
		rollbackOfReleaseID *int64
		createdBy           *int64
		updatedBy           *int64
		deletedAt           *time.Time
		deletedBy           *int64
	)
	if err := row.Scan(
		&r.ID, &r.UUID, &r.GroupID, &r.ReleaseNumber, &previousReleaseID, &imageID, &configVersion,
		&r.ReleaseType, &r.Replicas, &r.Strategy, &maxSurge, &maxUnavailable, &batchSize, &batchIntervalSec, &targetPercentage, &targetPodNames, &r.Paused, &r.Status,
		&r.ProgressPercent, &failureReason, &r.StartedAt, &finishedAt, &durationMs, &r.TriggeredBy, &triggerSource,
		&r.AutoRollbackOnFailure, &rollbackOfReleaseID, &r.Version, &r.CreatedAt, &createdBy, &r.UpdatedAt, &updatedBy,
		&r.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if previousReleaseID != nil {
		r.PreviousReleaseID = *previousReleaseID
	}
	if imageID != nil {
		r.ImageID = *imageID
	}
	if configVersion != nil {
		r.ConfigVersion = *configVersion
	}
	if maxSurge != nil {
		r.MaxSurge = *maxSurge
	}
	if maxUnavailable != nil {
		r.MaxUnavailable = *maxUnavailable
	}
	if batchSize != nil {
		r.BatchSize = *batchSize
	}
	if batchIntervalSec != nil {
		r.BatchIntervalSec = *batchIntervalSec
	}
	if targetPercentage != nil {
		r.TargetPercentage = *targetPercentage
	}
	if len(targetPodNames) > 0 && string(targetPodNames) != "null" {
		_ = json.Unmarshal(targetPodNames, &r.TargetPodNames)
	}
	if failureReason != nil {
		r.FailureReason = *failureReason
	}
	if finishedAt != nil {
		r.FinishedAt = finishedAt
	}
	if durationMs != nil {
		r.DurationMs = *durationMs
	}
	if triggerSource != nil {
		r.TriggerSource = release.TriggerSource(*triggerSource)
	}
	if rollbackOfReleaseID != nil {
		r.RollbackOfReleaseID = *rollbackOfReleaseID
	}
	if createdBy != nil {
		r.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		r.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		r.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		r.DeletedBy = *deletedBy
	}
	return r, nil
}

// CreateRelease 创建发布记录。
func (r *Repository) CreateRelease(ctx context.Context, rel *release.Release) error {
	if rel.UUID == uuid.Nil {
		rel.UUID = uuid.New()
	}
	now := r.now()
	if rel.StartedAt.IsZero() {
		rel.StartedAt = now
	}
	if rel.CreatedAt.IsZero() {
		rel.CreatedAt = now
		rel.UpdatedAt = now
	}
	if rel.Status == "" {
		rel.Status = release.StatusPending
	}
	if rel.Strategy == "" {
		rel.Strategy = release.StrategyRolling
	}
	if rel.TriggerSource == "" {
		rel.TriggerSource = release.TriggerManual
	}
	const q = `INSERT INTO vo_releases
		(uuid, group_id, release_number, previous_release_id, image_id, config_version, release_type, replicas,
		 strategy, max_surge, max_unavailable, batch_size, batch_interval_sec, target_percentage, target_pod_names, paused, status, progress_percent,
		 failure_reason, started_at, finished_at, duration_ms, triggered_by, trigger_source, auto_rollback_on_failure,
		 rollback_of_release_id, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31)
		RETURNING id, version, created_at, updated_at, started_at`
	err := r.pool.QueryRow(ctx, q,
		rel.UUID, rel.GroupID, rel.ReleaseNumber, nullableInt64(rel.PreviousReleaseID), nullableInt64(rel.ImageID),
		nullableInt(rel.ConfigVersion), rel.ReleaseType, rel.Replicas, rel.Strategy, nullableStr(rel.MaxSurge),
		nullableStr(rel.MaxUnavailable), nullableInt(rel.BatchSize), nullableInt(rel.BatchIntervalSec),
		nullableInt(rel.TargetPercentage), targetPodNamesJSON(rel.TargetPodNames), rel.Paused,
		rel.Status, rel.ProgressPercent, nullableStr(rel.FailureReason), rel.StartedAt, rel.FinishedAt,
		nullableInt64(rel.DurationMs), rel.TriggeredBy, rel.TriggerSource, rel.AutoRollbackOnFailure,
		nullableInt64(rel.RollbackOfReleaseID), rel.Version, rel.CreatedAt, nullableInt64(rel.CreatedBy),
		rel.UpdatedAt, nullableInt64(rel.CreatedBy),
	).Scan(&rel.ID, &rel.Version, &rel.CreatedAt, &rel.UpdatedAt, &rel.StartedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("insert release: %w", err)
	}
	return nil
}

// GetReleaseByID 按 ID 查询发布。
func (r *Repository) GetReleaseByID(ctx context.Context, id int64) (*release.Release, error) {
	q := `SELECT ` + releaseColumns + ` FROM vo_releases WHERE id=$1 AND deleted=false`
	rel, err := scanRelease(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrReleaseNotFound
		}
		return nil, err
	}
	return rel, nil
}

// GetReleaseByUUID 按 UUID 查询。
func (r *Repository) GetReleaseByUUID(ctx context.Context, id uuid.UUID) (*release.Release, error) {
	q := `SELECT ` + releaseColumns + ` FROM vo_releases WHERE uuid=$1 AND deleted=false`
	rel, err := scanRelease(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrReleaseNotFound
		}
		return nil, err
	}
	return rel, nil
}

// NextReleaseNumber 获取下一个 release_number（按 group 自增）。
func (r *Repository) NextReleaseNumber(ctx context.Context, groupID int64) (int, error) {
	var maxNum int
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(release_number), 0) FROM vo_releases WHERE group_id=$1`, groupID).Scan(&maxNum)
	if err != nil {
		return 0, fmt.Errorf("get max release number: %w", err)
	}
	return maxNum + 1, nil
}

// ListReleases 分页查询发布。
func (r *Repository) ListReleases(ctx context.Context, q release.ReleaseQuery) ([]*release.Release, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if q.GroupID != 0 {
		conds = append(conds, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, q.GroupID)
	}
	if q.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, q.Status)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_releases WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count releases: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_releases WHERE %s ORDER BY release_number DESC LIMIT $%d OFFSET $%d",
		releaseColumns, where, len(args)+1, len(args)+2)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query releases: %w", err)
	}
	defer rows.Close()
	var items []*release.Release
	for rows.Next() {
		rel, err := scanRelease(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, rel)
	}
	return items, total, rows.Err()
}

// UpdateReleaseStatus 更新发布状态/进度。
func (r *Repository) UpdateReleaseStatus(ctx context.Context, id int64, status release.Status, progress int, failureReason string, version int) (*release.Release, error) {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_releases SET status=$1, progress_percent=$2, failure_reason=$3, updated_at=$4, version=version+1
		 WHERE id=$5 AND version=$6 AND deleted=false`,
		status, progress, nullableStr(failureReason), now, id, version)
	if err != nil {
		return nil, fmt.Errorf("update release status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetReleaseByID(ctx, id)
}

// CompleteRelease 完成发布（终态）。
func (r *Repository) CompleteRelease(ctx context.Context, id int64, status release.Status, durationMs int64, finishedAt time.Time, version int) (*release.Release, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_releases SET status=$1, duration_ms=$2, finished_at=$3, progress_percent=100, updated_at=$4, version=version+1
		 WHERE id=$5 AND version=$6 AND deleted=false`,
		status, durationMs, finishedAt, r.now(), id, version)
	if err != nil {
		return nil, fmt.Errorf("complete release: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrConflict
	}
	return r.GetReleaseByID(ctx, id)
}

// GetLastSuccessfulRelease 取最近一次成功的发布（用于回滚目标）。
func (r *Repository) GetLastSuccessfulRelease(ctx context.Context, groupID int64) (*release.Release, error) {
	q := `SELECT ` + releaseColumns + ` FROM vo_releases
		WHERE group_id=$1 AND status='succeeded' AND deleted=false
		ORDER BY release_number DESC LIMIT 1`
	rel, err := scanRelease(r.pool.QueryRow(ctx, q, groupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrNoPreviousRelease
		}
		return nil, err
	}
	return rel, nil
}

// GetCurrentRelease 取当前发布（最新非回滚的成功发布）。
func (r *Repository) GetCurrentRelease(ctx context.Context, groupID int64) (*release.Release, error) {
	q := `SELECT ` + releaseColumns + ` FROM vo_releases
		WHERE group_id=$1 AND status='succeeded' AND deleted=false
		ORDER BY release_number DESC LIMIT 1`
	rel, err := scanRelease(r.pool.QueryRow(ctx, q, groupID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrReleaseNotFound
		}
		return nil, err
	}
	return rel, nil
}

// GetReleasesByStatus 返回指定分组下处于指定状态的发布（按 release_number 降序）。
func (r *Repository) GetReleasesByStatus(ctx context.Context, groupID int64, status release.Status) ([]*release.Release, error) {
	q := `SELECT ` + releaseColumns + ` FROM vo_releases
		WHERE group_id=$1 AND status=$2 AND deleted=false
		ORDER BY release_number DESC`
	rows, err := r.pool.Query(ctx, q, groupID, string(status))
	if err != nil {
		return nil, fmt.Errorf("query releases by status: %w", err)
	}
	defer rows.Close()
	var items []*release.Release
	for rows.Next() {
		rel, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, rel)
	}
	return items, rows.Err()
}

// --- 发布事件 ---

// AppendEvent 追加发布事件。
func (r *Repository) AppendEvent(ctx context.Context, e *release.ReleaseEvent) error {
	const q = `INSERT INTO vo_release_events (release_id, seq, event_type, message, operator_id, operator_name, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		e.ReleaseID, e.Seq, e.EventType, nullableStr(e.Message), nullableInt64(e.OperatorID),
		nullableStr(e.OperatorName), e.OccurredAt,
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("insert release event: %w", err)
	}
	return nil
}

// ListEvents 列出发布事件。
func (r *Repository) ListEvents(ctx context.Context, releaseID int64) ([]*release.ReleaseEvent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, release_id, seq, event_type, message, operator_id, operator_name, occurred_at
		 FROM vo_release_events WHERE release_id=$1 ORDER BY seq ASC`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("query release events: %w", err)
	}
	defer rows.Close()
	var items []*release.ReleaseEvent
	for rows.Next() {
		e := &release.ReleaseEvent{}
		var (
			message      *string
			operatorID   *int64
			operatorName *string
		)
		if err := rows.Scan(&e.ID, &e.ReleaseID, &e.Seq, &e.EventType, &message, &operatorID, &operatorName, &e.OccurredAt); err != nil {
			return nil, err
		}
		if message != nil {
			e.Message = *message
		}
		if operatorID != nil {
			e.OperatorID = *operatorID
		}
		if operatorName != nil {
			e.OperatorName = *operatorName
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

// --- 批次记录 ---

// CreateBatchRecord 创建批次记录。
func (r *Repository) CreateBatchRecord(ctx context.Context, b *release.ReleaseBatchRecord) error {
	const q = `INSERT INTO vo_release_batch_records (release_id, batch_index, status, started_at, finished_at, message)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`
	err := r.pool.QueryRow(ctx, q,
		b.ReleaseID, b.BatchIndex, b.Status, b.StartedAt, b.FinishedAt, nullableStr(b.Message),
	).Scan(&b.ID)
	if err != nil {
		return fmt.Errorf("insert batch record: %w", err)
	}
	return nil
}

// UpdateBatchRecord 更新批次记录。
func (r *Repository) UpdateBatchRecord(ctx context.Context, b *release.ReleaseBatchRecord) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_release_batch_records SET status=$1, started_at=$2, finished_at=$3, message=$4 WHERE id=$5`,
		b.Status, b.StartedAt, b.FinishedAt, nullableStr(b.Message), b.ID)
	if err != nil {
		return fmt.Errorf("update batch record: %w", err)
	}
	return nil
}

// ListBatchRecords 列出批次记录。
func (r *Repository) ListBatchRecords(ctx context.Context, releaseID int64) ([]*release.ReleaseBatchRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, release_id, batch_index, status, started_at, finished_at, message
		 FROM vo_release_batch_records WHERE release_id=$1 ORDER BY batch_index ASC`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("query batch records: %w", err)
	}
	defer rows.Close()
	var items []*release.ReleaseBatchRecord
	for rows.Next() {
		b := &release.ReleaseBatchRecord{}
		var (
			startedAt  *time.Time
			finishedAt *time.Time
			message    *string
		)
		if err := rows.Scan(&b.ID, &b.ReleaseID, &b.BatchIndex, &b.Status, &startedAt, &finishedAt, &message); err != nil {
			return nil, err
		}
		if startedAt != nil {
			b.StartedAt = startedAt
		}
		if finishedAt != nil {
			b.FinishedAt = finishedAt
		}
		if message != nil {
			b.Message = *message
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// --- 预设 ---

const presetColumns = `id, uuid, scope, scope_id, name, description, strategy, max_surge, max_unavailable,
	batch_size, batch_interval_sec, auto_rollback_on_failure, is_default, version, created_at, created_by,
	updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanPreset(row pgx.Row) (*release.ReleasePreset, error) {
	p := &release.ReleasePreset{}
	var (
		desc           *string
		maxSurge       *string
		maxUnavailable *string
		batchSize      *int
		batchInterval  *int
		createdBy      *int64
		updatedBy      *int64
		deletedAt      *time.Time
		deletedBy      *int64
	)
	if err := row.Scan(
		&p.ID, &p.UUID, &p.Scope, &p.ScopeID, &p.Name, &desc, &p.Strategy, &maxSurge, &maxUnavailable,
		&batchSize, &batchInterval, &p.AutoRollbackOnFailure, &p.IsDefault, &p.Version, &p.CreatedAt, &createdBy,
		&p.UpdatedAt, &updatedBy, &p.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if desc != nil {
		p.Description = *desc
	}
	if maxSurge != nil {
		p.MaxSurge = *maxSurge
	}
	if maxUnavailable != nil {
		p.MaxUnavailable = *maxUnavailable
	}
	if batchSize != nil {
		p.BatchSize = *batchSize
	}
	if batchInterval != nil {
		p.BatchIntervalSec = *batchInterval
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		p.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		p.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		p.DeletedBy = *deletedBy
	}
	return p, nil
}

// CreatePreset 创建预设。
func (r *Repository) CreatePreset(ctx context.Context, p *release.ReleasePreset) error {
	if p.UUID == uuid.Nil {
		p.UUID = uuid.New()
	}
	now := r.now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
		p.UpdatedAt = now
	}
	if p.Strategy == "" {
		p.Strategy = release.StrategyRolling
	}
	const q = `INSERT INTO vo_release_presets
		(uuid, scope, scope_id, name, description, strategy, max_surge, max_unavailable, batch_size,
		 batch_interval_sec, auto_rollback_on_failure, is_default, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		p.UUID, p.Scope, nullableInt64(p.ScopeID), p.Name, nullableStr(p.Description), p.Strategy,
		nullableStr(p.MaxSurge), nullableStr(p.MaxUnavailable), nullableInt(p.BatchSize), nullableInt(p.BatchIntervalSec),
		p.AutoRollbackOnFailure, p.IsDefault, p.Version, p.CreatedAt, nullableInt64(p.CreatedBy), p.UpdatedAt, nullableInt64(p.CreatedBy),
	).Scan(&p.ID, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert preset: %w", err)
	}
	return nil
}

// GetPresetByID 按 ID 查询预设。
func (r *Repository) GetPresetByID(ctx context.Context, id int64) (*release.ReleasePreset, error) {
	q := `SELECT ` + presetColumns + ` FROM vo_release_presets WHERE id=$1 AND deleted=false`
	p, err := scanPreset(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrPresetNotFound
		}
		return nil, err
	}
	return p, nil
}

// ListPresets 分页列出预设。
func (r *Repository) ListPresets(ctx context.Context, scope release.PresetScope, scopeID int64, offset, limit int) ([]*release.ReleasePreset, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		conds []string
		args  []any
	)
	conds = append(conds, "deleted = false")
	if scope != "" {
		conds = append(conds, fmt.Sprintf("scope = $%d", len(args)+1))
		args = append(args, scope)
	}
	if scopeID != 0 {
		conds = append(conds, fmt.Sprintf("scope_id = $%d", len(args)+1))
		args = append(args, scopeID)
	}
	where := joinConds(conds)
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_release_presets WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count presets: %w", err)
	}
	listQ := fmt.Sprintf("SELECT %s FROM vo_release_presets WHERE %s ORDER BY is_default DESC, name ASC LIMIT $%d OFFSET $%d",
		presetColumns, where, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query presets: %w", err)
	}
	defer rows.Close()
	var items []*release.ReleasePreset
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	return items, total, rows.Err()
}

// UpdatePreset 更新预设。
func (r *Repository) UpdatePreset(ctx context.Context, p *release.ReleasePreset) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_presets SET name=$1, description=$2, strategy=$3, max_surge=$4, max_unavailable=$5,
		 batch_size=$6, batch_interval_sec=$7, auto_rollback_on_failure=$8, is_default=$9, updated_at=$10, updated_by=$11, version=version+1
		 WHERE id=$12 AND version=$13 AND deleted=false`,
		p.Name, nullableStr(p.Description), p.Strategy, nullableStr(p.MaxSurge), nullableStr(p.MaxUnavailable),
		nullableInt(p.BatchSize), nullableInt(p.BatchIntervalSec), p.AutoRollbackOnFailure, p.IsDefault,
		now, nullableInt64(p.UpdatedBy), p.ID, p.Version)
	if err != nil {
		return fmt.Errorf("update preset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeletePreset 软删除预设。
func (r *Repository) DeletePreset(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_presets SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete preset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return release.ErrPresetNotFound
	}
	return nil
}

// --- 窗口 ---

const windowColumns = `id, uuid, application_id, name, timezone, crontab, duration_minutes, is_active, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanWindow(row pgx.Row) (*release.ReleaseWindow, error) {
	w := &release.ReleaseWindow{}
	var (
		createdBy *int64
		updatedBy *int64
		deletedAt *time.Time
		deletedBy *int64
	)
	if err := row.Scan(
		&w.ID, &w.UUID, &w.ApplicationID, &w.Name, &w.Timezone, &w.Crontab, &w.DurationMinutes, &w.IsActive,
		&w.Version, &w.CreatedAt, &createdBy, &w.UpdatedAt, &updatedBy, &w.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if createdBy != nil {
		w.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		w.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		w.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		w.DeletedBy = *deletedBy
	}
	return w, nil
}

// CreateWindow 创建发布窗口。
func (r *Repository) CreateWindow(ctx context.Context, w *release.ReleaseWindow) error {
	if w.UUID == uuid.Nil {
		w.UUID = uuid.New()
	}
	now := r.now()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
		w.UpdatedAt = now
	}
	if w.Timezone == "" {
		w.Timezone = "Asia/Shanghai"
	}
	if w.DurationMinutes == 0 {
		w.DurationMinutes = 60
	}
	const q = `INSERT INTO vo_release_windows
		(uuid, application_id, name, timezone, crontab, duration_minutes, is_active, version, created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q,
		w.UUID, w.ApplicationID, w.Name, w.Timezone, w.Crontab, w.DurationMinutes, w.IsActive,
		w.Version, w.CreatedAt, nullableInt64(w.CreatedBy), w.UpdatedAt, nullableInt64(w.CreatedBy),
	).Scan(&w.ID, &w.Version, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert window: %w", err)
	}
	return nil
}

// GetWindowByID 按 ID 查询窗口。
func (r *Repository) GetWindowByID(ctx context.Context, id int64) (*release.ReleaseWindow, error) {
	q := `SELECT ` + windowColumns + ` FROM vo_release_windows WHERE id=$1 AND deleted=false`
	w, err := scanWindow(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, release.ErrWindowNotFound
		}
		return nil, err
	}
	return w, nil
}

// ListWindows 列出应用的窗口。
func (r *Repository) ListWindows(ctx context.Context, appID int64) ([]*release.ReleaseWindow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+windowColumns+` FROM vo_release_windows WHERE application_id=$1 AND deleted=false ORDER BY name ASC`, appID)
	if err != nil {
		return nil, fmt.Errorf("query windows: %w", err)
	}
	defer rows.Close()
	var items []*release.ReleaseWindow
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, rows.Err()
}

// UpdateWindow 更新窗口。
func (r *Repository) UpdateWindow(ctx context.Context, w *release.ReleaseWindow) error {
	now := r.now()
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_windows SET name=$1, timezone=$2, crontab=$3, duration_minutes=$4, is_active=$5,
		 updated_at=$6, updated_by=$7, version=version+1
		 WHERE id=$8 AND version=$9 AND deleted=false`,
		w.Name, w.Timezone, w.Crontab, w.DurationMinutes, w.IsActive,
		now, nullableInt64(w.UpdatedBy), w.ID, w.Version)
	if err != nil {
		return fmt.Errorf("update window: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

// DeleteWindow 软删除窗口。
func (r *Repository) DeleteWindow(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_release_windows SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1
		 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(actorID), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete window: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return release.ErrWindowNotFound
	}
	return nil
}

// UpdateGroupCurrentRelease 回写 group 的当前发布/镜像/配置（发布成功后调用）。
func (r *Repository) UpdateGroupCurrentRelease(ctx context.Context, groupID, releaseID, imageID int64, configVersion int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_groups SET current_release_id=$1, current_image_id=$2, current_config_id=(SELECT id FROM vo_configs WHERE group_id=$3 AND config_version=$4 AND deleted=false LIMIT 1), updated_at=now(), version=version+1
		 WHERE id=$3`,
		nullableInt64(releaseID), nullableInt64(imageID), groupID, configVersion)
	if err != nil {
		return fmt.Errorf("update group current release: %w", err)
	}
	return nil
}

// UpdateGroupCandidate 设置分组候选版本（发布分批推进中调用）。
// candidateReplicas 为候选 Deployment 当前副本数。releaseID/imageID 为候选发布与镜像。
func (r *Repository) UpdateGroupCandidate(ctx context.Context, groupID, releaseID, imageID int64, candidateReplicas int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_groups SET candidate_release_id=$1, candidate_image_id=$2, candidate_replicas=$3, updated_at=now(), version=version+1
		 WHERE id=$4`,
		nullableInt64(releaseID), nullableInt64(imageID), candidateReplicas, groupID)
	if err != nil {
		return fmt.Errorf("update group candidate: %w", err)
	}
	return nil
}

// ClearGroupCandidate 清空分组候选版本（发布晋升或回滚后调用）。
func (r *Repository) ClearGroupCandidate(ctx context.Context, groupID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE vo_groups SET candidate_release_id=NULL, candidate_image_id=NULL, candidate_replicas=0, updated_at=now(), version=version+1
		 WHERE id=$1`, groupID)
	if err != nil {
		return fmt.Errorf("clear group candidate: %w", err)
	}
	return nil
}

// --- helpers ---

// targetPodNamesJSON 将 Pod 名列表序列化为 JSONB（空列表返回 NULL，省存储）。
func targetPodNamesJSON(names []string) any {
	if len(names) == 0 {
		return nil
	}
	b, _ := json.Marshal(names)
	return b
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func joinConds(conds []string) string {
	if len(conds) == 0 {
		return "true"
	}
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}
