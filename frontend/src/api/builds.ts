import { get, getPaged, post, put, del } from './client';
import type {
  Build,
  BuildStep,
  GitSource,
  GitRef,
  GitCommit,
  Registry,
  BaseImage,
  BuildTool,
  BuildTemplate,
  Image,
  ImageTag,
  JenkinsInstance,
  Credential,
  Paged,
} from '@/types';

// 构建集成配置（系统变量化默认 Jenkins + Registry）。
export interface BuildIntegration {
  jenkins?: JenkinsInstance | null;
  registry?: Registry | null;
}

// 开发语言选项：与后端 BaseImageRuntime 枚举对齐。
// 新建应用时选择语言，新建构建时按语言过滤基础镜像与构建工具列表。
export const LANGUAGE_OPTIONS = [
  { label: 'Java', value: 'java' },
  { label: 'Go', value: 'go' },
  { label: 'Python', value: 'python' },
  { label: 'Node.js', value: 'node' },
  { label: '其他', value: 'custom' },
] as const;

// 注：构建工具选项与默认命令/制品路径已迁移到 vo_build_tools 表（可配置化）。
// 前端通过 buildApi.listBuildTools 查询，不再使用硬编码常量。

// 触发构建入参：Jenkins/Registry 由系统默认实例决定，不再由前端传入。
export interface TriggerBuildInput {
  git_source_id?: number;
  ref_type?: string;
  ref_value: string;
  commit_sha?: string;
  commit_message?: string;
  build_template_id?: number;
  build_strategy?: string;
  build_command?: string;
  build_tool?: string;
  context_path?: string;
  artifact_path?: string;
  dockerfile_path?: string;
  base_image_id?: number;
  dockerfile_source?: string;
  dockerfile_content?: string;
  build_args?: Record<string, string>;
  target_repository?: string;
  target_tag?: string;
  trigger_source?: string;
  idempotency_key?: string;
  metadata?: Record<string, unknown>;
}

// 更新构建可编辑信息入参（全量字段，与新建构建对齐；仅终态构建可改）。
// 未传入的字段后端不变更；version 必传用于乐观锁。
export interface UpdateBuildInput {
  commit_message?: string;
  target_tag?: string;
  metadata?: Record<string, unknown>;
  ref_type?: string;
  ref_value?: string;
  git_source_id?: number;
  build_command?: string;
  build_tool?: string;
  context_path?: string;
  artifact_path?: string;
  dockerfile_path?: string;
  base_image_id?: number;
  dockerfile_source?: string;
  build_args?: Record<string, string>;
  target_repository?: string;
  version: number;
}

