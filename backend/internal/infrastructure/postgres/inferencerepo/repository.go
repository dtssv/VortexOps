// Package inferencerepo 是 inference 领域的 PostgreSQL 仓储实现。
package inferencerepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vortexops/vortexops/internal/domain/inference"
)

// Repository 实现 inference.Repository。
type Repository struct {
	pool Querier
}

// Querier pgx 兼容接口（支持 Tx）。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// New 创建仓储。
func New(pool Querier) *Repository {
	return &Repository{pool: pool}
}

// --- registry ---

func (r *Repository) CreateRegistry(ctx context.Context, reg *inference.ModelRegistry) error {
	if reg.UUID == uuid.Nil {
		reg.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_model_registries
(uuid, workspace_id, name, provider, endpoint, credential_id, cache_pvc_name, cache_path, cache_size_bytes, status,
 version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,$11)
RETURNING id, version, created_at, updated_at`,
		reg.UUID, reg.WorkspaceID, reg.Name, string(reg.Provider), nullStr(reg.Endpoint), nullInt64(reg.CredentialID),
		nullStr(reg.CachePVCName), nullStr(reg.CachePath), nullInt64(reg.CacheSizeBytes), nullStrDefault(reg.Status, "active"),
		reg.Audit.CreatedBy,
	)
	return row.Scan(&reg.ID, &reg.Audit.Version, &reg.Audit.CreatedAt, &reg.Audit.UpdatedAt)
}

func (r *Repository) GetRegistryByID(ctx context.Context, id int64) (*inference.ModelRegistry, error) {
	reg := &inference.ModelRegistry{}
	var provider string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, workspace_id, name, provider, endpoint, credential_id, cache_pvc_name, cache_path, cache_size_bytes, status,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_registries WHERE id=$1 AND deleted=false`, id).
		Scan(&reg.ID, &reg.UUID, &reg.WorkspaceID, &reg.Name, &provider, &reg.Endpoint, &reg.CredentialID,
			&reg.CachePVCName, &reg.CachePath, &reg.CacheSizeBytes, &reg.Status,
			&reg.Audit.Version, &reg.Audit.CreatedAt, &reg.Audit.CreatedBy, &reg.Audit.UpdatedAt, &reg.Audit.UpdatedBy,
			&reg.Audit.Deleted, &reg.Audit.DeletedAt, &reg.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrRegistryNotFound
	}
	if err != nil {
		return nil, err
	}
	reg.Provider = inference.RegistryProvider(provider)
	return reg, nil
}

func (r *Repository) GetRegistryByName(ctx context.Context, workspaceID int64, name string) (*inference.ModelRegistry, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM vo_model_registries WHERE workspace_id=$1 AND name=$2 AND deleted=false`, workspaceID, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetRegistryByID(ctx, id)
}

func (r *Repository) ListRegistries(ctx context.Context, workspaceID int64, offset, limit int) ([]*inference.ModelRegistry, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM vo_model_registries WHERE workspace_id=$1 AND deleted=false`, workspaceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, workspace_id, name, provider, endpoint, credential_id, cache_pvc_name, cache_path, cache_size_bytes, status,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_registries WHERE workspace_id=$1 AND deleted=false ORDER BY name ASC LIMIT $2 OFFSET $3`, workspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	return scanRegistries(rows, total)
}

func (r *Repository) DeleteRegistry(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_model_registries SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrRegistryNotFound
	}
	return nil
}

// --- model ---

func (r *Repository) CreateModel(ctx context.Context, m *inference.Model) error {
	if m.UUID == uuid.Nil {
		m.UUID = uuid.New()
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_models
(uuid, workspace_id, registry_id, name, display_name, description, base_architecture, parameter_count, license, tags,
 version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,$11)
RETURNING id, version, created_at, updated_at`,
		m.UUID, m.WorkspaceID, m.RegistryID, m.Name, nullStr(m.DisplayName), nullStr(m.Description),
		nullStr(m.BaseArchitecture), nullStr(m.ParameterCount), nullStr(m.License), m.Tags, m.Audit.CreatedBy,
	)
	return row.Scan(&m.ID, &m.Audit.Version, &m.Audit.CreatedAt, &m.Audit.UpdatedAt)
}

func (r *Repository) GetModelByID(ctx context.Context, id int64) (*inference.Model, error) {
	m := &inference.Model{}
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, workspace_id, registry_id, name, display_name, description, base_architecture, parameter_count, license, tags,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_models WHERE id=$1 AND deleted=false`, id).
		Scan(&m.ID, &m.UUID, &m.WorkspaceID, &m.RegistryID, &m.Name, &m.DisplayName, &m.Description,
			&m.BaseArchitecture, &m.ParameterCount, &m.License, &m.Tags,
			&m.Audit.Version, &m.Audit.CreatedAt, &m.Audit.CreatedBy, &m.Audit.UpdatedAt, &m.Audit.UpdatedBy,
			&m.Audit.Deleted, &m.Audit.DeletedAt, &m.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrModelNotFound
	}
	return m, err
}

func (r *Repository) GetModelByName(ctx context.Context, workspaceID int64, name string) (*inference.Model, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM vo_models WHERE workspace_id=$1 AND name=$2 AND deleted=false`, workspaceID, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetModelByID(ctx, id)
}

