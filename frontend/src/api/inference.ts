import { get, getPaged, post, del } from './client';
import type {
  ModelRegistry,
  Model,
  ModelVersion,
  ModelAdapter,
  InferenceService,
  InferenceRelease,
  InferenceAPIKey,
  InferenceRoute,
  InferenceUsage,
  InferenceUsageSummary,
  Paged,
} from '@/types';

export const inferenceApi = {
  listRegistries: (workspaceId: number) =>
    getPaged<ModelRegistry>('/model-registries', { workspace_id: workspaceId, page: 1, size: 200 }).then((p) => p.items),
  createRegistry: (body: Partial<ModelRegistry>) => post<ModelRegistry>('/model-registries', body),
  getRegistry: (id: number) => get<ModelRegistry>(`/model-registries/${id}`),
  deleteRegistry: (id: number) => del(`/model-registries/${id}`),
  listModels: (workspaceId: number, params?: { registry_id?: number }) =>
    getPaged<Model>('/models', { workspace_id: workspaceId, ...params, page: 1, size: 200 }).then((p) => p.items),
  createModel: (body: Partial<Model>) => post<Model>('/models', body),
  getModel: (id: number) => get<Model>(`/models/${id}`),
  deleteModel: (id: number) => del(`/models/${id}`),
  listModelVersions: (modelId: number) => get<ModelVersion[]>(`/models/${modelId}/versions`),
  createModelVersion: (modelId: number, body: Partial<ModelVersion>) => post<ModelVersion>(`/models/${modelId}/versions`, body),
  getModelVersion: (id: number) => get<ModelVersion>(`/model-versions/${id}`),
  deleteModelVersion: (id: number) => del(`/model-versions/${id}`),
  downloadModelVersion: (id: number, body: { cluster_id: number; namespace: string }) =>
    post(`/model-versions/${id}/download`, body),
  listAdapters: (baseModelVersionId: number) =>
    get<ModelAdapter[]>(`/model-versions/${baseModelVersionId}/adapters`),
  createAdapter: (body: Partial<ModelAdapter>) => post<ModelAdapter>('/model-adapters', body),
  deleteAdapter: (id: number) => del(`/model-adapters/${id}`),
  listServices: (params?: { workspace_id?: number; cluster_id?: number; status?: string; page?: number; size?: number }) =>
    getPaged<InferenceService>('/inference-services', params),
  getService: (id: number) => get<InferenceService>(`/inference-services/${id}`),
  createService: (body: Partial<InferenceService>) => post<InferenceService>('/inference-services', body),
  updateService: (id: number, body: Partial<InferenceService>) => post<InferenceService>(`/inference-services/${id}`, body),
  deploy: (id: number, body: { target_model_version_id: number; adapter_ids?: number[]; strategy?: string; replicas?: number }) =>
    post<InferenceRelease>(`/inference-services/${id}/deploy`, body),
  scale: (id: number, body: { replicas: number }) => post<InferenceService>(`/inference-services/${id}/scale`, body),
  rollback: (id: number, body: { release_id: number }) => post<InferenceRelease>(`/inference-services/${id}/rollback`, body),
  deleteService: (id: number) => del(`/inference-services/${id}`),
  listReleases: (serviceId: number) => get<InferenceRelease[]>(`/inference-services/${serviceId}/releases`),
  listAPIKeys: (serviceId: number) => get<InferenceAPIKey[]>(`/inference-services/${serviceId}/api-keys`),
  createAPIKey: (serviceId: number, body: { name: string; daily_token_quota?: number; rate_limit_per_min?: number; expires_at?: string }) =>
    post<InferenceAPIKey>(`/inference-services/${serviceId}/api-keys`, body),
  revokeAPIKey: (serviceId: number, keyId: number) => post(`/inference-services/${serviceId}/api-keys/${keyId}/revoke`),
  listRoutes: (workspaceId: number) => get<InferenceRoute[]>('/inference-routes', { workspace_id: workspaceId }),
  createRoute: (body: Partial<InferenceRoute>) => post<InferenceRoute>('/inference-routes', body),
  deleteRoute: (id: number) => del(`/inference-routes/${id}`),
  listUsage: (serviceId: number, params?: { start_time?: string; end_time?: string; page?: number; size?: number }) =>
    getPaged<InferenceUsage>(`/inference-services/${serviceId}/usage`, params),
  usageSummary: (serviceId: number, params?: { start_time?: string; end_time?: string }) =>
    get<InferenceUsageSummary>(`/inference-services/${serviceId}/usage/summary`, params),
};