export const buildApi = {
  listGitSources: (applicationId: number) => get<GitSource[]>(`/applications/${applicationId}/git-sources`),
  createGitSource: (applicationId: number, body: Partial<GitSource>) =>
    post<GitSource>(`/applications/${applicationId}/git-sources`, body),
  deleteGitSource: (id: number) => del(`/git-sources/${id}`),
  list: (applicationId: number, params?: { page?: number; size?: number; status?: string }) =>
    getPaged<Build>(`/applications/${applicationId}/builds`, params),
  get: (id: number) => get<Build>(`/builds/${id}`),
  trigger: (applicationId: number, body: TriggerBuildInput) => post<Build>(`/applications/${applicationId}/builds`, body),
  rebuild: (id: number) => post<Build>(`/builds/${id}/rebuild`),
  cancel: (id: number) => post<Build>(`/builds/${id}/cancel`),
  update: (id: number, body: UpdateBuildInput) => put<Build>(`/builds/${id}`, body),
  remove: (id: number) => del<void>(`/builds/${id}`),
  listSteps: (id: number) => get<BuildStep[]>(`/builds/${id}/steps`),
  logs: (id: number, params?: { step?: string; from?: number; to?: number }) =>
    get<string>(`/builds/${id}/logs`, params),
  listRegistries: (params?: { page?: number; size?: number }) =>
    getPaged<Registry>('/registries', params),
  getRegistry: (id: number) => get<Registry>(`/registries/${id}`),
  createRegistry: (body: Partial<Registry>) => post<Registry>('/registries', body),
  updateRegistry: (id: number, body: Partial<Registry>) => put<Registry>(`/registries/${id}`, body),
  deleteRegistry: (id: number) => del(`/registries/${id}`),
  listJenkins: (params?: { page?: number; size?: number }) =>
    getPaged<JenkinsInstance>('/jenkins-instances', params),
  getJenkins: (id: number) => get<JenkinsInstance>(`/jenkins-instances/${id}`),
  createJenkins: (body: Partial<JenkinsInstance>) => post<JenkinsInstance>('/jenkins-instances', body),
  updateJenkins: (id: number, body: Partial<JenkinsInstance>) => put<JenkinsInstance>(`/jenkins-instances/${id}`, body),
  deleteJenkins: (id: number) => del(`/jenkins-instances/${id}`),
  // 构建集成（系统变量化）：应用详情页读取默认 Jenkins/Registry；系统设置页测试连接。
  getBuildIntegration: () => get<BuildIntegration>('/system-settings/build-integration'),
  testJenkinsConnection: (body: { id?: number; url?: string; credential_id?: number }) =>
    post<{ ok: boolean }>('/jenkins-instances/test', body),
  testRegistryConnection: (body: { id?: number; type?: string; url?: string; credential_id?: number }) =>
    post<{ ok: boolean }>('/registries/test', body),
  // 凭证管理（复用 /credentials 通用接口）。
  listCredentials: (params?: { kind?: string; scope?: string; page?: number; size?: number }) =>
    getPaged<Credential>('/credentials', params),
  createCredential: (body: {
    name: string;
    kind: string;
    scope: string;
    scope_id?: number;
    payload: Record<string, string>;
    description?: string;
  }) => {
    // 后端 Payload 字段为 []byte，JSON 反序列化期望 base64 字符串。
    // 这里把 {username, api_token} 之类的对象 JSON 化后再 base64 编码。
    const json = JSON.stringify(body.payload);
    const base64 =
      typeof btoa === 'function'
        ? btoa(unescape(encodeURIComponent(json)))
        : Buffer.from(json, 'utf-8').toString('base64');
    return post<Credential>('/credentials', {
      name: body.name,
      kind: body.kind,
      scope: body.scope,
      scope_id: body.scope_id,
      payload: base64,
    });
  },
  listBaseImages: (params?: { page?: number; size?: number; runtime?: string }) =>
    getPaged<BaseImage>('/base-images', params),
  getBaseImage: (id: number) => get<BaseImage>(`/base-images/${id}`),
  createBaseImage: (body: Partial<BaseImage>) => post<BaseImage>('/base-images', body),
  updateBaseImage: (id: number, body: Partial<BaseImage> & { version: number }) =>
    put<BaseImage>(`/base-images/${id}`, body),
  deleteBaseImage: (id: number) => del(`/base-images/${id}`),
  // 构建工具：可配置化的构建工具元数据（runtime+tool+command+artifactPath+builder_image）。
  listBuildTools: (params?: { page?: number; size?: number; runtime?: string }) =>
    getPaged<BuildTool>('/build-tools', params),
  getBuildTool: (id: number) => get<BuildTool>(`/build-tools/${id}`),
  createBuildTool: (body: Partial<BuildTool>) => post<BuildTool>('/build-tools', body),
  updateBuildTool: (id: number, body: Partial<BuildTool> & { version: number }) =>
    put<BuildTool>(`/build-tools/${id}`, body),
  deleteBuildTool: (id: number) => del(`/build-tools/${id}`),
  listTemplates: (params?: { page?: number; size?: number; scope?: string; scope_id?: number }) =>
    getPaged<BuildTemplate>('/build-templates', params),
  createTemplate: (body: Partial<BuildTemplate>) => post<BuildTemplate>('/build-templates', body),
  listImages: (applicationId: number, params?: { page?: number; size?: number }) =>
    getPaged<Image>(`/applications/${applicationId}/images`, params),
  listGitRefs: (applicationId: number, query?: string) =>
    get<{ items: GitRef[] }>(`/applications/${applicationId}/git/refs`, { query }).then((r) => r.items),
  getGitCommit: (applicationId: number, ref: string) =>
    get<GitCommit>(`/applications/${applicationId}/git/commit`, { ref }),
  getImage: (id: number) => get<Image>(`/images/${id}`),
  retireImage: (id: number) => post<Image>(`/images/${id}/retire`),
  listImageTags: (applicationId: number) => get<ImageTag[]>(`/applications/${applicationId}/image-tags`),
  createImageTag: (applicationId: number, body: Partial<ImageTag>) =>
    post<ImageTag>(`/applications/${applicationId}/image-tags`, body),
  updateImageTag: (id: number, body: Partial<ImageTag>) => post<ImageTag>(`/image-tags/${id}`, body),
  deleteImageTag: (id: number) => del(`/image-tags/${id}`),
};