func (r *Repository) ListModels(ctx context.Context, workspaceID, registryID int64, offset, limit int) ([]*inference.Model, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	where := "deleted=false AND workspace_id=$1"
	args := []any{workspaceID}
	idx := 2
	if registryID > 0 {
		where += fmt.Sprintf(" AND registry_id=$%d", idx)
		args = append(args, registryID)
		idx++
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_models WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `
SELECT id, uuid, workspace_id, registry_id, name, display_name, description, base_architecture, parameter_count, license, tags,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_models WHERE ` + where + fmt.Sprintf(" ORDER BY name ASC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*inference.Model, 0)
	for rows.Next() {
		m := &inference.Model{}
		if err := rows.Scan(&m.ID, &m.UUID, &m.WorkspaceID, &m.RegistryID, &m.Name, &m.DisplayName, &m.Description,
			&m.BaseArchitecture, &m.ParameterCount, &m.License, &m.Tags,
			&m.Audit.Version, &m.Audit.CreatedAt, &m.Audit.CreatedBy, &m.Audit.UpdatedAt, &m.Audit.UpdatedBy,
			&m.Audit.Deleted, &m.Audit.DeletedAt, &m.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (r *Repository) DeleteModel(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_models SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrModelNotFound
	}
	return nil
}

// --- model version ---

func (r *Repository) CreateModelVersion(ctx context.Context, v *inference.ModelVersion) error {
	if v.UUID == uuid.Nil {
		v.UUID = uuid.New()
	}
	if v.FrameworkConfig == nil {
		v.FrameworkConfig = map[string]any{}
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_model_versions
(uuid, model_id, version, precision, quantization, weights_path, weights_size_bytes, weights_checksum,
 framework, framework_config, min_gpu_memory_bytes, recommended_gpu_count, download_status, download_progress, is_default,
 version_col, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,1,$16,$16)
RETURNING id, version_col, created_at, updated_at`,
		v.UUID, v.ModelID, v.Version, string(v.Precision), nullQuant(v.Quantization), nullStr(v.WeightsPath),
		nullInt64(v.WeightsSizeBytes), nullStr(v.WeightsChecksum), string(v.Framework), v.FrameworkConfig,
		nullInt64(v.MinGPUMemoryBytes), nullInt(v.RecommendedGPUCount), string(v.DownloadStatus), v.DownloadProgress, v.IsDefault,
		v.Audit.CreatedBy,
	)
	return row.Scan(&v.ID, &v.Audit.Version, &v.Audit.CreatedAt, &v.Audit.UpdatedAt)
}

func (r *Repository) GetModelVersionByID(ctx context.Context, id int64) (*inference.ModelVersion, error) {
	v := &inference.ModelVersion{}
	var precision, quantization, framework, dlStatus string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, model_id, version, precision, quantization, weights_path, weights_size_bytes, weights_checksum,
       framework, framework_config, min_gpu_memory_bytes, recommended_gpu_count, download_status, download_progress, is_default,
       version_col, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_versions WHERE id=$1 AND deleted=false`, id).
		Scan(&v.ID, &v.UUID, &v.ModelID, &v.Version, &precision, &quantization, &v.WeightsPath, &v.WeightsSizeBytes, &v.WeightsChecksum,
			&framework, &v.FrameworkConfig, &v.MinGPUMemoryBytes, &v.RecommendedGPUCount, &dlStatus, &v.DownloadProgress, &v.IsDefault,
			&v.Audit.Version, &v.Audit.CreatedAt, &v.Audit.CreatedBy, &v.Audit.UpdatedAt, &v.Audit.UpdatedBy,
			&v.Audit.Deleted, &v.Audit.DeletedAt, &v.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrModelVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	v.Precision = inference.Precision(precision)
	if quantization != "" {
		v.Quantization = inference.Quantization(quantization)
	}
	v.Framework = inference.Framework(framework)
	v.DownloadStatus = inference.DownloadStatus(dlStatus)
	return v, nil
}

func (r *Repository) ListModelVersions(ctx context.Context, modelID int64) ([]*inference.ModelVersion, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, model_id, version, precision, quantization, weights_path, weights_size_bytes, weights_checksum,
       framework, framework_config, min_gpu_memory_bytes, recommended_gpu_count, download_status, download_progress, is_default,
       version_col, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_versions WHERE model_id=$1 AND deleted=false ORDER BY id DESC`, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*inference.ModelVersion, 0)
	for rows.Next() {
		v := &inference.ModelVersion{}
		var precision, quantization, framework, dlStatus string
		if err := rows.Scan(&v.ID, &v.UUID, &v.ModelID, &v.Version, &precision, &quantization, &v.WeightsPath, &v.WeightsSizeBytes, &v.WeightsChecksum,
			&framework, &v.FrameworkConfig, &v.MinGPUMemoryBytes, &v.RecommendedGPUCount, &dlStatus, &v.DownloadProgress, &v.IsDefault,
			&v.Audit.Version, &v.Audit.CreatedAt, &v.Audit.CreatedBy, &v.Audit.UpdatedAt, &v.Audit.UpdatedBy,
			&v.Audit.Deleted, &v.Audit.DeletedAt, &v.Audit.DeletedBy); err != nil {
			return nil, err
		}
		v.Precision = inference.Precision(precision)
		if quantization != "" {
			v.Quantization = inference.Quantization(quantization)
		}
		v.Framework = inference.Framework(framework)
		v.DownloadStatus = inference.DownloadStatus(dlStatus)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateModelVersionDownload(ctx context.Context, id int64, status inference.DownloadStatus, progress int) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_model_versions SET download_status=$2, download_progress=$3, version_col=version_col+1, updated_at=now()
WHERE id=$1 AND deleted=false`, id, string(status), progress)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrModelVersionNotFound
	}
	return nil
}

func (r *Repository) DeleteModelVersion(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_model_versions SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrModelVersionNotFound
	}
	return nil
}

// --- adapter ---

func (r *Repository) CreateAdapter(ctx context.Context, a *inference.ModelAdapter) error {
	if a.UUID == uuid.Nil {
		a.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_model_adapters
(uuid, base_model_version_id, name, adapter_type, weights_path, rank, scale, version_col, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$8)
RETURNING id, version_col, created_at, updated_at`,
		a.UUID, a.BaseModelVersionID, a.Name, string(a.AdapterType), nullStr(a.WeightsPath), nullInt(a.Rank), nullFloat(a.Scale), a.Audit.CreatedBy,
	)
	return row.Scan(&a.ID, &a.Audit.Version, &a.Audit.CreatedAt, &a.Audit.UpdatedAt)
}

func (r *Repository) GetAdapterByID(ctx context.Context, id int64) (*inference.ModelAdapter, error) {
	a := &inference.ModelAdapter{}
	var adapterType string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, base_model_version_id, name, adapter_type, weights_path, rank, scale,
       version_col, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_adapters WHERE id=$1 AND deleted=false`, id).
		Scan(&a.ID, &a.UUID, &a.BaseModelVersionID, &a.Name, &adapterType, &a.WeightsPath, &a.Rank, &a.Scale,
			&a.Audit.Version, &a.Audit.CreatedAt, &a.Audit.CreatedBy, &a.Audit.UpdatedAt, &a.Audit.UpdatedBy,
			&a.Audit.Deleted, &a.Audit.DeletedAt, &a.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrAdapterNotFound
	}
	if err != nil {
		return nil, err
	}
	a.AdapterType = inference.AdapterType(adapterType)
	return a, nil
}

func (r *Repository) ListAdapters(ctx context.Context, baseModelVersionID int64) ([]*inference.ModelAdapter, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, base_model_version_id, name, adapter_type, weights_path, rank, scale,
       version_col, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_model_adapters WHERE base_model_version_id=$1 AND deleted=false ORDER BY name ASC`, baseModelVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*inference.ModelAdapter, 0)
	for rows.Next() {
		a := &inference.ModelAdapter{}
		var adapterType string
		if err := rows.Scan(&a.ID, &a.UUID, &a.BaseModelVersionID, &a.Name, &adapterType, &a.WeightsPath, &a.Rank, &a.Scale,
			&a.Audit.Version, &a.Audit.CreatedAt, &a.Audit.CreatedBy, &a.Audit.UpdatedAt, &a.Audit.UpdatedBy,
			&a.Audit.Deleted, &a.Audit.DeletedAt, &a.Audit.DeletedBy); err != nil {
			return nil, err
		}
		a.AdapterType = inference.AdapterType(adapterType)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteAdapter(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_model_adapters SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrAdapterNotFound
	}
	return nil
}

// --- inference service ---

func (r *Repository) CreateService(ctx context.Context, s *inference.InferenceService) error {
	if s.UUID == uuid.Nil {
		s.UUID = uuid.New()
	}
	if s.AdapterIDs == nil {
		s.AdapterIDs = []int64{}
	}
	if s.FrameworkConfig == nil {
		s.FrameworkConfig = map[string]any{}
	}
	if s.Resources == nil {
		s.Resources = map[string]any{}
	}
	if s.Labels == nil {
		s.Labels = map[string]any{}
	}
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_services
(uuid, workspace_id, application_id, group_id, name, display_name, description, cluster_id, namespace, workload_name, service_name,
 base_model_version_id, adapter_ids, framework, framework_config, replicas, resources, gpu_count, gpu_type,
 tensor_parallel_size, pipeline_parallel_size, storage_size_bytes, current_status, readiness,
 autoscaling_enabled, hpa_min_replicas, hpa_max_replicas, hpa_metrics, access_mode, external_endpoint, labels, metadata,
 version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,1,$34,$34)
RETURNING id, version, created_at, updated_at`,
		s.UUID, s.WorkspaceID, nullInt64(s.ApplicationID), nullInt64(s.GroupID), s.Name, nullStr(s.DisplayName), nullStr(s.Description), s.ClusterID, s.Namespace,
		nullStr(s.WorkloadName), nullStr(s.ServiceName), s.BaseModelVersionID, s.AdapterIDs, string(s.Framework), s.FrameworkConfig,
		s.Replicas, s.Resources, s.GPUCount, nullStr(s.GPUType), s.TensorParallelSize, s.PipelineParallelSize, nullInt64(s.StorageSizeBytes),
		string(s.CurrentStatus), string(s.Readiness), s.AutoscalingEnabled, nullInt(s.HPAMinReplicas), nullInt(s.HPAMaxReplicas),
		s.HPAMetrics, string(s.AccessMode), nullStr(s.ExternalEndpoint), s.Labels, s.Metadata, s.Audit.CreatedBy,
	)
	return row.Scan(&s.ID, &s.Audit.Version, &s.Audit.CreatedAt, &s.Audit.UpdatedAt)
}

func scanService(row pgx.Row) (*inference.InferenceService, error) {
	s := &inference.InferenceService{}
	var framework, status, readiness, access string
	err := row.Scan(
		&s.ID, &s.UUID, &s.WorkspaceID, &s.ApplicationID, &s.GroupID, &s.Name, &s.DisplayName, &s.Description, &s.ClusterID, &s.Namespace,
		&s.WorkloadName, &s.ServiceName, &s.BaseModelVersionID, &s.AdapterIDs, &framework, &s.FrameworkConfig,
		&s.Replicas, &s.Resources, &s.GPUCount, &s.GPUType, &s.TensorParallelSize, &s.PipelineParallelSize, &s.StorageSizeBytes,
		&s.CurrentReleaseID, &status, &readiness, &s.AutoscalingEnabled, &s.HPAMinReplicas, &s.HPAMaxReplicas, &s.HPAMetrics,
		&access, &s.ExternalEndpoint, &s.Labels, &s.Metadata,
		&s.Audit.Version, &s.Audit.CreatedAt, &s.Audit.CreatedBy, &s.Audit.UpdatedAt, &s.Audit.UpdatedBy,
		&s.Audit.Deleted, &s.Audit.DeletedAt, &s.Audit.DeletedBy,
	)
	if err != nil {
		return nil, err
	}
	s.Framework = inference.Framework(framework)
	s.CurrentStatus = inference.ServiceStatus(status)
	s.Readiness = inference.Readiness(readiness)
	s.AccessMode = inference.AccessMode(access)
	return s, nil
}

const serviceColumns = `
id, uuid, workspace_id, application_id, group_id, name, display_name, description, cluster_id, namespace, workload_name, service_name,
base_model_version_id, adapter_ids, framework, framework_config, replicas, resources, gpu_count, gpu_type,
tensor_parallel_size, pipeline_parallel_size, storage_size_bytes, current_release_id, current_status, readiness,
autoscaling_enabled, hpa_min_replicas, hpa_max_replicas, hpa_metrics, access_mode, external_endpoint, labels, metadata,
version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by`

func (r *Repository) GetServiceByID(ctx context.Context, id int64) (*inference.InferenceService, error) {
	row := r.pool.QueryRow(ctx, `SELECT`+serviceColumns+` FROM vo_inference_services WHERE id=$1 AND deleted=false`, id)
	s, err := scanService(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrServiceNotFound
	}
	return s, err
}

func (r *Repository) GetServiceByName(ctx context.Context, workspaceID int64, name string) (*inference.InferenceService, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM vo_inference_services WHERE workspace_id=$1 AND name=$2 AND deleted=false`, workspaceID, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetServiceByID(ctx, id)
}

func (r *Repository) ListServices(ctx context.Context, q inference.ServiceQuery) ([]*inference.InferenceService, int64, error) {
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
		add(fmt.Sprintf("workspace_id=$%d", idx), q.WorkspaceID)
	}
	if q.ClusterID > 0 {
		add(fmt.Sprintf("cluster_id=$%d", idx), q.ClusterID)
	}
	if q.Status != "" {
		add(fmt.Sprintf("current_status=$%d", idx), string(q.Status))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_inference_services WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `SELECT` + serviceColumns + ` FROM vo_inference_services WHERE ` + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*inference.InferenceService, 0)
	for rows.Next() {
		s, err := scanService(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdateService(ctx context.Context, s *inference.InferenceService) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_services SET
  display_name=$2, description=$3, base_model_version_id=$4, adapter_ids=$5, framework=$6, framework_config=$7,
  replicas=$8, resources=$9, gpu_count=$10, gpu_type=$11, tensor_parallel_size=$12, pipeline_parallel_size=$13,
  storage_size_bytes=$14, autoscaling_enabled=$15, hpa_min_replicas=$16, hpa_max_replicas=$17, hpa_metrics=$18,
  access_mode=$19, external_endpoint=$20, labels=$21, metadata=$22,
  version=version+1, updated_at=now(), updated_by=$23
WHERE id=$1 AND deleted=false AND version=$24`,
		s.ID, nullStr(s.DisplayName), nullStr(s.Description), s.BaseModelVersionID, s.AdapterIDs, string(s.Framework), s.FrameworkConfig,
		s.Replicas, s.Resources, s.GPUCount, nullStr(s.GPUType), s.TensorParallelSize, s.PipelineParallelSize, nullInt64(s.StorageSizeBytes),
		s.AutoscalingEnabled, nullInt(s.HPAMinReplicas), nullInt(s.HPAMaxReplicas), s.HPAMetrics,
		string(s.AccessMode), nullStr(s.ExternalEndpoint), s.Labels, s.Metadata, s.Audit.UpdatedBy, s.Audit.Version,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrServiceNotFound
	}
	refreshed, err := r.GetServiceByID(ctx, s.ID)
	if err != nil {
		return err
	}
	*s = *refreshed
	return nil
}

