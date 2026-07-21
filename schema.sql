-- ============================================================================
-- VortexOps 数据库 schema（最终版，单一源）
-- 数据库: PostgreSQL 16+
-- 说明: 完整 schema，含扩展、枚举、表、索引、约束、分区、seed 数据。
--       本文件已合并历史迁移 0001-0015 的全部变更：所有列直接以最终形态定义在
--       CREATE TABLE 中，无 ALTER TABLE ADD COLUMN；seed 数据直接以最终值 INSERT，
--       无 UPDATE DML。仅保留少量 ALTER TABLE ADD CONSTRAINT 用于循环外键后置声明
--       （表间相互引用，无法在 CREATE TABLE 内联）。
-- 对应文档: docs/data-model.md
-- 初始化: psql -h <host> -U <user> -d <db> -f schema.sql
-- ============================================================================

-- ---------- 扩展 ----------
CREATE EXTENSION IF NOT EXISTS "pgcrypto";        -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "pg_trgm";         -- 模糊搜索
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";       -- uuid 工具（备用）

-- ============================================================================
-- 1. 平台基础
-- ============================================================================

-- ---------- 1.1 vo_users ----------
CREATE TABLE vo_users (
    id                     BIGSERIAL PRIMARY KEY,
    uuid                   UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    username               VARCHAR(64) NOT NULL UNIQUE,
    email                  VARCHAR(128) NOT NULL UNIQUE,
    phone                  VARCHAR(32),
    display_name           VARCHAR(128),
    avatar_url             VARCHAR(512),
    password_hash          VARCHAR(255),
    auth_source            VARCHAR(16) NOT NULL DEFAULT 'local',
    external_id            VARCHAR(128),
    status                 VARCHAR(16) NOT NULL DEFAULT 'active',
    last_login_at          TIMESTAMPTZ,
    last_login_ip          VARCHAR(64),
    password_changed_at    TIMESTAMPTZ,
    must_change_password   BOOLEAN NOT NULL DEFAULT false,
    locale                 VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
    timezone               VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    metadata               JSONB NOT NULL DEFAULT '{}',
    -- MFA (TOTP) 字段。mfa_secret 以 AES-256-GCM 密文存储，仅在 mfa_enabled=true 时有值。
    mfa_enabled            BOOLEAN NOT NULL DEFAULT false,
    mfa_secret             TEXT,
    mfa_backup_codes       JSONB NOT NULL DEFAULT '[]'::jsonb,
    version                INT NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by             BIGINT,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by             BIGINT,
    deleted                BOOLEAN NOT NULL DEFAULT false,
    deleted_at             TIMESTAMPTZ,
    deleted_by             BIGINT,
    CONSTRAINT users_status_chk CHECK (status IN ('active','disabled','locked')),
    CONSTRAINT users_auth_source_chk CHECK (auth_source IN ('local','oidc','ldap'))
);
CREATE INDEX idx_users_status ON vo_users (status) WHERE deleted = false;
CREATE INDEX idx_users_email  ON vo_users (email)  WHERE deleted = false;
COMMENT ON COLUMN vo_users.mfa_enabled IS '是否启用 MFA（TOTP）';
COMMENT ON COLUMN vo_users.mfa_secret IS 'TOTP 共享密钥（AES-256-GCM 密文），仅在 mfa_enabled=true 时有值';
COMMENT ON COLUMN vo_users.mfa_backup_codes IS '备份码列表（bcrypt 哈希），每条单次使用后移除';

-- ---------- 1.2 vo_refresh_tokens ----------
CREATE TABLE vo_refresh_tokens (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES vo_users(id),
    token_hash  VARCHAR(255) NOT NULL UNIQUE,
    device_id   VARCHAR(64),
    device_name VARCHAR(128),
    ip          VARCHAR(64),
    user_agent  VARCHAR(512),
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user ON vo_refresh_tokens (user_id);

-- ---------- 1.3 vo_api_tokens ----------
CREATE TABLE vo_api_tokens (
    id                    BIGSERIAL PRIMARY KEY,
    uuid                  UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id               BIGINT NOT NULL REFERENCES vo_users(id),
    name                  VARCHAR(128) NOT NULL,
    token_prefix          VARCHAR(16) NOT NULL,
    token_hash            VARCHAR(255) NOT NULL UNIQUE,
    scopes                JSONB NOT NULL DEFAULT '[]',
    allowed_workspaces    BIGINT[],
    allowed_apps          BIGINT[],
    rate_limit_per_min    INT,
    ip_allowlist          JSONB,
    webhook_url           VARCHAR(512),
    token_type            VARCHAR(16) NOT NULL DEFAULT 'personal',
    expires_at            TIMESTAMPTZ,
    last_used_at          TIMESTAMPTZ,
    last_used_ip          VARCHAR(64),
    status                VARCHAR(16) NOT NULL DEFAULT 'active',
    version               INT NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by            BIGINT,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by            BIGINT,
    deleted               BOOLEAN NOT NULL DEFAULT false,
    deleted_at            TIMESTAMPTZ,
    deleted_by            BIGINT,
    CONSTRAINT api_tokens_type_chk CHECK (token_type IN ('personal','service','external')),
    CONSTRAINT api_tokens_status_chk CHECK (status IN ('active','revoked'))
);
CREATE INDEX idx_api_tokens_user   ON vo_api_tokens (user_id) WHERE deleted = false;
CREATE INDEX idx_api_tokens_status ON vo_api_tokens (status) WHERE deleted = false;

-- ---------- 1.4 vo_user_preferences ----------
CREATE TABLE vo_user_preferences (
    id                     BIGSERIAL PRIMARY KEY,
    user_id                BIGINT NOT NULL REFERENCES vo_users(id) UNIQUE,
    theme                  VARCHAR(16) NOT NULL DEFAULT 'light',
    sidebar_collapsed      BOOLEAN NOT NULL DEFAULT false,
    default_workspace_id   BIGINT,
    table_page_size        INT NOT NULL DEFAULT 20,
    recent_resources       JSONB NOT NULL DEFAULT '[]',
    dashboard_layout       JSONB,
    notification_settings  JSONB,
    onboarded              BOOLEAN NOT NULL DEFAULT false,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT user_prefs_theme_chk CHECK (theme IN ('light','dark','system'))
);

-- ---------- 1.5 vo_sys_dictionaries ----------
CREATE TABLE vo_sys_dictionaries (
    id          BIGSERIAL PRIMARY KEY,
    category    VARCHAR(64) NOT NULL,
    code        VARCHAR(64) NOT NULL,
    label       VARCHAR(128) NOT NULL,
    label_en    VARCHAR(128),
    sort_order  INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    metadata    JSONB,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    UNIQUE (category, code)
);

-- ---------- 1.6 vo_system_settings ----------
CREATE TABLE vo_system_settings (
    id          BIGSERIAL PRIMARY KEY,
    key         VARCHAR(128) NOT NULL UNIQUE,
    value       JSONB NOT NULL,
    description TEXT,
    is_public   BOOLEAN NOT NULL DEFAULT false,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT
);

-- ---------- 1.7 vo_external_api_call_logs（分区表） ----------
CREATE TABLE vo_external_api_call_logs (
    id             BIGSERIAL,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid(),
    token_id       BIGINT REFERENCES vo_api_tokens(id),
    token_prefix   VARCHAR(16),
    method         VARCHAR(8) NOT NULL,
    path           VARCHAR(255) NOT NULL,
    operation      VARCHAR(64),
    workspace_id   BIGINT,
    resource_type  VARCHAR(32),
    resource_uuid  VARCHAR(64),
    request_id     VARCHAR(64),
    status_code    INT,
    duration_ms    INT,
    client_ip      VARCHAR(64),
    user_agent     VARCHAR(255),
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);
CREATE INDEX idx_ext_api_token    ON vo_external_api_call_logs (token_id, created_at DESC);
CREATE INDEX idx_ext_api_ws       ON vo_external_api_call_logs (workspace_id, created_at DESC);
CREATE INDEX idx_ext_api_op       ON vo_external_api_call_logs (operation, status_code);

-- ---------- 1.8 vo_workspace_creation_policies ----------
CREATE TABLE vo_workspace_creation_policies (
    id                          BIGSERIAL PRIMARY KEY,
    uuid                        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name                        VARCHAR(64) NOT NULL,
    applies_to_roles            JSONB NOT NULL DEFAULT '[]',
    allow_self_create           BOOLEAN NOT NULL DEFAULT true,
    max_workspaces_per_user     INT NOT NULL DEFAULT 5,
    default_quota               JSONB NOT NULL DEFAULT '{}',
    default_clusters            BIGINT[] NOT NULL DEFAULT '{}',
    require_approval            BOOLEAN NOT NULL DEFAULT false,
    approver_role               VARCHAR(64),
    auto_bind_catalog           BOOLEAN NOT NULL DEFAULT true,
    version                     INT NOT NULL DEFAULT 1,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by                  BIGINT,
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                  BIGINT,
    deleted                     BOOLEAN NOT NULL DEFAULT false,
    deleted_at                  TIMESTAMPTZ,
    deleted_by                  BIGINT
);

-- ============================================================================
-- 2. 权限与菜单
-- ============================================================================

-- ---------- 2.1 vo_permissions ----------
CREATE TABLE vo_permissions (
    id          BIGSERIAL PRIMARY KEY,
    code        VARCHAR(128) NOT NULL UNIQUE,
    name        VARCHAR(128) NOT NULL,
    category    VARCHAR(32) NOT NULL,
    scope       VARCHAR(16) NOT NULL,
    description TEXT,
    sort_order  INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    CONSTRAINT permissions_category_chk CHECK (category IN ('menu','action','data')),
    CONSTRAINT permissions_scope_chk    CHECK (scope IN ('platform','workspace','application'))
);

-- ---------- 2.2 vo_menus ----------
CREATE TABLE vo_menus (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    parent_id       BIGINT REFERENCES vo_menus(id),
    code            VARCHAR(64) NOT NULL UNIQUE,
    name            VARCHAR(128) NOT NULL,
    name_en         VARCHAR(128),
    path            VARCHAR(255),
    icon            VARCHAR(64),
    component       VARCHAR(255),
    menu_type       VARCHAR(16) NOT NULL DEFAULT 'menu',
    scope           VARCHAR(16) NOT NULL DEFAULT 'platform',
    permission_code VARCHAR(128) REFERENCES vo_permissions(code),
    visible         BOOLEAN NOT NULL DEFAULT true,
    sort_order      INT NOT NULL DEFAULT 0,
    keep_alive      BOOLEAN NOT NULL DEFAULT false,
    external_link   VARCHAR(512),
    metadata        JSONB,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT menus_type_chk  CHECK (menu_type IN ('directory','menu','button')),
    CONSTRAINT menus_scope_chk CHECK (scope IN ('platform','workspace','application'))
);
CREATE INDEX idx_menus_parent ON vo_menus (parent_id) WHERE deleted = false;

-- ---------- 2.3 vo_roles ----------
CREATE TABLE vo_roles (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope       VARCHAR(16) NOT NULL,
    scope_id    BIGINT,
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    description TEXT,
    is_builtin  BOOLEAN NOT NULL DEFAULT false,
    is_default  BOOLEAN NOT NULL DEFAULT false,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    metadata    JSONB,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    CONSTRAINT roles_scope_chk CHECK (scope IN ('platform','workspace','application')),
    UNIQUE (scope, scope_id, code)
);

-- ---------- 2.4 vo_role_permissions ----------
CREATE TABLE vo_role_permissions (
    id            BIGSERIAL PRIMARY KEY,
    role_id       BIGINT NOT NULL REFERENCES vo_roles(id),
    permission_id BIGINT NOT NULL REFERENCES vo_permissions(id),
    granted       BOOLEAN NOT NULL DEFAULT true,
    created_by    BIGINT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role_id, permission_id)
);
CREATE INDEX idx_role_permissions_role ON vo_role_permissions (role_id);
CREATE INDEX idx_role_permissions_perm ON vo_role_permissions (permission_id);

-- ---------- 2.4b vo_role_menus ----------
-- 角色-菜单直接绑定。/me/menus 解析时：permission_code 为空的菜单（分组目录）对所有登录用户可见；
-- 菜单通过本表直接绑定到用户任一角色 → 可见；菜单的 permission_code 命中用户权限集 → 可见。三者 OR。
CREATE TABLE vo_role_menus (
    id          BIGSERIAL PRIMARY KEY,
    role_id     BIGINT NOT NULL REFERENCES vo_roles(id),
    menu_id     BIGINT NOT NULL REFERENCES vo_menus(id),
    created_by  BIGINT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role_id, menu_id)
);
CREATE INDEX idx_role_menus_role ON vo_role_menus (role_id);
CREATE INDEX idx_role_menus_menu ON vo_role_menus (menu_id);

-- ---------- 2.5 vo_platform_role_bindings ----------
CREATE TABLE vo_platform_role_bindings (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES vo_users(id),
    role_id     BIGINT NOT NULL REFERENCES vo_roles(id),
    expires_at  TIMESTAMPTZ,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    UNIQUE (user_id, role_id)
);

-- ---------- 2.6 vo_workspace_members ----------
-- FK 到 vo_workspaces 见后置 ALTER（vo_workspaces 在 §3 创建）
CREATE TABLE vo_workspace_members (
    id            BIGSERIAL PRIMARY KEY,
    workspace_id  BIGINT NOT NULL,
    user_id       BIGINT NOT NULL REFERENCES vo_users(id),
    role_id       BIGINT NOT NULL REFERENCES vo_roles(id),
    invited_by    BIGINT REFERENCES vo_users(id),
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        VARCHAR(16) NOT NULL DEFAULT 'active',
    version       INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    BIGINT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    BIGINT,
    deleted       BOOLEAN NOT NULL DEFAULT false,
    deleted_at    TIMESTAMPTZ,
    deleted_by    BIGINT,
    CONSTRAINT ws_members_status_chk CHECK (status IN ('active','pending','removed'))
);
CREATE UNIQUE INDEX uk_ws_members ON vo_workspace_members (workspace_id, user_id) WHERE deleted = false;

-- ---------- 2.7 vo_application_members ----------
-- FK 到 vo_applications 见后置 ALTER
CREATE TABLE vo_application_members (
    id            BIGSERIAL PRIMARY KEY,
    application_id BIGINT NOT NULL,
    user_id       BIGINT NOT NULL REFERENCES vo_users(id),
    role_id       BIGINT NOT NULL REFERENCES vo_roles(id),
    invited_by    BIGINT REFERENCES vo_users(id),
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    status        VARCHAR(16) NOT NULL DEFAULT 'active',
    version       INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    BIGINT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    BIGINT,
    deleted       BOOLEAN NOT NULL DEFAULT false,
    deleted_at    TIMESTAMPTZ,
    deleted_by    BIGINT,
    CONSTRAINT app_members_status_chk CHECK (status IN ('active','pending','removed'))
);
CREATE UNIQUE INDEX uk_app_members ON vo_application_members (application_id, user_id) WHERE deleted = false;

-- ============================================================================
-- 3. 集群与基础设施
-- ============================================================================

-- ---------- 3.1 vo_clusters ----------
CREATE TABLE vo_clusters (
    id                       BIGSERIAL PRIMARY KEY,
    uuid                     UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name                     VARCHAR(64) NOT NULL UNIQUE,
    display_name             VARCHAR(128),
    description              TEXT,
    api_server               VARCHAR(255) NOT NULL,
    kubeconfig_encrypted     BYTEA,
    ca_cert_encrypted        BYTEA,
    default_namespace_prefix VARCHAR(64),
    insecure_skip_tls        BOOLEAN NOT NULL DEFAULT false,
    region                   VARCHAR(64),
    environment              VARCHAR(16),
    k8s_version              VARCHAR(32),
    node_count               INT,
    -- 集群容量（周期性同步，供调度与展示）。
    allocatable_cpu_m        INT NOT NULL DEFAULT 0,
    allocatable_memory_bytes BIGINT NOT NULL DEFAULT 0,
    allocatable_gpu          INT NOT NULL DEFAULT 0,
    capacity_synced_at       TIMESTAMPTZ,
    status                   VARCHAR(16) NOT NULL DEFAULT 'healthy',
    last_checked_at          TIMESTAMPTZ,
    last_error               TEXT,
    labels                   JSONB NOT NULL DEFAULT '{}',
    metadata                 JSONB NOT NULL DEFAULT '{}',
    version                  INT NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by               BIGINT,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by               BIGINT,
    deleted                  BOOLEAN NOT NULL DEFAULT false,
    deleted_at               TIMESTAMPTZ,
    deleted_by               BIGINT,
    CONSTRAINT clusters_status_chk CHECK (status IN ('healthy','degraded','unreachable','disabled'))
);

