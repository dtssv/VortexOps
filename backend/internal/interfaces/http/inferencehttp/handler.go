// Package inferencehttp 是大模型推理 HTTP handlers。
package inferencehttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vortexops/vortexops/internal/application/inferenceapp"
	"github.com/vortexops/vortexops/internal/domain/inference"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpauth"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// Handler 处理推理相关路由。
type Handler struct {
	svc   *inferenceapp.Service
	proxy *inferenceapp.Proxy
}

// NewHandler 创建 handler。
func NewHandler(svc *inferenceapp.Service, proxy *inferenceapp.Proxy) *Handler {
	return &Handler{svc: svc, proxy: proxy}
}

// ProxyOpenAI OpenAI 兼容代理入口。
func (h *Handler) ProxyOpenAI(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

// --- model registries ---

func (h *Handler) CreateRegistry(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID    int64  `json:"workspace_id"`
		Name           string `json:"name"`
		Provider       string `json:"provider"`
		Endpoint       string `json:"endpoint"`
		CredentialID   int64  `json:"credential_id"`
		CachePVCName   string `json:"cache_pvc_name"`
		CachePath      string `json:"cache_path"`
		CacheSizeBytes int64  `json:"cache_size_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	reg, err := h.svc.CreateRegistry(r.Context(), inferenceapp.CreateRegistryInput{
		WorkspaceID: req.WorkspaceID, Name: req.Name, Provider: inference.RegistryProvider(req.Provider),
		Endpoint: req.Endpoint, CredentialID: req.CredentialID, CachePVCName: req.CachePVCName,
		CachePath: req.CachePath, CacheSizeBytes: req.CacheSizeBytes, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRegistryDTO(reg))
}

func (h *Handler) ListRegistries(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	items, total, err := h.svc.ListRegistries(r.Context(), wsID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[registryDTO]{Items: toRegistryDTOs(items), Total: total, Page: page, Size: size})
}

func (h *Handler) GetRegistry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	reg, err := h.svc.GetRegistry(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRegistryDTO(reg))
}

func (h *Handler) DeleteRegistry(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteRegistry(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- models ---

func (h *Handler) CreateModel(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID      int64    `json:"workspace_id"`
		RegistryID       int64    `json:"registry_id"`
		Name             string   `json:"name"`
		DisplayName      string   `json:"display_name"`
		Description      string   `json:"description"`
		BaseArchitecture string   `json:"base_architecture"`
		ParameterCount   string   `json:"parameter_count"`
		License          string   `json:"license"`
		Tags             []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	m, err := h.svc.CreateModel(r.Context(), inferenceapp.CreateModelInput{
		WorkspaceID: req.WorkspaceID, RegistryID: req.RegistryID, Name: req.Name, DisplayName: req.DisplayName,
		Description: req.Description, BaseArchitecture: req.BaseArchitecture, ParameterCount: req.ParameterCount,
		License: req.License, Tags: req.Tags, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toModelDTO(m))
}

func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	regID, _ := strconv.ParseInt(r.URL.Query().Get("registry_id"), 10, 64)
	items, total, err := h.svc.ListModels(r.Context(), wsID, regID, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[modelDTO]{Items: toModelDTOs(items), Total: total, Page: page, Size: size})
}

func (h *Handler) GetModel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	m, err := h.svc.GetModel(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toModelDTO(m))
}

func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteModel(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- model versions ---

func (h *Handler) CreateModelVersion(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	modelID, ok := parseID(w, chi.URLParam(r, "modelId"))
	if !ok {
		return
	}
	var req struct {
		Version             string         `json:"version"`
		Precision           string         `json:"precision"`
		Quantization        string         `json:"quantization"`
		WeightsPath         string         `json:"weights_path"`
		WeightsSizeBytes    int64          `json:"weights_size_bytes"`
		WeightsChecksum     string         `json:"weights_checksum"`
		Framework           string         `json:"framework"`
		FrameworkConfig     map[string]any `json:"framework_config"`
		MinGPUMemoryBytes   int64          `json:"min_gpu_memory_bytes"`
		RecommendedGPUCount int            `json:"recommended_gpu_count"`
		IsDefault           bool           `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	v, err := h.svc.CreateModelVersion(r.Context(), inferenceapp.CreateModelVersionInput{
		ModelID: modelID, Version: req.Version, Precision: inference.Precision(req.Precision),
		Quantization: inference.Quantization(req.Quantization), WeightsPath: req.WeightsPath,
		WeightsSizeBytes: req.WeightsSizeBytes, WeightsChecksum: req.WeightsChecksum,
		Framework: inference.Framework(req.Framework), FrameworkConfig: req.FrameworkConfig,
		MinGPUMemoryBytes: req.MinGPUMemoryBytes, RecommendedGPUCount: req.RecommendedGPUCount,
		IsDefault: req.IsDefault, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toModelVersionDTO(v))
}

func (h *Handler) ListModelVersions(w http.ResponseWriter, r *http.Request) {
	modelID, ok := parseID(w, chi.URLParam(r, "modelId"))
	if !ok {
		return
	}
	items, err := h.svc.ListModelVersions(r.Context(), modelID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toModelVersionDTOs(items))
}

func (h *Handler) GetModelVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	v, err := h.svc.GetModelVersion(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toModelVersionDTO(v))
}

func (h *Handler) DeleteModelVersion(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteModelVersion(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// DownloadModelVersion 触发权重下载 Job。
func (h *Handler) DownloadModelVersion(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		ClusterID int64  `json:"cluster_id"`
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// 允许空 body：从 model version 反查所属服务的 cluster/namespace 作为兜底。
	}
	if req.ClusterID == 0 || req.Namespace == "" {
		httpx.WriteError(w, apperr.Validation("cluster_id and namespace are required", nil))
		return
	}
	if err := h.svc.DownloadModelVersion(r.Context(), inferenceapp.DownloadModelVersionInput{
		ModelVersionID: id, ClusterID: req.ClusterID, Namespace: req.Namespace, StartedBy: uid,
	}); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Accepted(w, map[string]any{"status": "downloading"})
}

// --- adapters ---

func (h *Handler) CreateAdapter(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		BaseModelVersionID int64   `json:"base_model_version_id"`
		Name               string  `json:"name"`
		AdapterType        string  `json:"adapter_type"`
		WeightsPath        string  `json:"weights_path"`
		Rank               int     `json:"rank"`
		Scale              float64 `json:"scale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	a, err := h.svc.CreateAdapter(r.Context(), inferenceapp.CreateAdapterInput{
		BaseModelVersionID: req.BaseModelVersionID, Name: req.Name, AdapterType: inference.AdapterType(req.AdapterType),
		WeightsPath: req.WeightsPath, Rank: req.Rank, Scale: req.Scale, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toAdapterDTO(a))
}

func (h *Handler) ListAdapters(w http.ResponseWriter, r *http.Request) {
	baseID, _ := strconv.ParseInt(r.URL.Query().Get("base_model_version_id"), 10, 64)
	items, err := h.svc.ListAdapters(r.Context(), baseID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toAdapterDTOs(items))
}

func (h *Handler) GetAdapter(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	a, err := h.svc.GetAdapter(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toAdapterDTO(a))
}

func (h *Handler) DeleteAdapter(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteAdapter(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- inference services ---

func (h *Handler) CreateService(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID          int64          `json:"workspace_id"`
		Name                 string         `json:"name"`
		DisplayName          string         `json:"display_name"`
		Description          string         `json:"description"`
		ClusterID            int64          `json:"cluster_id"`
		Namespace            string         `json:"namespace"`
		BaseModelVersionID   int64          `json:"base_model_version_id"`
		AdapterIDs           []int64        `json:"adapter_ids"`
		Framework            string         `json:"framework"`
		FrameworkConfig      map[string]any `json:"framework_config"`
		Replicas             int            `json:"replicas"`
		Resources            map[string]any `json:"resources"`
		GPUCount             int            `json:"gpu_count"`
		GPUType              string         `json:"gpu_type"`
		TensorParallelSize   int            `json:"tensor_parallel_size"`
		PipelineParallelSize int            `json:"pipeline_parallel_size"`
		StorageSizeBytes     int64          `json:"storage_size_bytes"`
		AutoscalingEnabled   bool           `json:"autoscaling_enabled"`
		HPAMinReplicas       int            `json:"hpa_min_replicas"`
		HPAMaxReplicas       int            `json:"hpa_max_replicas"`
		HPAMetrics           map[string]any `json:"hpa_metrics"`
		AccessMode           string         `json:"access_mode"`
		Labels               map[string]any `json:"labels"`
		Metadata             map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	svc, err := h.svc.CreateService(r.Context(), inferenceapp.CreateServiceInput{
		WorkspaceID: req.WorkspaceID, Name: req.Name, DisplayName: req.DisplayName, Description: req.Description,
		ClusterID: req.ClusterID, Namespace: req.Namespace, BaseModelVersionID: req.BaseModelVersionID,
		AdapterIDs: req.AdapterIDs, Framework: inference.Framework(req.Framework), FrameworkConfig: req.FrameworkConfig,
		Replicas: req.Replicas, Resources: req.Resources, GPUCount: req.GPUCount, GPUType: req.GPUType,
		TensorParallelSize: req.TensorParallelSize, PipelineParallelSize: req.PipelineParallelSize,
		StorageSizeBytes: req.StorageSizeBytes, AutoscalingEnabled: req.AutoscalingEnabled,
		HPAMinReplicas: req.HPAMinReplicas, HPAMaxReplicas: req.HPAMaxReplicas, HPAMetrics: req.HPAMetrics,
		AccessMode: inference.AccessMode(req.AccessMode), Labels: req.Labels, Metadata: req.Metadata, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toServiceDTO(svc))
}

func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	q := inference.ServiceQuery{
		WorkspaceID: parseQueryInt64(r, "workspace_id"),
		ClusterID:   parseQueryInt64(r, "cluster_id"),
		Status:      inference.ServiceStatus(r.URL.Query().Get("status")),
	}
	items, total, err := h.svc.ListServices(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[serviceDTO]{Items: toServiceDTOs(items), Total: total, Page: page, Size: size})
}

func (h *Handler) GetService(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	svc, err := h.svc.GetService(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toServiceDTO(svc))
}

func (h *Handler) DeleteService(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteService(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) DeployService(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		ModelVersionID int64   `json:"model_version_id"`
		AdapterIDs     []int64 `json:"adapter_ids"`
		Strategy       string  `json:"strategy"`
		Replicas       int     `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	rel, err := h.svc.Deploy(r.Context(), inferenceapp.DeployInput{
		ServiceID: id, ModelVersionID: req.ModelVersionID, AdapterIDs: req.AdapterIDs,
		Strategy: inference.ReleaseStrategy(req.Strategy), Replicas: req.Replicas, StartedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Accepted(w, toReleaseDTO(rel))
}

func (h *Handler) ScaleService(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	svc, err := h.svc.Scale(r.Context(), id, req.Replicas, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toServiceDTO(svc))
}

func (h *Handler) RollbackService(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.Rollback(r.Context(), id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Accepted(w, toReleaseDTO(rel))
}

// --- releases ---

func (h *Handler) ListReleases(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	q := inference.ReleaseQuery{
		ServiceID: parseQueryInt64(r, "service_id"),
		Status:    inference.ReleaseStatus(r.URL.Query().Get("status")),
	}
	items, total, err := h.svc.ListReleases(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[releaseDTO]{Items: toReleaseDTOs(items), Total: total, Page: page, Size: size})
}

func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rel, err := h.svc.GetRelease(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toReleaseDTO(rel))
}

// --- api keys ---

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	serviceID, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		Name            string `json:"name"`
		DailyTokenQuota int64  `json:"daily_token_quota"`
		RateLimitPerMin int    `json:"rate_limit_per_min"`
		ExpiresAt       string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	in := inferenceapp.CreateAPIKeyInput{
		InferenceServiceID: serviceID, Name: req.Name, DailyTokenQuota: req.DailyTokenQuota,
		RateLimitPerMin: req.RateLimitPerMin, CreatedBy: uid,
	}
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			httpx.WriteError(w, apperr.Validation("invalid expires_at", err))
			return
		}
		in.ExpiresAt = &t
	}
	res, err := h.svc.CreateAPIKey(r.Context(), in)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toCreateAPIKeyResponse(res))
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	serviceID, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	items, err := h.svc.ListAPIKeys(r.Context(), serviceID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toAPIKeyDTOs(items))
}