func (r *Repository) UpdateServiceStatus(ctx context.Context, id int64, status inference.ServiceStatus, readiness inference.Readiness, releaseID int64, version int) (*inference.InferenceService, error) {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_services SET current_status=$2, readiness=$3, current_release_id=$4,
  version=version+1, updated_at=now()
WHERE id=$1 AND deleted=false AND version=$5`, id, string(status), string(readiness), nullInt64(releaseID), version)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, inference.ErrServiceNotFound
	}
	return r.GetServiceByID(ctx, id)
}

func (r *Repository) DeleteService(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_services SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2,
  current_status='stopped'
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrServiceNotFound
	}
	return nil
}

// --- release ---

func (r *Repository) CreateRelease(ctx context.Context, rel *inference.InferenceRelease) error {
	if rel.UUID == uuid.Nil {
		rel.UUID = uuid.New()
	}
	if rel.TargetAdapterIDs == nil {
		rel.TargetAdapterIDs = []int64{}
	}
	if rel.StartedAt.IsZero() {
		rel.StartedAt = time.Now()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_releases
(uuid, inference_service_id, group_id, release_number, previous_release_id, target_model_version_id, target_adapter_ids,
 strategy, replicas, status, progress_percent, failure_reason, started_by, started_at, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,1,$16,$16)
RETURNING id, version, created_at, updated_at`,
		rel.UUID, rel.InferenceServiceID, nullInt64(rel.GroupID), rel.ReleaseNumber, nullInt64(rel.PreviousReleaseID), rel.TargetModelVersionID, rel.TargetAdapterIDs,
		string(rel.Strategy), rel.Replicas, string(rel.Status), rel.ProgressPercent, nullStr(rel.FailureReason),
		rel.StartedBy, rel.StartedAt, rel.Audit.CreatedBy,
	)
	return row.Scan(&rel.ID, &rel.Audit.Version, &rel.Audit.CreatedAt, &rel.Audit.UpdatedAt)
}

