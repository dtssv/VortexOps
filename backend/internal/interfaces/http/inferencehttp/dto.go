package inferencehttp

import (
	"time"

	"github.com/vortexops/vortexops/internal/application/inferenceapp"
	"github.com/vortexops/vortexops/internal/domain/inference"
)

type registryDTO struct {
	ID             int64  `json:"id"`
	UUID           string `json:"uuid"`
	WorkspaceID    int64  `json:"workspace_id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Endpoint       string `json:"endpoint,omitempty"`
	CredentialID   int64  `json:"credential_id,omitempty"`
	CachePVCName   string `json:"cache_pvc_name,omitempty"`
	CachePath      string `json:"cache_path,omitempty"`
	CacheSizeBytes int64  `json:"cache_size_bytes,omitempty"`
	Status         string `json:"status"`
	VersionCol     int    `json:"version_col"`
	CreatedAt      string `json:"created_at"`
}

func toRegistryDTO(r *inference.ModelRegistry) *registryDTO {
	if r == nil {
		return nil
	}
	return &registryDTO{
		ID: r.ID, UUID: r.UUID.String(), WorkspaceID: r.WorkspaceID, Name: r.Name, Provider: string(r.Provider),
		Endpoint: r.Endpoint, CredentialID: r.CredentialID, CachePVCName: r.CachePVCName, CachePath: r.CachePath,
		CacheSizeBytes: r.CacheSizeBytes, Status: r.Status, VersionCol: r.Audit.Version,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

func toRegistryDTOs(items []*inference.ModelRegistry) []registryDTO {
	out := make([]registryDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toRegistryDTO(r))
	}
	return out
}

type modelDTO struct {
	ID               int64    `json:"id"`
	UUID             string   `json:"uuid"`
	WorkspaceID      int64    `json:"workspace_id"`
	RegistryID       int64    `json:"registry_id"`
	Name             string   `json:"name"`
	DisplayName      string   `json:"display_name,omitempty"`
	Description      string   `json:"description,omitempty"`
	BaseArchitecture string   `json:"base_architecture,omitempty"`
	ParameterCount   string   `json:"parameter_count,omitempty"`
	License          string   `json:"license,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	VersionCol       int      `json:"version_col"`
	CreatedAt        string   `json:"created_at"`
}