func (h *Handler) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "keyId"))
	if !ok {
		return
	}
	if err := h.svc.RevokeAPIKey(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- routes ---

func (h *Handler) CreateRoute(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	var req struct {
		WorkspaceID      int64          `json:"workspace_id"`
		Name             string         `json:"name"`
		Description      string         `json:"description"`
		Strategy         string         `json:"strategy"`
		Rules            map[string]any `json:"rules"`
		DefaultServiceID int64          `json:"default_service_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	rt, err := h.svc.CreateRoute(r.Context(), inferenceapp.CreateRouteInput{
		WorkspaceID: req.WorkspaceID, Name: req.Name, Description: req.Description,
		Strategy: inference.RouteStrategy(req.Strategy), Rules: req.Rules,
		DefaultServiceID: req.DefaultServiceID, CreatedBy: uid,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.Created(w, toRouteDTO(rt))
}

func (h *Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	wsID, _ := strconv.ParseInt(r.URL.Query().Get("workspace_id"), 10, 64)
	items, err := h.svc.ListRoutes(r.Context(), wsID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRouteDTOs(items))
}

func (h *Handler) GetRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rt, err := h.svc.GetRoute(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRouteDTO(rt))
}

func (h *Handler) UpdateRoute(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rt, err := h.svc.GetRoute(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	var req struct {
		Description      string         `json:"description"`
		Strategy         string         `json:"strategy"`
		Rules            map[string]any `json:"rules"`
		DefaultServiceID int64          `json:"default_service_id"`
		Status           string         `json:"status"`
		VersionCol       int            `json:"version_col"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, apperr.Validation("invalid body", err))
		return
	}
	if req.Description != "" {
		rt.Description = req.Description
	}
	if req.Strategy != "" {
		rt.Strategy = inference.RouteStrategy(req.Strategy)
	}
	if req.Rules != nil {
		rt.Rules = req.Rules
	}
	if req.DefaultServiceID > 0 {
		rt.DefaultServiceID = req.DefaultServiceID
	}
	if req.Status != "" {
		rt.Status = req.Status
	}
	if req.VersionCol > 0 {
		rt.Audit.Version = req.VersionCol
	}
	rt.UpdatedBy = uid
	if err := h.svc.UpdateRoute(r.Context(), rt); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toRouteDTO(rt))
}