func (r *Repository) GetReleaseByID(ctx context.Context, id int64) (*inference.InferenceRelease, error) {
	rel := &inference.InferenceRelease{}
	var strategy, status string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, inference_service_id, group_id, release_number, previous_release_id, target_model_version_id, target_adapter_ids,
       strategy, replicas, status, progress_percent, failure_reason, started_by, started_at, finished_at, duration_ms,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_releases WHERE id=$1 AND deleted=false`, id).
		Scan(&rel.ID, &rel.UUID, &rel.InferenceServiceID, &rel.GroupID, &rel.ReleaseNumber, &rel.PreviousReleaseID, &rel.TargetModelVersionID, &rel.TargetAdapterIDs,
			&strategy, &rel.Replicas, &status, &rel.ProgressPercent, &rel.FailureReason, &rel.StartedBy, &rel.StartedAt, &rel.FinishedAt, &rel.DurationMs,
			&rel.Audit.Version, &rel.Audit.CreatedAt, &rel.Audit.CreatedBy, &rel.Audit.UpdatedAt, &rel.Audit.UpdatedBy,
			&rel.Audit.Deleted, &rel.Audit.DeletedAt, &rel.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrReleaseNotFound
	}
	if err != nil {
		return nil, err
	}
	rel.Strategy = inference.ReleaseStrategy(strategy)
	rel.Status = inference.ReleaseStatus(status)
	return rel, nil
}

func (r *Repository) ListReleases(ctx context.Context, q inference.ReleaseQuery) ([]*inference.InferenceRelease, int64, error) {
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
	if q.ServiceID > 0 {
		add(fmt.Sprintf("inference_service_id=$%d", idx), q.ServiceID)
	}
	if q.Status != "" {
		add(fmt.Sprintf("status=$%d", idx), string(q.Status))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_inference_releases WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `
SELECT id, uuid, inference_service_id, group_id, release_number, previous_release_id, target_model_version_id, target_adapter_ids,
       strategy, replicas, status, progress_percent, failure_reason, started_by, started_at, finished_at, duration_ms,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_releases WHERE ` + where + fmt.Sprintf(" ORDER BY release_number DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*inference.InferenceRelease, 0)
	for rows.Next() {
		rel := &inference.InferenceRelease{}
		var strategy, status string
		if err := rows.Scan(&rel.ID, &rel.UUID, &rel.InferenceServiceID, &rel.GroupID, &rel.ReleaseNumber, &rel.PreviousReleaseID, &rel.TargetModelVersionID, &rel.TargetAdapterIDs,
			&strategy, &rel.Replicas, &status, &rel.ProgressPercent, &rel.FailureReason, &rel.StartedBy, &rel.StartedAt, &rel.FinishedAt, &rel.DurationMs,
			&rel.Audit.Version, &rel.Audit.CreatedAt, &rel.Audit.CreatedBy, &rel.Audit.UpdatedAt, &rel.Audit.UpdatedBy,
			&rel.Audit.Deleted, &rel.Audit.DeletedAt, &rel.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		rel.Strategy = inference.ReleaseStrategy(strategy)
		rel.Status = inference.ReleaseStatus(status)
		out = append(out, rel)
	}
	return out, total, rows.Err()
}

func (r *Repository) UpdateRelease(ctx context.Context, rel *inference.InferenceRelease) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_releases SET
  status=$2, progress_percent=$3, failure_reason=$4, finished_at=$5, duration_ms=$6,
  version=version+1, updated_at=now(), updated_by=$7
WHERE id=$1 AND deleted=false AND version=$8`,
		rel.ID, string(rel.Status), rel.ProgressPercent, nullStr(rel.FailureReason), rel.FinishedAt, nullInt64(rel.DurationMs),
		rel.Audit.UpdatedBy, rel.Audit.Version,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrReleaseNotFound
	}
	refreshed, err := r.GetReleaseByID(ctx, rel.ID)
	if err != nil {
		return err
	}
	*rel = *refreshed
	return nil
}

