import { get, getPaged, post, put, del, api } from './client';
import type { Application, Group, GroupStableIPsResponse, PodSummary, Paged, ApplicationMember, PodFileEntry, ProbeConfig } from '@/types';

export interface CreateApplicationInput {
  workspace_id: number;
  name: string;
  code?: string;
  display_name?: string;
  description?: string;
  app_type?: string;
  workload_type?: string;
  git_url?: string;
  default_branch?: string;
  language?: string;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  probe?: ProbeConfig;
  version?: number;
}

export interface ListApplicationsParams {
  page?: number;
  size?: number;
  search?: string;
  app_type?: string;
  lifecycle?: string;
  owner_id?: number;
}

export const applicationApi = {
  // 工作空间内应用列表。
  list: (workspaceId: number, params?: ListApplicationsParams) =>
    getPaged<Application>(`/workspaces/${workspaceId}/applications`, params),
  // 跨工作空间统一应用列表：供「应用/中间件/模型推理」统一列表页使用。
  listAll: (params?: ListApplicationsParams) =>
    getPaged<Application>(`/applications`, params),
  get: (id: number) => get<Application>(`/applications/${id}`),
  create: (workspaceId: number, body: Omit<CreateApplicationInput, 'workspace_id'>) =>
    post<Application>(`/workspaces/${workspaceId}/applications`, body),
  update: (id: number, body: Partial<CreateApplicationInput>) => put<Application>(`/applications/${id}`, body),
  delete: (id: number) => del(`/applications/${id}`),
  listMembers: (applicationId: number, params?: { page?: number; size?: number }) =>
    getPaged<ApplicationMember>(`/applications/${applicationId}/members`, params),
  addMember: (applicationId: number, body: { user_id: number; role_id: number }) =>
    post<ApplicationMember>(`/applications/${applicationId}/members`, body),
  updateMember: (applicationId: number, userId: number, body: { role_id: number }) =>
    put<void>(`/applications/${applicationId}/members/${userId}`, body),
  removeMember: (applicationId: number, userId: number) =>
    del(`/applications/${applicationId}/members/${userId}`),
};

export interface CreateGroupInput {
  application_id: number;
  name: string;
  display_name?: string;
  description?: string;
  environment: string;
  cluster_id: number;
  namespace: string;
  replicas?: number;
  workload: Group['workload'];
  resources: Group['resources'];
  storage?: Group['storage'];
  mesh_enabled?: boolean;
  scheduling?: Group['scheduling'];
  autoscaling?: Group['autoscaling'];
  health_check?: Group['health_check'];
  release_requires_approval?: boolean;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  version?: number;
}

export interface ListGroupsParams {
  page?: number;
  size?: number;
  search?: string;
  app_type?: string;
  environment?: string;
  cluster_id?: number;
}

export const groupApi = {
  list: (applicationId: number, params?: ListGroupsParams) =>
    getPaged<Group>(`/applications/${applicationId}/groups`, params),
  get: (id: number) => get<Group>(`/groups/${id}`),
  create: (body: CreateGroupInput) => post<Group>(`/applications/${body.application_id}/groups`, body),
  update: (id: number, body: Partial<CreateGroupInput>) => put<Group>(`/groups/${id}`, body),
  delete: (id: number) => del(`/groups/${id}`),
  // 扩缩容：仅改副本数并强制同步 K8s（修复编辑副本数不生效）。
  scale: (id: number, body: { replicas: number; version?: number; remove_pod_names?: string[] }) => post<Group>(`/groups/${id}/scale`, body),
  // 机器运维：分组级重启/关机/开机。
  restart: (id: number) => post<{ restarted: boolean }>(`/groups/${id}:restart`, {}),
  shutdown: (id: number) => post<Group>(`/groups/${id}:shutdown`, {}),
  startup: (id: number) => post<Group>(`/groups/${id}:startup`, {}),
  // 单 Pod 重启。
  restartPod: (id: number, pod: string) => post<{ restarted: boolean }>(`/groups/${id}/pods/${pod}:restart`, {}),
  listPods: (id: number) => get<{ items: PodSummary[] }>(`/groups/${id}/pods`).then((r) => r.items),
  listStableIPs: (id: number) => get<GroupStableIPsResponse>(`/groups/${id}/stable-ips`),
  podLogs: (groupId: number, pod: string, params?: { container?: string; tail?: number }) =>
    get<string>(`/groups/${groupId}/pods/${pod}/logs`, params),
  // 文件浏览器（tar-over-exec）。
  listFiles: (groupId: number, pod: string, params: { path: string; container?: string }) =>
    get<{ items: PodFileEntry[] }>(`/groups/${groupId}/pods/${pod}/files`, params).then((r) => r.items),
  downloadFileUrl: (groupId: number, pod: string, params: { path: string; container?: string }) => {
    const q = new URLSearchParams({ path: params.path });
    if (params.container) q.set('container', params.container);
    return `/api/v1/groups/${groupId}/pods/${pod}/files/download?${q.toString()}`;
  },
  uploadFile: (groupId: number, pod: string, file: File, params: { path: string; container?: string }) => {
    const form = new FormData();
    form.append('file', file);
    const q = new URLSearchParams({ path: params.path });
    if (params.container) q.set('container', params.container);
    return api.post(`/groups/${groupId}/pods/${pod}/files/upload?${q.toString()}`, form, {
      headers: { 'Content-Type': 'multipart/form-data' },
    }) as unknown as Promise<{ uploaded: boolean }>;
  },
  deleteFile: (groupId: number, pod: string, params: { path: string; container?: string }) => {
    const q = new URLSearchParams({ path: params.path });
    if (params.container) q.set('container', params.container);
    return api.delete(`/groups/${groupId}/pods/${pod}/files?${q.toString()}`) as unknown as Promise<{ deleted: boolean }>;
  },
  cleanupFiles: (groupId: number, pod: string, params: { preset: 'tmp' | 'logs' | 'cache'; container?: string }) => {
    const q = new URLSearchParams({ preset: params.preset });
    if (params.container) q.set('container', params.container);
    return api.post(`/groups/${groupId}/pods/${pod}/files/cleanup?${q.toString()}`, {}) as unknown as Promise<Record<string, string>>;
  },
  // 网络命令（流式响应）：后端以 text/plain 分块返回 stdout/stderr。
  // 前端在 PodNetCmd 组件中用 fetch + ReadableStream 消费，不走此 helper。
  // 读取文件文本内容。
  readFile: (groupId: number, pod: string, params: { path: string; container?: string; max_lines?: number }) =>
    get<{ content: string; path: string }>(`/groups/${groupId}/pods/${pod}/files/read`, params),
  // 搜索日志文件路径（支持 glob 模式）。
  searchLogPaths: (groupId: number, pod: string, params: { pattern?: string; container?: string }) =>
    get<{ items: string[] }>(`/groups/${groupId}/pods/${pod}/files/search-logs`, params).then((r) => r.items),
  listEvents: (id: number) => get<{ items: GroupEvent[] }>(`/groups/${id}/events`).then((r) => r.items),
  getYAML: (id: number) => get<{ items: RenderedResource[] }>(`/groups/${id}/yaml`).then((r) => r.items),
  runtime: (id: number) => get<Record<string, any>>(`/groups/${id}/runtime`),
};

export interface GroupEvent {
  type: string;
  reason: string;
  message: string;
  count: number;
  last_time: string;
  first_time: string;
}

export interface RenderedResource {
  kind: string;
  name: string;
  yaml: string;
}