func (h *Handler) DeleteRoute(w http.ResponseWriter, r *http.Request) {
	uid := mustAuth(w, r)
	id, ok := parseID(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if err := h.svc.DeleteRoute(r.Context(), id, uid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- usage ---

func (h *Handler) ListUsage(w http.ResponseWriter, r *http.Request) {
	page, size, _ := httpx.Pagination(r)
	q := parseUsageQuery(r)
	items, total, err := h.svc.ListUsage(r.Context(), q, page, size)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, httpx.Paged[usageDTO]{Items: toUsageDTOs(items), Total: total, Page: page, Size: size})
}

func (h *Handler) SummarizeUsage(w http.ResponseWriter, r *http.Request) {
	q := parseUsageQuery(r)
	summary, err := h.svc.SummarizeUsage(r.Context(), q)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toUsageSummaryDTO(summary))
}

// --- helpers ---

func mustAuth(w http.ResponseWriter, r *http.Request) int64 {
	uid := httpauth.UserID(r.Context())
	if uid == 0 {
		httpx.WriteError(w, apperr.Unauthorized("not authenticated", nil))
		return 0
	}
	return uid
}

func parseID(w http.ResponseWriter, raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, apperr.Validation("invalid id", err))
		return 0, false
	}
	return id, true
}

func parseQueryInt64(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return v
}

func parseUsageQuery(r *http.Request) inference.UsageQuery {
	q := inference.UsageQuery{
		ServiceID: parseQueryInt64(r, "service_id"),
		APIKeyID:  parseQueryInt64(r, "api_key_id"),
	}
	if s := r.URL.Query().Get("start_time"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			q.StartTime = t
		}
	}
	if s := r.URL.Query().Get("end_time"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			q.EndTime = t
		}
	}
	return q
}