func (r *Repository) NextReleaseNumber(ctx context.Context, serviceID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(MAX(release_number), 0) + 1 FROM vo_inference_releases
WHERE inference_service_id=$1 AND deleted=false`, serviceID).Scan(&n)
	return n, err
}

// --- api key ---

func (r *Repository) CreateAPIKey(ctx context.Context, k *inference.InferenceAPIKey) error {
	if k.UUID == uuid.Nil {
		k.UUID = uuid.New()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_api_keys
(uuid, inference_service_id, name, key_prefix, key_hash, daily_token_quota, rate_limit_per_min, expires_at, status,
 version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,$10)
RETURNING id, version, created_at, updated_at`,
		k.UUID, k.InferenceServiceID, k.Name, k.KeyPrefix, k.KeyHash, nullInt64(k.DailyTokenQuota), nullInt(k.RateLimitPerMin),
		k.ExpiresAt, string(k.Status), k.Audit.CreatedBy,
	)
	return row.Scan(&k.ID, &k.Audit.Version, &k.Audit.CreatedAt, &k.Audit.UpdatedAt)
}

func (r *Repository) GetAPIKeyByHash(ctx context.Context, hash string) (*inference.InferenceAPIKey, error) {
	k := &inference.InferenceAPIKey{}
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, inference_service_id, name, key_prefix, key_hash, daily_token_quota, rate_limit_per_min,
       expires_at, last_used_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_api_keys WHERE key_hash=$1 AND deleted=false`, hash).
		Scan(&k.ID, &k.UUID, &k.InferenceServiceID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.DailyTokenQuota, &k.RateLimitPerMin,
			&k.ExpiresAt, &k.LastUsedAt, &status, &k.Audit.Version, &k.Audit.CreatedAt, &k.Audit.CreatedBy, &k.Audit.UpdatedAt, &k.Audit.UpdatedBy,
			&k.Audit.Deleted, &k.Audit.DeletedAt, &k.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	k.Status = inference.APIKeyStatus(status)
	return k, nil
}

func (r *Repository) GetAPIKeyByID(ctx context.Context, id int64) (*inference.InferenceAPIKey, error) {
	k := &inference.InferenceAPIKey{}
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, inference_service_id, name, key_prefix, key_hash, daily_token_quota, rate_limit_per_min,
       expires_at, last_used_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_api_keys WHERE id=$1 AND deleted=false`, id).
		Scan(&k.ID, &k.UUID, &k.InferenceServiceID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.DailyTokenQuota, &k.RateLimitPerMin,
			&k.ExpiresAt, &k.LastUsedAt, &status, &k.Audit.Version, &k.Audit.CreatedAt, &k.Audit.CreatedBy, &k.Audit.UpdatedAt, &k.Audit.UpdatedBy,
			&k.Audit.Deleted, &k.Audit.DeletedAt, &k.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	k.Status = inference.APIKeyStatus(status)
	return k, nil
}

