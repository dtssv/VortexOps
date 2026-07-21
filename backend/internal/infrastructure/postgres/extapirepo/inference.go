package extapirepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/vortexops/vortexops/internal/domain/inference"
)

// GetInferenceServiceByUUID 按 UUID 查询推理服务。
func (r *Repository) GetInferenceServiceByUUID(ctx context.Context, wsID int64, svcUUID uuid.UUID) (*inference.InferenceService, error) {
	q := `SELECT id, uuid, workspace_id, name, display_name, description, cluster_id, namespace, workload_name,
		service_name, base_model_version_id, adapter_ids, framework, framework_config, replicas, resources, gpu_count,
		gpu_type, tensor_parallel_size, pipeline_parallel_size, storage_size_bytes, current_release_id, current_status,
		readiness, autoscaling_enabled, hpa_min_replicas, hpa_max_replicas, hpa_metrics, access_mode, external_endpoint,
		labels, metadata, version, created_at, created_by, updated_at, updated_by, deleted, deleted_at, deleted_by
		FROM vo_inference_services WHERE uuid=$1 AND workspace_id=$2 AND deleted=false`
	s, err := scanInferenceService(r.pool.QueryRow(ctx, q, svcUUID, wsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, inference.ErrServiceNotFound
	}
	return s, err
}

// UpdateInferenceServiceReplicas 更新推理服务副本数。
func (r *Repository) UpdateInferenceServiceReplicas(ctx context.Context, id int64, replicas int, actorID int64) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vo_inference_services SET replicas=$1, updated_at=now(), updated_by=$2, version=version+1
WHERE id=$3 AND deleted=false`, replicas, actorID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return inference.ErrServiceNotFound
	}
	return nil
}

// CreateInferenceService 创建推理服务记录（期望态；K8s 部署由后续 worker 推进）。
func (r *Repository) CreateInferenceService(ctx context.Context, s *inference.InferenceService) error {
	if s.UUID == uuid.Nil {
		s.UUID = uuid.New()
	}
	adapterIDs := s.AdapterIDs
	if adapterIDs == nil {
		adapterIDs = []int64{}
	}
	fwCfg, _ := json.Marshal(s.FrameworkConfig)
	res, _ := json.Marshal(s.Resources)
	_, _ = json.Marshal(s.HPAMetrics)
	labels, _ := json.Marshal(s.Labels)
	meta, _ := json.Marshal(s.Metadata)
	row := r.pool.QueryRow(ctx, `
INSERT INTO vo_inference_services (uuid, workspace_id, name, display_name, description, cluster_id, namespace,
  base_model_version_id, adapter_ids, framework, framework_config, replicas, resources, gpu_count, gpu_type,
  tensor_parallel_size, pipeline_parallel_size, storage_size_bytes, current_status, readiness, access_mode,
  labels, metadata, version, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,1,$24,$24)
RETURNING id, created_at, updated_at`,
		s.UUID, s.WorkspaceID, s.Name, nullableStr(s.DisplayName), nullableStr(s.Description), s.ClusterID, s.Namespace,
		s.BaseModelVersionID, adapterIDs, string(s.Framework), fwCfg, s.Replicas, res, s.GPUCount,
		nullableStr(s.GPUType), s.TensorParallelSize, s.PipelineParallelSize, nullableInt64(s.StorageSizeBytes),
		string(inference.SvcStarting), string(inference.ReadinessUnknown), string(s.AccessMode), labels, meta, s.CreatedBy)
	return row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

// ResolveModelVersionUUID 解析模型版本 UUID 为 ID。
func (r *Repository) ResolveModelVersionUUID(ctx context.Context, versionUUID uuid.UUID) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT mv.id FROM vo_model_versions mv WHERE mv.uuid=$1 AND mv.deleted=false`, versionUUID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, inference.ErrModelVersionNotFound
	}
	return id, err
}