-- ---------- 3.2 vo_credentials ----------
-- 先建：被 vo_registries / vo_jenkins_instances / vo_git_sources / vo_model_registries 引用
CREATE TABLE vo_credentials (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name              VARCHAR(128) NOT NULL,
    kind              VARCHAR(32) NOT NULL,
    scope             VARCHAR(16) NOT NULL DEFAULT 'platform',
    scope_id          BIGINT,
    payload_encrypted BYTEA NOT NULL,
    expires_at        TIMESTAMPTZ,
    last_rotated_at   TIMESTAMPTZ,
    version           INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT creds_kind_chk  CHECK (kind IN ('git_password','git_ssh','git_token','registry','kubeconfig','jenkins')),
    CONSTRAINT creds_scope_chk CHECK (scope IN ('platform','workspace'))
);

-- ---------- 3.3 vo_registries ----------
CREATE TABLE vo_registries (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name          VARCHAR(64) NOT NULL UNIQUE,
    type          VARCHAR(16) NOT NULL,
    url           VARCHAR(255) NOT NULL,
    credential_id BIGINT REFERENCES vo_credentials(id),
    is_default    BOOLEAN NOT NULL DEFAULT false,
    status        VARCHAR(16) NOT NULL DEFAULT 'active',
    version       INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    BIGINT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    BIGINT,
    deleted       BOOLEAN NOT NULL DEFAULT false,
    deleted_at    TIMESTAMPTZ,
    deleted_by    BIGINT,
    CONSTRAINT registries_type_chk   CHECK (type IN ('harbor','docker_registry','acr','ecr')),
    CONSTRAINT registries_status_chk CHECK (status IN ('active','disabled'))
);

-- ---------- 3.4 vo_jenkins_instances ----------
CREATE TABLE vo_jenkins_instances (
    id                 BIGSERIAL PRIMARY KEY,
    uuid               UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name               VARCHAR(64) NOT NULL UNIQUE,
    url                VARCHAR(255) NOT NULL,
    credential_id      BIGINT REFERENCES vo_credentials(id),
    default_job_folder VARCHAR(128),
    is_default         BOOLEAN NOT NULL DEFAULT true,
    status             VARCHAR(16) NOT NULL DEFAULT 'active',
    last_checked_at    TIMESTAMPTZ,
    version            INT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by         BIGINT,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         BIGINT,
    deleted            BOOLEAN NOT NULL DEFAULT false,
    deleted_at         TIMESTAMPTZ,
    deleted_by         BIGINT,
    CONSTRAINT jenkins_status_chk CHECK (status IN ('active','disabled'))
);

-- ---------- 3.5 vo_cluster_node_pools ----------
CREATE TABLE vo_cluster_node_pools (
    id                     BIGSERIAL PRIMARY KEY,
    uuid                   UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    cluster_id             BIGINT NOT NULL REFERENCES vo_clusters(id),
    name                   VARCHAR(128) NOT NULL,
    node_count             INT,
    cpu_cores_per_node     INT,
    memory_bytes_per_node  BIGINT,
    gpu_count_per_node     INT NOT NULL DEFAULT 0,
    gpu_type               VARCHAR(64),
    gpu_resource_name      VARCHAR(128),
    taints                 JSONB,
    labels                 JSONB,
    storage_classes        JSONB,
    available              BOOLEAN NOT NULL DEFAULT true,
    last_synced_at         TIMESTAMPTZ
);
CREATE INDEX idx_node_pools_cluster ON vo_cluster_node_pools (cluster_id);

-- ---------- 3.5b vo_node_pools（云厂商节点池扩缩容操作记录） ----------
-- 记录云厂商节点池的扩缩容操作，便于审计与状态回查。与 vo_cluster_node_pools 不同：
-- 后者存 K8s 节点池元信息，本表存扩缩容操作历史。
CREATE TABLE vo_node_pools (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    cluster_id      BIGINT NOT NULL REFERENCES vo_clusters(id),
    node_pool_id    VARCHAR(128) NOT NULL,
    provider        VARCHAR(32) NOT NULL,
    region          VARCHAR(64),
    name            VARCHAR(128),
    desired_count   INT NOT NULL,
    current_count   INT,
    status          VARCHAR(32) NOT NULL DEFAULT 'scaling',
    error_message   TEXT,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT node_pools_status_chk CHECK (status IN ('scaling','completed','failed','pending'))
);
CREATE INDEX IF NOT EXISTS idx_node_pools_cluster_id ON vo_node_pools (cluster_id) WHERE deleted = false;
CREATE INDEX IF NOT EXISTS idx_node_pools_pool_id ON vo_node_pools (cluster_id, node_pool_id) WHERE deleted = false;
COMMENT ON TABLE vo_node_pools IS '云厂商节点池扩缩容操作记录';
COMMENT ON COLUMN vo_node_pools.node_pool_id IS '云厂商节点池 ID（阿里云 nodepool_id / EKS nodegroup name / TKE nodePoolId）';
COMMENT ON COLUMN vo_node_pools.provider IS '云厂商（aliyun/tencent/aws/azure/gcp/huawei）';

-- ---------- 3.5c 单一默认实例约束（部分唯一索引） ----------
-- 确保 vo_jenkins_instances / vo_registries 全局只有一个 is_default=true 且未删除的实例。
CREATE UNIQUE INDEX uq_jenkins_instances_single_default
  ON vo_jenkins_instances (is_default) WHERE is_default = true AND deleted = false;
CREATE UNIQUE INDEX uq_registries_single_default
  ON vo_registries (is_default) WHERE is_default = true AND deleted = false;

-- ---------- 3.6 vo_cluster_ip_pools ----------
CREATE TABLE vo_cluster_ip_pools (
    id               BIGSERIAL PRIMARY KEY,
    uuid             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    cluster_id       BIGINT NOT NULL REFERENCES vo_clusters(id),
    name             VARCHAR(128) NOT NULL,
    cidr             VARCHAR(64) NOT NULL,
    gateway          VARCHAR(64),
    provider         VARCHAR(32) NOT NULL,
    total_count      INT,
    allocated_count  INT NOT NULL DEFAULT 0,
    reserved_ips     JSONB,
    version          INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       BIGINT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       BIGINT,
    deleted          BOOLEAN NOT NULL DEFAULT false,
    deleted_at       TIMESTAMPTZ,
    deleted_by       BIGINT,
    CONSTRAINT ip_pools_provider_chk CHECK (provider IN ('metallb','calico-ipam','whereabouts','kube-ovn'))
);

-- ---------- 3.7 vo_cluster_ip_allocations ----------
CREATE TABLE vo_cluster_ip_allocations (
    id             BIGSERIAL PRIMARY KEY,
    ip_pool_id     BIGINT NOT NULL REFERENCES vo_cluster_ip_pools(id),
    cluster_id     BIGINT NOT NULL REFERENCES vo_clusters(id),
    ip_address     VARCHAR(64) NOT NULL,
    resource_type  VARCHAR(32) NOT NULL,
    resource_id    BIGINT NOT NULL,
    replica_index  INT,
    status         VARCHAR(16) NOT NULL DEFAULT 'allocated',
    allocated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at    TIMESTAMPTZ,
    UNIQUE (ip_pool_id, ip_address),
    CONSTRAINT ip_alloc_status_chk CHECK (status IN ('allocated','released')),
    CONSTRAINT ip_alloc_rtype_chk CHECK (resource_type IN ('group','middleware_instance','service'))
);

-- ---------- 3.7b vo_cluster_operations ----------
-- 集群计划运维任务（重启/drain/cordon 等），持久化以支持调度器周期扫描与审计。
CREATE TABLE vo_cluster_operations (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    cluster_id      BIGINT NOT NULL REFERENCES vo_clusters(id),
    node_name       VARCHAR(256),                  -- NULL 表示集群级操作
    operation_type  VARCHAR(32) NOT NULL,          -- restart|drain|cordon|uncordon|sync_status
    scheduled_at    TIMESTAMPTZ NOT NULL,          -- 计划执行时间
    status          VARCHAR(16) NOT NULL DEFAULT 'pending', -- pending|running|completed|failed|cancelled
    executed_at     TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error_message   TEXT,
    notify_affected BOOLEAN NOT NULL DEFAULT true, -- 是否通知受影响应用参与人
    notified_user_ids BIGINT[] NOT NULL DEFAULT '{}',
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT cluster_ops_type_chk CHECK (operation_type IN
      ('restart','drain','cordon','uncordon','sync_status')),
    CONSTRAINT cluster_ops_status_chk CHECK (status IN
      ('pending','running','completed','failed','cancelled'))
);
CREATE INDEX idx_cluster_ops_pending ON vo_cluster_operations (scheduled_at)
  WHERE deleted = false AND status = 'pending';
CREATE INDEX idx_cluster_ops_cluster ON vo_cluster_operations (cluster_id)
  WHERE deleted = false;

-- ---------- 3.7c vo_cluster_node_status ----------
-- 节点状态缓存表（避免每次列表都直连 K8s）。用 partial unique index 支撑 ON CONFLICT ... WHERE deleted=false。
CREATE TABLE vo_cluster_node_status (
    id                       BIGSERIAL PRIMARY KEY,
    uuid                     UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    cluster_id               BIGINT NOT NULL REFERENCES vo_clusters(id),
    node_name                VARCHAR(256) NOT NULL,
    status                   VARCHAR(16) NOT NULL DEFAULT 'unknown', -- ready|not_ready|unknown
    unschedulable            BOOLEAN NOT NULL DEFAULT false,
    kubelet_version          VARCHAR(32),
    allocatable_cpu_m        INT NOT NULL DEFAULT 0,
    allocatable_memory_bytes BIGINT NOT NULL DEFAULT 0,
    allocatable_gpu          INT NOT NULL DEFAULT 0,
    used_cpu_m               INT NOT NULL DEFAULT 0,
    used_memory_bytes        BIGINT NOT NULL DEFAULT 0,
    used_gpu                 INT NOT NULL DEFAULT 0,
    pod_count                INT NOT NULL DEFAULT 0,
    abnormal_pod_count       INT NOT NULL DEFAULT 0,
    roles                    JSONB NOT NULL DEFAULT '[]',
    taints                   JSONB NOT NULL DEFAULT '[]',
    addresses                JSONB NOT NULL DEFAULT '[]',
    last_synced_at           TIMESTAMPTZ,
    version                  INT NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by               BIGINT,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by               BIGINT,
    deleted                  BOOLEAN NOT NULL DEFAULT false,
    deleted_at               TIMESTAMPTZ,
    deleted_by               BIGINT
);
CREATE INDEX idx_cluster_node_status_cluster ON vo_cluster_node_status (cluster_id)
  WHERE deleted = false;
CREATE UNIQUE INDEX uq_cluster_node_status_active
  ON vo_cluster_node_status (cluster_id, node_name)
  WHERE deleted = false;

-- ---------- 3.8 vo_resource_templates ----------
CREATE TABLE vo_resource_templates (
    id                                BIGSERIAL PRIMARY KEY,
    uuid                              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name                              VARCHAR(128) NOT NULL,
    scope                             VARCHAR(16) NOT NULL DEFAULT 'platform',
    scope_id                          BIGINT,
    cpu_m                             INT NOT NULL,
    cpu_limit_m                       INT,
    memory_bytes                      BIGINT NOT NULL,
    memory_limit_bytes                BIGINT,
    gpu                               INT NOT NULL DEFAULT 0,
    gpu_type                          VARCHAR(64),
    storage_size_bytes                BIGINT,
    ephemeral_storage_request_bytes   BIGINT,
    ephemeral_storage_limit_bytes     BIGINT,
    node_selector                     JSONB,
    tolerations                       JSONB,
    description                       VARCHAR(255),
    is_system                         BOOLEAN NOT NULL DEFAULT false,
    version                           INT NOT NULL DEFAULT 1,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by                        BIGINT,
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                        BIGINT,
    deleted                           BOOLEAN NOT NULL DEFAULT false,
    deleted_at                        TIMESTAMPTZ,
    deleted_by                        BIGINT,
    CONSTRAINT rtmpl_scope_chk CHECK (scope IN ('platform','workspace'))
);

-- （vo_registries/jenkins 的 credential_id FK 已在 §3.2/3.4 内联定义）

-- ============================================================================
-- 4. 空间与应用
-- ============================================================================

-- ---------- 4.1 vo_workspaces ----------
CREATE TABLE vo_workspaces (
    id                   BIGSERIAL PRIMARY KEY,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name                 VARCHAR(64) NOT NULL UNIQUE,
    display_name         VARCHAR(128),
    description          TEXT,
    logo_url             VARCHAR(512),
    status               VARCHAR(16) NOT NULL DEFAULT 'active',
    owner_id             BIGINT NOT NULL REFERENCES vo_users(id),
    default_registry_id  BIGINT,
    default_jenkins_id   BIGINT,
    labels               JSONB NOT NULL DEFAULT '{}',
    metadata             JSONB NOT NULL DEFAULT '{}',
    version              INT NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by           BIGINT,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by           BIGINT,
    deleted              BOOLEAN NOT NULL DEFAULT false,
    deleted_at           TIMESTAMPTZ,
    deleted_by           BIGINT,
    -- workspace 类型：app（普通应用工作区）/middleware（中间件）/inference（AI 推理）。0013 合并。
    ws_type              VARCHAR(16) NOT NULL DEFAULT 'app',
    CONSTRAINT ws_status_chk CHECK (status IN ('active','archived','frozen')),
    CONSTRAINT ws_type_chk CHECK (ws_type IN ('app','middleware','inference'))
);
ALTER TABLE vo_workspaces ADD CONSTRAINT ws_default_registry_fkey FOREIGN KEY (default_registry_id) REFERENCES vo_registries(id);
ALTER TABLE vo_workspaces ADD CONSTRAINT ws_default_jenkins_fkey  FOREIGN KEY (default_jenkins_id)  REFERENCES vo_jenkins_instances(id);
-- 后置补 vo_workspace_members 的 FK（vo_workspaces 已建）
ALTER TABLE vo_workspace_members ADD CONSTRAINT ws_members_workspace_fkey FOREIGN KEY (workspace_id) REFERENCES vo_workspaces(id);
-- vo_application_members 的 FK 在 vo_applications 表创建后添加（见 §4.4）

-- ---------- 4.2 vo_workspace_clusters ----------
CREATE TABLE vo_workspace_clusters (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id            BIGINT NOT NULL REFERENCES vo_workspaces(id),
    cluster_id              BIGINT NOT NULL REFERENCES vo_clusters(id),
    namespace               VARCHAR(128) NOT NULL,
    role                    VARCHAR(16) NOT NULL DEFAULT 'primary',
    auto_create_namespace   BOOLEAN NOT NULL DEFAULT false,
    resource_quota          JSONB,
    version                 INT NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by              BIGINT,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by              BIGINT,
    deleted                 BOOLEAN NOT NULL DEFAULT false,
    deleted_at              TIMESTAMPTZ,
    deleted_by              BIGINT,
    CONSTRAINT ws_clusters_role_chk CHECK (role IN ('primary','secondary'))
);
CREATE UNIQUE INDEX uk_ws_clusters ON vo_workspace_clusters (workspace_id, cluster_id, namespace) WHERE deleted = false;

-- ---------- 4.3 vo_workspace_quotas ----------
CREATE TABLE vo_workspace_quotas (
    id                      BIGSERIAL PRIMARY KEY,
    workspace_id            BIGINT NOT NULL REFERENCES vo_workspaces(id) UNIQUE,
    max_applications        INT NOT NULL DEFAULT 50,
    max_groups              INT NOT NULL DEFAULT 200,
    max_concurrent_builds   INT NOT NULL DEFAULT 10,
    max_images_retained     INT NOT NULL DEFAULT 100,
    max_members             INT NOT NULL DEFAULT 100,
    version                 INT NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by              BIGINT,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by              BIGINT,
    deleted                 BOOLEAN NOT NULL DEFAULT false,
    deleted_at              TIMESTAMPTZ,
    deleted_by              BIGINT
);

-- ---------- 4.4 vo_applications ----------
CREATE TABLE vo_applications (
    id                      BIGSERIAL PRIMARY KEY,
    uuid                    UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id            BIGINT NOT NULL REFERENCES vo_workspaces(id),
    -- code: workspace 内唯一编号（0013 合并），用于 AI 推理 / 中间件统一为「应用-分组」体系。
    code                    VARCHAR(32) NOT NULL,
    name                    VARCHAR(64) NOT NULL,
    display_name            VARCHAR(128),
    description             TEXT,
    icon                    VARCHAR(64),
    default_git_source_id   BIGINT,
    default_registry_id     BIGINT REFERENCES vo_registries(id),
    lifecycle               VARCHAR(16) NOT NULL DEFAULT 'active',
    owner_id                BIGINT NOT NULL REFERENCES vo_users(id),
    labels                  JSONB NOT NULL DEFAULT '{}',
    metadata                JSONB NOT NULL DEFAULT '{}',
    version                 INT NOT NULL DEFAULT 1,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by              BIGINT,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by              BIGINT,
    deleted                 BOOLEAN NOT NULL DEFAULT false,
    deleted_at              TIMESTAMPTZ,
    deleted_by              BIGINT,
    CONSTRAINT app_lifecycle_chk CHECK (lifecycle IN ('active','frozen','archived'))
);
CREATE UNIQUE INDEX uk_app_ws_name ON vo_applications (workspace_id, name) WHERE deleted = false;
-- workspace 内 code 唯一（0013 合并）。
CREATE UNIQUE INDEX uk_app_ws_code ON vo_applications (workspace_id, code) WHERE deleted = false;
CREATE INDEX idx_app_workspace ON vo_applications (workspace_id) WHERE deleted = false;
-- vo_application_members 的 FK（vo_applications 已建，见 §2.7 表定义）
ALTER TABLE vo_application_members ADD CONSTRAINT app_members_app_fkey FOREIGN KEY (application_id) REFERENCES vo_applications(id);