func (r *Repository) ListAPIKeys(ctx context.Context, serviceID int64) ([]*inference.InferenceAPIKey, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, inference_service_id, name, key_prefix, key_hash, daily_token_quota, rate_limit_per_min,
       expires_at, last_used_at, status, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_api_keys WHERE inference_service_id=$1 AND deleted=false ORDER BY id DESC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*inference.InferenceAPIKey, 0)
	for rows.Next() {
		k := &inference.InferenceAPIKey{}
		var status string
		if err := rows.Scan(&k.ID, &k.UUID, &k.InferenceServiceID, &k.Name, &k.KeyPrefix, &k.KeyHash, &k.DailyTokenQuota, &k.RateLimitPerMin,
			&k.ExpiresAt, &k.LastUsedAt, &status, &k.Audit.Version, &k.Audit.CreatedAt, &k.Audit.CreatedBy, &k.Audit.UpdatedAt, &k.Audit.UpdatedBy,
			&k.Audit.Deleted, &k.Audit.DeletedAt, &k.Audit.DeletedBy); err != nil {
			return nil, err
		}
		k.Status = inference.APIKeyStatus(status)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateAPIKeyLastUsed(ctx context.Context, id int64, lastUsed time.Time) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_api_keys SET last_used_at=$2, version=version+1, updated_at=now()
WHERE id=$1 AND deleted=false`, id, lastUsed)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrAPIKeyNotFound
	}
	return nil
}

func (r *Repository) RevokeAPIKey(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_api_keys SET status='revoked', version=version+1, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrAPIKeyNotFound
	}
	return nil
}

// --- route ---

func (r *Repository) CreateRoute(ctx context.Context, rt *inference.InferenceRoute) error {
	if rt.UUID == uuid.Nil {
		rt.UUID = uuid.New()
	}
	if rt.Rules == nil {
		rt.Rules = map[string]any{}
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_routes
(uuid, workspace_id, name, description, strategy, rules, default_service_id, status, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9)
RETURNING id, version, created_at, updated_at`,
		rt.UUID, rt.WorkspaceID, rt.Name, nullStr(rt.Description), string(rt.Strategy), rt.Rules,
		nullInt64(rt.DefaultServiceID), nullStrDefault(rt.Status, "active"), rt.Audit.CreatedBy,
	)
	return row.Scan(&rt.ID, &rt.Audit.Version, &rt.Audit.CreatedAt, &rt.Audit.UpdatedAt)
}

