import { get, getPaged, post, put, del } from './client';
import type { Cluster, Credential, Paged } from '@/types';

export interface ClusterCapacity {
  cluster_id: number;
  allocatable_cpu_m: number;
  allocatable_memory_bytes: number;
  allocatable_gpu: number;
  used_cpu_m: number;
  used_memory_bytes: number;
  used_gpu: number;
  max_replicas: number;
  source: string;
}

export interface UpdateClusterInput {
  display_name?: string;
  description?: string;
  region?: string;
  environment?: string;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  kubeconfig?: string;
  insecure_skip_tls?: boolean;
  version?: number;
}

export const clusterApi = {
  list: (params?: { page?: number; size?: number }) => getPaged<Cluster>('/clusters', params),
  get: (id: number) => get<Cluster>(`/clusters/${id}`),
  create: (body: Partial<Cluster>) => post<Cluster>('/clusters', body),
  update: (id: number, body: Partial<UpdateClusterInput>) => put<Cluster>(`/clusters/${id}`, body),
  delete: (id: number) => del(`/clusters/${id}`),
  probe: (id: number) => post<Record<string, any>>(`/clusters/${id}/probe`),
  getCapacity: (id: number, params: { cpu_m?: number; memory_bytes?: number; gpu?: number }) =>
    get<ClusterCapacity>(`/clusters/${id}/capacity`, params),
  listCredentials: (params?: { scope?: string; scope_id?: number; page?: number; size?: number }) =>
    getPaged<Credential>('/credentials', params),
  createCredential: (body: { name: string; type: string; description?: string; kubeconfig: string }) =>
    post<Credential>('/credentials', body),
  rotateCredential: (id: number, body: { kubeconfig: string }) =>
    post<Credential>(`/credentials/${id}/rotate`, body),
  deleteCredential: (id: number) => del(`/credentials/${id}`),
  listIPPools: (clusterId: number) => get<any[]>(`/clusters/${clusterId}/ip-pools`),
  createIPPool: (clusterId: number, body: Record<string, any>) =>
    post<any>(`/clusters/${clusterId}/ip-pools`, body),
  deleteIPPool: (id: number) => del(`/ip-pools/${id}`),
};