-- ---------- 4.5 vo_git_sources ----------
CREATE TABLE vo_git_sources (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id      BIGINT NOT NULL REFERENCES vo_applications(id),
    name                VARCHAR(64) NOT NULL,
    provider            VARCHAR(16) NOT NULL,
    repo_url            VARCHAR(512) NOT NULL,
    default_branch      VARCHAR(128),
    credential_id       BIGINT REFERENCES vo_credentials(id),
    webhook_enabled     BOOLEAN NOT NULL DEFAULT false,
    webhook_secret_hash VARCHAR(255),
    last_synced_at      TIMESTAMPTZ,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT git_provider_chk CHECK (provider IN ('github','gitlab','gitea','generic'))
);
ALTER TABLE vo_applications ADD CONSTRAINT app_default_git_fkey FOREIGN KEY (default_git_source_id) REFERENCES vo_git_sources(id);

-- ---------- 4.6 vo_groups ----------
CREATE TABLE vo_groups (
    id                                BIGSERIAL PRIMARY KEY,
    uuid                              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id                    BIGINT NOT NULL REFERENCES vo_applications(id),
    name                              VARCHAR(64) NOT NULL,
    display_name                      VARCHAR(128),
    description                       TEXT,
    environment                       VARCHAR(16) NOT NULL DEFAULT 'dev',
    cluster_id                        BIGINT NOT NULL REFERENCES vo_clusters(id),
    namespace                         VARCHAR(128) NOT NULL,
    deployment_name                   VARCHAR(128),
    service_name                      VARCHAR(128),
    replicas                          INT NOT NULL DEFAULT 1,
    current_image_id                  BIGINT,
    current_config_id                 BIGINT,
    current_release_id                BIGINT,
    -- 候选版本（candidate Deployment 模式，与 current 并存最多两版本）。
    candidate_image_id                BIGINT,
    candidate_release_id              BIGINT,
    candidate_replicas                INT NOT NULL DEFAULT 0,
    resources_cpu_m                   INT NOT NULL,
    resources_cpu_limit_m             INT,
    resources_memory_bytes            BIGINT NOT NULL,
    resources_memory_limit_bytes      BIGINT,
    resources_gpu                     INT NOT NULL DEFAULT 0,
    gpu_type                          VARCHAR(64),
    gpu_resource_name                 VARCHAR(128),
    storage_size_bytes                BIGINT,
    storage_class                     VARCHAR(128),
    ephemeral_storage_request_bytes   BIGINT,
    ephemeral_storage_limit_bytes     BIGINT,
    resource_template_id              BIGINT REFERENCES vo_resource_templates(id),
    network_mode                      VARCHAR(16) NOT NULL DEFAULT 'clusterip',
    service_port_info                 JSONB NOT NULL DEFAULT '[]',
    keep_pod_ip                       BOOLEAN NOT NULL DEFAULT false,
    allow_egress_internet             BOOLEAN NOT NULL DEFAULT false,
    egress_allowlist                  JSONB,
    network_policy_enabled            BOOLEAN NOT NULL DEFAULT false,
    ingress_enabled                   BOOLEAN NOT NULL DEFAULT false,
    ingress_host                      VARCHAR(255),
    ingress_path                      VARCHAR(255),
    dns_policy                        VARCHAR(32) NOT NULL DEFAULT 'ClusterFirst',
    host_network                      BOOLEAN NOT NULL DEFAULT false,
    strategy                          VARCHAR(16) NOT NULL DEFAULT 'rolling',
    max_surge                         VARCHAR(16) NOT NULL DEFAULT '25%',
    max_unavailable                   VARCHAR(16) NOT NULL DEFAULT '25%',
    health_check                      JSONB,
    node_selector                     JSONB,
    node_affinity                     JSONB,
    tolerations                       JSONB,
    priority_class                    VARCHAR(128),
    workload_type                     VARCHAR(16) NOT NULL DEFAULT 'deployment',
    cron_schedule                     VARCHAR(64),
    job_policy                        JSONB,
    autoscaling_enabled               BOOLEAN NOT NULL DEFAULT false,
    hpa_min_replicas                  INT,
    hpa_max_replicas                  INT,
    hpa_metrics                       JSONB,
    hpa_behavior                      JSONB,
    release_requires_approval         BOOLEAN NOT NULL DEFAULT false,
    labels                            JSONB NOT NULL DEFAULT '{}',
    metadata                          JSONB NOT NULL DEFAULT '{}',
    -- app_type: 冗余自 application.metadata，便于查询过滤（0013 合并）。
    app_type                          VARCHAR(16) NOT NULL DEFAULT 'web',
    version                           INT NOT NULL DEFAULT 1,
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by                        BIGINT,
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                        BIGINT,
    deleted                           BOOLEAN NOT NULL DEFAULT false,
    deleted_at                        TIMESTAMPTZ,
    deleted_by                        BIGINT,
    CONSTRAINT groups_env_chk          CHECK (environment IN ('dev','test','staging','prod')),
    CONSTRAINT groups_netmode_chk      CHECK (network_mode IN ('clusterip','nodeport','loadbalancer','hostnetwork')),
    CONSTRAINT groups_workload_chk     CHECK (workload_type IN ('deployment','statefulset','cronjob','job')),
    CONSTRAINT groups_strategy_chk     CHECK (strategy IN ('rolling','recreate'))
);
CREATE INDEX idx_groups_app_type ON vo_groups (app_type) WHERE deleted = false;
CREATE UNIQUE INDEX uk_group_app_name ON vo_groups (application_id, name) WHERE deleted = false;
CREATE INDEX idx_group_app ON vo_groups (application_id) WHERE deleted = false;
CREATE INDEX idx_group_cluster_ns ON vo_groups (cluster_id, namespace);

-- ============================================================================
-- 5. 镜像与构建
-- ============================================================================

-- ---------- 5.1 vo_base_images ----------
CREATE TABLE vo_base_images (
    id                 BIGSERIAL PRIMARY KEY,
    uuid               UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name               VARCHAR(128) NOT NULL,
    runtime            VARCHAR(16) NOT NULL,
    registry           VARCHAR(255),
    image_ref          VARCHAR(512) NOT NULL,
    digest             VARCHAR(128),
    is_system          BOOLEAN NOT NULL DEFAULT false,
    is_recommended     BOOLEAN NOT NULL DEFAULT false,
    description        TEXT,
    dockerfile_template TEXT,
    -- 构建工具默认配置（兼容字段，新逻辑改从 vo_build_tools 读取）。
    build_tool            VARCHAR(32),
    default_build_command TEXT,
    default_artifact_path VARCHAR(255),
    default_build_args    JSONB,
    -- 容器启动命令（JSON 数组），渲染 Dockerfile 时注入 {{.Entrypoint}}。
    entrypoint         JSONB,
    version            INT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by         BIGINT,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         BIGINT,
    deleted            BOOLEAN NOT NULL DEFAULT false,
    deleted_at         TIMESTAMPTZ,
    deleted_by         BIGINT,
    CONSTRAINT base_img_runtime_chk CHECK (runtime IN ('java','python','go','node','custom'))
);

-- ---------- 5.1b vo_build_tools ----------
-- 构建工具配置（runtime+tool+command+artifactPath+builder_image），可通过维护页面 CRUD，
-- 新增构建工具零代码改动。builder_image 供 Tekton build Task 与 Jenkins docker run 使用。
CREATE TABLE vo_build_tools (
    id                    BIGSERIAL PRIMARY KEY,
    uuid                  UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name                  VARCHAR(128) NOT NULL,
    runtime               VARCHAR(16) NOT NULL,
    tool                  VARCHAR(32) NOT NULL,
    default_build_command TEXT,
    default_artifact_path VARCHAR(255),
    builder_image         VARCHAR(512) NOT NULL,
    is_system             BOOLEAN NOT NULL DEFAULT false,
    description           TEXT,
    version               INT NOT NULL DEFAULT 1,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by            BIGINT,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by            BIGINT,
    deleted               BOOLEAN NOT NULL DEFAULT false,
    deleted_at            TIMESTAMPTZ,
    deleted_by            BIGINT,
    CONSTRAINT build_tool_runtime_chk CHECK (runtime IN ('java','python','go','node','custom')),
    CONSTRAINT build_tool_uniq UNIQUE (runtime, tool, deleted)
);
CREATE INDEX idx_build_tools_runtime ON vo_build_tools (runtime) WHERE deleted = false;

-- ---------- 5.2 vo_images（制品版本） ----------
CREATE TABLE vo_images (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id      BIGINT NOT NULL REFERENCES vo_applications(id),
    registry_id         BIGINT NOT NULL REFERENCES vo_registries(id),
    repository          VARCHAR(255) NOT NULL,
    tag                 VARCHAR(128) NOT NULL,
    digest              VARCHAR(128) NOT NULL,
    full_reference      VARCHAR(512) NOT NULL,
    version_number      INT NOT NULL,
    version_label       VARCHAR(64),
    source              VARCHAR(16) NOT NULL DEFAULT 'build',
    build_id            BIGINT,
    git_commit_sha      VARCHAR(64),
    git_branch          VARCHAR(128),
    git_commit_message  TEXT,
    git_author          VARCHAR(128),
    size_bytes          BIGINT,
    scan_status         VARCHAR(16) NOT NULL DEFAULT 'pending',
    scan_result         JSONB,
    status              VARCHAR(16) NOT NULL DEFAULT 'available',
    is_rollback_target  BOOLEAN NOT NULL DEFAULT false,
    labels              JSONB NOT NULL DEFAULT '{}',
    version_col         INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT img_source_chk   CHECK (source IN ('build','manual','import')),
    CONSTRAINT img_scan_chk     CHECK (scan_status IN ('pending','passed','failed','skipped')),
    CONSTRAINT img_status_chk   CHECK (status IN ('available','retired','deleted'))
);
CREATE UNIQUE INDEX uk_img_version ON vo_images (application_id, version_number) WHERE deleted = false;
CREATE INDEX idx_images_app ON vo_images (application_id) WHERE deleted = false;

-- ---------- 5.2.1 vo_image_version_tags ----------
CREATE TABLE vo_image_version_tags (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id BIGINT NOT NULL REFERENCES vo_applications(id),
    name           VARCHAR(64) NOT NULL,
    image_id       BIGINT NOT NULL REFERENCES vo_images(id),
    description    VARCHAR(255),
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT
);
CREATE UNIQUE INDEX uk_img_tag ON vo_image_version_tags (application_id, name) WHERE deleted = false;

-- ---------- 5.3 vo_build_templates ----------
CREATE TABLE vo_build_templates (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope             VARCHAR(16) NOT NULL DEFAULT 'platform',
    scope_id          BIGINT,
    name              VARCHAR(128) NOT NULL,
    description       TEXT,
    build_strategy    VARCHAR(16) NOT NULL,
    build_command     TEXT,
    base_image_id     BIGINT NOT NULL REFERENCES vo_base_images(id),
    dockerfile_source VARCHAR(16) NOT NULL DEFAULT 'template',
    dockerfile_content TEXT,
    context_path      VARCHAR(255) NOT NULL DEFAULT '.',
    build_args        JSONB NOT NULL DEFAULT '{}',
    env_vars          JSONB NOT NULL DEFAULT '{}',
    is_default        BOOLEAN NOT NULL DEFAULT false,
    usage_count       INT NOT NULL DEFAULT 0,
    version           INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT btmpl_scope_chk CHECK (scope IN ('platform','workspace','application'))
);

-- ---------- 5.4 vo_builds ----------
CREATE TABLE vo_builds (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id      BIGINT NOT NULL REFERENCES vo_applications(id),
    build_number        INT NOT NULL,
    git_source_id       BIGINT NOT NULL REFERENCES vo_git_sources(id),
    ref_type            VARCHAR(16) NOT NULL,
    ref_value           VARCHAR(128) NOT NULL,
    commit_sha          VARCHAR(64),
    commit_message      TEXT,
    build_template_id   BIGINT REFERENCES vo_build_templates(id),
    build_strategy      VARCHAR(16) NOT NULL,
    build_command       TEXT,
    context_path        VARCHAR(255),
    -- 制品路径（template 模式）与 Dockerfile 路径（repo 模式），拆分自 context_path。
    artifact_path       VARCHAR(255),
    dockerfile_path     VARCHAR(255),
    base_image_id       BIGINT REFERENCES vo_base_images(id),
    dockerfile_source   VARCHAR(16),
    dockerfile_content  TEXT,
    build_args          JSONB,
    -- 持久化构建工具与 builder_image，供 RebuildBuild 恢复。
    build_tool          VARCHAR(32),
    builder_image       VARCHAR(512),
    target_registry_id  BIGINT REFERENCES vo_registries(id),
    target_repository   VARCHAR(255),
    target_tag          VARCHAR(128),
    output_image_id     BIGINT,
    jenkins_instance_id BIGINT REFERENCES vo_jenkins_instances(id),
    jenkins_queue_id    VARCHAR(64),
    jenkins_build_number INT,
    jenkins_job_name    VARCHAR(255),
    -- Tekton PipelineRun 名称（构建引擎切换为 tekton 时写入）。
    pipeline_run_name   VARCHAR(128),
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    progress_percent    INT NOT NULL DEFAULT 0,
    current_step        VARCHAR(64),
    duration_ms         BIGINT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    log_storage_key     VARCHAR(512),
    log_excerpt         TEXT,
    failure_reason      TEXT,
    triggered_by        BIGINT NOT NULL REFERENCES vo_users(id),
    trigger_source      VARCHAR(16) NOT NULL DEFAULT 'manual',
    idempotency_key     VARCHAR(64),
    metadata            JSONB,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT build_ref_chk   CHECK (ref_type IN ('branch','tag','commit')),
    CONSTRAINT build_status_chk CHECK (status IN ('pending','queued','running','success','failed','canceled','timeout')),
    CONSTRAINT build_trig_chk  CHECK (trigger_source IN ('manual','webhook','api','schedule'))
);
CREATE UNIQUE INDEX uk_build_number ON vo_builds (application_id, build_number) WHERE deleted = false;
CREATE INDEX idx_builds_app_status ON vo_builds (application_id, status, created_at DESC);
CREATE INDEX idx_builds_triggered  ON vo_builds (triggered_by, created_at DESC);
CREATE INDEX idx_builds_pipeline_run ON vo_builds (pipeline_run_name) WHERE pipeline_run_name IS NOT NULL;
ALTER TABLE vo_builds ADD CONSTRAINT builds_output_img_fkey FOREIGN KEY (output_image_id) REFERENCES vo_images(id);
ALTER TABLE vo_images ADD CONSTRAINT images_build_fkey      FOREIGN KEY (build_id) REFERENCES vo_builds(id);

-- ---------- 5.5 vo_build_steps ----------
CREATE TABLE vo_build_steps (
    id              BIGSERIAL PRIMARY KEY,
    build_id        BIGINT NOT NULL REFERENCES vo_builds(id),
    seq             INT NOT NULL,
    name            VARCHAR(64) NOT NULL,
    status          VARCHAR(16) NOT NULL,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    duration_ms     BIGINT,
    message         TEXT,
    log_offset_start BIGINT,
    log_offset_end   BIGINT,
    log_storage_key  VARCHAR(512),
    log_size_bytes   BIGINT NOT NULL DEFAULT 0,
    error_line       TEXT
);
CREATE INDEX idx_build_steps_build ON vo_build_steps (build_id, seq);

-- ---------- 5.6 vo_pipelines ----------
CREATE TABLE vo_pipelines (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id        BIGINT NOT NULL REFERENCES vo_workspaces(id),
    scope               VARCHAR(16) NOT NULL DEFAULT 'workspace',
    scope_id            BIGINT,
    name                VARCHAR(128) NOT NULL,
    description         TEXT,
    trigger             VARCHAR(16) NOT NULL DEFAULT 'manual',
    trigger_config      JSONB NOT NULL DEFAULT '{}',
    trigger_on_pipeline BIGINT REFERENCES vo_pipelines(id),
    stages_config       JSONB NOT NULL DEFAULT '[]',
    enabled             BOOLEAN NOT NULL DEFAULT true,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT pipe_scope_chk   CHECK (scope IN ('workspace','application')),
    CONSTRAINT pipe_trigger_chk CHECK (trigger IN ('manual','webhook','schedule','promotion'))
);