func scanInferenceService(row pgx.Row) (*inference.InferenceService, error) {
	s := &inference.InferenceService{
		FrameworkConfig: map[string]any{},
		Resources:       map[string]any{},
		HPAMetrics:      map[string]any{},
		Labels:          map[string]any{},
		Metadata:        map[string]any{},
	}
	var (
		displayName, description, workloadName, serviceName, gpuType, externalEndpoint *string
		adapterIDs                                                                     []int64
		framework, currentStatus, readiness, accessMode                                string
		fwCfg, res, hpaMetrics, labels, meta                                           []byte
		storageSize                                                                    *int64
		currentReleaseID, hpaMin, hpaMax                                               *int64
		createdBy, updatedBy, deletedBy                                                *int64
		deletedAt                                                                      *time.Time
	)
	if err := row.Scan(
		&s.ID, &s.UUID, &s.WorkspaceID, &s.Name, &displayName, &description, &s.ClusterID, &s.Namespace,
		&workloadName, &serviceName, &s.BaseModelVersionID, &adapterIDs, &framework, &fwCfg, &s.Replicas, &res,
		&s.GPUCount, &gpuType, &s.TensorParallelSize, &s.PipelineParallelSize, &storageSize, &currentReleaseID,
		&currentStatus, &readiness, &s.AutoscalingEnabled, &hpaMin, &hpaMax, &hpaMetrics, &accessMode,
		&externalEndpoint, &labels, &meta, &s.Version, &s.CreatedAt, &createdBy, &s.UpdatedAt, &updatedBy,
		&s.Deleted, &deletedAt, &deletedBy,
	); err != nil {
		return nil, err
	}
	if displayName != nil {
		s.DisplayName = *displayName
	}
	if description != nil {
		s.Description = *description
	}
	if workloadName != nil {
		s.WorkloadName = *workloadName
	}
	if serviceName != nil {
		s.ServiceName = *serviceName
	}
	s.AdapterIDs = adapterIDs
	s.Framework = inference.Framework(framework)
	if fwCfg != nil {
		_ = json.Unmarshal(fwCfg, &s.FrameworkConfig)
	}
	if res != nil {
		_ = json.Unmarshal(res, &s.Resources)
	}
	if gpuType != nil {
		s.GPUType = *gpuType
	}
	if storageSize != nil {
		s.StorageSizeBytes = *storageSize
	}
	if currentReleaseID != nil {
		s.CurrentReleaseID = *currentReleaseID
	}
	s.CurrentStatus = inference.ServiceStatus(currentStatus)
	s.Readiness = inference.Readiness(readiness)
	if hpaMin != nil {
		s.HPAMinReplicas = int(*hpaMin)
	}
	if hpaMax != nil {
		s.HPAMaxReplicas = int(*hpaMax)
	}
	if hpaMetrics != nil {
		_ = json.Unmarshal(hpaMetrics, &s.HPAMetrics)
	}
	s.AccessMode = inference.AccessMode(accessMode)
	if externalEndpoint != nil {
		s.ExternalEndpoint = *externalEndpoint
	}
	if labels != nil {
		_ = json.Unmarshal(labels, &s.Labels)
	}
	if meta != nil {
		_ = json.Unmarshal(meta, &s.Metadata)
	}
	if createdBy != nil {
		s.CreatedBy = *createdBy
	}
	if updatedBy != nil {
		s.UpdatedBy = *updatedBy
	}
	if deletedBy != nil {
		s.DeletedBy = *deletedBy
	}
	return s, nil
}

// GetPipelineByUUID 按 UUID 查询流水线 ID 与 workspace。
func (r *Repository) GetPipelineByUUID(ctx context.Context, wsID int64, pipelineUUID uuid.UUID) (pipelineID int64, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT id FROM vo_pipelines WHERE uuid=$1 AND workspace_id=$2 AND deleted=false`,
		pipelineUUID, wsID).Scan(&pipelineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("pipeline not found")
	}
	return pipelineID, err
}

// GetRunByUUID 按 UUID 查询流水线运行 ID。
func (r *Repository) GetRunByUUID(ctx context.Context, wsID int64, runUUID uuid.UUID) (runID int64, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT id FROM vo_pipeline_runs WHERE uuid=$1 AND workspace_id=$2 AND deleted=false`,
		runUUID, wsID).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("pipeline run not found")
	}
	return runID, err
}

// GetImageByUUID 按 UUID 查询制品（校验 workspace 归属）。
func (r *Repository) GetImageByUUID(ctx context.Context, wsID int64, imageUUID uuid.UUID) (imageID int64, appID int64, err error) {
	err = r.pool.QueryRow(ctx, `
SELECT i.id, i.application_id FROM vo_images i
JOIN vo_applications a ON a.id=i.application_id
WHERE i.uuid=$1 AND a.workspace_id=$2 AND i.deleted=false AND a.deleted=false`,
		imageUUID, wsID).Scan(&imageID, &appID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, fmt.Errorf("image not found")
	}
	return imageID, appID, err
}
