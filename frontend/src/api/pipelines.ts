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
  listPromotions: (params?: { pipeline_id?: number }) => get<Promotion[]>('/promotions', params),
  createPromotion: (body: Record<string, any>) => post<Promotion>('/promotions', body),
  recordSignature: (body: Partial<ArtifactSignature>) => post<ArtifactSignature>('/artifacts/signatures', body),
  getSignature: (imageId: number) => get<ArtifactSignature>(`/images/${imageId}/signature`),
};
