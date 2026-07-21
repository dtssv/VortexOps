package applicationrepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vortexops/vortexops/internal/domain"
	"github.com/vortexops/vortexops/internal/domain/application"
)

const groupColumns = `id, uuid, application_id, name, display_name, description, app_type, environment, cluster_id, namespace,
	deployment_name, service_name, replicas, current_image_id, current_config_id, current_release_id,
	candidate_image_id, candidate_release_id, candidate_replicas,
	resources_cpu_m, resources_cpu_limit_m, resources_memory_bytes, resources_memory_limit_bytes,
	resources_gpu, gpu_type, gpu_resource_name, storage_size_bytes, storage_class,
	ephemeral_storage_request_bytes, ephemeral_storage_limit_bytes, resource_template_id,
	mesh_enabled,
	strategy, max_surge, max_unavailable, health_check, node_selector, node_affinity, tolerations,
	priority_class, workload_type, cron_schedule, job_policy, autoscaling_enabled, hpa_min_replicas,
	hpa_max_replicas, hpa_metrics, hpa_behavior, release_requires_approval, labels, metadata, version,
	created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func scanGroup(row pgx.Row) (*application.Group, error) {
	g := &application.Group{Labels: map[string]string{}, Metadata: map[string]any{}}
	var (
		displayName    *string
		description    *string
		deploymentName *string
		serviceName    *string
		cpuLimitM      *int
		memoryLimit    *int64
		gpuType        *string
		gpuResourceName *string
		storageSize     *int64
		storageClass    *string
		ephemeralReq    *int64
		ephemeralLimit  *int64
		resourceTemplateID *int64
		cronSchedule    *string
		jobPolicy       []byte
		autoscalingEnabled bool
		hpaMinReplicas  *int
		hpaMaxReplicas  *int
		hpaMetrics      []byte
		hpaBehavior     []byte
		healthCheck     []byte
		nodeSelector    []byte
		nodeAffinity    []byte
		tolerations     []byte
		priorityClass   *string
		currentImageID  *int64
		currentConfigID *int64
		currentReleaseID *int64
		candidateImageID *int64
		candidateReleaseID *int64
		candidateReplicas int
		labels          []byte
		metadata        []byte
		createdBy       *int64
		updatedBy       *int64
		deletedAt       *time.Time
		deletedBy       *int64
		maxSurge        string
		maxUnavailable  string
	)
	if err := row.Scan(
		&g.ID, &g.UUID, &g.ApplicationID, &g.Name, &displayName, &description, &g.AppType, &g.Environment, &g.ClusterID, &g.Namespace,
		&deploymentName, &serviceName, &g.Replicas, &currentImageID, &currentConfigID, &currentReleaseID,
		&candidateImageID, &candidateReleaseID, &candidateReplicas,
		&g.Resources.CPUm, &cpuLimitM, &g.Resources.MemoryBytes, &memoryLimit, &g.Resources.GPU, &gpuType, &gpuResourceName,
		&storageSize, &storageClass, &ephemeralReq, &ephemeralLimit, &resourceTemplateID,
		&g.MeshEnabled,
		&g.Workload.Strategy, &maxSurge, &maxUnavailable, &healthCheck, &nodeSelector, &nodeAffinity, &tolerations,
		&priorityClass, &g.Workload.Type, &cronSchedule, &jobPolicy, &autoscalingEnabled, &hpaMinReplicas,
		&hpaMaxReplicas, &hpaMetrics, &hpaBehavior, &g.ReleaseRequiresApproval, &labels, &metadata, &g.Version,
		&g.CreatedAt, &createdBy, &g.UpdatedAt, &updatedBy, &g.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if displayName != nil {
		g.DisplayName = *displayName
	}
	if description != nil {
		g.Description = *description
	}
	if deploymentName != nil {
		g.DeploymentName = *deploymentName
	}
	if serviceName != nil {
		g.ServiceName = *serviceName
	}
	if currentImageID != nil {
		g.CurrentImageID = *currentImageID
	}
	if currentConfigID != nil {
		g.CurrentConfigID = *currentConfigID
	}
	if currentReleaseID != nil {
		g.CurrentReleaseID = *currentReleaseID
	}
	if candidateImageID != nil {
		g.CandidateImageID = *candidateImageID
	}
	if candidateReleaseID != nil {
		g.CandidateReleaseID = *candidateReleaseID
	}
	g.CandidateReplicas = candidateReplicas
	if cpuLimitM != nil {
		g.Resources.CPULimitM = *cpuLimitM
	}
	if memoryLimit != nil {
		g.Resources.MemoryLimitBytes = *memoryLimit
	}
	if gpuType != nil {
		g.Resources.GPUType = *gpuType
	}
	if gpuResourceName != nil {
		g.Resources.GPUResourceName = *gpuResourceName
	}
	if storageSize != nil {
		g.Storage.StorageSizeBytes = *storageSize
	}
	if storageClass != nil {
		g.Storage.StorageClass = *storageClass
	}
	if ephemeralReq != nil {
		g.Storage.EphemeralStorageRequestBytes = *ephemeralReq
	}
	if ephemeralLimit != nil {
		g.Storage.EphemeralStorageLimitBytes = *ephemeralLimit
	}
	if resourceTemplateID != nil {
		g.Storage.ResourceTemplateID = *resourceTemplateID
	}
	g.Workload.MaxSurge = maxSurge
	g.Workload.MaxUnavailable = maxUnavailable
	if cronSchedule != nil {
		g.Workload.CronSchedule = *cronSchedule
	}
	if priorityClass != nil {
		g.Scheduling.PriorityClass = *priorityClass
	}
	if autoscalingEnabled {
		g.Autoscaling = &application.Autoscaling{Enabled: true}
		if hpaMinReplicas != nil {
			g.Autoscaling.MinReplicas = *hpaMinReplicas
		}
		if hpaMaxReplicas != nil {
			g.Autoscaling.MaxReplicas = *hpaMaxReplicas
		}
	}
	if healthCheck != nil {
		hc := &application.HealthCheck{}
		if err := json.Unmarshal(healthCheck, hc); err == nil {
			g.HealthCheck = hc
		}
	}
	if jobPolicy != nil {
		g.Workload.JobPolicy = map[string]any{}
		_ = json.Unmarshal(jobPolicy, &g.Workload.JobPolicy)
	}
	if hpaMetrics != nil && g.Autoscaling != nil {
		_ = json.Unmarshal(hpaMetrics, &g.Autoscaling.Metrics)
	}
	if hpaBehavior != nil && g.Autoscaling != nil {
		_ = json.Unmarshal(hpaBehavior, &g.Autoscaling.Behavior)
	}
	if nodeSelector != nil {
		g.Scheduling.NodeSelector = map[string]string{}
		_ = json.Unmarshal(nodeSelector, &g.Scheduling.NodeSelector)
	}
	if nodeAffinity != nil {
		g.Scheduling.NodeAffinity = map[string]any{}
		_ = json.Unmarshal(nodeAffinity, &g.Scheduling.NodeAffinity)
	}
	if tolerations != nil {
		_ = json.Unmarshal(tolerations, &g.Scheduling.Tolerations)
	}
	if labels != nil {
		_ = json.Unmarshal(labels, &g.Labels)
	}
	if metadata != nil {
		_ = json.Unmarshal(metadata, &g.Metadata)
	}
	if createdBy != nil {
		g.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		g.UpdatedBy = *updatedBy
	}
	if deletedAt != nil {
		g.DeletedAt = deletedAt
	}
	if deletedBy != nil {
		g.DeletedBy = *deletedBy
	}
	return g, nil
}

// CreateGroup 创建分组。
func (r *Repository) CreateGroup(ctx context.Context, g *application.Group) error {
	if g.UUID == uuid.Nil {
		g.UUID = uuid.New()
	}
	now := r.now()
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
		g.UpdatedAt = now
	}
	if g.Workload.Type == "" {
		g.Workload.Type = application.WorkloadDeployment
	}
	if g.Workload.Strategy == "" {
		g.Workload.Strategy = application.StrategyRolling
	}
	if g.Workload.MaxSurge == "" {
		g.Workload.MaxSurge = "25%"
	}
	if g.Workload.MaxUnavailable == "" {
		g.Workload.MaxUnavailable = "25%"
	}
	if g.Environment == "" {
		g.Environment = application.EnvDev
	}
	if g.Labels == nil {
		g.Labels = map[string]string{}
	}
	if g.Metadata == nil {
		g.Metadata = map[string]any{}
	}

	healthCheck := nullableJSONPtr(g.HealthCheck)
	nodeSelector, _ := json.Marshal(g.Scheduling.NodeSelector)
	nodeAffinity := nullableJSON(g.Scheduling.NodeAffinity)
	tolerations := nullableJSON(g.Scheduling.Tolerations)
	jobPolicy := nullableJSON(g.Workload.JobPolicy)
	labels, _ := json.Marshal(g.Labels)
	metadata, _ := json.Marshal(g.Metadata)

	var (
		hpaMinReplicas any
		hpaMaxReplicas any
		hpaMetrics     any
		hpaBehavior    any
	)
	if g.Autoscaling != nil && g.Autoscaling.Enabled {
		hpaMinReplicas = g.Autoscaling.MinReplicas
		hpaMaxReplicas = g.Autoscaling.MaxReplicas
		hpaMetrics = nullableJSON(g.Autoscaling.Metrics)
		hpaBehavior = nullableJSON(g.Autoscaling.Behavior)
	}

	const q = `INSERT INTO vo_groups
		(uuid, application_id, name, display_name, description, app_type, environment, cluster_id, namespace,
		 deployment_name, service_name, replicas, current_image_id, current_config_id, current_release_id,
		 candidate_image_id, candidate_release_id, candidate_replicas,
		 resources_cpu_m, resources_cpu_limit_m, resources_memory_bytes, resources_memory_limit_bytes,
		 resources_gpu, gpu_type, gpu_resource_name, storage_size_bytes, storage_class,
		 ephemeral_storage_request_bytes, ephemeral_storage_limit_bytes, resource_template_id,
		 mesh_enabled,
		 strategy, max_surge, max_unavailable, health_check, node_selector, node_affinity, tolerations,
		 priority_class, workload_type, cron_schedule, job_policy, autoscaling_enabled, hpa_min_replicas,
		 hpa_max_replicas, hpa_metrics, hpa_behavior, release_requires_approval, labels, metadata, version,
		 created_at, created_by, updated_at, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,$52,$53,$54,$55)
		RETURNING id, version, created_at, updated_at`
	appType := g.AppType
	if appType == "" {
		appType = application.AppTypeWeb
	}
	err := r.pool.QueryRow(ctx, q,
		g.UUID, g.ApplicationID, g.Name, nullableStr(g.DisplayName), nullableStr(g.Description), appType, g.Environment, g.ClusterID, g.Namespace,
		nullableStr(g.DeploymentName), nullableStr(g.ServiceName), g.Replicas, nullableInt64(g.CurrentImageID), nullableInt64(g.CurrentConfigID), nullableInt64(g.CurrentReleaseID),
		nullableInt64(g.CandidateImageID), nullableInt64(g.CandidateReleaseID), g.CandidateReplicas,
		g.Resources.CPUm, nullableInt(g.Resources.CPULimitM), g.Resources.MemoryBytes, nullableInt64(g.Resources.MemoryLimitBytes),
		g.Resources.GPU, nullableStr(g.Resources.GPUType), nullableStr(g.Resources.GPUResourceName),
		nullableInt64(g.Storage.StorageSizeBytes), nullableStr(g.Storage.StorageClass),
		nullableInt64(g.Storage.EphemeralStorageRequestBytes), nullableInt64(g.Storage.EphemeralStorageLimitBytes), nullableInt64(g.Storage.ResourceTemplateID),
		g.MeshEnabled,
		g.Workload.Strategy, g.Workload.MaxSurge, g.Workload.MaxUnavailable, healthCheck, nodeSelector, nodeAffinity, tolerations,
		nullableStr(g.Scheduling.PriorityClass), g.Workload.Type, nullableStr(g.Workload.CronSchedule), jobPolicy,
		g.Autoscaling != nil && g.Autoscaling.Enabled, hpaMinReplicas, hpaMaxReplicas, hpaMetrics, hpaBehavior,
		g.ReleaseRequiresApproval, labels, metadata, g.Version, g.CreatedAt, nullableInt64(g.CreatedBy), g.UpdatedAt, nullableInt64(g.CreatedBy),
	).Scan(&g.ID, &g.Version, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return application.ErrGroupNameExists
		}
		return fmt.Errorf("insert group: %w", err)
	}
	return nil
}

// GetGroupByID 按 ID 查询分组。
func (r *Repository) GetGroupByID(ctx context.Context, id int64) (*application.Group, error) {
	q := `SELECT ` + groupColumns + ` FROM vo_groups WHERE id=$1 AND deleted=false`
	g, err := scanGroup(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrGroupNotFound
		}
		return nil, err
	}
	return g, nil
}

// GetGroupByUUID 按 UUID 查询分组。
func (r *Repository) GetGroupByUUID(ctx context.Context, id uuid.UUID) (*application.Group, error) {
	q := `SELECT ` + groupColumns + ` FROM vo_groups WHERE uuid=$1 AND deleted=false`
	g, err := scanGroup(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrGroupNotFound
		}
		return nil, err
	}
	return g, nil
}

// GetGroupByName 按应用内名称查询分组。
func (r *Repository) GetGroupByName(ctx context.Context, applicationID int64, name string) (*application.Group, error) {
	q := `SELECT ` + groupColumns + ` FROM vo_groups WHERE application_id=$1 AND name=$2 AND deleted=false`
	g, err := scanGroup(r.pool.QueryRow(ctx, q, applicationID, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, application.ErrGroupNotFound
		}
		return nil, err
	}
	return g, nil
}

// UpdateGroup 更新分组（乐观锁）。
func (r *Repository) UpdateGroup(ctx context.Context, in application.UpdateGroupInput) (*application.Group, error) {
	now := r.now()
	var (
		sets   []string
		args   []any
		argIdx = 1
	)
	addSet := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, argIdx))
		args = append(args, val)
		argIdx++
	}
	if in.DisplayName != nil {
		addSet("display_name", nullableStr(*in.DisplayName))
	}
	if in.Description != nil {
		addSet("description", nullableStr(*in.Description))
	}
	if in.Replicas != nil {
		addSet("replicas", *in.Replicas)
	}
	if in.Resources != nil {
		addSet("resources_cpu_m", in.Resources.CPUm)
		addSet("resources_cpu_limit_m", nullableInt(in.Resources.CPULimitM))
		addSet("resources_memory_bytes", in.Resources.MemoryBytes)
		addSet("resources_memory_limit_bytes", nullableInt64(in.Resources.MemoryLimitBytes))
		addSet("resources_gpu", in.Resources.GPU)
		addSet("gpu_type", nullableStr(in.Resources.GPUType))
		addSet("gpu_resource_name", nullableStr(in.Resources.GPUResourceName))
	}
	if in.Storage != nil {
		addSet("storage_size_bytes", nullableInt64(in.Storage.StorageSizeBytes))
		addSet("storage_class", nullableStr(in.Storage.StorageClass))
		addSet("ephemeral_storage_request_bytes", nullableInt64(in.Storage.EphemeralStorageRequestBytes))
		addSet("ephemeral_storage_limit_bytes", nullableInt64(in.Storage.EphemeralStorageLimitBytes))
		addSet("resource_template_id", nullableInt64(in.Storage.ResourceTemplateID))
	}
	if in.MeshEnabled != nil {
		addSet("mesh_enabled", *in.MeshEnabled)
	}
	if in.Scheduling != nil {
		nodeSelector, _ := json.Marshal(in.Scheduling.NodeSelector)
		addSet("node_selector", nodeSelector)
		addSet("node_affinity", nullableJSON(in.Scheduling.NodeAffinity))
		addSet("tolerations", nullableJSON(in.Scheduling.Tolerations))
		addSet("priority_class", nullableStr(in.Scheduling.PriorityClass))
	}
	if in.Workload != nil {
		addSet("workload_type", in.Workload.Type)
		addSet("cron_schedule", nullableStr(in.Workload.CronSchedule))
		addSet("job_policy", nullableJSON(in.Workload.JobPolicy))
		addSet("strategy", in.Workload.Strategy)
		addSet("max_surge", in.Workload.MaxSurge)
		addSet("max_unavailable", in.Workload.MaxUnavailable)
	}
	if in.HealthCheck != nil {
		addSet("health_check", nullableJSONPtr(in.HealthCheck))
	}
	if in.Autoscaling != nil {
		enabled := in.Autoscaling.Enabled
		addSet("autoscaling_enabled", enabled)
		if enabled {
			addSet("hpa_min_replicas", in.Autoscaling.MinReplicas)
			addSet("hpa_max_replicas", in.Autoscaling.MaxReplicas)
			addSet("hpa_metrics", nullableJSON(in.Autoscaling.Metrics))
			addSet("hpa_behavior", nullableJSON(in.Autoscaling.Behavior))
		}
	}
	if in.ReleaseRequiresApproval != nil {
		addSet("release_requires_approval", *in.ReleaseRequiresApproval)
	}
	if in.Labels != nil {
		b, _ := json.Marshal(in.Labels)
		addSet("labels", b)
	}
	if in.Metadata != nil {
		b, _ := json.Marshal(in.Metadata)
		addSet("metadata", b)
	}
	if len(sets) == 0 {
		return r.GetGroupByID(ctx, in.ID)
	}
	addSet("updated_at", now)
	addSet("updated_by", nullableInt64(in.UpdatedBy))
	addSet("version", in.Version+1)

	args = append(args, in.ID, in.Version)
	q := fmt.Sprintf(`UPDATE vo_groups SET %s WHERE id=$%d AND version=$%d AND deleted=false`,
		strings.Join(sets, ", "), argIdx, argIdx+1)
	tag, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, gerr := r.GetGroupByID(ctx, in.ID)
		if gerr != nil {
			return nil, application.ErrGroupNotFound
		}
		if existing.Version != in.Version {
			return nil, domain.ErrConflict
		}
		return nil, application.ErrGroupNotFound
	}
	return r.GetGroupByID(ctx, in.ID)
}

// ListGroups 分页查询分组。
func (r *Repository) ListGroups(ctx context.Context, q application.GroupQuery) ([]*application.Group, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 20
	}
	var (
		conds  []string
		args   []any
		argIdx = 1
	)
	conds = append(conds, "deleted = false")
	if q.ApplicationID != 0 {
		conds = append(conds, fmt.Sprintf("application_id = $%d", argIdx))
		args = append(args, q.ApplicationID)
		argIdx++
	}
	if q.Environment != "" {
		conds = append(conds, fmt.Sprintf("environment = $%d", argIdx))
		args = append(args, q.Environment)
		argIdx++
	}
	if q.ClusterID != 0 {
		conds = append(conds, fmt.Sprintf("cluster_id = $%d", argIdx))
		args = append(args, q.ClusterID)
		argIdx++
	}
	if q.AppType != "" {
		conds = append(conds, fmt.Sprintf("app_type = $%d", argIdx))
		args = append(args, q.AppType)
		argIdx++
	}
	if q.Search != "" {
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR display_name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+q.Search+"%")
		argIdx++
	}
	where := strings.Join(conds, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_groups WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count groups: %w", err)
	}

	listQ := fmt.Sprintf("SELECT %s FROM vo_groups WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		groupColumns, where, argIdx, argIdx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query groups: %w", err)
	}
	defer rows.Close()
	var items []*application.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, g)
	}
	return items, total, rows.Err()
}

// DeleteGroup 软删除分组。
func (r *Repository) DeleteGroup(ctx context.Context, id, deletedBy int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE vo_groups SET deleted=true, deleted_at=$1, deleted_by=$2, updated_at=$3, version=version+1 WHERE id=$4 AND deleted=false`,
		r.now(), nullableInt64(deletedBy), r.now(), id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return application.ErrGroupNotFound
	}
	return nil
}

// nullableJSON 将可能为空的 map/slice 序列化为 []byte，nil 返回 nil（写入 NULL）。
func nullableJSON(v any) any {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			return nil
		}
	case map[string]string:
		if len(t) == 0 {
			return nil
		}
	case []any:
		if len(t) == 0 {
			return nil
		}
	case []map[string]any:
		if len(t) == 0 {
			return nil
		}
	case []string:
		if len(t) == 0 {
			return nil
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// nullableJSONPtr 将可能为 nil 的指针结构体序列化。
func nullableJSONPtr(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func nullableInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
