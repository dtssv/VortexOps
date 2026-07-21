import { get, getPaged, post, put, del } from './client';
import type { Workspace, WorkspaceMember, ClusterBinding, Paged } from '@/types';

export interface CreateWorkspaceInput {
  name: string;
  display_name?: string;
  description?: string;
  logo_url?: string;
  status?: string;
  default_registry_id?: number;
  default_jenkins_id?: number;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  max_applications?: number;
  max_groups?: number;
  max_members?: number;
  version?: number;
}

export const workspaceApi = {
  list: (params?: { page?: number; size?: number; search?: string; status?: string }) =>
    getPaged<Workspace>('/workspaces', params),
  get: (id: number) => get<Workspace>(`/workspaces/${id}`),
  create: (body: CreateWorkspaceInput) => post<Workspace>('/workspaces', body),
  update: (id: number, body: Partial<CreateWorkspaceInput>) => put<Workspace>(`/workspaces/${id}`, body),
  delete: (id: number) => del(`/workspaces/${id}`),
  getQuota: (id: number) => get<Record<string, any>>(`/workspaces/${id}/quota`),
  updateQuota: (id: number, body: Record<string, any>) => put(`/workspaces/${id}/quota`, body),
  listMembers: (id: number) => get<WorkspaceMember[]>(`/workspaces/${id}/members`),
  addMember: (id: number, body: { user_id: number; role_id: number }) =>
    post<WorkspaceMember>(`/workspaces/${id}/members`, body),
  updateMember: (id: number, userId: number, body: { role_id: number }) =>
    put<WorkspaceMember>(`/workspaces/${id}/members/${userId}`, body),
  removeMember: (id: number, userId: number) => del(`/workspaces/${id}/members/${userId}`),
  listClusterBindings: (id: number) => get<ClusterBinding[]>(`/workspaces/${id}/clusters`),
  addClusterBinding: (id: number, body: { cluster_id: number; namespace: string; role: string; resource_quota?: Record<string, any> }) =>
    post<ClusterBinding>(`/workspaces/${id}/clusters`, body),
  removeClusterBinding: (id: number, clusterId: number) =>
    del(`/workspaces/${id}/clusters/${clusterId}`),
};
