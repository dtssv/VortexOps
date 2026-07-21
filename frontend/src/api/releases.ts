import { get, getPaged, post, del } from './client';
import type {
  Release,
  ReleaseEvent,
  ReleasePreset,
  ReleaseWindow,
  ReleaseOrchestration,
  OrchestrationDetail,
  Paged,
  TriggerReleaseInput,
} from '@/types';

export const releaseApi = {
  list: (groupId: number, params?: { page?: number; size?: number; status?: string }) =>
    getPaged<Release>(`/groups/${groupId}/releases`, params),
  get: (id: number) => get<Release>(`/releases/${id}`),
  trigger: (groupId: number, body: TriggerReleaseInput) => post<Release>(`/groups/${groupId}/releases`, body),
  rollback: (groupId: number, body: { release_id?: number; image_id?: number }) =>
    post<Release>(`/groups/${groupId}/rollback`, body),
  abort: (id: number) => post<Release>(`/releases/${id}/abort`),
  pause: (id: number) => post<Release>(`/releases/${id}/pause`),
  resume: (id: number) => post<Release>(`/releases/${id}/resume`),
  listEvents: (id: number) => get<ReleaseEvent[]>(`/releases/${id}/events`),
  listPresets: () => get<ReleasePreset[]>('/release-presets'),
  createPreset: (body: Partial<ReleasePreset>) => post<ReleasePreset>('/release-presets', body),
  deletePreset: (id: number) => del(`/release-presets/${id}`),
  listWindows: (applicationId: number) => get<ReleaseWindow[]>(`/applications/${applicationId}/release-windows`),
  createWindow: (applicationId: number, body: Partial<ReleaseWindow>) =>
    post<ReleaseWindow>(`/applications/${applicationId}/release-windows`, body),
  deleteWindow: (id: number) => del(`/release-windows/${id}`),

  // 多集群发布编排
  listOrchestrations: (appId: number, params?: { page?: number; size?: number }) =>
    getPaged<ReleaseOrchestration>(`/applications/${appId}/orchestrations`, params),
  getOrchestration: (id: number) => get<OrchestrationDetail>(`/orchestrations/${id}`),
  triggerOrchestration: (appId: number, body: Record<string, any>) =>
    post<ReleaseOrchestration>(`/applications/${appId}/multi-release`, body),
  abortOrchestration: (id: number) => post<ReleaseOrchestration>(`/orchestrations/${id}/abort`),
};