func (r *Repository) GetRouteByID(ctx context.Context, id int64) (*inference.InferenceRoute, error) {
	rt := &inference.InferenceRoute{}
	var strategy string
	err := r.pool.QueryRow(ctx, `
SELECT id, uuid, workspace_id, name, description, strategy, rules, default_service_id, status,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_routes WHERE id=$1 AND deleted=false`, id).
		Scan(&rt.ID, &rt.UUID, &rt.WorkspaceID, &rt.Name, &rt.Description, &strategy, &rt.Rules, &rt.DefaultServiceID, &rt.Status,
			&rt.Audit.Version, &rt.Audit.CreatedAt, &rt.Audit.CreatedBy, &rt.Audit.UpdatedAt, &rt.Audit.UpdatedBy,
			&rt.Audit.Deleted, &rt.Audit.DeletedAt, &rt.Audit.DeletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrRouteNotFound
	}
	if err != nil {
		return nil, err
	}
	rt.Strategy = inference.RouteStrategy(strategy)
	return rt, nil
}

func (r *Repository) ListRoutes(ctx context.Context, workspaceID int64) ([]*inference.InferenceRoute, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, uuid, workspace_id, name, description, strategy, rules, default_service_id, status,
       version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
FROM vo_inference_routes WHERE workspace_id=$1 AND deleted=false ORDER BY name ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*inference.InferenceRoute, 0)
	for rows.Next() {
		rt := &inference.InferenceRoute{}
		var strategy string
		if err := rows.Scan(&rt.ID, &rt.UUID, &rt.WorkspaceID, &rt.Name, &rt.Description, &strategy, &rt.Rules, &rt.DefaultServiceID, &rt.Status,
			&rt.Audit.Version, &rt.Audit.CreatedAt, &rt.Audit.CreatedBy, &rt.Audit.UpdatedAt, &rt.Audit.UpdatedBy,
			&rt.Audit.Deleted, &rt.Audit.DeletedAt, &rt.Audit.DeletedBy); err != nil {
			return nil, err
		}
		rt.Strategy = inference.RouteStrategy(strategy)
		out = append(out, rt)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateRoute(ctx context.Context, rt *inference.InferenceRoute) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_routes SET description=$2, strategy=$3, rules=$4, default_service_id=$5, status=$6,
  version=version+1, updated_at=now(), updated_by=$7
WHERE id=$1 AND deleted=false AND version=$8`,
		rt.ID, nullStr(rt.Description), string(rt.Strategy), rt.Rules, nullInt64(rt.DefaultServiceID), rt.Status,
		rt.Audit.UpdatedBy, rt.Audit.Version,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrRouteNotFound
	}
	refreshed, err := r.GetRouteByID(ctx, rt.ID)
	if err != nil {
		return err
	}
	*rt = *refreshed
	return nil
}

func (r *Repository) DeleteRoute(ctx context.Context, id, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_routes SET deleted=true, deleted_at=now(), deleted_by=$2, updated_at=now(), updated_by=$2
WHERE id=$1 AND deleted=false`, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrRouteNotFound
	}
	return nil
}