func toModelDTO(m *inference.Model) *modelDTO {
	if m == nil {
		return nil
	}
	return &modelDTO{
		ID: m.ID, UUID: m.UUID.String(), WorkspaceID: m.WorkspaceID, RegistryID: m.RegistryID,
		Name: m.Name, DisplayName: m.DisplayName, Description: m.Description,
		BaseArchitecture: m.BaseArchitecture, ParameterCount: m.ParameterCount, License: m.License, Tags: m.Tags,
		VersionCol: m.Audit.Version, CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
}

func toModelDTOs(items []*inference.Model) []modelDTO {
	out := make([]modelDTO, 0, len(items))
	for _, m := range items {
		out = append(out, *toModelDTO(m))
	}
	return out
}

type modelVersionDTO struct {
	ID                  int64          `json:"id"`
	UUID                string         `json:"uuid"`
	ModelID             int64          `json:"model_id"`
	Version             string         `json:"version"`
	Precision           string         `json:"precision"`
	Quantization        string         `json:"quantization,omitempty"`
	WeightsPath         string         `json:"weights_path,omitempty"`
	WeightsSizeBytes    int64          `json:"weights_size_bytes,omitempty"`
	Framework           string         `json:"framework"`
	FrameworkConfig     map[string]any `json:"framework_config,omitempty"`
	MinGPUMemoryBytes   int64          `json:"min_gpu_memory_bytes,omitempty"`
	RecommendedGPUCount int            `json:"recommended_gpu_count,omitempty"`
	DownloadStatus      string         `json:"download_status"`
	DownloadProgress    int            `json:"download_progress"`
	IsDefault           bool           `json:"is_default"`
	VersionCol          int            `json:"version_col"`
	CreatedAt           string         `json:"created_at"`
}

func toModelVersionDTO(v *inference.ModelVersion) *modelVersionDTO {
	if v == nil {
		return nil
	}
	return &modelVersionDTO{
		ID: v.ID, UUID: v.UUID.String(), ModelID: v.ModelID, Version: v.Version, Precision: string(v.Precision),
		Quantization: string(v.Quantization), WeightsPath: v.WeightsPath, WeightsSizeBytes: v.WeightsSizeBytes,
		Framework: string(v.Framework), FrameworkConfig: v.FrameworkConfig, MinGPUMemoryBytes: v.MinGPUMemoryBytes,
		RecommendedGPUCount: v.RecommendedGPUCount, DownloadStatus: string(v.DownloadStatus),
		DownloadProgress: v.DownloadProgress, IsDefault: v.IsDefault, VersionCol: v.Audit.Version,
		CreatedAt: v.CreatedAt.Format(time.RFC3339),
	}
}

func toModelVersionDTOs(items []*inference.ModelVersion) []modelVersionDTO {
	out := make([]modelVersionDTO, 0, len(items))
	for _, v := range items {
		out = append(out, *toModelVersionDTO(v))
	}
	return out
}

type adapterDTO struct {
	ID                 int64   `json:"id"`
	UUID               string  `json:"uuid"`
	BaseModelVersionID int64   `json:"base_model_version_id"`
	Name               string  `json:"name"`
	AdapterType        string  `json:"adapter_type"`
	WeightsPath        string  `json:"weights_path,omitempty"`
	Rank               int     `json:"rank,omitempty"`
	Scale              float64 `json:"scale,omitempty"`
	VersionCol         int     `json:"version_col"`
	CreatedAt          string  `json:"created_at"`
}

func toAdapterDTO(a *inference.ModelAdapter) *adapterDTO {
	if a == nil {
		return nil
	}
	return &adapterDTO{
		ID: a.ID, UUID: a.UUID.String(), BaseModelVersionID: a.BaseModelVersionID, Name: a.Name,
		AdapterType: string(a.AdapterType), WeightsPath: a.WeightsPath, Rank: a.Rank, Scale: a.Scale,
		VersionCol: a.Audit.Version, CreatedAt: a.CreatedAt.Format(time.RFC3339),
	}
}

func toAdapterDTOs(items []*inference.ModelAdapter) []adapterDTO {
	out := make([]adapterDTO, 0, len(items))
	for _, a := range items {
		out = append(out, *toAdapterDTO(a))
	}
	return out
}

type serviceDTO struct {
	ID                   int64          `json:"id"`
	UUID                 string         `json:"uuid"`
	WorkspaceID          int64          `json:"workspace_id"`
	ApplicationID        int64          `json:"application_id,omitempty"`
	GroupID              int64          `json:"group_id,omitempty"`
	Name                 string         `json:"name"`
	DisplayName          string         `json:"display_name,omitempty"`
	Description          string         `json:"description,omitempty"`
	ClusterID            int64          `json:"cluster_id"`
	Namespace            string         `json:"namespace"`
	WorkloadName         string         `json:"workload_name,omitempty"`
	ServiceName          string         `json:"service_name,omitempty"`
	BaseModelVersionID   int64          `json:"base_model_version_id"`
	AdapterIDs           []int64        `json:"adapter_ids,omitempty"`
	Framework            string         `json:"framework"`
	FrameworkConfig      map[string]any `json:"framework_config,omitempty"`
	Replicas             int            `json:"replicas"`
	Resources            map[string]any `json:"resources,omitempty"`
	GPUCount             int            `json:"gpu_count"`
	GPUType              string         `json:"gpu_type,omitempty"`
	TensorParallelSize   int            `json:"tensor_parallel_size"`
	PipelineParallelSize int            `json:"pipeline_parallel_size"`
	StorageSizeBytes     int64          `json:"storage_size_bytes,omitempty"`
	CurrentReleaseID     int64          `json:"current_release_id,omitempty"`
	CurrentStatus        string         `json:"current_status"`
	Readiness            string         `json:"readiness"`
	AutoscalingEnabled   bool           `json:"autoscaling_enabled"`
	HPAMinReplicas       int            `json:"hpa_min_replicas,omitempty"`
	HPAMaxReplicas       int            `json:"hpa_max_replicas,omitempty"`
	HPAMetrics           map[string]any `json:"hpa_metrics,omitempty"`
	AccessMode           string         `json:"access_mode"`
	ExternalEndpoint     string         `json:"external_endpoint,omitempty"`
	Labels               map[string]any `json:"labels,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	VersionCol           int            `json:"version_col"`
	CreatedAt            string         `json:"created_at"`
}

func toServiceDTO(s *inference.InferenceService) *serviceDTO {
	if s == nil {
		return nil
	}
	return &serviceDTO{
		ID: s.ID, UUID: s.UUID.String(), WorkspaceID: s.WorkspaceID, ApplicationID: s.ApplicationID, GroupID: s.GroupID,
		Name: s.Name, DisplayName: s.DisplayName,
		Description: s.Description, ClusterID: s.ClusterID, Namespace: s.Namespace, WorkloadName: s.WorkloadName,
		ServiceName: s.ServiceName, BaseModelVersionID: s.BaseModelVersionID, AdapterIDs: s.AdapterIDs,
		Framework: string(s.Framework), FrameworkConfig: s.FrameworkConfig, Replicas: s.Replicas, Resources: s.Resources,
		GPUCount: s.GPUCount, GPUType: s.GPUType, TensorParallelSize: s.TensorParallelSize,
		PipelineParallelSize: s.PipelineParallelSize, StorageSizeBytes: s.StorageSizeBytes,
		CurrentReleaseID: s.CurrentReleaseID, CurrentStatus: string(s.CurrentStatus), Readiness: string(s.Readiness),
		AutoscalingEnabled: s.AutoscalingEnabled, HPAMinReplicas: s.HPAMinReplicas, HPAMaxReplicas: s.HPAMaxReplicas,
		HPAMetrics: s.HPAMetrics, AccessMode: string(s.AccessMode), ExternalEndpoint: s.ExternalEndpoint,
		Labels: s.Labels, Metadata: s.Metadata, VersionCol: s.Audit.Version, CreatedAt: s.CreatedAt.Format(time.RFC3339),
	}
}

func toServiceDTOs(items []*inference.InferenceService) []serviceDTO {
	out := make([]serviceDTO, 0, len(items))
	for _, s := range items {
		out = append(out, *toServiceDTO(s))
	}
	return out
}

type releaseDTO struct {
	ID                   int64   `json:"id"`
	UUID                 string  `json:"uuid"`
	InferenceServiceID   int64   `json:"inference_service_id"`
	GroupID              int64   `json:"group_id,omitempty"`
	ReleaseNumber        int     `json:"release_number"`
	PreviousReleaseID    int64   `json:"previous_release_id,omitempty"`
	TargetModelVersionID int64   `json:"target_model_version_id"`
	TargetAdapterIDs     []int64 `json:"target_adapter_ids,omitempty"`
	Strategy             string  `json:"strategy"`
	Replicas             int     `json:"replicas"`
	Status               string  `json:"status"`
	ProgressPercent      int     `json:"progress_percent"`
	FailureReason        string  `json:"failure_reason,omitempty"`
	StartedBy            int64   `json:"started_by"`
	StartedAt            string  `json:"started_at"`
	FinishedAt           string  `json:"finished_at,omitempty"`
	DurationMs           int64   `json:"duration_ms,omitempty"`
}

func toReleaseDTO(r *inference.InferenceRelease) *releaseDTO {
	if r == nil {
		return nil
	}
	dto := &releaseDTO{
		ID: r.ID, UUID: r.UUID.String(), InferenceServiceID: r.InferenceServiceID, GroupID: r.GroupID,
		ReleaseNumber: r.ReleaseNumber,
		PreviousReleaseID: r.PreviousReleaseID, TargetModelVersionID: r.TargetModelVersionID, TargetAdapterIDs: r.TargetAdapterIDs,
		Strategy: string(r.Strategy), Replicas: r.Replicas, Status: string(r.Status), ProgressPercent: r.ProgressPercent,
		FailureReason: r.FailureReason, StartedBy: r.StartedBy, StartedAt: r.StartedAt.Format(time.RFC3339), DurationMs: r.DurationMs,
	}
	if r.FinishedAt != nil {
		dto.FinishedAt = r.FinishedAt.Format(time.RFC3339)
	}
	return dto
}

func toReleaseDTOs(items []*inference.InferenceRelease) []releaseDTO {
	out := make([]releaseDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toReleaseDTO(r))
	}
	return out
}

type apiKeyDTO struct {
	ID                 int64  `json:"id"`
	UUID               string `json:"uuid"`
	InferenceServiceID int64  `json:"inference_service_id"`
	Name               string `json:"name"`
	KeyPrefix          string `json:"key_prefix"`
	DailyTokenQuota    int64  `json:"daily_token_quota,omitempty"`
	RateLimitPerMin    int    `json:"rate_limit_per_min,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	LastUsedAt         string `json:"last_used_at,omitempty"`
	Status             string `json:"status"`
	CreatedAt          string `json:"created_at"`
}

func toAPIKeyDTO(k *inference.InferenceAPIKey) *apiKeyDTO {
	if k == nil {
		return nil
	}
	dto := &apiKeyDTO{
		ID: k.ID, UUID: k.UUID.String(), InferenceServiceID: k.InferenceServiceID, Name: k.Name,
		KeyPrefix: k.KeyPrefix, DailyTokenQuota: k.DailyTokenQuota, RateLimitPerMin: k.RateLimitPerMin,
		Status: string(k.Status), CreatedAt: k.CreatedAt.Format(time.RFC3339),
	}
	if k.ExpiresAt != nil {
		dto.ExpiresAt = k.ExpiresAt.Format(time.RFC3339)
	}
	if k.LastUsedAt != nil {
		dto.LastUsedAt = k.LastUsedAt.Format(time.RFC3339)
	}
	return dto
}

func toAPIKeyDTOs(items []*inference.InferenceAPIKey) []apiKeyDTO {
	out := make([]apiKeyDTO, 0, len(items))
	for _, k := range items {
		out = append(out, *toAPIKeyDTO(k))
	}
	return out
}

type createAPIKeyResponse struct {
	apiKeyDTO
	Secret string `json:"secret"`
}

func toCreateAPIKeyResponse(res *inferenceapp.CreateAPIKeyResult) *createAPIKeyResponse {
	dto := toAPIKeyDTO(res.Key)
	return &createAPIKeyResponse{apiKeyDTO: *dto, Secret: res.Secret}
}

type routeDTO struct {
	ID               int64          `json:"id"`
	UUID             string         `json:"uuid"`
	WorkspaceID      int64          `json:"workspace_id"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Strategy         string         `json:"strategy"`
	Rules            map[string]any `json:"rules"`
	DefaultServiceID int64          `json:"default_service_id,omitempty"`
	Status           string         `json:"status"`
	VersionCol       int            `json:"version_col"`
	CreatedAt        string         `json:"created_at"`
}

func toRouteDTO(r *inference.InferenceRoute) *routeDTO {
	if r == nil {
		return nil
	}
	return &routeDTO{
		ID: r.ID, UUID: r.UUID.String(), WorkspaceID: r.WorkspaceID, Name: r.Name, Description: r.Description,
		Strategy: string(r.Strategy), Rules: r.Rules, DefaultServiceID: r.DefaultServiceID, Status: r.Status,
		VersionCol: r.Audit.Version, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

func toRouteDTOs(items []*inference.InferenceRoute) []routeDTO {
	out := make([]routeDTO, 0, len(items))
	for _, r := range items {
		out = append(out, *toRouteDTO(r))
	}
	return out
}

type usageDTO struct {
	ID                 int64  `json:"id"`
	UUID               string `json:"uuid"`
	InferenceServiceID int64  `json:"inference_service_id"`
	APIKeyID           int64  `json:"api_key_id,omitempty"`
	PromptTokens       int    `json:"prompt_tokens"`
	CompletionTokens   int    `json:"completion_tokens"`
	TotalTokens        int    `json:"total_tokens"`
	DurationMs         int    `json:"duration_ms,omitempty"`
	StatusCode         int    `json:"status_code,omitempty"`
	ModelVersionID     int64  `json:"model_version_id,omitempty"`
	CreatedAt          string `json:"created_at"`
}

func toUsageDTO(u *inference.InferenceUsage) usageDTO {
	return usageDTO{
		ID: u.ID, UUID: u.UUID.String(), InferenceServiceID: u.InferenceServiceID, APIKeyID: u.APIKeyID,
		PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens,
		DurationMs: u.DurationMs, StatusCode: u.StatusCode, ModelVersionID: u.ModelVersionID,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func toUsageDTOs(items []*inference.InferenceUsage) []usageDTO {
	out := make([]usageDTO, 0, len(items))
	for _, u := range items {
		out = append(out, toUsageDTO(u))
	}
	return out
}

type usageSummaryDTO struct {
	ServiceID             int64   `json:"service_id"`
	TotalRequests         int64   `json:"total_requests"`
	TotalPromptTokens     int64   `json:"total_prompt_tokens"`
	TotalCompletionTokens int64   `json:"total_completion_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	AvgDurationMs         float64 `json:"avg_duration_ms"`
}

func toUsageSummaryDTO(s *inference.UsageSummary) *usageSummaryDTO {
	if s == nil {
		return nil
	}
	return &usageSummaryDTO{
		ServiceID: s.ServiceID, TotalRequests: s.TotalRequests, TotalPromptTokens: s.TotalPromptTokens,
		TotalCompletionTokens: s.TotalCompletionTokens, TotalTokens: s.TotalTokens, AvgDurationMs: s.AvgDurationMs,
	}
}