-- ---------- 5.7 vo_pipeline_stages ----------
CREATE TABLE vo_pipeline_stages (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    pipeline_id BIGINT NOT NULL REFERENCES vo_pipelines(id),
    seq         INT NOT NULL,
    name        VARCHAR(64) NOT NULL,
    type        VARCHAR(16) NOT NULL DEFAULT 'sequential',
    gate        JSONB NOT NULL DEFAULT '{}',
    on_failure  VARCHAR(16) NOT NULL DEFAULT 'abort',
    params      JSONB NOT NULL DEFAULT '{}',
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    CONSTRAINT pstage_type_chk    CHECK (type IN ('parallel','sequential')),
    CONSTRAINT pstage_failure_chk CHECK (on_failure IN ('abort','manual_retry','continue'))
);
CREATE INDEX idx_pstages_pipeline ON vo_pipeline_stages (pipeline_id, seq);

-- ---------- 5.8 vo_pipeline_runs ----------
CREATE TABLE vo_pipeline_runs (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    pipeline_id         BIGINT NOT NULL REFERENCES vo_pipelines(id),
    workspace_id        BIGINT NOT NULL,
    run_number          INT NOT NULL,
    trigger             VARCHAR(16) NOT NULL,
    trigger_ref         VARCHAR(128),
    trigger_commit_sha  VARCHAR(64),
    trigger_by          BIGINT REFERENCES vo_users(id),
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    current_stage_seq   INT NOT NULL DEFAULT 0,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at         TIMESTAMPTZ,
    duration_ms         BIGINT,
    artifacts_image_ids BIGINT[] NOT NULL DEFAULT '{}',
    metadata            JSONB NOT NULL DEFAULT '{}',
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT prun_status_chk CHECK (status IN ('pending','running','paused','succeeded','failed','aborted','canceled'))
);
CREATE UNIQUE INDEX uk_prun_number ON vo_pipeline_runs (pipeline_id, run_number) WHERE deleted = false;
CREATE INDEX idx_pruns_running ON vo_pipeline_runs (status) WHERE status IN ('running','paused');

-- ---------- 5.9 vo_pipeline_stage_runs ----------
CREATE TABLE vo_pipeline_stage_runs (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    pipeline_run_id     BIGINT NOT NULL REFERENCES vo_pipeline_runs(id),
    stage_id            BIGINT NOT NULL REFERENCES vo_pipeline_stages(id),
    seq                 INT NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    related_build_id    BIGINT REFERENCES vo_builds(id),
    related_release_id  BIGINT,
    related_image_id    BIGINT REFERENCES vo_images(id),
    gate_result         JSONB NOT NULL DEFAULT '{}',
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    message             TEXT,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT psrun_status_chk CHECK (status IN ('pending','running','paused','succeeded','failed','skipped'))
);
CREATE INDEX idx_psruns_prun ON vo_pipeline_stage_runs (pipeline_run_id, seq);

-- ---------- 5.10 vo_promotions ----------
CREATE TABLE vo_promotions (
    id                         BIGSERIAL PRIMARY KEY,
    uuid                       UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id               BIGINT NOT NULL REFERENCES vo_workspaces(id),
    application_id             BIGINT NOT NULL REFERENCES vo_applications(id),
    source_env                 VARCHAR(16) NOT NULL,
    target_env                 VARCHAR(16) NOT NULL,
    artifact_image_id          BIGINT NOT NULL REFERENCES vo_images(id),
    artifact_config_version    INT,
    target_group_ids           BIGINT[] NOT NULL DEFAULT '{}',
    strategy                   VARCHAR(16) NOT NULL DEFAULT 'auto',
    auto_promote_on_verify     BOOLEAN NOT NULL DEFAULT true,
    status                     VARCHAR(16) NOT NULL DEFAULT 'pending',
    pipeline_run_id            BIGINT REFERENCES vo_pipeline_runs(id),
    approval_instance_id       BIGINT,
    started_by                 BIGINT NOT NULL REFERENCES vo_users(id),
    started_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at                TIMESTAMPTZ,
    version                    INT NOT NULL DEFAULT 1,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by                 BIGINT,
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by                 BIGINT,
    deleted                    BOOLEAN NOT NULL DEFAULT false,
    deleted_at                 TIMESTAMPTZ,
    deleted_by                 BIGINT,
    CONSTRAINT promo_strategy_chk CHECK (strategy IN ('auto','canary','manual')),
    CONSTRAINT promo_status_chk   CHECK (status IN ('pending','deploying','verifying','succeeded','failed','aborted'))
);
CREATE INDEX idx_promo_app_status ON vo_promotions (application_id, status);
CREATE INDEX idx_promo_active ON vo_promotions (status) WHERE status IN ('deploying','verifying');

-- ---------- 5.11 vo_artifacts_signatures ----------
CREATE TABLE vo_artifacts_signatures (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    image_id            BIGINT NOT NULL REFERENCES vo_images(id) UNIQUE,
    signature_type      VARCHAR(16) NOT NULL,
    signature_payload   TEXT NOT NULL,
    public_key_ref      VARCHAR(255),
    signed_by           VARCHAR(128),
    signed_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    sbom_storage_key    VARCHAR(512),
    sbom_format         VARCHAR(16),
    provenance          JSONB,
    verification_status VARCHAR(16) NOT NULL DEFAULT 'pending',
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT sig_type_chk CHECK (signature_type IN ('cosign','notation')),
    CONSTRAINT sig_sbom_chk CHECK (sbom_format IS NULL OR sbom_format IN ('cyclonedx','spdx')),
    CONSTRAINT sig_vstatus_chk CHECK (verification_status IN ('pending','verified','failed'))
);

-- ============================================================================
-- 6. 发布与部署
-- ============================================================================

-- ---------- 6.1 vo_releases ----------
CREATE TABLE vo_releases (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    group_id            BIGINT NOT NULL REFERENCES vo_groups(id),
    release_number      INT NOT NULL,
    previous_release_id BIGINT,
    image_id            BIGINT REFERENCES vo_images(id),
    config_version      INT,
    release_type        VARCHAR(16) NOT NULL,
    replicas            INT NOT NULL DEFAULT 1,
    strategy            VARCHAR(16) NOT NULL DEFAULT 'rolling',
    max_surge           VARCHAR(16) DEFAULT '25%',
    max_unavailable     VARCHAR(16) DEFAULT '25%',
    batch_size          INT,
    batch_interval_sec  INT,
    paused              BOOLEAN NOT NULL DEFAULT false,
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    progress_percent    INT NOT NULL DEFAULT 0,
    failure_reason      VARCHAR(64),
    -- 分批发布策略：percentage / machine_count（按 Pod 名指定）。
    target_percentage   INT,
    target_pod_names    JSONB,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at         TIMESTAMPTZ,
    duration_ms         BIGINT,
    triggered_by        BIGINT NOT NULL REFERENCES vo_users(id),
    trigger_source      VARCHAR(16) NOT NULL DEFAULT 'manual',
    auto_rollback_on_failure BOOLEAN NOT NULL DEFAULT false,
    rollback_of_release_id   BIGINT,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT rel_type_chk   CHECK (release_type IN ('initial','rolling','rollback','pause','resume','config','scale','restart')),
    CONSTRAINT rel_status_chk CHECK (status IN ('pending','pending_approval','running','paused','succeeded','failed','aborted','interrupted','rolled_back')),
    CONSTRAINT rel_trig_chk   CHECK (trigger_source IN ('manual','webhook','api','schedule'))
);
CREATE UNIQUE INDEX uk_rel_number ON vo_releases (group_id, release_number) WHERE deleted = false;
CREATE INDEX idx_releases_group_status ON vo_releases (group_id, status, created_at DESC);
ALTER TABLE vo_releases  ADD CONSTRAINT rel_prev_fkey    FOREIGN KEY (previous_release_id) REFERENCES vo_releases(id);
ALTER TABLE vo_releases  ADD CONSTRAINT rel_rollback_fkey FOREIGN KEY (rollback_of_release_id) REFERENCES vo_releases(id);
ALTER TABLE vo_groups    ADD CONSTRAINT group_curr_rel_fkey FOREIGN KEY (current_release_id) REFERENCES vo_releases(id);
ALTER TABLE vo_groups    ADD CONSTRAINT group_curr_img_fkey FOREIGN KEY (current_image_id)   REFERENCES vo_images(id);
ALTER TABLE vo_groups    ADD CONSTRAINT group_cand_img_fkey FOREIGN KEY (candidate_image_id) REFERENCES vo_images(id);
ALTER TABLE vo_groups    ADD CONSTRAINT group_cand_rel_fkey FOREIGN KEY (candidate_release_id) REFERENCES vo_releases(id);

