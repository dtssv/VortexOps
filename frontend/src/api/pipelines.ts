import { get, getPaged, post, del } from './client';
import type { Pipeline, PipelineRun, Promotion, ArtifactSignature, Paged } from '@/types';

export const pipelineApi = {
  list: (params?: { workspace_id?: number; page?: number; size?: number }) =>
    getPaged<Pipeline>('/pipelines', params),
  get: (id: number) => get<Pipeline>(`/pipelines/${id}`),
  create: (body: Partial<Pipeline>) => post<Pipeline>('/pipelines', body),
  delete: (id: number) => del(`/pipelines/${id}`),
  triggerRun: (id: number, body?: Record<string, any>) => post<PipelineRun>(`/pipelines/${id}/runs`, body),
  listRuns: (params?: { pipeline_id?: number; status?: string; page?: number; size?: number }) =>
    getPaged<PipelineRun>('/pipeline-runs', params),
  getRun: (id: number) => get<PipelineRun>(`/pipeline-runs/${id}`),
  cancelRun: (id: number) => post<PipelineRun>(`/pipeline-runs/${id}/cancel`),
  listPromotions: (params?: { workspace_id?: number; application_id?: number; status?: string; page?: number; size?: number }) =>
    getPaged<Promotion>('/promotions', params),
  createPromotion: (body: Record<string, any>) => post<Promotion>('/promotions', body),
  // 环境晋升状态机
  startPromotion: (id: number) => post<Promotion>(`/promotions/${id}/start`),
  completePromotionDeployment: (id: number) => post<Promotion>(`/promotions/${id}/complete-deployment`),
  completePromotionVerify: (id: number, body: { passed: boolean; reason?: string }) =>
    post<Promotion>(`/promotions/${id}/complete-verify`, body),
  abortPromotion: (id: number) => post<Promotion>(`/promotions/${id}/abort`),
  recordSignature: (body: Partial<ArtifactSignature>) => post<ArtifactSignature>('/artifacts/signatures', body),
  getSignature: (imageId: number) => get<ArtifactSignature>(`/images/${imageId}/signature`),
};
