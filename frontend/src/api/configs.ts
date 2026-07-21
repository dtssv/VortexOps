import { get, getPaged, post, put, del, ApiError } from './client';
import type {
  ConfigItem,
  ConfigVersion,
  ConfigSet,
  ConfigBinding,
  GroupLocalConfig,
  ConfigContentSnapshot,
  ConfigFileDiffResult,
  Paged,
} from '@/types';

export const configApi = {
  list: (params?: { workspace_id?: number; group_id?: number; page?: number; size?: number }) =>
    getPaged<ConfigItem>('/configs', params),
  get: (id: number) => get<ConfigItem>(`/configs/${id}`),
  create: (body: { group_id?: number; workspace_id?: number; name: string; description?: string; config_type: string }) =>
    post<ConfigItem>('/configs', body),
  archive: (id: number) => post<ConfigItem>(`/configs/${id}/archive`),
  diff: (params: { config_id: number; from_version: number; to_version: number }) =>
    get<Record<string, any>>('/configs/diff', params),
  diffCrossGroup: (params: { group_a: number; group_b: number; version_a?: number; version_b?: number }) =>
    get<Record<string, any>>('/configs/diff-cross-group', params),
  listVersions: (configId: number) => get<ConfigVersion[]>(`/configs/${configId}/versions`),
  createVersion: (configId: number, body: Partial<ConfigVersion>) =>
    post<ConfigVersion>(`/configs/${configId}/versions`, body),
  listConfigSets: (workspaceId: number) => get<ConfigSet[]>(`/workspaces/${workspaceId}/config-sets`),
  listAppConfigSets: (applicationId: number) => get<ConfigSet[]>(`/applications/${applicationId}/config-sets`),
  getConfigSet: (id: number) => get<ConfigSet>(`/config-sets/${id}`),
  createConfigSet: (workspaceId: number, body: Partial<ConfigSet>) =>
    post<ConfigSet>(`/workspaces/${workspaceId}/config-sets`, body),
  createAppConfigSet: (applicationId: number, body: Partial<ConfigSet>) =>
    post<ConfigSet>(`/applications/${applicationId}/config-sets`, body),
  updateConfigSet: (id: number, body: Partial<ConfigSet>) => put<ConfigSet>(`/config-sets/${id}`, body),
  deleteConfigSet: (id: number) => del(`/config-sets/${id}`),
  listBindings: (groupId: number) => get<ConfigBinding[]>(`/groups/${groupId}/config-bindings`),
  createBinding: (groupId: number, body: { config_set_id: number; priority: number; pinned_version?: number }) =>
    post<ConfigBinding>(`/groups/${groupId}/config-bindings`, body),
  deleteBinding: (id: number) => del(`/config-bindings/${id}`),
  // 分组本地配置（与配置集绑定互斥）。未创建时后端返回 not_found，按空配置处理。
  getLocalConfig: async (groupId: number): Promise<GroupLocalConfig | null> => {
    try {
      return await get<GroupLocalConfig>(`/groups/${groupId}/local-config`);
    } catch (e) {
      if (e instanceof ApiError && (e.code === 'not_found' || e.httpStatus === 404)) {
        return null;
      }
      throw e;
    }
  },
  upsertLocalConfig: (groupId: number, body: { name?: string; description?: string; content: Record<string, any>; version?: number }) =>
    put<GroupLocalConfig>(`/groups/${groupId}/local-config`, body),
  deleteLocalConfig: (groupId: number) => del(`/groups/${groupId}/local-config`),
  // 配置内容快照与 diff
  listConfigSetSnapshots: (configSetId: number) =>
    get<ConfigContentSnapshot[]>(`/config-sets/${configSetId}/snapshots`),
  listLocalConfigSnapshots: (groupId: number) =>
    get<ConfigContentSnapshot[]>(`/groups/${groupId}/local-config/snapshots`),
  listGroupBindSnapshots: (groupId: number) =>
    get<ConfigContentSnapshot[]>(`/groups/${groupId}/config-bind-snapshots`),
  diffConfigFile: (snapshotId: number, params: { file_path: string; target_type: string; target_id: number }) =>
    get<ConfigFileDiffResult>(`/config-snapshots/${snapshotId}/diff`, params),
  listGroupConfigFiles: (groupId: number) =>
    get<{ files: string[] }>(`/groups/${groupId}/config/files`).then((r) => r.files),
  cloneLocalConfigFromGroup: (
    groupId: number,
    body: {
      source_group_id: number;
      file_paths: string[];
      include_env?: boolean;
      include_command?: boolean;
      include_args?: boolean;
    },
  ) => post<GroupLocalConfig>(`/groups/${groupId}/local-config/clone-from`, body),
};
