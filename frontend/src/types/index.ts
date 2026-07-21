// Common types shared across the frontend. Mirrors backend DTOs.

export interface ApiEnvelope<T> {
  success: boolean;
  data?: T;
  error?: { code: string; message: string };
}

export interface Paged<T> {
  items: T[];
  total: number;
  page: number;
  size: number;
}

export interface User {
  id: number;
  uuid: string;
  username: string;
  email: string;
  phone: string;
  display_name: string;
  avatar_url: string;
  auth_source: string;
  status: string;
  locale: string;
  timezone: string;
  mfa_enabled: boolean;
  version: number;
  created_at: string;
}

export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_at: string;
  user?: User;
}

export type MenuType = 'directory' | 'menu' | 'button' | 'link';
export type MenuScope = 'platform' | 'workspace' | 'application';

export interface Menu {
  id: number;
  uuid: string;
  parent_id: number;
  code: string;
  name: string;
  name_en?: string;
  path?: string;
  icon?: string;
  component?: string;
  menu_type: string;
  scope: string;
  permission_code?: string;
  visible: boolean;
  sort_order: number;
  keep_alive: boolean;
  external_link?: string;
  version: number;
  created_at: string;
  children?: Menu[];
}

export interface Permission {
  id: number;
  code: string;
  name: string;
  category: string;
  scope: string;
  description?: string;
  sort_order: number;
  enabled: boolean;
  version: number;
  created_at: string;
}

export interface Role {
  id: number;
  uuid: string;
  code: string;
  name: string;
  scope: string;
  description?: string;
  is_builtin: boolean;
  is_system: boolean;
  enabled: boolean;
  sort_order: number;
  metadata?: Record<string, any>;
  version: number;
  created_at: string;
}