-- ---------- 6.2 vo_release_events ----------
CREATE TABLE vo_release_events (
    id             BIGSERIAL PRIMARY KEY,
    release_id     BIGINT NOT NULL REFERENCES vo_releases(id),
    seq            INT NOT NULL,
    event_type     VARCHAR(32) NOT NULL,
    message        TEXT,
    operator_id    BIGINT REFERENCES vo_users(id),
    operator_name  VARCHAR(128),
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_rel_events_release ON vo_release_events (release_id, seq);

-- ---------- 6.3 vo_release_batch_records ----------
CREATE TABLE vo_release_batch_records (
    id          BIGSERIAL PRIMARY KEY,
    release_id  BIGINT NOT NULL REFERENCES vo_releases(id),
    batch_index INT NOT NULL,
    status      VARCHAR(16) NOT NULL DEFAULT 'pending',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    message     TEXT
);
CREATE INDEX idx_rel_batch_release ON vo_release_batch_records (release_id, batch_index);

-- ---------- 6.4 vo_release_presets ----------
CREATE TABLE vo_release_presets (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope          VARCHAR(16) NOT NULL,
    scope_id       BIGINT,
    name           VARCHAR(128) NOT NULL,
    description    TEXT,
    strategy       VARCHAR(16) NOT NULL DEFAULT 'rolling',
    max_surge      VARCHAR(16) DEFAULT '25%',
    max_unavailable VARCHAR(16) DEFAULT '25%',
    batch_size     INT,
    batch_interval_sec INT,
    auto_rollback_on_failure BOOLEAN NOT NULL DEFAULT false,
    is_default     BOOLEAN NOT NULL DEFAULT false,
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT,
    CONSTRAINT preset_scope_chk CHECK (scope IN ('platform','workspace','application'))
);

-- ---------- 6.5 vo_release_windows ----------
CREATE TABLE vo_release_windows (
    id               BIGSERIAL PRIMARY KEY,
    uuid             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    application_id   BIGINT NOT NULL REFERENCES vo_applications(id),
    name             VARCHAR(128) NOT NULL,
    timezone         VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    crontab          VARCHAR(255) NOT NULL,
    duration_minutes INT NOT NULL DEFAULT 60,
    is_active        BOOLEAN NOT NULL DEFAULT true,
    version          INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       BIGINT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       BIGINT,
    deleted          BOOLEAN NOT NULL DEFAULT false,
    deleted_at       TIMESTAMPTZ,
    deleted_by       BIGINT
);

-- ---------- 6.6 vo_release_orchestrations（多集群发布编排主表） ----------
-- 记录一次多集群发布编排（strategy: sequential|parallel|canary），逐 group 推进。
CREATE TABLE vo_release_orchestrations (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    application_id  BIGINT NOT NULL REFERENCES vo_applications(id),
    name            VARCHAR(128) NOT NULL,
    strategy        VARCHAR(16) NOT NULL DEFAULT 'sequential',
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    progress_percent INT NOT NULL DEFAULT 0,
    image_id        BIGINT,
    config_version  INT,
    replicas        INT,
    max_surge       VARCHAR(16) DEFAULT '25%',
    max_unavailable VARCHAR(16) DEFAULT '25%',
    batch_size      INT,
    batch_interval_sec INT,
    failure_reason  TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    triggered_by    BIGINT NOT NULL REFERENCES vo_users(id),
    trigger_source  VARCHAR(16) NOT NULL DEFAULT 'manual',
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX idx_orch_app ON vo_release_orchestrations (application_id, deleted);
CREATE INDEX idx_orch_status ON vo_release_orchestrations (status, started_at);

-- ---------- 6.7 vo_release_orchestration_targets（编排目标，每 group 一行） ----------
CREATE TABLE vo_release_orchestration_targets (
    id                 BIGSERIAL PRIMARY KEY,
    orchestration_id   BIGINT NOT NULL REFERENCES vo_release_orchestrations(id) ON DELETE CASCADE,
    group_id           BIGINT NOT NULL,
    cluster_id         BIGINT NOT NULL,
    image_id           BIGINT,
    config_version     INT,
    replicas           INT,
    seq                INT NOT NULL DEFAULT 0,
    batch_size         INT,
    batch_interval_sec INT,
    release_id         BIGINT,
    status             VARCHAR(16) NOT NULL DEFAULT 'pending',
    failure_reason     TEXT,
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    version            INT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_orch_tgt_orch ON vo_release_orchestration_targets (orchestration_id, seq);
CREATE INDEX idx_orch_tgt_group ON vo_release_orchestration_targets (group_id);

-- ============================================================================
-- 7. 配置管理
-- ============================================================================

-- ---------- 7.1 vo_configs ----------
CREATE TABLE vo_configs (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope          VARCHAR(16) NOT NULL,
    scope_id       BIGINT,
    group_id       BIGINT REFERENCES vo_groups(id),
    name           VARCHAR(128) NOT NULL,
    config_type    VARCHAR(16) NOT NULL,
    config_version INT NOT NULL,
    description    TEXT,
    rendered_content TEXT,
    diff_with_previous TEXT,
    checksum       VARCHAR(64) NOT NULL,
    status         VARCHAR(16) NOT NULL DEFAULT 'active',
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT,
    CONSTRAINT cfg_scope_chk  CHECK (scope IN ('workspace','application','group')),
    CONSTRAINT cfg_type_chk   CHECK (config_type IN ('env','file','mount','secret','configmap'))
);
CREATE UNIQUE INDEX uk_cfg_version ON vo_configs (scope, scope_id, name, config_version) WHERE deleted = false;
CREATE INDEX idx_configs_group ON vo_configs (group_id, config_version DESC);
ALTER TABLE vo_groups ADD CONSTRAINT group_curr_cfg_fkey FOREIGN KEY (current_config_id) REFERENCES vo_configs(id);

-- ---------- 7.2 vo_config_sets ----------
-- 配置集支持 workspace 维度与 application 维度两种归属：
--   - workspace 维度：workspace_id 非空，application_id 为空（历史模式）。
--   - application 维度：application_id 非空，workspace_id 可空（可由 application_id 反查得到）。
-- workspace_id 因此改为可空（0003/0015 合并）。
CREATE TABLE vo_config_sets (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id   BIGINT REFERENCES vo_workspaces(id),
    application_id BIGINT REFERENCES vo_applications(id),
    name           VARCHAR(128) NOT NULL,
    description    TEXT,
    content        JSONB NOT NULL,
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT
);
CREATE UNIQUE INDEX uk_cfgset_ws_name ON vo_config_sets (workspace_id, name) WHERE deleted = false AND workspace_id IS NOT NULL;
-- 应用维度唯一名（同应用下配置集名唯一），0003 合并。
CREATE UNIQUE INDEX uk_cfgset_app_name ON vo_config_sets (application_id, name) WHERE deleted = false AND application_id IS NOT NULL;

-- ---------- 7.3 vo_group_config_bindings ----------
-- 分组绑定配置集：扩展支持 config_set_id（指向 vo_config_sets），与历史 config_id（指向 vo_configs）并存。
-- 单绑定约束：一个分组至多绑定一个配置集（0014 合并；uk_group_single_binding）。
CREATE TABLE vo_group_config_bindings (
    id            BIGSERIAL PRIMARY KEY,
    group_id      BIGINT NOT NULL REFERENCES vo_groups(id),
    config_id     BIGINT NOT NULL REFERENCES vo_configs(id),
    config_set_id BIGINT REFERENCES vo_config_sets(id),
    priority      INT NOT NULL DEFAULT 100,
    pinned_version INT,
    mount_path    VARCHAR(255),
    sub_path      VARCHAR(255),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    BIGINT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by    BIGINT,
    deleted       BOOLEAN NOT NULL DEFAULT false,
    deleted_at    TIMESTAMPTZ,
    deleted_by    BIGINT
);
CREATE UNIQUE INDEX uk_group_cfg_binding ON vo_group_config_bindings (group_id, config_id) WHERE deleted = false;
-- 同一分组下同配置集不可重复绑定（0003 合并）。
CREATE UNIQUE INDEX uk_group_cfgset_binding ON vo_group_config_bindings (group_id, config_set_id) WHERE deleted = false AND config_set_id IS NOT NULL;
-- 单绑定约束：一个分组至多一条未删除的绑定（0014 合并）。
CREATE UNIQUE INDEX uk_group_single_binding ON vo_group_config_bindings (group_id) WHERE deleted = false;

-- ---------- 7.4 vo_group_local_configs ----------
-- 分组本地配置：分组在不绑定配置集时，可独立维护 files/env/command/args 配置（0014 合并）。
-- 与 vo_group_config_bindings 互斥：绑定配置集后，本地配置被覆盖且不可编辑（应用层强制）。
-- 结构与 vo_config_sets.content 同构。
CREATE TABLE vo_group_local_configs (
    id           BIGSERIAL PRIMARY KEY,
    uuid         UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    group_id     BIGINT NOT NULL REFERENCES vo_groups(id),
    name         VARCHAR(128) NOT NULL DEFAULT 'local',
    description  TEXT,
    content      JSONB NOT NULL DEFAULT '{}'::jsonb,
    version      INT NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   BIGINT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   BIGINT,
    deleted      BOOLEAN NOT NULL DEFAULT false,
    deleted_at   TIMESTAMPTZ,
    deleted_by   BIGINT
);
-- 每个分组至多一条未删除的本地配置。
CREATE UNIQUE INDEX uk_group_local_cfg ON vo_group_local_configs (group_id) WHERE deleted = false;
CREATE INDEX idx_group_local_cfg_group ON vo_group_local_configs (group_id) WHERE deleted = false;

-- ============================================================================
-- 8. 中间件
-- ============================================================================

-- ---------- 8.1 vo_middleware_catalog ----------
CREATE TABLE vo_middleware_catalog (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name           VARCHAR(64) NOT NULL UNIQUE,
    type           VARCHAR(32) NOT NULL,
    version        VARCHAR(32) NOT NULL,
    description    TEXT,
    chart_repo     VARCHAR(255),
    chart_name     VARCHAR(128),
    chart_version  VARCHAR(32),
    parameters_schema JSONB,
    is_recommended BOOLEAN NOT NULL DEFAULT false,
    is_system      BOOLEAN NOT NULL DEFAULT false,
    version_col    INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT,
    CONSTRAINT mw_type_chk CHECK (type IN ('mysql','postgresql','redis','mongodb','kafka','zookeeper','elasticsearch','minio','rabbitmq','nacos','consul','etcd'))
);

-- ---------- 8.2 vo_middleware_instances ----------
CREATE TABLE vo_middleware_instances (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id      BIGINT NOT NULL REFERENCES vo_workspaces(id),
    -- application_id / group_id: 统一为「应用-分组」体系反查列（0013 合并）。
    application_id    BIGINT REFERENCES vo_applications(id),
    group_id          BIGINT REFERENCES vo_groups(id),
    catalog_id        BIGINT NOT NULL REFERENCES vo_middleware_catalog(id),
    name              VARCHAR(64) NOT NULL,
    display_name      VARCHAR(128),
    description       TEXT,
    cluster_id        BIGINT NOT NULL REFERENCES vo_clusters(id),
    namespace         VARCHAR(128) NOT NULL,
    release_name      VARCHAR(128),
    version           VARCHAR(32) NOT NULL,
    replicas          INT NOT NULL DEFAULT 1,
    parameters        JSONB NOT NULL DEFAULT '{}',
    resources         JSONB,
    storage_size      BIGINT,
    storage_class     VARCHAR(128),
    persistence       BOOLEAN NOT NULL DEFAULT true,
    access_info       JSONB,
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    current_version   VARCHAR(32),
    operation_status  VARCHAR(16) NOT NULL DEFAULT 'idle',
    last_operation_at TIMESTAMPTZ,
    owner_id          BIGINT NOT NULL REFERENCES vo_users(id),
    version_col       INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT mw_inst_status_chk CHECK (status IN ('pending','running','updating','stopped','failed')),
    CONSTRAINT mw_inst_op_chk     CHECK (operation_status IN ('idle','installing','upgrading','scaling','deleting'))
);
CREATE UNIQUE INDEX uk_mw_inst_ws_name ON vo_middleware_instances (workspace_id, name) WHERE deleted = false;
CREATE INDEX idx_mw_inst_group ON vo_middleware_instances (group_id) WHERE deleted = false;

-- ---------- 8.3 vo_middleware_operations ----------
CREATE TABLE vo_middleware_operations (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    instance_id   BIGINT NOT NULL REFERENCES vo_middleware_instances(id),
    operation     VARCHAR(16) NOT NULL,
    from_version  VARCHAR(32),
    to_version    VARCHAR(32),
    status        VARCHAR(16) NOT NULL DEFAULT 'running',
    message       TEXT,
    triggered_by  BIGINT NOT NULL REFERENCES vo_users(id),
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    duration_ms   BIGINT
);
CREATE INDEX idx_mw_op_instance ON vo_middleware_operations (instance_id, started_at DESC);
ALTER TABLE vo_middleware_operations ADD CONSTRAINT mw_op_type_chk CHECK (operation IN ('install','upgrade','scale','restart','delete'));

-- ============================================================================
-- 9. 大模型服务
-- ============================================================================

-- ---------- 9.1 vo_model_registries ----------
CREATE TABLE vo_model_registries (
    id               BIGSERIAL PRIMARY KEY,
    uuid             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id     BIGINT NOT NULL REFERENCES vo_workspaces(id),
    name             VARCHAR(128) NOT NULL,
    provider         VARCHAR(16) NOT NULL,
    endpoint         VARCHAR(512),
    credential_id    BIGINT REFERENCES vo_credentials(id),
    cache_pvc_name   VARCHAR(128),
    cache_path       VARCHAR(512),
    cache_size_bytes BIGINT,
    status           VARCHAR(16) NOT NULL DEFAULT 'active',
    version          INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       BIGINT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       BIGINT,
    deleted          BOOLEAN NOT NULL DEFAULT false,
    deleted_at       TIMESTAMPTZ,
    deleted_by       BIGINT,
    CONSTRAINT mreg_provider_chk CHECK (provider IN ('huggingface','oss','s3','local','custom'))
);
CREATE UNIQUE INDEX uk_mreg_ws_name ON vo_model_registries (workspace_id, name) WHERE deleted = false;

-- ---------- 9.2 vo_models ----------
CREATE TABLE vo_models (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    registry_id     BIGINT NOT NULL REFERENCES vo_model_registries(id),
    name            VARCHAR(128) NOT NULL,
    display_name    VARCHAR(255),
    description     TEXT,
    base_architecture VARCHAR(64),
    parameter_count VARCHAR(32),
    license         VARCHAR(64),
    tags            JSONB NOT NULL DEFAULT '[]',
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT
);
CREATE UNIQUE INDEX uk_model_ws_name ON vo_models (workspace_id, name) WHERE deleted = false;

-- ---------- 9.3 vo_model_versions ----------
CREATE TABLE vo_model_versions (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    model_id          BIGINT NOT NULL REFERENCES vo_models(id),
    version           VARCHAR(64) NOT NULL,
    precision         VARCHAR(16) NOT NULL DEFAULT 'fp16',
    quantization      VARCHAR(16),
    weights_path      VARCHAR(512),
    weights_size_bytes BIGINT,
    weights_checksum  VARCHAR(128),
    framework         VARCHAR(16) NOT NULL DEFAULT 'vllm',
    framework_config  JSONB,
    min_gpu_memory_bytes BIGINT,
    recommended_gpu_count INT DEFAULT 1,
    download_status   VARCHAR(16) NOT NULL DEFAULT 'not_downloaded',
    download_progress INT NOT NULL DEFAULT 0,
    is_default        BOOLEAN NOT NULL DEFAULT false,
    version_col       INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT mver_precision_chk CHECK (precision IN ('fp32','fp16','bf16','int8','int4')),
    CONSTRAINT mver_quant_chk     CHECK (quantization IS NULL OR quantization IN ('gptq','awq','squeezellm','none')),
    CONSTRAINT mver_framework_chk CHECK (framework IN ('vllm','tgi','triton','sglang','ollama','custom')),
    CONSTRAINT mver_dlstatus_chk  CHECK (download_status IN ('not_downloaded','downloading','ready','failed'))
);
CREATE UNIQUE INDEX uk_mver_model_version ON vo_model_versions (model_id, version) WHERE deleted = false;

-- ---------- 9.4 vo_model_adapters ----------
CREATE TABLE vo_model_adapters (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    base_model_version_id BIGINT NOT NULL REFERENCES vo_model_versions(id),
    name            VARCHAR(128) NOT NULL,
    adapter_type    VARCHAR(16) NOT NULL DEFAULT 'lora',
    weights_path    VARCHAR(512),
    rank            INT,
    scale           FLOAT,
    version_col     INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT mada_type_chk CHECK (adapter_type IN ('lora','qlora','prefix'))
);

-- ---------- 9.5 vo_inference_services ----------
CREATE TABLE vo_inference_services (
    id                       BIGSERIAL PRIMARY KEY,
    uuid                     UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id             BIGINT NOT NULL REFERENCES vo_workspaces(id),
    -- application_id / group_id: 统一为「应用-分组」体系反查列（0013 合并）。
    application_id           BIGINT REFERENCES vo_applications(id),
    group_id                 BIGINT REFERENCES vo_groups(id),
    name                     VARCHAR(64) NOT NULL,
    display_name             VARCHAR(128),
    description              TEXT,
    cluster_id               BIGINT NOT NULL REFERENCES vo_clusters(id),
    namespace                VARCHAR(128) NOT NULL,
    workload_name            VARCHAR(128),
    service_name             VARCHAR(128),
    base_model_version_id    BIGINT NOT NULL REFERENCES vo_model_versions(id),
    adapter_ids              BIGINT[] NOT NULL DEFAULT '{}',
    framework                VARCHAR(16) NOT NULL,
    framework_config         JSONB NOT NULL DEFAULT '{}',
    replicas                 INT NOT NULL DEFAULT 1,
    resources                JSONB NOT NULL,
    gpu_count                INT NOT NULL DEFAULT 1,
    gpu_type                 VARCHAR(64),
    tensor_parallel_size     INT NOT NULL DEFAULT 1,
    pipeline_parallel_size   INT NOT NULL DEFAULT 1,
    storage_size_bytes       BIGINT,
    current_release_id       BIGINT,
    current_status           VARCHAR(16) NOT NULL DEFAULT 'stopped',
    readiness                VARCHAR(16) NOT NULL DEFAULT 'unknown',
    autoscaling_enabled      BOOLEAN NOT NULL DEFAULT false,
    hpa_min_replicas         INT,
    hpa_max_replicas         INT,
    hpa_metrics              JSONB,
    access_mode              VARCHAR(16) NOT NULL DEFAULT 'internal',
    external_endpoint        VARCHAR(512),
    labels                   JSONB NOT NULL DEFAULT '{}',
    metadata                 JSONB NOT NULL DEFAULT '{}',
    version                  INT NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by               BIGINT,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by               BIGINT,
    deleted                  BOOLEAN NOT NULL DEFAULT false,
    deleted_at               TIMESTAMPTZ,
    deleted_by               BIGINT,
    CONSTRAINT inf_framework_chk CHECK (framework IN ('vllm','tgi','triton','sglang','ollama','custom')),
    CONSTRAINT inf_status_chk    CHECK (current_status IN ('stopped','starting','running','updating','failed')),
    CONSTRAINT inf_ready_chk     CHECK (readiness IN ('unknown','not_ready','partial_ready','ready')),
    CONSTRAINT inf_access_chk    CHECK (access_mode IN ('internal','external'))
);
CREATE UNIQUE INDEX uk_inf_ws_name ON vo_inference_services (workspace_id, name) WHERE deleted = false;
CREATE INDEX idx_inf_cluster_ns ON vo_inference_services (cluster_id, namespace);
CREATE INDEX idx_inf_svc_group ON vo_inference_services (group_id) WHERE deleted = false;

-- ---------- 9.6 vo_inference_releases ----------
CREATE TABLE vo_inference_releases (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    inference_service_id BIGINT NOT NULL REFERENCES vo_inference_services(id),
    -- group_id: 对齐 vo_releases 语义，便于统一发布流关联（0013 合并）。
    group_id            BIGINT REFERENCES vo_groups(id),
    release_number      INT NOT NULL,
    previous_release_id BIGINT,
    target_model_version_id BIGINT NOT NULL REFERENCES vo_model_versions(id),
    target_adapter_ids  BIGINT[] NOT NULL DEFAULT '{}',
    strategy            VARCHAR(16) NOT NULL DEFAULT 'blue_green',
    replicas            INT NOT NULL DEFAULT 1,
    status              VARCHAR(16) NOT NULL DEFAULT 'pending',
    progress_percent    INT NOT NULL DEFAULT 0,
    failure_reason      VARCHAR(64),
    started_by          BIGINT NOT NULL REFERENCES vo_users(id),
    started_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at         TIMESTAMPTZ,
    duration_ms         BIGINT,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT infrel_strategy_chk CHECK (strategy IN ('rolling','blue_green','canary')),
    CONSTRAINT infrel_status_chk   CHECK (status IN ('pending','running','verifying','succeeded','failed','aborted','rolled_back'))
);
CREATE UNIQUE INDEX uk_infrel_number ON vo_inference_releases (inference_service_id, release_number) WHERE deleted = false;
CREATE INDEX idx_infrel_group ON vo_inference_releases (group_id) WHERE deleted = false;
ALTER TABLE vo_inference_releases ADD CONSTRAINT infrel_prev_fkey FOREIGN KEY (previous_release_id) REFERENCES vo_inference_releases(id);
ALTER TABLE vo_inference_services ADD CONSTRAINT inf_curr_rel_fkey FOREIGN KEY (current_release_id) REFERENCES vo_inference_releases(id);

-- ---------- 9.7 vo_inference_api_keys ----------
CREATE TABLE vo_inference_api_keys (
    id               BIGSERIAL PRIMARY KEY,
    uuid             UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    inference_service_id BIGINT NOT NULL REFERENCES vo_inference_services(id),
    name             VARCHAR(128) NOT NULL,
    key_prefix       VARCHAR(16) NOT NULL,
    key_hash         VARCHAR(255) NOT NULL UNIQUE,
    daily_token_quota BIGINT,
    rate_limit_per_min INT,
    expires_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ,
    status           VARCHAR(16) NOT NULL DEFAULT 'active',
    version          INT NOT NULL DEFAULT 1,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by       BIGINT,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by       BIGINT,
    deleted          BOOLEAN NOT NULL DEFAULT false,
    deleted_at       TIMESTAMPTZ,
    deleted_by       BIGINT,
    CONSTRAINT infkey_status_chk CHECK (status IN ('active','revoked'))
);

-- ---------- 9.8 vo_inference_usage（分区表） ----------
CREATE TABLE vo_inference_usage (
    id                   BIGSERIAL,
    uuid                 UUID NOT NULL DEFAULT gen_random_uuid(),
    inference_service_id BIGINT NOT NULL,
    api_key_id           BIGINT,
    caller_id            BIGINT,
    prompt_tokens        INT NOT NULL DEFAULT 0,
    completion_tokens    INT NOT NULL DEFAULT 0,
    total_tokens         INT NOT NULL DEFAULT 0,
    duration_ms          INT,
    status_code          INT,
    model_version_id     BIGINT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);
CREATE INDEX idx_infu_svc_time ON vo_inference_usage (inference_service_id, created_at DESC);
CREATE INDEX idx_infu_key_time ON vo_inference_usage (api_key_id, created_at DESC);

-- ---------- 9.9 vo_inference_routes ----------
CREATE TABLE vo_inference_routes (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    name            VARCHAR(128) NOT NULL,
    description     TEXT,
    strategy        VARCHAR(16) NOT NULL DEFAULT 'weighted',
    rules           JSONB NOT NULL,
    default_service_id BIGINT REFERENCES vo_inference_services(id),
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT infrt_strategy_chk CHECK (strategy IN ('weighted','header','failover'))
);

-- ============================================================================
-- 10. 审批与通知
-- ============================================================================

-- ---------- 10.1 vo_approvals ----------
CREATE TABLE vo_approvals (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    resource_type   VARCHAR(32) NOT NULL,
    resource_id     BIGINT NOT NULL,
    operation       VARCHAR(32) NOT NULL,
    requested_by    BIGINT NOT NULL REFERENCES vo_users(id),
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    approver_role   VARCHAR(64),
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    approver_id     BIGINT REFERENCES vo_users(id),
    approved_at     TIMESTAMPTZ,
    comment         TEXT,
    expires_at      TIMESTAMPTZ,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT appr_status_chk CHECK (status IN ('pending','approved','rejected','expired','canceled')),
    CONSTRAINT appr_rtype_chk  CHECK (resource_type IN ('release','promotion','middleware_op','config','inference_release','workspace_creation'))
);
CREATE INDEX idx_approvals_pending ON vo_approvals (status, requested_at) WHERE status = 'pending';
CREATE INDEX idx_approvals_resource ON vo_approvals (resource_type, resource_id, status);

-- ---------- 10.2 vo_notification_templates ----------
CREATE TABLE vo_notification_templates (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    code        VARCHAR(64) NOT NULL UNIQUE,
    name        VARCHAR(128) NOT NULL,
    channel     VARCHAR(16) NOT NULL,
    subject_tpl TEXT,
    body_tpl    TEXT NOT NULL,
    variables   JSONB NOT NULL DEFAULT '[]',
    is_system   BOOLEAN NOT NULL DEFAULT false,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT,
    CONSTRAINT ntmpl_channel_chk CHECK (channel IN ('email','sms','webhook','im','inapp'))
);

-- ---------- 10.3 vo_notifications（分区表） ----------
CREATE TABLE vo_notifications (
    id            BIGSERIAL,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id       BIGINT REFERENCES vo_users(id),
    channel       VARCHAR(16) NOT NULL,
    template_code VARCHAR(64),
    recipient     VARCHAR(255),
    subject       VARCHAR(255),
    body          TEXT,
    payload       JSONB,
    status        VARCHAR(16) NOT NULL DEFAULT 'pending',
    sent_at       TIMESTAMPTZ,
    read_at       TIMESTAMPTZ,
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);
CREATE INDEX idx_notif_user_time ON vo_notifications (user_id, created_at DESC);
CREATE INDEX idx_notif_status ON vo_notifications (status, created_at);

-- ---------- 10.4 vo_notification_channels ----------
CREATE TABLE vo_notification_channels (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope           VARCHAR(16) NOT NULL,
    scope_id        BIGINT,
    name            VARCHAR(128) NOT NULL,
    type            VARCHAR(16) NOT NULL,
    config          JSONB NOT NULL,
    is_default      BOOLEAN NOT NULL DEFAULT false,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT nchan_type_chk CHECK (type IN ('email','sms','webhook','dingtalk','feishu','wechat','slack')),
    CONSTRAINT nchan_scope_chk CHECK (scope IN ('platform','workspace','application'))
);

-- ============================================================================
-- 11. 告警
-- ============================================================================

-- ---------- 11.1 vo_alert_rules ----------
CREATE TABLE vo_alert_rules (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    scope               VARCHAR(16) NOT NULL,
    scope_id            BIGINT,
    name                VARCHAR(128) NOT NULL,
    description         TEXT,
    metric              VARCHAR(128) NOT NULL,
    condition           VARCHAR(16) NOT NULL,
    threshold           FLOAT,
    window_minutes      INT NOT NULL DEFAULT 5,
    severity            VARCHAR(16) NOT NULL DEFAULT 'warning',
    enabled             BOOLEAN NOT NULL DEFAULT true,
    notify_channels     BIGINT[] NOT NULL DEFAULT '{}',
    cooldown_minutes    INT NOT NULL DEFAULT 30,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT,
    CONSTRAINT alert_cond_chk CHECK (condition IN ('gt','gte','lt','lte','eq','neq')),
    CONSTRAINT alert_sev_chk  CHECK (severity IN ('info','warning','critical'))
);

-- ---------- 11.2 vo_alert_events ----------
CREATE TABLE vo_alert_events (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    rule_id         BIGINT NOT NULL REFERENCES vo_alert_rules(id),
    scope           VARCHAR(16) NOT NULL,
    scope_id        BIGINT,
    resource_type   VARCHAR(32),
    resource_id     BIGINT,
    severity        VARCHAR(16) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'firing',
    message         TEXT,
    current_value   FLOAT,
    fired_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    notified_count  INT NOT NULL DEFAULT 0,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT,
    CONSTRAINT alert_evt_status_chk CHECK (status IN ('firing','resolved','suppressed'))
);
CREATE INDEX idx_alert_evt_firing ON vo_alert_events (status, fired_at) WHERE status = 'firing';

-- ============================================================================
-- 12. 审计
-- ============================================================================

-- ---------- 12.1 vo_audit_logs（分区表） ----------
CREATE TABLE vo_audit_logs (
    id             BIGSERIAL,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id        BIGINT,
    user_name      VARCHAR(128),
    workspace_id   BIGINT,
    resource_type  VARCHAR(32) NOT NULL,
    resource_id    BIGINT,
    resource_name  VARCHAR(255),
    action         VARCHAR(32) NOT NULL,
    operation      VARCHAR(64),
    request_id     VARCHAR(64),
    method         VARCHAR(8),
    path           VARCHAR(255),
    status_code    INT,
    client_ip      VARCHAR(64),
    user_agent     VARCHAR(255),
    request_body   JSONB,
    response_summary JSONB,
    duration_ms    INT,
    error_message  TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);
CREATE INDEX idx_audit_user_time ON vo_audit_logs (user_id, created_at DESC);
CREATE INDEX idx_audit_resource  ON vo_audit_logs (resource_type, resource_id, created_at DESC);
CREATE INDEX idx_audit_workspace ON vo_audit_logs (workspace_id, created_at DESC);
CREATE INDEX idx_audit_action    ON vo_audit_logs (action, created_at DESC);

-- ============================================================================
-- 12b. 堡垒机（JumpServer 集成）
-- ============================================================================

-- ---------- 12b.1 vo_bastion_assets（堡垒机资产，与 JumpServer 同步） ----------
CREATE TABLE vo_bastion_assets (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    name            VARCHAR(128) NOT NULL,
    host            VARCHAR(255) NOT NULL,
    port            INT NOT NULL DEFAULT 22,
    protocol        VARCHAR(16) NOT NULL DEFAULT 'ssh',
    platform        VARCHAR(32) DEFAULT 'Linux',
    username        VARCHAR(64),
    credential_id   BIGINT,
    jms_asset_id    VARCHAR(64),
    jms_org_id      VARCHAR(64),
    tags            JSONB NOT NULL DEFAULT '[]',
    comment         TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ
);
CREATE INDEX idx_bastion_ws ON vo_bastion_assets (workspace_id, deleted);
CREATE INDEX idx_bastion_jms ON vo_bastion_assets (jms_asset_id) WHERE jms_asset_id IS NOT NULL;

-- ---------- 12b.2 vo_bastion_sessions（堡垒机会话元数据，同步自 JumpServer） ----------
CREATE TABLE vo_bastion_sessions (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id    BIGINT NOT NULL REFERENCES vo_workspaces(id),
    asset_id        BIGINT REFERENCES vo_bastion_assets(id),
    jms_session_id  VARCHAR(64),
    user_id         BIGINT REFERENCES vo_users(id),
    username        VARCHAR(64),
    asset_name      VARCHAR(128),
    protocol        VARCHAR(16),
    remote_addr     VARCHAR(64),
    login_from      VARCHAR(32),
    status          VARCHAR(16) NOT NULL DEFAULT 'active',
    started_at      TIMESTAMPTZ,
    ended_at        TIMESTAMPTZ,
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    replay_url      TEXT,
    command_count   INT NOT NULL DEFAULT 0,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bastion_sess_ws ON vo_bastion_sessions (workspace_id, started_at);
CREATE INDEX idx_bastion_sess_status ON vo_bastion_sessions (status, started_at);

-- ============================================================================
-- 12c. 运维会话（Pod WebSSH / 端口转发 会话元数据 + 行为审计）
-- ============================================================================

-- ---------- 12c.1 vo_ops_sessions（WebSSH/端口转发会话元数据 + MinIO 录像 key） ----------
CREATE TABLE vo_ops_sessions (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id  BIGINT NOT NULL DEFAULT 0,
    cluster_id    BIGINT NOT NULL,
    namespace     TEXT NOT NULL,
    pod           TEXT NOT NULL,
    container     TEXT,
    type          TEXT NOT NULL DEFAULT 'exec',
    status        TEXT NOT NULL DEFAULT 'active',
    user_id       BIGINT NOT NULL,
    user_name     TEXT NOT NULL DEFAULT '',
    client_ip     TEXT NOT NULL DEFAULT '',
    recording_key TEXT NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at      TIMESTAMPTZ,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    version       INT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_ops_sessions_workspace ON vo_ops_sessions (workspace_id);
CREATE INDEX idx_ops_sessions_cluster ON vo_ops_sessions (cluster_id);
CREATE INDEX idx_ops_sessions_user ON vo_ops_sessions (user_id);
CREATE INDEX idx_ops_sessions_status ON vo_ops_sessions (status);
CREATE INDEX idx_ops_sessions_started ON vo_ops_sessions (started_at DESC);

-- ---------- 12c.2 vo_behavior_audit_logs（WebSSH 命令行为审计） ----------
CREATE TABLE vo_behavior_audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id  BIGINT NOT NULL DEFAULT 0,
    session_id    BIGINT,
    cluster_id    BIGINT NOT NULL,
    namespace     TEXT NOT NULL,
    pod           TEXT NOT NULL,
    user_id       BIGINT NOT NULL,
    user_name     TEXT NOT NULL DEFAULT '',
    command       TEXT NOT NULL,
    risk_level    TEXT NOT NULL DEFAULT 'info',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_behavior_audit_session ON vo_behavior_audit_logs (session_id);
CREATE INDEX idx_behavior_audit_user ON vo_behavior_audit_logs (user_id);
CREATE INDEX idx_behavior_audit_workspace ON vo_behavior_audit_logs (workspace_id);
CREATE INDEX idx_behavior_audit_created ON vo_behavior_audit_logs (created_at DESC);

-- ============================================================================
-- 13. 配置快照
-- ============================================================================

-- ---------- 13.1 vo_config_snapshots ----------
CREATE TABLE vo_config_snapshots (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id      BIGINT NOT NULL REFERENCES vo_workspaces(id),
    name              VARCHAR(128) NOT NULL,
    description       TEXT,
    snapshot_type     VARCHAR(16) NOT NULL,
    payload           JSONB NOT NULL,
    size_bytes        BIGINT,
    storage_key       VARCHAR(512),
    version           INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT cfgsnap_type_chk CHECK (snapshot_type IN ('manual','scheduled','pre_release'))
);

-- ============================================================================
-- 14. 分区与索引策略
-- ============================================================================

-- ---------- 14.1 时间分区（按月，示例创建 2026 年分区） ----------
-- vo_external_api_call_logs
CREATE TABLE vo_external_api_call_logs_2026_01 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE vo_external_api_call_logs_2026_02 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE vo_external_api_call_logs_2026_03 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE vo_external_api_call_logs_2026_04 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE vo_external_api_call_logs_2026_05 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE vo_external_api_call_logs_2026_06 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE vo_external_api_call_logs_2026_07 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE vo_external_api_call_logs_2026_08 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE vo_external_api_call_logs_2026_09 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE vo_external_api_call_logs_2026_10 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE vo_external_api_call_logs_2026_11 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE vo_external_api_call_logs_2026_12 PARTITION OF vo_external_api_call_logs FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- vo_notifications
CREATE TABLE vo_notifications_2026_01 PARTITION OF vo_notifications FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE vo_notifications_2026_02 PARTITION OF vo_notifications FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE vo_notifications_2026_03 PARTITION OF vo_notifications FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE vo_notifications_2026_04 PARTITION OF vo_notifications FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE vo_notifications_2026_05 PARTITION OF vo_notifications FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE vo_notifications_2026_06 PARTITION OF vo_notifications FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE vo_notifications_2026_07 PARTITION OF vo_notifications FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE vo_notifications_2026_08 PARTITION OF vo_notifications FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE vo_notifications_2026_09 PARTITION OF vo_notifications FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE vo_notifications_2026_10 PARTITION OF vo_notifications FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE vo_notifications_2026_11 PARTITION OF vo_notifications FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE vo_notifications_2026_12 PARTITION OF vo_notifications FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- vo_audit_logs
CREATE TABLE vo_audit_logs_2026_01 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE vo_audit_logs_2026_02 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE vo_audit_logs_2026_03 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE vo_audit_logs_2026_04 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE vo_audit_logs_2026_05 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE vo_audit_logs_2026_06 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE vo_audit_logs_2026_07 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE vo_audit_logs_2026_08 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE vo_audit_logs_2026_09 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE vo_audit_logs_2026_10 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE vo_audit_logs_2026_11 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE vo_audit_logs_2026_12 PARTITION OF vo_audit_logs FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- vo_inference_usage
CREATE TABLE vo_inference_usage_2026_01 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE vo_inference_usage_2026_02 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE vo_inference_usage_2026_03 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE vo_inference_usage_2026_04 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE vo_inference_usage_2026_05 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE vo_inference_usage_2026_06 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE vo_inference_usage_2026_07 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE vo_inference_usage_2026_08 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE vo_inference_usage_2026_09 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE vo_inference_usage_2026_10 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE vo_inference_usage_2026_11 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE vo_inference_usage_2026_12 PARTITION OF vo_inference_usage FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- ---------- 14.2 模糊搜索索引 ----------
CREATE INDEX idx_users_username_trgm ON vo_users USING gin (username gin_trgm_ops) WHERE deleted = false;
CREATE INDEX idx_users_email_trgm    ON vo_users USING gin (email    gin_trgm_ops) WHERE deleted = false;
CREATE INDEX idx_apps_name_trgm      ON vo_applications USING gin (name gin_trgm_ops) WHERE deleted = false;
CREATE INDEX idx_groups_name_trgm    ON vo_groups USING gin (name gin_trgm_ops) WHERE deleted = false;

-- ---------- 14.3 Citus 分片声明（仅在 Citus 环境启用） ----------
-- 启用方式：安装 Citus 扩展后取消注释并执行
-- CREATE EXTENSION IF NOT EXISTS citus;
-- SELECT create_distributed_table('vo_groups',                'cluster_id');
-- SELECT create_distributed_table('vo_releases',              'group_id');
-- SELECT create_distributed_table('vo_builds',                'application_id');
-- SELECT create_distributed_table('vo_images',                'application_id');
-- SELECT create_distributed_table('vo_configs',               'scope_id');
-- SELECT create_distributed_table('vo_audit_logs',            'workspace_id');
-- SELECT create_distributed_table('vo_external_api_call_logs','workspace_id');
-- SELECT create_distributed_table('vo_inference_usage',       'inference_service_id');
-- SELECT create_distributed_table('vo_pipeline_runs',         'workspace_id');

-- ============================================================================
-- 15. Seed 数据
-- ============================================================================

-- ---------- 15.1 平台角色 ----------
INSERT INTO vo_roles (scope, code, name, is_builtin, is_default, description) VALUES
 ('platform','platform_admin','平台管理员',true,false,'拥有平台全部权限'),
 ('platform','platform_auditor','平台审计员',true,false,'只读审计权限'),
 ('workspace','workspace_owner','空间所有者',true,true,'空间内全部权限'),
 ('workspace','workspace_developer','空间开发者',true,false,'空间内开发与发布权限'),
 ('workspace','workspace_viewer','空间访客',true,false,'空间内只读权限'),
 ('application','application_owner','应用所有者',true,true,'应用内全部权限'),
 ('application','application_developer','应用开发者',true,false,'应用内开发权限'),
 ('application','application_operator','应用运维',true,false,'应用发布与运维权限'),
 ('application','application_viewer','应用访客',true,false,'应用只读权限');

-- ---------- 15.1b 权限码矩阵（菜单可见性 + 操作授权） ----------
-- platform_admin 持有通配 '*'，绕过所有细粒度检查；其余角色按 vo_role_permissions 授权。
INSERT INTO vo_permissions (code, name, category, scope, description, sort_order, enabled) VALUES
 ('*',                       '通配（全部权限）',     'action', 'platform', 'platform_admin 隐式持有，绕过所有权限检查', 1, true),
 -- 平台级菜单权限
 ('menu:dashboard:view',     '总览-查看',           'menu',   'platform', '访问总览页面',                 5,   true),
 ('menu:workspace:view',     '空间-查看',           'menu',   'platform', '访问工作空间列表',             20,  true),
 ('menu:application:view',   '应用-查看',           'menu',   'platform', '访问应用',                     30,  true),
 ('menu:middleware:view',    '中间件-查看',         'menu',   'platform', '访问中间件目录与实例',         40,  true),
 ('menu:model:view',         '大模型-查看',         'menu',   'platform', '访问模型仓库与推理服务',       50,  true),
 ('menu:pipeline:view',      'CI/CD-查看',          'menu',   'platform', '访问流水线',                   60,  true),
 ('menu:release:view',       '发布-查看',           'menu',   'platform', '访问发布中心',                 70,  true),
 ('menu:release:orch:view',  '多集群发布-查看',     'menu',   'workspace','查看多集群发布编排',           61,  true),
 ('menu:config:view',        '配置-查看',           'menu',   'platform', '访问应用配置',                 80,  true),
 ('menu:monitor:view',       '监控-查看',           'menu',   'platform', '访问容器监控',                 90,  true),
 ('menu:alert:view',         '告警-查看',           'menu',   'platform', '访问告警规则与事件',           100, true),
 ('menu:audit:view',         '审计-查看',           'menu',   'platform', '访问审计日志',                 110, true),
 ('menu:k8s:view',           'K8s 运维-查看',       'menu',   'platform', '访问 K8s 控制台（节点/工作负载/存储/网络/事件）', 135, true),
 ('menu:approval:view',      '审批-查看',           'menu',   'platform', '访问发布审批列表',             145, true),
 ('menu:diagnosis:view',     'AI 诊断-查看',        'menu',   'platform', '访问 AI 诊断',                 155, true),
 ('menu:bastion:view',       '堡垒机-查看',         'menu',   'workspace','查看堡垒机资产列表',           520, true),
 ('menu:cluster:view',       '集群-查看',           'menu',   'platform', '访问集群管理页面',             305, true),
 ('menu:admin:role',         '平台管理-权限/设置',  'menu',   'platform', '管理权限角色与系统设置',       175, true),
 ('menu:admin:user',         '平台管理-用户',       'menu',   'platform', '管理平台用户（列表/禁用/锁定）',176, true),
 -- 平台级 action 权限
 ('cluster:manage',          '集群-管理',           'action', 'platform', '创建/更新/删除集群与凭证',     210, true),
 ('cluster:probe',           '集群-探测',           'action', 'platform', '触发集群连通性探测',           211, true),
 ('system:settings:write',   '系统设置-写入',       'action', 'platform', '修改系统设置',                 220, true),
 ('rbac:manage',             '权限-管理',           'action', 'platform', '管理角色/权限/菜单/绑定',      230, true),
 ('user:manage',             '用户-管理',           'action', 'platform', '管理平台用户状态与角色绑定',   240, true),
 ('audit:export',            '审计-导出',           'action', 'platform', '导出审计日志',                 250, true),
 -- K8s 运维 action 权限
 ('k8s:workload:view',       'K8s-工作负载查看',    'action', 'platform', '查看 Deployment/StatefulSet 等',410, true),
 ('k8s:workload:scale',      'K8s-工作负载扩缩容',  'action', 'platform', '调整工作负载副本数',           411, true),
 ('k8s:workload:delete',     'K8s-工作负载删除',    'action', 'platform', '删除 Pod/工作负载',            412, true),
 ('k8s:node:manage',         'K8s-节点运维',        'action', 'platform', 'cordon/uncordon/drain 节点',   420, true),
 ('k8s:configmap:manage',    'K8s-ConfigMap 管理',  'action', 'platform', '创建/更新/删除 ConfigMap/Secret',430, true),
 ('k8s:storage:view',        'K8s-存储查看',        'action', 'platform', '查看 PV/PVC/StorageClass',     440, true),
 ('k8s:network:view',        'K8s-网络查看',        'action', 'platform', '查看 Service/Ingress/NetworkPolicy', 450, true),
 ('cluster:nodepool:scale',  '集群-节点池扩缩容',   'action', 'platform', '调整云厂商节点池 desired 数',  460, true),
 -- 运维工具 action 权限
 ('ops:exec',                '运维-Pod exec',       'action', 'workspace','进入 Pod 终端',                470, true),
 ('ops:exec:ws',             '运维-Pod WebSSH',     'action', 'platform', '通过 WebSocket 交互登录 Pod 终端',420, true),
 ('ops:portforward',         '运维-端口转发',       'action', 'workspace','建立 Pod 端口转发',            471, true),
 ('ops:session:view',        '运维-会话查看',       'action', 'platform', '查看运维会话与录像回放',       421, true),
 ('audit:behavior:view',     '审计-行为查看',       'action', 'platform', '查看 WebSSH 命令行为审计',     180, true),
 -- 工作空间级 action 权限
 ('workspace:manage',        '空间-管理',           'action', 'workspace','更新空间/成员/配额/集群绑定',   310, true),
 ('application:manage',      '应用-管理',           'action', 'workspace','创建/更新/删除应用与成员',     320, true),
 ('group:manage',            '分组-管理',           'action', 'workspace','创建/更新/删除分组',           330, true),
 ('group:scale',             '分组-扩缩容',         'action', 'workspace','调整分组副本数与 HPA',         331, true),
 ('build:trigger',           '构建-触发',           'action', 'workspace','触发应用构建',                 340, true),
 ('build:cancel',            '构建-取消',           'action', 'workspace','取消运行中的构建',             341, true),
 ('release:trigger',         '发布-触发',           'action', 'workspace','触发分组发布',                 350, true),
 ('release:rollback',        '发布-回滚',           'action', 'workspace','回滚到历史版本',               351, true),
 ('release:abort',           '发布-终止',           'action', 'workspace','终止运行中的发布',             352, true),
 ('release:pause',           '发布-暂停/继续',      'action', 'workspace','暂停/继续发布',                353, true),
 ('release:approve',         '发布-审批',           'action', 'workspace','审批发布申请',                 354, true),
 ('release:orch:create',     '多集群发布-创建',     'action', 'workspace','创建多集群发布编排',           62,  true),
 ('release:orch:abort',      '多集群发布-中止',     'action', 'workspace','中止运行中的发布编排',         63,  true),
 ('config:manage',           '配置-管理',           'action', 'workspace','创建/更新/归档配置与绑定',     360, true),
 ('middleware:manage',       '中间件-管理',         'action', 'workspace','部署/升级/扩缩/备份中间件',    370, true),
 ('pipeline:manage',         '流水线-管理',         'action', 'workspace','创建/删除流水线，触发运行',    380, true),
 ('pipeline:cancel',         '流水线-取消',         'action', 'workspace','取消流水线运行',               381, true),
 ('inference:manage',        '推理-管理',           'action', 'workspace','管理模型仓库/推理服务/路由',   390, true),
 ('inference:deploy',        '推理-部署',           'action', 'workspace','部署/扩缩/回滚推理服务',       391, true),
 -- 堡垒机 action 权限
 ('bastion:asset:manage',    '堡垒机-资产管理',     'action', 'workspace','创建/更新/删除堡垒机资产',     510, true),
 ('bastion:asset:connect',   '堡垒机-连接资产',     'action', 'workspace','通过堡垒机连接 SSH/RDP 资产',  513, true),
 ('bastion:session:connect', '堡垒机-连接会话',     'action', 'workspace','发起 SSH/RDP 连接',            511, true),
 ('bastion:session:view',    '堡垒机-会话审计',     'action', 'platform', '查看会话历史与录像',           512, true),
 ('bastion:sync',            '堡垒机-资产同步',     'action', 'workspace','从 JumpServer 同步资产与权限', 514, true);

-- ---------- 15.1c 内置角色默认授权 ----------
-- platform_admin：通配 '*'
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r, vo_permissions p
WHERE r.code = 'platform_admin' AND p.code = '*';

-- platform_admin：多集群发布 + 运维会话 + 行为审计权限
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r, vo_permissions p
WHERE r.code = 'platform_admin' AND p.code IN (
  'menu:release:orch:view','release:orch:create','release:orch:abort',
  'release:approve','release:pause','ops:exec:ws','ops:session:view','audit:behavior:view'
);

-- platform_admin：堡垒机权限
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r, vo_permissions p
WHERE r.code = 'platform_admin' AND p.code IN (
  'menu:bastion:view','bastion:asset:manage','bastion:asset:connect',
  'bastion:session:connect','bastion:session:view','bastion:sync'
);

-- platform_auditor：所有 menu:* + 审计/只读 action
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.code LIKE 'menu:%'
WHERE r.code = 'platform_auditor';

INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.code IN (
  'audit:export','k8s:workload:view','k8s:storage:view','k8s:network:view',
  'bastion:session:view','ops:session:view','audit:behavior:view'
)
WHERE r.code = 'platform_auditor';

-- workspace_owner：空间内全部 action 权限
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.scope = 'workspace'
WHERE r.code = 'workspace_owner';

-- workspace_developer：开发+发布+构建+配置+推理+中间件+ops+bastion（不含空间管理）
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.code IN (
  'application:manage','group:manage','group:scale','build:trigger','build:cancel',
  'release:trigger','release:rollback','release:abort','release:pause','release:approve',
  'config:manage','pipeline:manage','pipeline:cancel','inference:manage','inference:deploy',
  'middleware:manage','ops:exec','ops:portforward',
  'bastion:asset:manage','bastion:asset:connect','bastion:session:connect'
)
WHERE r.code = 'workspace_developer';

-- workspace_viewer：只读 menu + 会话审计
INSERT INTO vo_role_permissions (role_id, permission_id, granted, created_by)
SELECT r.id, p.id, true, NULL
FROM vo_roles r JOIN vo_permissions p ON p.code IN (
  'menu:application:view','menu:middleware:view','menu:model:view','menu:pipeline:view',
  'menu:release:view','menu:config:view','menu:monitor:view','menu:alert:view','menu:audit:view',
  'menu:k8s:view','menu:bastion:view','bastion:session:view'
)
WHERE r.code = 'workspace_viewer';

-- ---------- 15.2 平台菜单（两级分组树，最终态） ----------
-- 结构：8 个分组目录（directory，无 path/permission_code，对所有登录用户可见）
--       + 叶子菜单（menu，带 path，按 permission_code 过滤）。
-- 顶级保留：dashboard（总览）、diagnosis（AI 诊断）不归组。

-- 15.2a 分组目录
INSERT INTO vo_menus (code, name, path, icon, menu_type, scope, sort_order, visible, permission_code) VALUES
 ('grp-workspace','空间管理', NULL, 'application', 'directory', 'platform', 100, true, NULL),
 ('grp-delivery', '应用交付', NULL, 'build',       'directory', 'platform', 200, true, NULL),
 ('grp-cluster',  '集群运维', NULL, 'cluster',     'directory', 'platform', 300, true, NULL),
 ('grp-ops-tools','运维工具', NULL, 'ops',         'directory', 'platform', 500, true, NULL),
 ('grp-security', '安全审计', NULL, 'audit',       'directory', 'platform', 600, true, NULL),
 ('grp-admin',    '系统管理', NULL, 'setting',     'directory', 'platform', 700, true, NULL),
 ('grp-personal', '个人',     NULL, 'user',        'directory', 'platform', 800, true, NULL);

-- 15.2b 顶级叶子菜单
INSERT INTO vo_menus (parent_id, code, name, path, icon, menu_type, scope, sort_order, visible, permission_code) VALUES
 (NULL, 'dashboard', '总览',    '/dashboard', 'dashboard', 'menu', 'platform', 10,  true, 'menu:dashboard:view'),
 (NULL, 'diagnosis', 'AI 诊断', '/diagnosis', 'diagnose',  'menu', 'platform', 900, true, 'menu:diagnosis:view');

-- 15.2c 分组内叶子菜单（parent_id 通过子查询引用分组 code）
INSERT INTO vo_menus (parent_id, code, name, path, icon, menu_type, scope, sort_order, visible, permission_code)
SELECT g.id, v.code, v.name, v.path, v.icon, v.menu_type, v.scope, v.sort_order, v.visible, v.permission_code
FROM (VALUES
  -- 空间管理
  ('grp-workspace','workspace',       '空间',       '/workspaces',         'application',  'menu','platform',110,true,'menu:workspace:view'),
  ('grp-workspace','config',          '配置',       '/configs',            'config',       'menu','platform',130,true,'menu:config:view'),
  -- 应用交付
  ('grp-delivery', 'builds',          '构建中心',   '/builds',             'build',        'menu','platform',210,true,NULL),
  ('grp-delivery', 'pipeline',        'CI/CD',      '/pipelines',          'pipeline',     'menu','platform',220,true,'menu:pipeline:view'),
  ('grp-delivery', 'model',           '大模型',     '/inference',          'inference',    'menu','platform',230,true,'menu:model:view'),
  ('grp-delivery', 'middleware',      '中间件',     '/middleware',         'middleware',   'menu','platform',240,true,'menu:middleware:view'),
  ('grp-delivery', 'release',         '发布',       '/releases',           'release',      'menu','platform',250,true,'menu:release:view'),
  ('grp-delivery', 'release-orch',    '多集群发布', '/releases/orchestrations','release',   'menu','platform',260,true,'menu:release:orch:view'),
  ('grp-delivery', 'approvals',       '审批中心',   '/approvals',          'approval',     'menu','platform',270,true,'menu:approval:view'),
  -- 集群运维
  ('grp-cluster',  'clusters',        '集群管理',   '/admin/clusters',     'cluster',      'menu','platform',310,true,'menu:cluster:view'),
  ('grp-cluster',  'k8s_console',     'K8s 运维',   '/k8s/workloads',      'k8s',          'directory','platform',320,true,'menu:k8s:view'),
  ('grp-cluster',  'monitor',         '容器监控',   '/monitor',            'monitor',      'menu','platform',330,true,'menu:monitor:view'),
  ('grp-cluster',  'alert',           '告警中心',   '/alerts',             'alert',        'menu','platform',340,true,'menu:alert:view'),
  -- 运维工具
  ('grp-ops-tools','ops-terminal',    'Pod 终端',   '/ops/terminal',       'ops-terminal', 'menu','platform',510,true,'ops:exec'),
  ('grp-ops-tools','port-forward',    '端口转发',   '/ops/port-forward',   'ops',          'menu','platform',520,true,'ops:portforward'),
  ('grp-ops-tools','ops-sessions',    '运维会话',   '/ops/sessions',       'ops',          'menu','platform',530,true,'ops:session:view'),
  ('grp-ops-tools','ops-logs',        '运维日志',   '/ops/logs',           'ops',          'menu','platform',540,true,NULL),
  -- 安全审计
  ('grp-security', 'audit',           '审计日志',   '/audit',              'audit',        'menu','platform',610,true,'menu:audit:view'),
  ('grp-security', 'behavior-audit',  '行为审计',   '/audit/behavior',     'audit',        'menu','platform',620,true,'audit:behavior:view'),
  -- 系统管理
  ('grp-admin',    'rbac',            '权限管理',   '/admin/roles',        'rbac',         'menu','platform',710,true,'menu:admin:role'),
  ('grp-admin',    'admin_users',     '用户管理',   '/admin/users',        'user',         'menu','platform',720,true,'menu:admin:user'),
  ('grp-admin',    'system_settings', '系统设置',   '/admin/settings',     'setting',      'menu','platform',730,true,'menu:admin:role'),
  ('grp-admin',    'base-images',     '基础镜像',   '/admin/base-images',  'container',    'menu','platform',740,true,'menu:admin:role'),
  -- 个人
  ('grp-personal', 'profile',         '个人中心',   '/me',                 'user',         'menu','platform',810,true,NULL),
  ('grp-personal', 'tokens',          'API Token',  '/me/tokens',          'token',        'menu','platform',820,true,NULL)
) AS v(parent_code, code, name, path, icon, menu_type, scope, sort_order, visible, permission_code)
JOIN vo_menus g ON g.code = v.parent_code;

-- ---------- 15.3 系统字典（环境） ----------
INSERT INTO vo_sys_dictionaries (category, code, label, sort_order, enabled) VALUES
 ('environment','dev','开发',10,true),
 ('environment','test','测试',20,true),
 ('environment','staging','预发',30,true),
 ('environment','prod','生产',40,true);

INSERT INTO vo_sys_dictionaries (category, code, label, sort_order, enabled) VALUES
 ('workload_type','deployment','Deployment',10,true),
 ('workload_type','statefulset','StatefulSet',20,true),
 ('workload_type','cronjob','CronJob',30,true),
 ('workload_type','job','Job',40,true);

INSERT INTO vo_sys_dictionaries (category, code, label, sort_order, enabled) VALUES
 ('network_mode','clusterip','ClusterIP',10,true),
 ('network_mode','nodeport','NodePort',20,true),
 ('network_mode','loadbalancer','LoadBalancer',30,true),
 ('network_mode','hostnetwork','HostNetwork',40,true);

-- ---------- 15.4 资源模板 ----------
INSERT INTO vo_resource_templates (name, scope, cpu_m, cpu_limit_m, memory_bytes, memory_limit_bytes, gpu, description, is_system) VALUES
 ('极小 (0.1C/128Mi)','platform',100,200,134217728,268435456,0,'最低规格，适合测试',true),
 ('小 (0.5C/512Mi)','platform',500,1000,536870912,1073741824,0,'轻量服务',true),
 ('中 (1C/2Gi)','platform',1000,2000,2147483648,4294967296,0,'常规服务',true),
 ('大 (2C/4Gi)','platform',2000,4000,4294967296,8589934592,0,'计算密集',true),
 ('超大 (4C/8Gi)','platform',4000,8000,8589934592,17179869184,0,'高负载',true),
 ('GPU 小 (2C/8Gi + 1 GPU)','platform',2000,4000,8589934592,17179869184,1,'单卡推理',true),
 ('GPU 大 (8C/32Gi + 1 GPU)','platform',8000,16000,34359738368,68719476736,1,'大模型单卡',true),
 ('GPU 多卡 (8C/32Gi + 4 GPU)','platform',8000,16000,34359738368,68719476736,4,'大模型多卡',true);

-- ---------- 15.5 中间件目录 ----------
INSERT INTO vo_middleware_catalog (name, type, version, description, is_recommended, is_system) VALUES
 ('mysql-8.0','mysql','8.0','MySQL 8.0 关系型数据库',true,true),
 ('postgresql-16','postgresql','16','PostgreSQL 16 关系型数据库',true,true),
 ('redis-7','redis','7','Redis 7 内存数据库',true,true),
 ('mongodb-7','mongodb','7','MongoDB 7 文档数据库',true,true),
 ('kafka-3','kafka','3.7','Kafka 3.7 消息队列',true,true),
 ('zookeeper-3','zookeeper','3.8','ZooKeeper 3.8 协调服务',false,true),
 ('elasticsearch-8','elasticsearch','8.13','Elasticsearch 8.13 搜索引擎',true,true),
 ('minio','minio','latest','MinIO 对象存储',true,true),
 ('rabbitmq-3','rabbitmq','3.13','RabbitMQ 3.13 消息队列',false,true),
 ('nacos-2','nacos','2.3','Nacos 2.3 配置与注册中心',false,true),
 ('consul-1','consul','1.18','Consul 1.18 服务发现',false,true),
 ('etcd-3','etcd','3.5','etcd 3.5 KV 存储',false,true);

-- ---------- 15.6 基础镜像 ----------
-- 内置基础镜像的 dockerfile_template 为单阶段运行时模板（构建在引擎侧用 builder_image 完成，
-- 运行时镜像只 COPY 制品），占位符：{{.BaseImage}} / {{.ArtifactPath}} / {{.Entrypoint}}。
-- entrypoint 为容器启动命令（JSON 数组），渲染时注入 ENTRYPOINT。
-- 内置基础镜像：Dockerfile 模板统一 COPY 制品到 /app/artifacts/ 子目录，
-- 运行时 entrypoint 通过 glob（如 /app/artifacts/*.jar）解析，兼容单 jar/多 jar/多模块场景。
-- 历史 0001 初版曾用 `COPY {{.ArtifactPath}} /app/app.jar`，但 artifact_path 为 glob 且匹配多个 jar
-- 时构建失败；多模块项目 jar 在 module/target/ 也匹配失败。0002/0012 修复为此版本，本文件已合并。
INSERT INTO vo_base_images (name, runtime, image_ref, is_system, is_recommended, description, dockerfile_template, entrypoint) VALUES
 ('OpenJDK 17','java','eclipse-temurin:17-jre',true,true,'Java 17 运行时',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
EXPOSE 8080
ENTRYPOINT {{.Entrypoint}}',
  '["sh","-c","exec java $JAVA_OPTS -jar /app/artifacts/*.jar"]'::jsonb),
 ('OpenJDK 21','java','eclipse-temurin:21-jre',true,true,'Java 21 LTS 运行时',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
EXPOSE 8080
ENTRYPOINT {{.Entrypoint}}',
  '["sh","-c","exec java $JAVA_OPTS -jar /app/artifacts/*.jar"]'::jsonb),
 ('Python 3.12','python','python:3.12-slim',true,true,'Python 3.12 运行时',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
EXPOSE 8000
ENTRYPOINT {{.Entrypoint}}',
  '["sh","-c","cd /app/artifacts && gunicorn --bind 0.0.0.0:8000 app:app"]'::jsonb),
 ('Go 1.22','go','golang:1.22-alpine',true,true,'Go 1.22 运行时',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
EXPOSE 8080
ENTRYPOINT {{.Entrypoint}}',
  '["/app/artifacts/app"]'::jsonb),
 ('Node 20','node','node:20-alpine',true,true,'Node.js 20 LTS 运行时',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
EXPOSE 80
ENTRYPOINT {{.Entrypoint}}',
  '["nginx","-g","daemon off;"]'::jsonb),
 ('Ubuntu 22.04','custom','ubuntu:22.04',true,false,'通用基础镜像',
  'FROM {{.BaseImage}}
WORKDIR /app
COPY {{.ArtifactPath}} /app/artifacts/
ENTRYPOINT {{.Entrypoint}}',
  '["./artifacts/app"]'::jsonb)
ON CONFLICT DO NOTHING;

-- ---------- 15.6b 构建工具 ----------
INSERT INTO vo_build_tools (name, runtime, tool, default_build_command, default_artifact_path, builder_image, is_system, description) VALUES
 ('Java Maven',  'java',   'maven',  'mvn -B clean package -DskipTests',       'target/*.jar',      'maven:3.9-eclipse-temurin-17',  true, 'Java Maven 构建工具'),
 ('Java Gradle', 'java',   'gradle', 'gradle build -x test',                  'build/libs/*.jar',  'gradle:8-jdk17',                 true, 'Java Gradle 构建工具'),
 ('Go Build',    'go',     'go',     'go build -o app ./...',                 'app',               'golang:1.22-alpine',            true, 'Go 构建工具'),
 ('Node npm',    'node',   'npm',    'npm ci && npm run build',               'dist',              'node:20-alpine',                true, 'Node.js npm 构建工具'),
 ('Node yarn',   'node',   'yarn',   'yarn install && yarn build',            'dist',              'node:20-alpine',                true, 'Node.js yarn 构建工具'),
 ('Node pnpm',   'node',   'pnpm',   'pnpm install && pnpm build',            'dist',              'node:20-alpine',                true, 'Node.js pnpm 构建工具'),
 ('Python pip',  'python', 'pip',    'pip install -r requirements.txt',       '.',                 'python:3.11-slim',              true, 'Python pip 构建工具'),
 ('Python poetry','python','poetry', 'poetry install',                        '.',                 'python:3.11-slim',              true, 'Python poetry 构建工具')
ON CONFLICT DO NOTHING;

-- ---------- 15.7 通知模板 ----------
INSERT INTO vo_notification_templates (code, name, channel, subject_tpl, body_tpl, variables, is_system) VALUES
 ('release_started','发布已开始','inapp','发布 {{group_name}} #{{release_number}} 已开始','应用 {{application_name}} 的分组 {{group_name}} 发布 #{{release_number}} 已开始，策略：{{strategy}}', '["group_name","release_number","application_name","strategy"]', true),
 ('release_succeeded','发布成功','inapp','发布 {{group_name}} #{{release_number}} 成功','应用 {{application_name}} 的分组 {{group_name}} 发布 #{{release_number}} 已成功完成', '["group_name","release_number","application_name"]', true),
 ('release_failed','发布失败','inapp','发布 {{group_name}} #{{release_number}} 失败','应用 {{application_name}} 的分组 {{group_name}} 发布 #{{release_number}} 失败：{{reason}}', '["group_name","release_number","application_name","reason"]', true),
 ('approval_pending','待审批','inapp','{{resource_type}} 审批待处理','{{requester}} 提交的 {{resource_type}} 操作待您审批', '["resource_type","requester"]', true),
 ('alert_firing','告警触发','inapp','[{{severity}}] {{rule_name}}','告警规则 {{rule_name}} 触发，当前值 {{current_value}}', '["severity","rule_name","current_value"]', true),
 ('build_failed','构建失败','inapp','构建 {{application_name}} #{{build_number}} 失败','应用 {{application_name}} 构建 #{{build_number}} 失败：{{reason}}', '["application_name","build_number","reason"]', true);

-- ---------- 15.8 默认系统设置 ----------
INSERT INTO vo_system_settings (key, value, description, is_public) VALUES
 ('platform.name',          '"VortexOps"',                '平台名称', true),
 ('platform.version',       '"1.0.0"',                    '平台版本', true),
 ('platform.locale',        '"zh-CN"',                    '默认语言', true),
 ('platform.timezone',      '"Asia/Shanghai"',            '默认时区', true),
 ('build.default_timeout',  '3600',                       '构建默认超时（秒）', false),
 ('release.default_strategy','"rolling"',                 '默认发布策略', false),
 ('release.max_batch_size', '20',                         '单批最大副本数', false),
 ('audit.retention_days',   '365',                        '审计日志保留天数', false),
 ('notification.enabled',   'true',                       '是否启用通知', false),
 ('security.password_min_length','8',                     '密码最小长度', false),
 ('security.session_timeout_min','60',                    '会话超时（分钟）', false),
 ('security.mfa_required',  'false',                      '是否强制 MFA', false),
 -- 默认 Jenkins / Registry 实例 ID（创建实例后填入）
 ('platform.default_jenkins_id',  'null'::jsonb,          '默认 Jenkins 实例 ID', false),
 ('platform.default_registry_id', 'null'::jsonb,          '默认镜像仓库实例 ID', false),
 -- 容器监控 Prometheus 地址
 ('monitoring.prometheus_url',     '"http://prometheus-server:9090"'::jsonb, 'Prometheus 查询地址', false),
 -- Tekton 构建引擎
 ('platform.build_engine',  '"jenkins"',                  '构建引擎（jenkins | tekton）', false),
 ('tekton.namespace',       '"tekton-pipelines"',         'Tekton 所在命名空间', false),
 ('tekton.kubeconfig',      'null'::jsonb,                'Tekton 运行集群 kubeconfig（base64），为空则用平台默认集群', false),
 -- JumpServer 堡垒机集成
 ('jumpserver.base_url',    '""'::jsonb,                  'JumpServer 基础 URL（如 http://jumpserver:8080）', false),
 ('jumpserver.access_key',  '""'::jsonb,                  'JumpServer API Access Key', false),
 ('jumpserver.secret_key',  '""'::jsonb,                  'JumpServer API Secret Key', false),
 -- AI 诊断
 ('ai.diagnosis.provider',  '"openai"',                   'AI 诊断 Provider（openai | anthropic | ollama）', false),
 ('ai.diagnosis.url',       '""'::jsonb,                  'AI 诊断 API 基地址', false),
 ('ai.diagnosis.api_key',   '""'::jsonb,                  'AI 诊断 API Key', false),
 ('ai.diagnosis.model',     '"gpt-4o"',                   'AI 诊断模型名', false);

-- ---------- 15.9 工作空间创建策略 ----------
INSERT INTO vo_workspace_creation_policies (name, applies_to_roles, allow_self_create, max_workspaces_per_user, default_quota, default_clusters, require_approval, auto_bind_catalog) VALUES
 ('默认策略','[]',true,5,'{"max_applications":10,"max_groups":50,"max_concurrent_builds":5,"max_members":20}','{}',false,true);

-- ---------- 15.10 默认管理员用户（仅开发/演示环境） ----------
-- 密码 admin123 的 bcrypt cost=10 哈希。生产部署应通过正式用户管理流程创建管理员，可删除此段。
-- phone/avatar_url/external_id/last_login_ip 用 '' 而非 NULL，因 identityrepo.scanUser 将这些列扫入 string 字段。
INSERT INTO vo_users (
    uuid, username, email, phone, display_name, avatar_url,
    password_hash, auth_source, external_id, status,
    last_login_at, last_login_ip, password_changed_at, must_change_password,
    locale, timezone, metadata, version, created_by, updated_by
) VALUES (
    'a0000000-0000-0000-0000-000000000001',
    'admin',
    'admin@vortexops.local',
    '',
    '平台管理员',
    '',
    '$2a$10$lHuRbTOfhb1jhEXqUavea.yywOz0f5KE0ZxiyE7hYnQADjAwfB4yy',
    'local',
    '',
    'active',
    NULL, '', now(), false,
    'zh-CN', 'Asia/Shanghai', '{}'::jsonb, 1, 0, 0
)
ON CONFLICT (username) DO NOTHING;

-- 绑定 platform_admin 角色（拥有通配 '*' 权限，可见全部菜单）
INSERT INTO vo_platform_role_bindings (user_id, role_id, expires_at, version, created_by, updated_by)
SELECT u.id, r.id, NULL, 1, NULL, NULL
FROM vo_users u, vo_roles r
WHERE u.username = 'admin'
  AND r.scope = 'platform'
  AND r.code = 'platform_admin'
  AND NOT EXISTS (
      SELECT 1 FROM vo_platform_role_bindings b
      WHERE b.user_id = u.id AND b.role_id = r.id AND b.deleted = false
  )
ON CONFLICT (user_id, role_id) DO NOTHING;

-- ---------- 15.11 角色-菜单直接绑定 ----------
-- 为 platform_admin（持通配 '*' 权限）绑定全部平台菜单，确保管理员可见。
INSERT INTO vo_role_menus (role_id, menu_id, created_by)
SELECT r.id, m.id, 1
FROM vo_roles r
CROSS JOIN vo_menus m
WHERE r.scope = 'platform' AND r.code = 'platform_admin' AND r.deleted = false
  AND m.scope = 'platform' AND m.deleted = false
ON CONFLICT (role_id, menu_id) DO NOTHING;

-- ============================================================================
-- 16. 更新触发器（updated_at 自动维护）
-- ============================================================================
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 为所有带 updated_at 的表挂触发器
DO $$
DECLARE
    t RECORD;
BEGIN
    FOR t IN
        SELECT table_name FROM information_schema.columns
        WHERE column_name = 'updated_at'
          AND table_schema = 'public'
          AND table_name IN (
            'vo_users','vo_api_tokens','vo_sys_dictionaries','vo_system_settings',
            'vo_permissions','vo_menus','vo_roles','vo_role_permissions','vo_role_menus',
            'vo_workspace_members','vo_application_members',
            'vo_clusters','vo_registries','vo_jenkins_instances','vo_credentials',
            'vo_workspaces','vo_workspace_clusters','vo_workspace_quotas','vo_applications',
            'vo_git_sources','vo_groups','vo_base_images','vo_build_tools','vo_images','vo_image_version_tags',
            'vo_build_templates','vo_builds','vo_pipelines','vo_pipeline_stages',
            'vo_pipeline_runs','vo_pipeline_stage_runs','vo_promotions','vo_artifacts_signatures',
            'vo_releases','vo_release_presets','vo_release_windows','vo_configs','vo_config_sets',
            'vo_group_config_bindings','vo_group_local_configs','vo_middleware_catalog','vo_middleware_instances',
            'vo_model_registries','vo_models','vo_model_versions','vo_model_adapters',
            'vo_inference_services','vo_inference_releases','vo_inference_api_keys',
            'vo_inference_routes','vo_approvals','vo_notification_templates',
            'vo_notification_channels','vo_alert_rules','vo_alert_events','vo_config_snapshots',
            'vo_workspace_creation_policies','vo_resource_templates','vo_cluster_ip_pools',
            'vo_node_pools','vo_release_orchestrations','vo_release_orchestration_targets',
            'vo_cluster_operations','vo_cluster_node_status',
            'vo_bastion_assets','vo_bastion_sessions','vo_ops_sessions'
          )
    LOOP
        EXECUTE format(
            'CREATE TRIGGER trg_%s_updated BEFORE UPDATE ON %s
             FOR EACH ROW EXECUTE FUNCTION set_updated_at();',
            t.table_name, t.table_name
        );
    END LOOP;
END $$;

-- ============================================================================
-- 17. 视图（常用查询）
-- ============================================================================

-- ---------- 17.1 应用分组详情视图 ----------
CREATE OR REPLACE VIEW vo_v_group_detail AS
SELECT
    g.id, g.uuid, g.name, g.display_name, g.environment,
    g.replicas, g.workload_type, g.cluster_id, g.namespace,
    a.id AS application_id, a.name AS application_name,
    w.id AS workspace_id, w.name AS workspace_name,
    i.full_reference AS current_image_ref, i.tag AS current_image_tag,
    c.name AS current_config_name, c.config_version AS current_config_version
FROM vo_groups g
JOIN vo_applications a ON a.id = g.application_id
JOIN vo_workspaces w   ON w.id = a.workspace_id
LEFT JOIN vo_images i  ON i.id = g.current_image_id
LEFT JOIN vo_configs c ON c.id = g.current_config_id
WHERE g.deleted = false;

-- ---------- 17.2 空间概览视图 ----------
CREATE OR REPLACE VIEW vo_v_workspace_overview AS
SELECT
    w.id, w.uuid, w.name, w.display_name, w.status, w.owner_id,
    (SELECT count(*) FROM vo_applications a WHERE a.workspace_id = w.id AND a.deleted = false) AS application_count,
    (SELECT count(*) FROM vo_groups g JOIN vo_applications a ON a.id=g.application_id
       WHERE a.workspace_id = w.id AND g.deleted = false) AS group_count,
    (SELECT count(*) FROM vo_middleware_instances m WHERE m.workspace_id = w.id AND m.deleted = false) AS middleware_count,
    (SELECT count(*) FROM vo_inference_services s WHERE s.workspace_id = w.id AND s.deleted = false) AS inference_count,
    (SELECT count(*) FROM vo_workspace_members wm WHERE wm.workspace_id = w.id AND wm.deleted = false) AS member_count
FROM vo_workspaces w
WHERE w.deleted = false;

-- ============================================================================
-- 脚本结束
-- 文档参考: docs/data-model.md
-- 分区维护: 建议使用 pg_partman 或定时任务按月创建新分区
-- Citus: 大规模部署时启用分布式表，参见 §14.3
-- ============================================================================