// --- usage ---

func (r *Repository) AppendUsage(ctx context.Context, u *inference.InferenceUsage) error {
	if u.UUID == uuid.Nil {
		u.UUID = uuid.New()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_usage
(uuid, inference_service_id, api_key_id, caller_id, prompt_tokens, completion_tokens, total_tokens, duration_ms, status_code, model_version_id, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING id`,
		u.UUID, u.InferenceServiceID, nullInt64(u.APIKeyID), nullInt64(u.CallerID),
		u.PromptTokens, u.CompletionTokens, u.TotalTokens, nullInt(u.DurationMs), nullInt(u.StatusCode),
		nullInt64(u.ModelVersionID), u.CreatedAt,
	)
	return row.Scan(&u.ID)
}

func (r *Repository) ListUsage(ctx context.Context, q inference.UsageQuery) ([]*inference.InferenceUsage, int64, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	where := "1=1"
	args := []any{}
	idx := 1
	add := func(clause string, val any) {
		where += " AND " + clause
		args = append(args, val)
		idx++
	}
	if q.ServiceID > 0 {
		add(fmt.Sprintf("inference_service_id=$%d", idx), q.ServiceID)
	}
	if q.APIKeyID > 0 {
		add(fmt.Sprintf("api_key_id=$%d", idx), q.APIKeyID)
	}
	if !q.StartTime.IsZero() {
		add(fmt.Sprintf("created_at >= $%d", idx), q.StartTime)
	}
	if !q.EndTime.IsZero() {
		add(fmt.Sprintf("created_at <= $%d", idx), q.EndTime)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM vo_inference_usage WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `
SELECT id, uuid, inference_service_id, api_key_id, caller_id, prompt_tokens, completion_tokens, total_tokens,
       duration_ms, status_code, model_version_id, created_at
FROM vo_inference_usage WHERE ` + where + fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, q.Limit, q.Offset)
	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]*inference.InferenceUsage, 0)
	for rows.Next() {
		u := &inference.InferenceUsage{}
		if err := rows.Scan(&u.ID, &u.UUID, &u.InferenceServiceID, &u.APIKeyID, &u.CallerID,
			&u.PromptTokens, &u.CompletionTokens, &u.TotalTokens, &u.DurationMs, &u.StatusCode, &u.ModelVersionID, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (r *Repository) SummarizeUsage(ctx context.Context, q inference.UsageQuery) (*inference.UsageSummary, error) {
	where := "1=1"
	args := []any{}
	idx := 1
	add := func(clause string, val any) {
		where += " AND " + clause
		args = append(args, val)
		idx++
	}
	if q.ServiceID > 0 {
		add(fmt.Sprintf("inference_service_id=$%d", idx), q.ServiceID)
	}
	if q.APIKeyID > 0 {
		add(fmt.Sprintf("api_key_id=$%d", idx), q.APIKeyID)
	}
	if !q.StartTime.IsZero() {
		add(fmt.Sprintf("created_at >= $%d", idx), q.StartTime)
	}
	if !q.EndTime.IsZero() {
		add(fmt.Sprintf("created_at <= $%d", idx), q.EndTime)
	}
	summary := &inference.UsageSummary{ServiceID: q.ServiceID}
	err := r.pool.QueryRow(ctx, `
SELECT count(*), COALESCE(sum(prompt_tokens),0), COALESCE(sum(completion_tokens),0), COALESCE(sum(total_tokens),0),
       COALESCE(avg(duration_ms),0)
FROM vo_inference_usage WHERE `+where, args...).
		Scan(&summary.TotalRequests, &summary.TotalPromptTokens, &summary.TotalCompletionTokens, &summary.TotalTokens, &summary.AvgDurationMs)
	return summary, err
}

// --- helpers ---

func scanRegistries(rows pgx.Rows, total int64) ([]*inference.ModelRegistry, int64, error) {
	out := make([]*inference.ModelRegistry, 0)
	for rows.Next() {
		reg := &inference.ModelRegistry{}
		var provider string
		if err := rows.Scan(&reg.ID, &reg.UUID, &reg.WorkspaceID, &reg.Name, &provider, &reg.Endpoint, &reg.CredentialID,
			&reg.CachePVCName, &reg.CachePath, &reg.CacheSizeBytes, &reg.Status,
			&reg.Audit.Version, &reg.Audit.CreatedAt, &reg.Audit.CreatedBy, &reg.Audit.UpdatedAt, &reg.Audit.UpdatedBy,
			&reg.Audit.Deleted, &reg.Audit.DeletedAt, &reg.Audit.DeletedBy); err != nil {
			return nil, 0, err
		}
		reg.Provider = inference.RegistryProvider(provider)
		out = append(out, reg)
	}
	return out, total, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullQuant(q inference.Quantization) any {
	if q == "" {
		return nil
	}
	return string(q)
}