export type WorkspaceStatus = 'active' | 'archived';
export interface Workspace {
  id: number;
  uuid: string;
  name: string;
  display_name: string;
  description?: string;
  logo_url?: string;
  owner_id: number;
  owner_name?: string;
  status: string;
  default_registry_id?: number;
  default_jenkins_id?: number;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  max_applications: number;
  max_groups: number;
  max_members: number;
  application_count?: number;
  group_count?: number;
  member_count?: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface WorkspaceMember {
  id: number;
  workspace_id: number;
  user_id: number;
  invited_by?: number;
  role_id: number;
  role_code?: string;
  role_name?: string;
  joined_at: string;
  status?: string;
  version?: number;
  username?: string;
  display_name?: string;
  avatar_url?: string;
}

export interface ClusterBinding {
  id: number;
  workspace_id: number;
  cluster_id: number;
  cluster_name: string;
  cluster_uuid?: string;
  namespace: string;
  role: string;
  resource_quota?: Record<string, any>;
  version: number;
  created_at: string;
}

export interface Cluster {
  id: number;
  uuid: string;
  name: string;
  display_name?: string;
  description?: string;
  kubeconfig_credential_id?: number;
  api_server?: string;
  api_server_url?: string;
  default_namespace_prefix?: string;
  insecure_skip_tls?: boolean;
  environment?: string;
  k8s_version?: string;
  region?: string;
  zone?: string;
  provider?: string;
  status: string;
  health_status?: string;
  node_count?: number;
  cpu_total?: string;
  memory_total?: string;
  gpu_total?: string;
  allocatable_cpu_m?: number;
  allocatable_memory_bytes?: number;
  allocatable_gpu?: number;
  capacity_synced_at?: string;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  version?: number;
  version_col: number;
  created_at: string;
  updated_at: string;
}

export interface Credential {
  id: number;
  uuid: string;
  name: string;
  kind: string;
  type: string;
  scope?: string;
  scope_id?: number;
  description?: string;
  expires_at?: string;
  last_rotated_at?: string;
  version?: number;
  created_at: string;
}

// 应用探活方式。
export type ProbeMethod = 'tcp' | 'process' | 'both';

// 应用探活配置（应用维度，对该应用下所有分组生效）。
// 通过监听 Pod 端口（如 8080）或进程关键字（如 java）决定应用状态。
export interface ProbeConfig {
  enabled: boolean;
  method?: ProbeMethod;
  // TCP 探活端口（如 8080）。method=tcp/both 时必填。
  port?: number;
  // 进程关键字（如 java / nginx）。method=process/both 时必填。
  process_keyword?: string;
  // 以下时间为后端/K8s 默认值，UI 不再配置（兼容历史数据）。
  timeout_seconds?: number;
  period_seconds?: number;
  failure_threshold?: number;
}

export interface Application {
  id: number;
  uuid: string;
  workspace_id: number;
  name: string;
  code?: string;
  display_name: string;
  description?: string;
  app_type?: string;
  workload_type?: string;
  git_url?: string;
  default_branch?: string;
  language?: string;
  owner_id?: number;
  lifecycle?: string;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  probe?: ProbeConfig | null;
  group_count?: number;
  member_count?: number;
  version: number;
  created_at: string;
  updated_at: string;
}

/** 是否由开放 API 外部托管（中间件团队等）。 */
export function isExternalManaged(app: Application | undefined | null): boolean {
  return app?.metadata?.managed_by === 'ext_api';
}

export type WorkloadType = 'deployment' | 'statefulset' | 'cronjob' | 'job';

export interface ApplicationMember {
  id: number;
  application_id: number;
  user_id: number;
  role_id: number;
  username?: string;
  display_name?: string;
  email?: string;
  role_name?: string;
  invited_by?: number;
  joined_at: string;
  status: string;
  version: number;
}

export interface Group {
  id: number;
  uuid: string;
  application_id: number;
  workspace_id?: number;
  name: string;
  display_name: string;
  description?: string;
  app_type?: string;
  environment: string;
  cluster_id: number;
  cluster_name?: string;
  namespace: string;
  resources: {
    cpu_m: number;
    cpu_limit_m?: number;
    memory_bytes: number;
    memory_limit_bytes?: number;
    gpu?: number;
    gpu_type?: string;
    gpu_resource_name?: string;
  };
  storage?: {
    storage_size_bytes?: number;
    storage_class?: string;
    ephemeral_storage_request_bytes?: number;
    ephemeral_storage_limit_bytes?: number;
    resource_template_id?: number;
  };
  /** 分组维度是否启用 Mesh（Cilium L7 治理），默认 false；Phase 5 生效 */
  mesh_enabled?: boolean;
  scheduling?: {
    node_selector?: Record<string, string>;
    node_affinity?: Record<string, any>;
    tolerations?: Array<Record<string, any>>;
    priority_class?: string;
  };
  workload: {
    type: WorkloadType;
    cron_schedule?: string;
    job_policy?: Record<string, any>;
    strategy?: string;
    max_surge?: string;
    max_unavailable?: string;
    replicas: number;
    image_ref?: string;
    command?: string[];
    args?: string[];
  };
  health_check?: {
    liveness_probe?: Record<string, any>;
    readiness_probe?: Record<string, any>;
    startup_probe?: Record<string, any>;
  };
  autoscaling?: {
    enabled: boolean;
    min_replicas?: number;
    max_replicas?: number;
    metrics?: Array<Record<string, any>>;
    behavior?: Record<string, any>;
  };
  release_requires_approval?: boolean;
  config_version?: number;
  current_release_id?: number;
  current_image_id?: number;
  current_config_id?: number;
  current_image_ref?: string;
  // 多版本共存（candidate Deployment 模式）：发布进行中时记录候选版本。
  candidate_image_id?: number;
  candidate_release_id?: number;
  candidate_replicas?: number;
  deployment_name?: string;
  service_name?: string;
  status?: string;
  labels?: Record<string, string>;
  metadata?: Record<string, any>;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface PodSummary {
  name: string;
  namespace: string;
  status: string;
  phase: string;
  node_name?: string;
  pod_ip?: string;
  host_ip?: string;
  restart_count?: number;
  started_at?: string;
  age_seconds?: number;
  ready: boolean;
  containers?: Array<{ name: string; ready: boolean; started?: boolean; restart_count: number; image: string }>;
  /** 应用探活主动拨测结果；null/undefined 表示未配置探活。 */
  app_ready?: boolean | null;
  /** 未就绪时的简要原因。 */
  app_ready_detail?: string;
}

export interface GitSource {
  id: number;
  uuid: string;
  application_id: number;
  name: string;
  provider: string;
  repo_url: string;
  default_branch: string;
  credential_id?: number;
  webhook_secret?: string;
  enabled: boolean;
  version: number;
  created_at: string;
}

export interface GitRef {
  name: string;
  type: string;
}

export interface GitCommit {
  sha: string;
  message?: string;
  author?: string;
  date?: string;
}

export type BuildStatus = 'pending' | 'running' | 'success' | 'failed' | 'cancelled';
export interface Build {
  id: number;
  uuid: string;
  application_id: number;
  build_number?: number;
  group_id?: number;
  git_source_id?: number;
  ref_type?: string;
  ref_value?: string;
  branch: string;
  commit_sha?: string;
  commit_message?: string;
  build_template_id?: number;
  build_strategy?: string;
  build_command?: string;
  build_tool?: string;
  builder_image?: string;
  context_path?: string;
  artifact_path?: string;
  dockerfile_path?: string;
  base_image_id?: number;
  dockerfile_source?: string;
  dockerfile_content?: string;
  build_args?: Record<string, string>;
  target_registry_id?: number;
  target_repository?: string;
  target_tag?: string;
  output_image_id?: number;
  registry_id?: number;
  image_repository?: string;
  image_tag?: string;
  status: BuildStatus;
  triggered_by: number;
  triggered_by_name?: string;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  error_message?: string;
  failure_reason?: string;
  pipeline_run_name?: string;
  version: number;
  created_at: string;
  updated_at?: string;
}

export interface BuildStep {
  id: number;
  build_id: number;
  step_name: string;
  status: string;
  started_at?: string;
  finished_at?: string;
  duration_ms?: number;
  log_excerpt?: string;
  sequence: number;
}

export interface BuildLog {
  sequence: number;
  step?: string;
  timestamp: string;
  stream: string;
  message: string;
}

export interface Registry {
  id: number;
  uuid: string;
  name: string;
  type: string;
  endpoint: string;
  url?: string;
  username?: string;
  credential_id?: number;
  is_default: boolean;
  status?: string;
  version: number;
  created_at: string;
}

export interface BaseImage {
  id: number;
  uuid: string;
  name: string;
  runtime: string;
  image_ref: string;
  description?: string;
  dockerfile_template?: string;
  build_tool?: string;
  default_build_command?: string;
  default_artifact_path?: string;
  default_build_args?: Record<string, string>;
  /** 运行时启动命令（JSON 数组，如 ["java","-jar","/app/app.jar"]）。空数组表示用基础镜像 CMD。 */
  entrypoint?: string[];
  /** Web 镜像：除应用启动命令外额外启动 nginx（渲染 Dockerfile 时自动包装）。 */
  is_web?: boolean;
  is_recommended: boolean;
  version: number;
  created_at: string;
}

export interface BuildTool {
  id: number;
  uuid: string;
  name: string;
  runtime: string;
  tool: string;
  default_build_command?: string;
  default_artifact_path?: string;
  builder_image: string;
  is_system: boolean;
  description?: string;
  version: number;
  created_at: string;
}

export interface BuildTemplate {
  id: number;
  uuid: string;
  name: string;
  description?: string;
  build_type: string;
  base_image_id?: number;
  dockerfile_template?: string;
  build_args?: Record<string, string>;
  parameters_schema?: Record<string, any>;
  is_recommended: boolean;
  version: number;
  created_at: string;
}

export interface Image {
  id: number;
  uuid: string;
  application_id: number;
  registry_id: number;
  repository: string;
  tag: string;
  digest?: string;
  size_bytes?: number;
  version_number?: number;
  version_label?: string;
  full_reference?: string;
  source?: string;
  build_id?: number;
  git_commit?: string;
  git_branch?: string;
  git_commit_message?: string;
  status?: string;
  scan_status?: string;
  scan_result?: Record<string, any>;
  labels?: Record<string, string>;
  retired: boolean;
  version: number;
  created_at: string;
}

export interface ImageTag {
  id: number;
  uuid: string;
  image_id: number;
  tag: string;
  alias?: string;
  is_alias: boolean;
  pinned: boolean;
  version: number;
  created_at: string;
}

export interface JenkinsInstance {
  id: number;
  uuid: string;
  name: string;
  endpoint: string;
  url?: string;
  username?: string;
  credential_id?: number;
  default_job_folder?: string;
  is_default: boolean;
  status?: string;
  version: number;
  created_at: string;
}

export type ReleaseStrategy = 'rolling' | 'recreate' | 'blue_green' | 'canary' | 'percentage' | 'machine_count';
export type ReleaseStatus =
  | 'pending'
  | 'pending_approval'
  | 'running'
  | 'paused'
  | 'succeeded'
  | 'failed'
  | 'aborted'
  | 'interrupted'
  | 'rolled_back';

export interface Release {
  id: number;
  uuid: string;
  group_id: number;
  release_number: number;
  previous_release_id?: number;
  image_id?: number;
  image_ref?: string;
  config_version?: number;
  release_type: string;
  replicas: number;
  strategy: ReleaseStrategy;
  max_surge?: string;
  max_unavailable?: string;
  batch_size?: number;
  batch_interval_sec?: number;
  target_percentage?: number;
  target_pod_names?: string[];
  paused: boolean;
  status: ReleaseStatus;
  progress_percent: number;
  failure_reason?: string;
  started_at: string;
  finished_at?: string;
  duration_ms?: number;
  triggered_by: number;
  triggered_by_name?: string;
  trigger_source?: string;
  auto_rollback_on_failure: boolean;
  rollback_of_release_id?: number;
  version: number;
  created_at: string;
}

// TriggerReleaseInput 触发发布输入（与后端 releaseapp.TriggerReleaseInput 对应）。
export interface TriggerReleaseInput {
  group_id: number;
  image_id: number;
  config_version?: number;
  release_type?: string;
  replicas?: number;
  strategy: ReleaseStrategy;
  max_surge?: string;
  max_unavailable?: string;
  batch_size?: number;
  batch_interval_sec?: number;
  auto_rollback_on_failure?: boolean;
  target_percentage?: number;
  target_pod_names?: string[];
}

// 文件浏览器条目。
export interface PodFileEntry {
  name: string;
  size: number;
  mode: string;
  mod_time: string;
  is_dir: boolean;
}

export interface ReleaseEvent {
  id: number;
  release_id: number;
  event_type: string;
  message: string;
  level: string;
  occurred_at: string;
  metadata?: Record<string, any>;
}

export interface ReleasePreset {
  id: number;
  uuid: string;
  name: string;
  description?: string;
  strategy: ReleaseStrategy;
  max_surge?: string;
  max_unavailable?: string;
  auto_rollback_on_failure: boolean;
  version: number;
  created_at: string;
}

export interface ReleaseWindow {
  id: number;
  uuid: string;
  application_id: number;
  name: string;
  days_of_week: number[];
  start_time: string;
  end_time: string;
  timezone: string;
  enforce: boolean;
  version: number;
  created_at: string;
}

export interface ReleaseOrchestration {
  id: number;
  uuid: string;
  workspace_id: number;
  application_id: number;
  name: string;
  strategy: string;
  status: string;
  progress_percent: number;
  image_id: number;
  replicas: number;
  batch_size: number;
  batch_interval_sec: number;
  failure_reason: string;
  started_at?: string;
  finished_at?: string;
  duration_ms: number;
  triggered_by: number;
  created_at: string;
}

export interface OrchestrationTarget {
  id: number;
  group_id: number;
  cluster_id: number;
  image_id: number;
  replicas: number;
  seq: number;
  batch_size: number;
  release_id: number;
  status: string;
  failure_reason: string;
  started_at?: string;
  finished_at?: string;
}

export interface OrchestrationDetail {
  orchestration: ReleaseOrchestration;
  targets: OrchestrationTarget[];
}

export interface ConfigItem {
  id: number;
  uuid: string;
  group_id?: number;
  workspace_id?: number;
  name: string;
  description?: string;
  config_type: string;
  current_version: number;
  archived: boolean;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface ConfigVersion {
  id: number;
  config_id: number;
  version: number;
  change_summary?: string;
  files?: Array<{
    path: string;
    content: string;
    mode?: string;
    is_secret?: boolean;
  }>;
  command?: string[];
  args?: string[];
  env?: Array<{ name: string; value: string; is_secret?: boolean }>;
  env_from?: string[];
  resources?: Record<string, any>;
  network?: Record<string, any>;
  created_by: number;
  created_by_name?: string;
  created_at: string;
}

export interface ConfigSet {
  id: number;
  uuid: string;
  workspace_id: number;
  application_id?: number;
  name: string;
  description?: string;
  scope: string;
  merge_strategy: string;
  current_version: number;
  content?: Record<string, any>;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface ConfigBinding {
  id: number;
  uuid: string;
  group_id: number;
  config_id?: number;
  config_set_id: number;
  config_set_name?: string;
  priority: number;
  pinned_version?: number;
  mount_path?: string;
  sub_path?: string;
  version: number;
  created_at: string;
}

// 分组本地配置（与配置集绑定互斥）。
// Content 结构与 ConfigSet.content 同构：{files, env, command, args}。
export interface GroupLocalConfig {
  id: number;
  uuid: string;
  group_id: number;
  name: string;
  description?: string;
  content?: Record<string, any>;
  version: number;
  created_at: string;
  updated_at: string;
}

/** 配置内容历史快照（files 变更或绑定/解绑时生成）。 */
export interface ConfigContentSnapshot {
  id: number;
  target_type: 'config_set' | 'group_local' | 'group_bind';
  target_id: number;
  snapshot_no: number;
  change_reason: string;
  files_hash?: string;
  file_count: number;
  created_at: string;
}

export interface ConfigFileDiffResult {
  file_path: string;
  original: string;
  modified: string;
  language: string;
}


export interface Pipeline {
  id: number;
  uuid: string;
  workspace_id: number;
  application_id?: number;
  name: string;
  description?: string;
  trigger_mode: string;
  stages?: Array<Record<string, any>>;
  enabled: boolean;
  version: number;
  created_at: string;
  updated_at: string;
}

export type PipelineRunStatus =
  | 'pending'
  | 'running'
  | 'success'
  | 'failed'
  | 'cancelled'
  | 'paused';

export interface PipelineRun {
  id: number;
  uuid: string;
  pipeline_id: number;
  run_number: number;
  status: PipelineRunStatus;
  trigger_source?: string;
  triggered_by: number;
  triggered_by_name?: string;
  commit_sha?: string;
  started_at: string;
  finished_at?: string;
  duration_ms?: number;
  stages?: Array<Record<string, any>>;
  version: number;
  created_at: string;
}

export interface Promotion {
  id: number;
  uuid: string;
  pipeline_id: number;
  from_environment: string;
  to_environment: string;
  artifact_ref: string;
  status: string;
  version: number;
  created_at: string;
}

export interface ArtifactSignature {
  id: number;
  uuid: string;
  image_id: number;
  signer: string;
  signature_type: string;
  verified: boolean;
  payload?: Record<string, any>;
  version: number;
  created_at: string;
}

export interface ModelRegistry {
  id: number;
  uuid: string;
  workspace_id: number;
  name: string;
  provider: string;
  endpoint?: string;
  credential_id?: number;
  cache_pvc_name?: string;
  cache_path?: string;
  cache_size_bytes?: number;
  status: string;
  version_col: number;
  created_at: string;
}

export interface Model {
  id: number;
  uuid: string;
  workspace_id: number;
  registry_id: number;
  name: string;
  display_name?: string;
  description?: string;
  base_architecture?: string;
  parameter_count?: string;
  license?: string;
  tags?: string[];
  version_col: number;
  created_at: string;
}

export interface ModelVersion {
  id: number;
  uuid: string;
  model_id: number;
  version: string;
  precision: string;
  quantization?: string;
  weights_path?: string;
  weights_size_bytes?: number;
  weights_checksum?: string;
  framework: string;
  framework_config?: Record<string, any>;
  min_gpu_memory_bytes?: number;
  recommended_gpu_count?: number;
  download_status: string;
  download_progress: number;
  is_default: boolean;
  version_col: number;
  created_at: string;
}

export interface ModelAdapter {
  id: number;
  uuid: string;
  base_model_version_id: number;
  name: string;
  adapter_type: string;
  weights_path?: string;
  rank?: number;
  scale?: number;
  version_col: number;
  created_at: string;
}

export interface InferenceService {
  id: number;
  uuid: string;
  workspace_id: number;
  application_id?: number;
  group_id?: number;
  name: string;
  display_name?: string;
  description?: string;
  cluster_id: number;
  namespace: string;
  workload_name?: string;
  service_name?: string;
  base_model_version_id: number;
  adapter_ids?: number[];
  framework: string;
  framework_config?: Record<string, any>;
  replicas: number;
  resources?: Record<string, any>;
  gpu_count: number;
  gpu_type?: string;
  tensor_parallel_size: number;
  pipeline_parallel_size: number;
  storage_size_bytes?: number;
  current_release_id?: number;
  current_status: string;
  readiness: string;
  autoscaling_enabled: boolean;
  hpa_min_replicas?: number;
  hpa_max_replicas?: number;
  hpa_metrics?: Record<string, any>;
  access_mode: string;
  external_endpoint?: string;
  labels?: Record<string, any>;
  metadata?: Record<string, any>;
  version_col: number;
  created_at: string;
  updated_at: string;
}

export interface InferenceRelease {
  id: number;
  uuid: string;
  inference_service_id: number;
  release_number: number;
  previous_release_id?: number;
  target_model_version_id: number;
  target_adapter_ids?: number[];
  strategy: string;
  replicas: number;
  status: string;
  progress_percent: number;
  failure_reason?: string;
  started_by: number;
  started_at: string;
  finished_at?: string;
  duration_ms?: number;
  version: number;
  created_at: string;
}

export interface InferenceAPIKey {
  id: number;
  uuid: string;
  inference_service_id: number;
  name: string;
  key_prefix: string;
  daily_token_quota?: number;
  rate_limit_per_min?: number;
  expires_at?: string;
  last_used_at?: string;
  status: string;
  version: number;
  created_at: string;
  plaintext?: string;
}

export interface InferenceRoute {
  id: number;
  uuid: string;
  workspace_id: number;
  name: string;
  description?: string;
  strategy: string;
  rules?: Record<string, any>;
  default_service_id?: number;
  status: string;
  version_col: number;
  created_at: string;
}

export interface InferenceUsage {
  id: number;
  uuid: string;
  inference_service_id: number;
  api_key_id?: number;
  caller_id?: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  duration_ms?: number;
  status_code?: number;
  model_version_id?: number;
  created_at: string;
}

export interface InferenceUsageSummary {
  service_id: number;
  total_requests: number;
  total_prompt_tokens: number;
  total_completion_tokens: number;
  total_tokens: number;
  avg_duration_ms: number;
}

export interface ExternalToken {
  id: number;
  uuid: string;
  user_id: number;
  name: string;
  token_prefix: string;
  scopes: string[];
  allowed_workspaces?: number[];
  allowed_apps?: number[];
  rate_limit_per_min?: number;
  ip_allowlist?: string[];
  webhook_url?: string;
  token_type: string;
  expires_at?: string;
  last_used_at?: string;
  last_used_ip?: string;
  status: string;
  version: number;
  created_at: string;
  plaintext?: string;
}

export interface ExternalCallLog {
  id: number;
  uuid: string;
  token_id?: number;
  token_prefix?: string;
  method: string;
  path: string;
  operation?: string;
  workspace_id?: number;
  resource_type?: string;
  resource_uuid?: string;
  request_id?: string;
  status_code?: number;
  duration_ms?: number;
  client_ip?: string;
  user_agent?: string;
  error_message?: string;
  created_at: string;
}

export interface AuditLog {
  id: number;
  uuid: string;
  user_id?: number;
  user_name?: string;
  workspace_id?: number;
  resource_type: string;
  resource_id?: number;
  resource_name?: string;
  action: string;
  operation?: string;
  request_id?: string;
  method?: string;
  path?: string;
  status_code?: number;
  client_ip?: string;
  user_agent?: string;
  request_body?: Record<string, any>;
  response_summary?: Record<string, any>;
  duration_ms?: number;
  error_message?: string;
  created_at: string;
}

export interface Notification {
  id: number;
  uuid: string;
  user_id: number;
  type: string;
  title: string;
  content?: string;
  level: string;
  read: boolean;
  resource_type?: string;
  resource_id?: number;
  link?: string;
  payload?: Record<string, any>;
  created_at: string;
}

export interface AlertRule {
  id: number;
  uuid: string;
  scope: string;
  scope_id?: number;
  name: string;
  description?: string;
  metric: string;
  condition: string;
  threshold?: number;
  window_minutes: number;
  severity: string;
  enabled: boolean;
  notify_channels?: number[];
  cooldown_minutes: number;
  version: number;
  created_at: string;
}

export interface AlertEvent {
  id: number;
  uuid: string;
  rule_id: number;
  scope: string;
  scope_id?: number;
  resource_type?: string;
  resource_id?: number;
  severity: string;
  status: string;
  message?: string;
  current_value?: number;
  fired_at: string;
  resolved_at?: string;
  notified_count: number;
  version: number;
  created_at: string;
}

export interface ExecSession {
  id: string;
  cluster_id: number;
  namespace: string;
  pod: string;
  container?: string;
  command?: string[];
  status: string;
  created_at: string;
}

export interface ApiErrorShape {
  code: string;
  message: string;
  httpStatus: number;
  details?: any;
}

// ---------- Bastion (JumpServer) ----------
export interface BastionAsset {
  id: number;
  uuid: string;
  workspace_id: number;
  name: string;
  host: string;
  port: number;
  protocol: 'ssh' | 'rdp';
  platform: string;
  username: string;
  credential_id: number;
  jms_asset_id: string;
  tags: string[];
  comment: string;
  is_active: boolean;
  created_at: string;
}

export interface BastionSession {
  id: number;
  uuid: string;
  workspace_id: number;
  asset_id: number;
  username: string;
  asset_name: string;
  protocol: 'ssh' | 'rdp';
  remote_addr: string;
  login_from: string;
  status: 'active' | 'closed';
  started_at?: string;
  ended_at?: string;
  duration_ms: number;
  command_count: number;
}

// ---------- Cluster Ops（监控/运维/通知）----------

export type ClusterOperationType = 'restart' | 'drain' | 'cordon' | 'uncordon' | 'sync_status';
export type ClusterOperationStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
export type ClusterNodeHealth = 'ready' | 'not_ready' | 'unknown';

export interface ClusterNodeStatus {
  id: number;
  uuid: string;
  cluster_id: number;
  node_name: string;
  status: ClusterNodeHealth;
  unschedulable: boolean;
  kubelet_version?: string;
  allocatable_cpu_m: number;
  allocatable_memory_bytes: number;
  allocatable_gpu: number;
  used_cpu_m: number;
  used_memory_bytes: number;
  used_gpu: number;
  pod_count: number;
  abnormal_pod_count: number;
  roles: string[];
  taints: Array<{ key: string; value: string; effect: string }>;
  addresses: Array<{ type: string; address: string }>;
  last_synced_at?: string;
}

export interface ClusterOperation {
  id: number;
  uuid: string;
  cluster_id: number;
  node_name?: string;
  operation_type: ClusterOperationType;
  scheduled_at: string;
  status: ClusterOperationStatus;
  executed_at?: string;
  completed_at?: string;
  error_message?: string;
  notify_affected: boolean;
  notified_user_ids: number[];
  created_at: string;
}

export interface AbnormalPod {
  name: string;
  namespace: string;
  node_name: string;
  phase: string;
  restart_count: number;
  ready: boolean;
  reason?: string;
  message?: string;
  application_id?: number;
  application_name?: string;
  group_name?: string;
}

export interface AbnormalNode {
  node_name: string;
  status: ClusterNodeHealth;
  unschedulable: boolean;
  abnormal_pod_count: number;
  pod_count: number;
  addresses: Array<{ type: string; address: string }>;
  taints: Array<{ key: string; value: string; effect: string }>;
  last_synced_at?: string;
}

export interface AffectedMember {
  user_id: number;
  user_name: string;
  display_name: string;
  email: string;
  role_name: string;
}

export interface AffectedApp {
  application_id: number;
  application_name: string;
  group_names: string[];
  members: AffectedMember[];
}

export interface NotifyResult {
  affected_apps: AffectedApp[];
  notified_user_ids: number[];
  total_notified: number;
}

export interface NotifyAffectedInput {
  scope: 'pod' | 'node' | 'cluster';
  node_name?: string;
  pod_namespace?: string;
  pod_name?: string;
  subject?: string;
  body?: string;
}

export interface CreateClusterOperationInput {
  node_name?: string;
  operation_type: ClusterOperationType;
  scheduled_at?: string;
  notify_affected: boolean;
}

// ---------- 节点/Pod 指标采样（趋势图）----------

export type MetricRange = '1h' | '6h' | '24h' | '7d';

export interface NodeMetricSample {
  id: number;
  cluster_id: number;
  node_name: string;
  ts: string; // RFC3339
  // CPU
  cpu_usage_m: number;
  cpu_allocatable_m: number;
  // 内存
  mem_usage_bytes: number;
  mem_working_set_bytes: number;
  mem_available_bytes: number;
  mem_allocatable_bytes: number;
  // 磁盘
  fs_capacity_bytes: number;
  fs_used_bytes: number;
  fs_available_bytes: number;
  fs_inodes_total: number;
  fs_inodes_used: number;
  // 网络
  net_rx_bytes: number;
  net_tx_bytes: number;
  net_rx_bytes_per_sec: number;
  net_tx_bytes_per_sec: number;
  net_rx_errors: number;
  net_tx_errors: number;
  net_rx_dropped: number;
  net_tx_dropped: number;
  // 负载
  load1: number;
  load5: number;
  load15: number;
  // 派生百分比
  cpu_usage_pct: number;
  mem_usage_pct: number;
  fs_usage_pct: number;
  fs_inodes_pct: number;
}

export interface PodMetricSample {
  id: number;
  cluster_id: number;
  node_name: string; // Pod 所在节点
  namespace: string;
  pod_name: string;
  ts: string;
  cpu_usage_m: number;
  mem_usage_bytes: number;
  mem_working_set_bytes: number;
  net_rx_bytes: number;
  net_tx_bytes: number;
  net_rx_bytes_per_sec: number;
  net_tx_bytes_per_sec: number;
  restart_count: number;
  phase: string;
}
