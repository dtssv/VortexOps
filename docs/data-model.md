# 数据模型设计

## 1. 总览

- **数据库**：PostgreSQL 16
- **命名**：表名 `snake_case` 复数；字段 `snake_case`
- **主键**：内部 `id BIGSERIAL`；对外 API 统一 `uuid UUID DEFAULT gen_random_uuid()`
- **时间**：`TIMESTAMPTZ`，存储 UTC，展示按用户时区
- **软删除**：`deleted BOOLEAN` + `deleted_at` + `deleted_by`，列表默认过滤 `deleted = false`
- **乐观锁**：业务表含 `version INT`，更新时 `WHERE id = ? AND version = ?`，冲突返回 409
- **审计人**：`created_by` / `updated_by` / `deleted_by` 均 FK → `users.id`
- **扩展属性**：部分实体含 `metadata JSONB`、`labels JSONB`（K8s 风格标签）

### 1.1 通用审计字段（Mixin）

除纯关联表、日志表、分区表外，**所有业务实体表**均包含：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| version | INT NOT NULL DEFAULT 1 | 乐观锁版本号，每次 UPDATE +1 |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT now() | 创建时间 |
| created_by | BIGINT FK vo_users | 创建人 |
| updated_at | TIMESTAMPTZ NOT NULL DEFAULT now() | 最后修改时间 |
| updated_by | BIGINT FK vo_users | 最后修改人 |
| deleted | BOOLEAN NOT NULL DEFAULT false | 软删除标识 |
| deleted_at | TIMESTAMPTZ | 删除时间 |
| deleted_by | BIGINT FK vo_users | 删除人 |

> 下文表定义中，若未重复列出上述字段，默认均包含。

### 1.2 枚举与字典

系统枚举值统一维护在 `vo_sys_dictionaries` 表，便于前端下拉与国际化；核心状态仍用 CHECK 约束保证数据完整性。

---

## 2. ER 关系图（逻辑）

```
users ──┬──< refresh_tokens / user_preferences / user_favorites / notifications
        ├──< platform_role_bindings >── vo_platform_roles ──< vo_platform_role_permissions >── permissions
        ├──< workspace_members >── workspaces ──┬──< applications ──< groups
        │         │                      │      │                  │
        │         │                      │      │                  ├── images     ├── configs
        │         │                      │      │                  ├── builds     ├── releases
        │         │                      │      │                  ├── git_sources├── vo_webhooks
        │         │                      │      │                  └── app_members├── vo_approval_flows
        │         │                      │      │
        │         │                      │      └──< middleware_instances ──< vo_middleware_backups
        │         │                      │                  │            ├── vo_middleware_params
        │         │                      │                  │            └── vo_middleware_releases
        │         │                      │                  └──> middleware_catalog（中间件类型目录）
        │         └── workspace_clusters / workspace_quotas / vo_workspace_role_bindings
        │
        ├── clusters / registries / jenkins_instances / credentials
        ├── audit_logs / vo_activity_feeds / announcements
        └── vo_approval_instances / vo_notification_deliveries

menus ──< vo_menu_permissions >── permissions
permissions ──< role_permissions >── roles (platform/workspace/application scope)
```

> 平台同时管理「应用（无状态，Deployment）」与「中间件（有状态，StatefulSet/Operator/Helm）」两类工作负载。中间件与应用平级挂在空间下，独立成域，复用权限、审批、通知、审计、回收站等横切能力。

---

## 3. 平台基础

### 3.1 `vo_users`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | 对外 ID |
| username | VARCHAR(64) UNIQUE | 登录名 |
| email | VARCHAR(128) UNIQUE | |
| phone | VARCHAR(32) | 可选 |
| display_name | VARCHAR(128) | |
| avatar_url | VARCHAR(512) | 头像 |
| password_hash | VARCHAR(255) | bcrypt；OIDC/LDAP 为空 |
| auth_source | VARCHAR(16) | `local` / `oidc` / `ldap` |
| external_id | VARCHAR(128) | 外部身份 ID |
| status | VARCHAR(16) | `active` / `disabled` / `locked` |
| last_login_at | TIMESTAMPTZ | |
| last_login_ip | VARCHAR(64) | |
| password_changed_at | TIMESTAMPTZ | |
| must_change_password | BOOLEAN DEFAULT false | |
| locale | VARCHAR(16) DEFAULT 'zh-CN' | 语言偏好 |
| timezone | VARCHAR(64) DEFAULT 'Asia/Shanghai' | |
| metadata | JSONB DEFAULT '{}' | 扩展 |
| + 通用审计字段 | | |

**索引**：`(status) WHERE deleted = false`、`(email) WHERE deleted = false`

### 3.2 `vo_refresh_tokens`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| user_id | BIGINT FK vo_users | |
| token_hash | VARCHAR(255) UNIQUE | SHA-256 |
| device_id | VARCHAR(64) | 设备标识 |
| device_name | VARCHAR(128) | 浏览器/客户端描述 |
| ip | VARCHAR(64) | |
| user_agent | VARCHAR(512) | |
| expires_at | TIMESTAMPTZ | |
| revoked_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ | |

### 3.3 `vo_api_tokens`（个人/服务账号 API Token）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| user_id | BIGINT FK vo_users | 所属用户 |
| name | VARCHAR(128) | Token 名称 |
| token_prefix | VARCHAR(16) | 展示前缀 `vo_xxxx` |
| token_hash | VARCHAR(255) UNIQUE | 完整 Token 哈希 |
| scopes | JSONB | 权限范围 `["build:create","release:canary"]`；对外 Token 用 scope 集合如 `["ext:deploy","ext:scale","ext:config"]` |
| allowed_workspaces | BIGINT[] NULL | 空=全部可见空间；非空=限定空间（对外 Token 常用） |
| allowed_apps | BIGINT[] NULL | 空=全部；非空=限定应用 |
| rate_limit_per_min | INT NULL | 每分钟请求上限（对外 Token 限流） |
| ip_allowlist | JSONB NULL | IP 白名单 CIDR 列表 |
| webhook_url | VARCHAR(512) NULL | 回调地址（异步任务结果回调） |
| token_type | VARCHAR(16) DEFAULT 'personal' | `personal` / `service` / `external`（对外 API） |
| expires_at | TIMESTAMPTZ NULL | 空=永不过期 |
| last_used_at | TIMESTAMPTZ | |
| last_used_ip | VARCHAR(64) | |
| status | VARCHAR(16) | `active` / `revoked` |
| + 通用审计字段 | | |

### 3.4 `vo_user_preferences`（用户偏好）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| user_id | BIGINT FK vo_users UNIQUE | |
| theme | VARCHAR(16) DEFAULT 'light' | `light` / `dark` / `system` |
| sidebar_collapsed | BOOLEAN DEFAULT false | |
| default_workspace_id | BIGINT FK vo_workspaces NULL | 登录后默认空间 |
| table_page_size | INT DEFAULT 20 | |
| recent_resources | JSONB DEFAULT '[]' | 最近访问 `[{type,uuid,at}]` |
| dashboard_layout | JSONB | 工作台卡片布局 |
| notification_settings | JSONB | 通知渠道开关 |
| updated_at | TIMESTAMPTZ | |

### 3.5 `vo_sys_dictionaries`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| category | VARCHAR(64) | 如 `build_strategy` |
| code | VARCHAR(64) | 如 `java` |
| label | VARCHAR(128) | 展示名 |
| label_en | VARCHAR(128) | 英文 |
| sort_order | INT DEFAULT 0 | |
| enabled | BOOLEAN DEFAULT true | |
| metadata | JSONB | 扩展（图标、描述） |
| + 通用审计字段 | | |
| UNIQUE(category, code) | | |

### 3.6 `vo_system_settings`（全局配置）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| key | VARCHAR(128) UNIQUE | 如 `build.timeout_minutes` |
| value | JSONB | 配置值 |
| description | TEXT | |
| is_public | BOOLEAN DEFAULT false | 是否前端可读 |
| + 通用审计字段 | | |

### 3.7 `vo_external_api_call_logs`（对外 API 调用日志，分区表）

> 对外 API（部署/扩缩容/配置）每次调用审计，便于外部系统排查与计费。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL | |
| uuid | UUID | |
| token_id | BIGINT FK vo_api_tokens NULL | 调用 Token |
| token_prefix | VARCHAR(16) | 冗余展示 |
| method | VARCHAR(8) | HTTP 方法 |
| path | VARCHAR(255) | 请求路径 |
| operation | VARCHAR(64) | 业务操作（deploy/scale/config/build/rollback） |
| workspace_id | BIGINT NULL | |
| resource_type | VARCHAR(32) NULL | group/inference_service/middleware |
| resource_uuid | VARCHAR(64) NULL | |
| request_id | VARCHAR(64) | 链路 ID |
| status_code | INT | HTTP 状态码 |
| duration_ms | INT | |
| client_ip | VARCHAR(64) | |
| user_agent | VARCHAR(255) | |
| error_message | TEXT NULL | |
| created_at | TIMESTAMPTZ | |

### 3.8 `vo_workspace_creation_policies`（自助建空间策略）

> 控制普通用户能否自建空间及配额上限，满足「用户可自己建空间，不需要管理员创建」。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) | 策略名（如「默认策略」「VIP 用户策略」） |
| applies_to_roles | JSONB | 适用平台角色 `["user"]` 或用户组 |
| allow_self_create | BOOLEAN DEFAULT true | 允许自助创建 |
| max_workspaces_per_user | INT DEFAULT 5 | 单用户空间数上限 |
| default_quota | JSONB | 新空间默认配额 `{cpu_m:8000, memory_mi:16384, pod_count:50, gpu_count:0}` |
| default_clusters | BIGINT[] | 新空间默认绑定集群 |
| require_approval | BOOLEAN DEFAULT false | 创建是否需审批 |
| approver_role | VARCHAR(64) NULL | 审批角色 |
| auto_bind_catalog | BOOLEAN DEFAULT true | 自动绑定平台默认中间件目录/基础镜像 |
| + 通用审计字段 | | |

---

## 4. 权限与菜单

### 4.1 `vo_permissions`（权限码，全局唯一）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| code | VARCHAR(128) UNIQUE | 如 `menu:cluster:view`、`action:release:rolling` |
| name | VARCHAR(128) | 权限名称 |
| category | VARCHAR(32) | `menu` / `action` / `data` |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` |
| description | TEXT | |
| sort_order | INT DEFAULT 0 | |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

> 权限码分三类：`menu:*` 控制菜单可见；`action:*` 控制按钮/API；`data:*` 控制数据范围（如仅看自己触发的构建）。

### 4.2 `vo_menus`（菜单树）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| parent_id | BIGINT FK vo_menus NULL | 父菜单 |
| code | VARCHAR(64) UNIQUE | 如 `cluster`、`build-release` |
| name | VARCHAR(128) | 菜单名 |
| name_en | VARCHAR(128) | |
| path | VARCHAR(255) | 前端路由 |
| icon | VARCHAR(64) | Ant Design 图标名 |
| component | VARCHAR(255) | 前端组件路径 |
| menu_type | VARCHAR(16) | `directory` / `menu` / `button` |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` |
| permission_code | VARCHAR(128) FK vo_permissions.code | 关联权限码 |
| visible | BOOLEAN DEFAULT true | |
| sort_order | INT DEFAULT 0 | |
| keep_alive | BOOLEAN DEFAULT false | 路由缓存 |
| external_link | VARCHAR(512) | 外链 |
| metadata | JSONB | |
| + 通用审计字段 | | |

### 4.3 `vo_roles`（角色，支持平台/空间/应用三级 + 自定义）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` |
| scope_id | BIGINT NULL | workspace_id 或 application_id；platform 级为空 |
| code | VARCHAR(64) | 如 `admin`、`developer`、`ops-only` |
| name | VARCHAR(128) | 角色名 |
| description | TEXT | |
| is_builtin | BOOLEAN DEFAULT false | 内置角色不可删 |
| is_default | BOOLEAN DEFAULT false | 新成员默认角色 |
| enabled | BOOLEAN DEFAULT true | |
| metadata | JSONB | |
| + 通用审计字段 | | |
| UNIQUE(scope, scope_id, code) | | |

> 内置角色：`platform_admin`、`platform_ops`、`platform_auditor`；空间/应用级 `admin`/`developer`/`tester`/`viewer`。企业可创建自定义角色如「仅构建发布」「仅集群只读」。

### 4.4 `vo_role_permissions`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| role_id | BIGINT FK vo_roles | |
| permission_id | BIGINT FK vo_permissions | |
| granted | BOOLEAN DEFAULT true | false 表示显式拒绝（优先级高于授予） |
| created_by | BIGINT FK vo_users | |
| created_at | TIMESTAMPTZ | |
| UNIQUE(role_id, permission_id) | | |

### 4.5 `vo_platform_role_bindings`（平台级角色绑定）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| user_id | BIGINT FK vo_users | |
| role_id | BIGINT FK vo_roles | scope=platform |
| expires_at | TIMESTAMPTZ NULL | 临时授权 |
| + 通用审计字段 | | |
| UNIQUE(user_id, role_id) | | |

### 4.6 `vo_workspace_members`（空间成员，绑定角色）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| workspace_id | BIGINT FK vo_workspaces | |
| user_id | BIGINT FK vo_users | |
| role_id | BIGINT FK vo_roles | 空间级角色 |
| invited_by | BIGINT FK vo_users | 邀请人 |
| joined_at | TIMESTAMPTZ | |
| status | VARCHAR(16) | `active` / `pending` / `removed` |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, user_id) WHERE deleted = false | | |

### 4.7 `vo_application_members`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| application_id | BIGINT FK vo_applications | |
| user_id | BIGINT FK vo_users | |
| role_id | BIGINT FK vo_roles | 应用级角色 |
| invited_by | BIGINT FK vo_users | |
| joined_at | TIMESTAMPTZ | |
| status | VARCHAR(16) | `active` / `pending` / `removed` |
| + 通用审计字段 | | |
| UNIQUE(application_id, user_id) WHERE deleted = false | | |

---

## 5. 集群与基础设施

### 5.1 `vo_clusters`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) UNIQUE | |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| api_server | VARCHAR(255) | |
| kubeconfig_encrypted | BYTEA | KMS 加密 |
| ca_cert_encrypted | BYTEA | 可选单独 CA |
| default_namespace_prefix | VARCHAR(64) | |
| insecure_skip_tls | BOOLEAN DEFAULT false | |
| region | VARCHAR(64) | 地域 |
| environment | VARCHAR(16) | `prod` / `staging` / `dev` |
| k8s_version | VARCHAR(32) | 探测到的版本 |
| node_count | INT | 缓存 |
| status | VARCHAR(16) | `healthy` / `degraded` / `unreachable` / `disabled` |
| last_checked_at | TIMESTAMPTZ | |
| last_error | TEXT | 最近健康检查错误 |
| labels | JSONB DEFAULT '{}' | |
| metadata | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |

### 5.2 `vo_registries`（镜像仓库实例）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) UNIQUE | |
| type | VARCHAR(16) | `harbor` / `docker_registry` / `acr` / `ecr` |
| url | VARCHAR(255) | |
| credential_id | BIGINT FK vo_credentials | |
| is_default | BOOLEAN DEFAULT false | |
| status | VARCHAR(16) | `active` / `disabled` |
| + 通用审计字段 | | |

### 5.3 `vo_jenkins_instances`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) UNIQUE | |
| url | VARCHAR(255) | |
| credential_id | BIGINT FK vo_credentials | API Token |
| default_job_folder | VARCHAR(128) | Job 目录 |
| is_default | BOOLEAN DEFAULT true | |
| status | VARCHAR(16) | `active` / `disabled` |
| last_checked_at | TIMESTAMPTZ | |
| + 通用审计字段 | | |

### 5.4 `vo_credentials`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(128) | |
| kind | VARCHAR(32) | `git_password` / `git_ssh` / `git_token` / `registry` / `kubeconfig` / `jenkins` |
| scope | VARCHAR(16) | `platform` / `workspace` |
| scope_id | BIGINT NULL | workspace_id |
| payload_encrypted | BYTEA | KMS 加密 JSON |
| expires_at | TIMESTAMPTZ NULL | |
| last_rotated_at | TIMESTAMPTZ | |
| + 通用审计字段 | | |

### 5.5 `vo_cluster_node_pools`（集群节点池画像，缓存）

> 平台周期性从集群采集节点池/GPU/网络能力，供创建分组时下拉选择硬件规格。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| cluster_id | BIGINT FK vo_clusters | |
| name | VARCHAR(128) | 节点池/机型名（如 `gpu-a100-pool`） |
| node_count | INT | 节点数 |
| cpu_cores_per_node | INT | 单节点核数 |
| memory_bytes_per_node | BIGINT | 单节点内存 |
| gpu_count_per_node | INT DEFAULT 0 | 单节点 GPU 卡数 |
| gpu_type | VARCHAR(64) NULL | GPU 型号 |
| gpu_resource_name | VARCHAR(128) NULL | extended resource 名 |
| taints | JSONB | 污点（用于自动生成 tolerations） |
| labels | JSONB | 节点标签（含机型、GPU、专用节点标记） |
| storage_classes | JSONB | 该池可用 StorageClass |
| available | BOOLEAN DEFAULT true | 是否可选 |
| last_synced_at | TIMESTAMPTZ | |

### 5.6 `vo_cluster_ip_pools`（稳定 IP 池）

> 用于 `keep_pod_ip=true` 的分组与中间件实例。平台管理一段 IP，Pod 首次分配后自动保留，重新部署/滚动更新复用同 IP，无需用户显式指定。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| cluster_id | BIGINT FK vo_clusters | |
| name | VARCHAR(128) | IP 池名 |
| cidr | VARCHAR(64) | 如 `10.0.1.0/24` |
| gateway | VARCHAR(64) | |
| provider | VARCHAR(32) | `metallb` / `calico-ipam` / `whereabouts` / `kube-ovn` |
| total_count | INT | 总 IP 数 |
| allocated_count | INT | 已分配（含保留中） |
| reserved_ips | JSONB | 预留 IP（网关、广播等） |
| + 通用审计字段 | | |

### 5.7 `vo_cluster_ip_allocations`（IP 分配与保留记录）

> 记录稳定 IP 的分配与保留。`keep_pod_ip=true` 的 Pod 首次拿到 IP 后写入 `status=allocated`；Pod 重建时平台从本表查回同 IP 并注入；分组删除才 `status=released`。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| ip_pool_id | BIGINT FK vo_cluster_ip_pools | |
| cluster_id | BIGINT FK vo_clusters | 冗余 |
| ip_address | VARCHAR(64) | 分配的 IP |
| resource_type | VARCHAR(32) | `group` / `middleware_instance` / `service` |
| resource_id | BIGINT | |
| replica_index | INT NULL | 副本序号（StatefulSet/稳定 IP） |
| status | VARCHAR(16) | `allocated` / `released` |
| allocated_at | TIMESTAMPTZ | |
| released_at | TIMESTAMPTZ NULL | |
| UNIQUE(ip_pool_id, ip_address) | | |

### 5.8 `vo_resource_templates`（资源规格模板，空间/平台级）

> 预设规格（如 `4C8G` / `8C16G+1*A100`），创建分组时一键套用，减少手工填写。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(128) | 如 `标准-4C8G` |
| scope | VARCHAR(16) | `platform` / `workspace` |
| scope_id | BIGINT NULL | workspace_id |
| cpu_m | INT | |
| cpu_limit_m | INT NULL | |
| memory_bytes | BIGINT | |
| memory_limit_bytes | BIGINT NULL | |
| gpu | INT DEFAULT 0 | |
| gpu_type | VARCHAR(64) NULL | |
| storage_size_bytes | BIGINT NULL | |
| ephemeral_storage_request_bytes | BIGINT NULL | |
| ephemeral_storage_limit_bytes | BIGINT NULL | |
| node_selector | JSONB | 建议节点选择 |
| tolerations | JSONB | 建议容忍 |
| description | VARCHAR(255) | |
| is_system | BOOLEAN DEFAULT false | |
| + 通用审计字段 | | |

---

## 6. 空间与应用

### 6.1 `vo_workspaces`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) UNIQUE | 空间标识 |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| logo_url | VARCHAR(512) | |
| status | VARCHAR(16) | `active` / `archived` / `frozen` |
| owner_id | BIGINT FK vo_users | 空间 Owner |
| default_registry_id | BIGINT FK vo_registries NULL | |
| default_jenkins_id | BIGINT FK vo_jenkins_instances NULL | |
| labels | JSONB DEFAULT '{}' | |
| metadata | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |

### 6.2 `vo_workspace_clusters`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | |
| cluster_id | BIGINT FK vo_clusters | |
| namespace | VARCHAR(128) | |
| role | VARCHAR(16) | `primary` / `secondary` |
| auto_create_namespace | BOOLEAN DEFAULT false | |
| resource_quota | JSONB | CPU/内存/Pod 配额 |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, cluster_id, namespace) WHERE deleted = false | | |

### 6.3 `vo_workspace_quotas`（空间配额）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| workspace_id | BIGINT FK vo_workspaces UNIQUE | |
| max_applications | INT DEFAULT 50 | |
| max_groups | INT DEFAULT 200 | |
| max_concurrent_builds | INT DEFAULT 10 | |
| max_images_retained | INT DEFAULT 100 | |
| max_members | INT DEFAULT 100 | |
| + 通用审计字段 | | |

### 6.4 `vo_applications`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | |
| name | VARCHAR(64) | 应用标识（空间内唯一） |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| icon | VARCHAR(64) | 应用图标 |
| default_git_source_id | BIGINT FK vo_git_sources NULL | |
| default_registry_id | BIGINT FK vo_registries NULL | |
| lifecycle | VARCHAR(16) | `active` / `frozen` / `archived` |
| owner_id | BIGINT FK vo_users | 应用 Owner |
| labels | JSONB DEFAULT '{}' | |
| metadata | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, name) WHERE deleted = false | | |

### 6.5 `vo_git_sources`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| application_id | BIGINT FK vo_applications | |
| name | VARCHAR(64) | 源名称（应用内唯一） |
| provider | VARCHAR(16) | `github` / `gitlab` / `gitea` / `generic` |
| repo_url | VARCHAR(512) | |
| default_branch | VARCHAR(128) | |
| credential_id | BIGINT FK vo_credentials NULL | |
| webhook_enabled | BOOLEAN DEFAULT false | |
| webhook_secret_hash | VARCHAR(255) | |
| last_synced_at | TIMESTAMPTZ | |
| + 通用审计字段 | | |

### 6.6 `vo_groups`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| application_id | BIGINT FK vo_applications | |
| name | VARCHAR(64) | 分组名（应用内唯一） |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| environment | VARCHAR(16) | `dev` / `test` / `staging` / `prod` |
| cluster_id | BIGINT FK vo_clusters | |
| namespace | VARCHAR(128) | |
| deployment_name | VARCHAR(128) | K8s Deployment 名 |
| service_name | VARCHAR(128) | 可选 Service |
| replicas | INT DEFAULT 1 | |
| current_image_id | BIGINT FK vo_images NULL | |
| current_config_id | BIGINT FK vo_configs NULL | 当前生效配置 |
| current_release_id | BIGINT FK vo_releases NULL | 最近一次成功发布 |
| resources_cpu_m | INT | 请求 CPU（毫核） |
| resources_cpu_limit_m | INT NULL | 上限 CPU（毫核），空=不设 limit |
| resources_memory_bytes | BIGINT | 请求内存 |
| resources_memory_limit_bytes | BIGINT NULL | 上限内存，空=不设 limit |
| resources_gpu | INT DEFAULT 0 | GPU 卡数 |
| gpu_type | VARCHAR(64) NULL | GPU 型号/厂商：`nvidia-a100` / `nvidia-t4` / `ascend-910` 等 |
| gpu_resource_name | VARCHAR(128) NULL | K8s extended resource 名，如 `nvidia.com/gpu` |
| storage_size_bytes | BIGINT NULL | 每副本临时盘/emptyDir 大小 |
| storage_class | VARCHAR(128) NULL | 临时盘 StorageClass（可选） |
| ephemeral_storage_request_bytes | BIGINT NULL | ephemeral-storage 请求 |
| ephemeral_storage_limit_bytes | BIGINT NULL | ephemeral-storage 上限 |
| resource_template_id | BIGINT FK vo_resource_templates NULL | 套用的资源模板 |
| network_mode | VARCHAR(16) DEFAULT 'clusterip' | `clusterip` / `nodeport` / `loadbalancer` / `hostnetwork` |
| service_port_info | JSONB | 端口映射：`[{"port":80,"targetPort":8080,"protocol":"TCP","name":"http"}]` |
| keep_pod_ip | BOOLEAN DEFAULT false | 稳定 IP：重新部署/滚动更新后容器 IP 保持不变（自动保留，无需显式指定 IP） |
| allow_egress_internet | BOOLEAN DEFAULT false | 是否允许访问公网（egress）。false 时生成 NetworkPolicy 拒绝出公网，仅放行集群内/指定白名单 |
| egress_allowlist | JSONB NULL | 公网白名单：`[{"cidr":"0.0.0.0/0"},{"host":"npm.registry","port":443}]`，allow_egress_internet=false 时生效 |
| network_policy_enabled | BOOLEAN DEFAULT false | 是否生成 NetworkPolicy 限制入流量（仅放行同 Namespace + 指定来源） |
| ingress_enabled | BOOLEAN DEFAULT false | 是否创建 Ingress |
| ingress_host | VARCHAR(255) NULL | Ingress host |
| ingress_path | VARCHAR(255) NULL | Ingress path |
| dns_policy | VARCHAR(32) DEFAULT 'ClusterFirst' | `ClusterFirst` / `ClusterFirstWithHostNet` / `Default` / `None` |
| host_network | BOOLEAN DEFAULT false | 是否 hostNetwork（network_mode=hostnetwork 时 true） |
| strategy | VARCHAR(16) | `rolling` / `recreate` |
| max_surge | VARCHAR(16) DEFAULT '25%' | |
| max_unavailable | VARCHAR(16) DEFAULT '25%' | |
| health_check | JSONB | 探针配置 |
| node_selector | JSONB | 节点选择（含 GPU 型号、机型等） |
| node_affinity | JSONB NULL | 节点亲和性（required/preferred） |
| tolerations | JSONB | 容忍 GPU/专用节点污点 |
| priority_class | VARCHAR(128) NULL | PriorityClass 名 |
| workload_type | VARCHAR(16) DEFAULT 'deployment' | `deployment` / `statefulset` / `cronjob` / `job` |
| cron_schedule | VARCHAR(64) NULL | workload_type=cronjob 时的 cron 表达式 |
| job_policy | JSONB NULL | job/cronjob 策略：`{completions, parallelism, backoffLimit, ttlSecondsAfterFinished, concurrencyPolicy}` |
| autoscaling_enabled | BOOLEAN DEFAULT false | 是否启用 HPA |
| hpa_min_replicas | INT NULL | HPA 最小副本 |
| hpa_max_replicas | INT NULL | HPA 最大副本 |
| hpa_metrics | JSONB NULL | HPA 指标：`[{type:"cpu",target:70},{type:"memory",target:80},{type:"custom",name:"qps",target:1000}]` |
| hpa_behavior | JSONB NULL | 扩缩容行为（速率、稳定窗口） |
| release_requires_approval | BOOLEAN DEFAULT false | 发布需审批 |
| labels | JSONB DEFAULT '{}' | |
| metadata | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(application_id, name) WHERE deleted = false | | |

> **工作负载类型**：`deployment`（默认，无状态）、`statefulset`（需稳定身份/顺序，自动启用稳定 IP）、`cronjob`（定时任务，填 `cron_schedule`）、`job`（一次性任务）。不同类型对应不同 K8s 资源与发布策略，见 [K8s 集成 §工作负载映射](kubernetes.md#工作负载映射)。
> **弹性伸缩**：`autoscaling_enabled=true` 时平台生成 HPA（基于 CPU/内存/自定义指标），`replicas` 字段转为初始副本数；可在分组详情查看实时副本数与伸缩事件。
> **资源规格**：创建分组时选择 CPU 核数、内存、磁盘、GPU 卡数与型号，平台转换为 K8s `resources.requests/limits`、`nodeSelector`（GPU 型号）、`tolerations`（GPU 污点）。
> **稳定 IP**：`keep_pod_ip=true` 时，重新部署/滚动更新后容器 IP 保持不变（平台自动保留，无需显式指定 IP），适用于需要稳定 IP 的场景；`statefulset` 默认启用。
> **公网访问**：`allow_egress_internet` 控制分组 Pod 是否可访问公网；关闭时生成 NetworkPolicy 拒绝出公网，仅放行 `egress_allowlist`。
> 详见 [K8s 集成 §资源与网络映射](kubernetes.md#4-资源与网络映射)。

---

## 7. 镜像与构建

### 7.1 `vo_base_images`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(128) | |
| runtime | VARCHAR(16) | `java` / `python` / `go` / `node` / `custom` |
| registry | VARCHAR(255) | |
| image_ref | VARCHAR(512) | 完整引用 |
| digest | VARCHAR(128) | |
| is_system | BOOLEAN DEFAULT false | |
| is_recommended | BOOLEAN DEFAULT false | 推荐标记 |
| description | TEXT | |
| dockerfile_template | TEXT | 内置模板 |
| + 通用审计字段 | | |

### 7.2 `vo_images`（制品版本）

> 每次构建产出一个制品版本记录，按应用维度保留历史，支持随时回退到任意制品版本。一个制品版本 = 一条 image 记录（含 tag/digest/git 来源）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| application_id | BIGINT FK vo_applications | |
| registry_id | BIGINT FK vo_registries | |
| repository | VARCHAR(255) | |
| tag | VARCHAR(128) | |
| digest | VARCHAR(128) | 不可变标识，回退以 digest 为准 |
| full_reference | VARCHAR(512) | |
| version_number | INT | 应用内制品版本自增号（便于「回退到第 N 版」） |
| version_label | VARCHAR(64) NULL | 可选标签（如 `v2.1-hotfix`） |
| source | VARCHAR(16) | `build` / `manual` / `import` |
| build_id | BIGINT FK vo_builds NULL | |
| git_commit_sha | VARCHAR(64) | |
| git_branch | VARCHAR(128) | |
| git_commit_message | TEXT | |
| git_author | VARCHAR(128) | |
| size_bytes | BIGINT | |
| scan_status | VARCHAR(16) | `pending` / `passed` / `failed` / `skipped` |
| scan_result | JSONB | Trivy 结果摘要 |
| status | VARCHAR(16) | `available` / `retired` / `deleted` |
| is_rollback_target | BOOLEAN DEFAULT false | 是否曾被作为回退目标（统计用） |
| labels | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(application_id, version_number) WHERE deleted = false | | |

### 7.2.1 `vo_image_version_tags`（制品版本别名）

> 为制品版本打别名（如 `stable` / `production` / `canary`），别名可在版本间移动，回退时也可「回退到某别名当前指向的版本」。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| application_id | BIGINT FK vo_applications | |
| name | VARCHAR(64) | 别名（应用内唯一） |
| image_id | BIGINT FK vo_images | 当前指向的制品版本 |
| description | VARCHAR(255) | |
| + 通用审计字段 | | |
| UNIQUE(application_id, name) WHERE deleted = false | | |

### 7.3 `vo_build_templates`（构建模板，提升复用体验）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` |
| scope_id | BIGINT NULL | |
| name | VARCHAR(128) | 模板名 |
| description | TEXT | |
| build_strategy | VARCHAR(16) | |
| build_command | TEXT | |
| base_image_id | BIGINT FK vo_base_images | |
| dockerfile_source | VARCHAR(16) | |
| dockerfile_content | TEXT | |
| context_path | VARCHAR(255) DEFAULT '.' | |
| build_args | JSONB DEFAULT '{}' | |
| env_vars | JSONB DEFAULT '{}' | |
| is_default | BOOLEAN DEFAULT false | |
| usage_count | INT DEFAULT 0 | 使用次数 |
| + 通用审计字段 | | |

### 7.4 `vo_builds`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| application_id | BIGINT FK vo_applications | |
| build_number | INT | 应用内自增构建号 |
| git_source_id | BIGINT FK vo_git_sources | |
| ref_type | VARCHAR(16) | `branch` / `tag` / `commit` |
| ref_value | VARCHAR(128) | |
| commit_sha | VARCHAR(64) | |
| commit_message | TEXT | |
| build_template_id | BIGINT FK vo_build_templates NULL | |
| build_strategy | VARCHAR(16) | |
| build_command | TEXT | |
| context_path | VARCHAR(255) | |
| base_image_id | BIGINT FK vo_base_images | |
| dockerfile_source | VARCHAR(16) | |
| dockerfile_content | TEXT | |
| build_args | JSONB | |
| target_registry_id | BIGINT FK vo_registries | |
| target_repository | VARCHAR(255) | |
| target_tag | VARCHAR(128) | |
| output_image_id | BIGINT FK vo_images NULL | |
| jenkins_instance_id | BIGINT FK vo_jenkins_instances | |
| jenkins_queue_id | VARCHAR(64) | |
| jenkins_build_number | INT | |
| jenkins_job_name | VARCHAR(255) | |
| status | VARCHAR(16) | `pending` / `queued` / `running` / `success` / `failed` / `canceled` / `timeout` |
| progress_percent | INT DEFAULT 0 | 0-100 |
| current_step | VARCHAR(64) | 当前步骤 |
| duration_ms | BIGINT | 耗时 |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| log_storage_key | VARCHAR(512) | 完整日志对象存储 key |
| log_excerpt | TEXT | 失败摘要 |
| failure_reason | VARCHAR(64) | 分类：`compile_error` / `docker_error` / `push_error` / `timeout` |
| triggered_by | BIGINT FK vo_users | |
| trigger_source | VARCHAR(16) | `manual` / `webhook` / `api` / `schedule` |
| idempotency_key | VARCHAR(64) | |
| metadata | JSONB | |
| + 通用审计字段 | | |

**索引**：`(application_id, build_number DESC)`、`(application_id, status, created_at DESC)`

### 7.5 `vo_build_steps`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| build_id | BIGINT FK vo_builds | |
| seq | INT | 顺序 |
| name | VARCHAR(64) | `checkout` / `package` / `build_image` / `push` / `callback` |
| status | VARCHAR(16) | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| duration_ms | BIGINT | |
| message | TEXT | |
| log_offset_start | BIGINT | 日志起始偏移 |
| log_offset_end | BIGINT | 日志结束偏移 |
| log_storage_key | VARCHAR(512) NULL | 日志归档对象存储 key（完成后落 S3/OSS） |
| log_size_bytes | BIGINT DEFAULT 0 | 日志大小 |
| error_line | TEXT NULL | 首个错误行摘要（快速定位失败原因） |

### 7.6 `vo_pipelines`（CI/CD 流水线定义）

> 把「构建→测试→扫描→镜像→部署→验证」编排为可复用流水线，超越单次构建。流水线由阶段（stage）组成，阶段内可并行任务，阶段间按门禁推进。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| scope | VARCHAR(16) | `workspace` / `application` |
| scope_id | BIGINT NULL | |
| name | VARCHAR(128) | 如「标准发布流水线」 |
| description | TEXT | |
| trigger | VARCHAR(16) | `manual` / `webhook` / `schedule` / `promotion`（环境晋升触发） |
| trigger_config | JSONB | `{branchMatch:["main"], events:["push"], scheduleCron}` |
| trigger_on_pipeline | BIGINT FK vo_pipelines NULL | 上游流水线完成后触发（晋升链） |
| stages_config | JSONB | 阶段定义（见 pipeline_stages） |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

### 7.7 `vo_pipeline_stages`（阶段定义，存于 stages_config 也可独立表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| pipeline_id | BIGINT FK vo_pipelines | |
| seq | INT | 顺序 |
| name | VARCHAR(64) | `build` / `test` / `scan` / `image` / `deploy` / `verify` / `promote` |
| type | VARCHAR(16) | `parallel` / `sequential` |
| gate | JSONB | 门禁：`{requireTestsPass:true, maxCriticalCVE:0, requireApproval:false, customChecks:["sonar"]}` |
| on_failure | VARCHAR(16) | `abort` / `manual_retry` / `continue` |
| params | JSONB | 阶段参数（部署阶段填目标分组/环境等） |
| + 通用审计字段 | | |

### 7.8 `vo_pipeline_runs`（流水线执行实例）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| pipeline_id | BIGINT FK vo_pipelines | |
| workspace_id | BIGINT | 冗余分片 |
| run_number | INT | 流水线内自增号 |
| trigger | VARCHAR(16) | `manual` / `webhook` / `schedule` / `promotion` |
| trigger_ref | VARCHAR(128) | git ref |
| trigger_commit_sha | VARCHAR(64) | |
| trigger_by | BIGINT FK vo_users NULL | manual 时 |
| status | VARCHAR(16) | `pending` / `running` / `paused`（等门禁/审批）/ `succeeded` / `failed` / `aborted` / `canceled` |
| current_stage_seq | INT | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ NULL | |
| duration_ms | BIGINT NULL | |
| artifacts_image_ids | BIGINT[] | 产出制品版本 |
| metadata | JSONB | |
| + 通用审计字段 | | |
| UNIQUE(pipeline_id, run_number) WHERE deleted = false | | |

### 7.9 `vo_pipeline_stage_runs`（阶段执行）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| pipeline_run_id | BIGINT FK vo_pipeline_runs | |
| stage_id | BIGINT FK vo_pipeline_stages | |
| seq | INT | |
| status | VARCHAR(16) | `pending` / `running` / `paused` / `succeeded` / `failed` / `skipped` |
| related_build_id | BIGINT FK vo_builds NULL | build 阶段关联构建 |
| related_release_id | BIGINT FK vo_releases NULL | deploy 阶段关联发布 |
| related_image_id | BIGINT FK vo_images NULL | |
| gate_result | JSONB | 门禁评估结果（测试通过率、CVE 数等） |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ NULL | |
| message | TEXT | |
| + 通用审计字段 | | |

### 7.10 `vo_promotions`（环境晋升，dev→staging→prod）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | |
| application_id | BIGINT FK vo_applications | |
| source_env | VARCHAR(16) | `dev` / `test` / `staging` |
| target_env | VARCHAR(16) | `staging` / `prod` |
| artifact_image_id | BIGINT FK vo_images | 晋升的制品版本 |
| artifact_config_version | INT NULL | 配置版本（可不同环境不同配置） |
| target_group_ids | BIGINT[] | 目标环境分组 |
| strategy | VARCHAR(16) | `auto`（自动滚动）/ `canary`（灰度晋升）/ `manual`（人工发布） |
| auto_promote_on_verify | BOOLEAN DEFAULT true | 部署后验证通过自动完成；否则暂停 |
| status | VARCHAR(16) | `pending` / `deploying` / `verifying` / `succeeded` / `failed` / `aborted` |
| pipeline_run_id | BIGINT FK vo_pipeline_runs NULL | 关联流水线 |
| approval_instance_id | BIGINT FK vo_approval_instances NULL | prod 晋升审批 |
| started_by | BIGINT FK vo_users | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ NULL | |
| + 通用审计字段 | | |

### 7.11 `vo_artifacts_signatures`（制品签名与 SBOM）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| image_id | BIGINT FK vo_images UNIQUE | |
| signature_type | VARCHAR(16) | `cosign` / `notation` |
| signature_payload | TEXT | 签名内容/引用 |
| public_key_ref | VARCHAR(255) | 验签公钥引用 |
| signed_by | VARCHAR(128) | 签名者 |
| signed_at | TIMESTAMPTZ | |
| sbom_storage_key | VARCHAR(512) NULL | SBOM（CycloneDX/SPDX）对象存储 key |
| sbom_format | VARCHAR(16) NULL | `cyclonedx` / `spdx` |
| provenance | JSONB NULL | SLSA 来源证明 |
| verification_status | VARCHAR(16) | `pending` / `verified` / `failed` |
| + 通用审计字段 | | |

---

## 8. 配置与发布

### 8.1 `vo_configs`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| group_id | BIGINT FK vo_groups | |
| version | INT | 分组内版本号（业务版本，非乐观锁） |
| version_label | VARCHAR(64) | 可选语义化标签 `v1.2.0` |
| change_summary | VARCHAR(255) | |
| change_type | VARCHAR(16) | `file` / `command` / `env` / `mixed` |
| snapshot | JSONB | 完整配置快照 |
| snapshot_hash | VARCHAR(64) | SHA-256，用于 diff |
| is_current | BOOLEAN DEFAULT false | |
| is_draft | BOOLEAN DEFAULT true | 草稿/已应用 |
| applied_at | TIMESTAMPTZ | 生效时间 |
| applied_by | BIGINT FK vo_users | |
| applied_release_id | BIGINT FK vo_releases NULL | 关联发布 |
| parent_config_id | BIGINT FK vo_configs NULL | 基于哪版修改 |
| + 通用审计字段 | | |
| UNIQUE(group_id, version) WHERE deleted = false | | |

### 8.2 `config_files`（配置文件明细，便于单独 diff 与搜索）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| config_id | BIGINT FK vo_configs | |
| path | VARCHAR(512) | 容器内路径 |
| file_mode | VARCHAR(8) | `0644` |
| is_secret | BOOLEAN DEFAULT false | |
| content_inline | TEXT NULL | 小文件内联 |
| content_storage_key | VARCHAR(512) NULL | 大文件对象存储 |
| content_hash | VARCHAR(64) | |
| size_bytes | INT | |
| encoding | VARCHAR(16) DEFAULT 'utf-8' | |
| created_at | TIMESTAMPTZ | |

### 8.3 `vo_config_sets`（配置集，可关联多个分组共享）

> 配置集是一组可复用的配置（文件 + 命令参数 + env），独立于分组存在，可关联到多个分组；关联的分组共享该配置集内容。典型用途：多个分组共用同一份日志配置、同一份数据库连接配置、同一份证书。配置集自身也版本化，可回退。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| name | VARCHAR(64) | 配置集名（空间内唯一） |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| scope | VARCHAR(16) | `workspace` / `application` |
| application_id | BIGINT FK vo_applications NULL | scope=application 时归属应用 |
| current_version_id | BIGINT FK vo_config_set_versions NULL | 当前生效版本 |
| merge_strategy | VARCHAR(16) DEFAULT 'overlay' | 与分组自身配置合并策略：`overlay`（配置集为底座，分组配置覆盖）/ `prepend` / `append` |
| labels | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, name) WHERE deleted = false | | |

### 8.4 `vo_config_set_versions`（配置集版本）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| config_set_id | BIGINT FK vo_config_sets | |
| version | INT | 配置集内版本号 |
| version_label | VARCHAR(64) NULL | |
| change_summary | VARCHAR(255) | |
| change_type | VARCHAR(16) | `file` / `command` / `env` / `mixed` |
| snapshot | JSONB | 完整配置快照（结构同 configs.snapshot） |
| snapshot_hash | VARCHAR(64) | SHA-256 |
| is_current | BOOLEAN DEFAULT false | |
| is_draft | BOOLEAN DEFAULT true | |
| applied_at | TIMESTAMPTZ NULL | 被分组采用时间 |
| parent_version_id | BIGINT FK vo_config_set_versions NULL | 基于哪版修改 |
| + 通用审计字段 | | |
| UNIQUE(config_set_id, version) WHERE deleted = false | | |

### 8.5 `config_set_files`（配置集文件明细，便于单独 diff）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| config_set_version_id | BIGINT FK vo_config_set_versions | |
| path | VARCHAR(512) | |
| file_mode | VARCHAR(8) | |
| is_secret | BOOLEAN DEFAULT false | |
| content_inline | TEXT NULL | |
| content_storage_key | VARCHAR(512) NULL | |
| content_hash | VARCHAR(64) | |
| size_bytes | INT | |
| encoding | VARCHAR(16) DEFAULT 'utf-8' | |
| created_at | TIMESTAMPTZ | |

### 8.6 `vo_config_set_bindings`（配置集与分组的关联）

> 一个分组可关联多个配置集（按优先级合并），一个配置集可被多个分组关联。分组实际生效配置 = 分组自身 configs.snapshot ⊕（按 merge_strategy 合并的各配置集版本）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| config_set_id | BIGINT FK vo_config_sets | |
| group_id | BIGINT FK vo_groups | |
| config_set_version_id | BIGINT FK vo_config_set_versions NULL | 锁定版本（空=跟随 current） |
| pinned | BOOLEAN DEFAULT false | 是否锁定版本不随配置集升级而变 |
| priority | INT DEFAULT 0 | 多配置集合并优先级（数字大优先） |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |
| UNIQUE(config_set_id, group_id) WHERE deleted = false | | |

> **生效配置合并**：发布时平台计算分组最终配置 = 按 priority 依次以 merge_strategy 合并各配置集版本，再叠加分组自身 `configs.snapshot`（自身优先级最高）。合并结果作为发布的目标配置，记录到 release，可审计可回滚。

### 8.7 `vo_releases`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| release_number | INT | 分组内自增发布号 |
| group_id | BIGINT FK vo_groups | |
| type | VARCHAR(16) | `rolling` / `canary` / `config_only` / `rollback` |
| target_image_id | BIGINT FK vo_images NULL | config_only 可为空 |
| target_config_id | BIGINT FK vo_configs NULL | |
| canary_replicas | INT | |
| canary_deployment_name | VARCHAR(128) | |
| previous_image_id | BIGINT FK vo_images NULL | |
| previous_config_id | BIGINT FK vo_configs NULL | |
| k8s_revision | BIGINT | |
| status | VARCHAR(16) | `draft` / `pending_approval` / `approved` / `pending` / `running` / `paused` / `succeeded` / `failed` / `rolled_back` / `canceled` |
| progress_percent | INT DEFAULT 0 | |
| ready_replicas | INT DEFAULT 0 | |
| total_replicas | INT DEFAULT 0 | |
| change_summary | VARCHAR(255) | |
| message | TEXT | |
| approval_instance_id | BIGINT FK vo_approval_instances NULL | |
| started_by | BIGINT FK vo_users | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| duration_ms | BIGINT | |
| is_rollback | BOOLEAN DEFAULT false | |
| rollback_from_release_id | BIGINT FK vo_releases NULL | |
| metadata | JSONB | |
| + 通用审计字段 | | |

### 8.8 `vo_release_events`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| release_id | BIGINT FK vo_releases | |
| seq | INT | |
| type | VARCHAR(32) | `submitted` / `approved` / `rejected` / `patched` / `progress` / `pod_ready` / `pod_failed` / `paused` / `resumed` / `timeout` / `promoted` / `aborted` |
| level | VARCHAR(16) | `info` / `warn` / `error` |
| message | TEXT | |
| detail | JSONB | Pod 名、错误码等 |
| occurred_at | TIMESTAMPTZ | |

### 8.9 `vo_release_presets`（发布预设，复用常用发布参数）

> 把常用的发布参数（灰度副本数/比例、滚动策略、超时、是否审批、通知开关等）存为预设，发布时一键套用。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` |
| scope_id | BIGINT NULL | |
| name | VARCHAR(128) | 如「标准灰度 10%」 |
| release_type | VARCHAR(16) | `rolling` / `canary` / `config_only` |
| params | JSONB | `{canaryReplicas, canaryMaxPercent, maxSurge, maxUnavailable, timeoutSeconds, requireApproval, notifyChannels, autoPromoteAfterSec}` |
| description | VARCHAR(255) | |
| + 通用审计字段 | | |

### 8.10 `vo_release_windows`（发布窗口，可选）

> 限制生产发布只能在指定时间窗口内进行（如工作日 10:00-18:00），窗口外发布需特批。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `workspace` / `application` |
| scope_id | BIGINT NULL | |
| environment | VARCHAR(16) | 限制的环境，如 `prod` |
| schedule_cron | VARCHAR(64) | 允许窗口（如 `0 10 * * 1-5` 工作日 10 点起） |
| duration_minutes | INT | 窗口持续分钟 |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

---

## 9. 中间件

> 中间件（MySQL/Redis/Kafka/ES/RabbitMQ/MongoDB/PostgreSQL/MinIO 等）作为有状态工作负载独立管理，挂在空间下，与应用平级。复用权限、审批、通知、审计、回收站、动态流。详见 [构建与发布 §中间件部署](build-release.md#中间件部署) 与 [Kubernetes 集成 §中间件资源映射](kubernetes.md#中间件资源映射)。

### 9.1 `vo_middleware_catalog`（中间件类型目录，平台预置 + 扩展）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| code | VARCHAR(64) UNIQUE | 如 `mysql` / `redis` / `kafka` |
| name | VARCHAR(128) | 展示名 |
| category | VARCHAR(32) | `database` / `cache` / `queue` / `search` / `storage` |
| install_method | VARCHAR(16) | `helm` / `operator` / `manifest` |
| chart_repository | VARCHAR(255) | Helm 仓库地址 |
| chart_name | VARCHAR(128) | 如 `bitnami/mysql` |
| default_version | VARCHAR(64) | 默认 Chart 版本 |
| operator_info | JSONB | Operator 安装信息（name/channel/namespace） |
| supported_versions | JSONB | 支持的版本列表与说明 |
| schema_config | JSONB | 参数 schema（用于动态表单渲染，JSON Schema 格式） |
| port_info | JSONB | 默认端口与服务暴露说明 |
| icon | VARCHAR(64) | |
| description | TEXT | |
| is_system | BOOLEAN DEFAULT false | 系统内置不可删 |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

> `schema_config` 示例（MySQL）：
> ```json
> {
>   "auth.rootPassword": {"type":"string","secret":true,"label":"root 密码"},
>   "architecture": {"type":"string","enum":["standalone","replication"],"default":"standalone"},
>   "primary.persistence.size": {"type":"string","default":"8Gi","label":"存储大小"},
>   "primary.resources.requests.cpu": {"type":"string","default":"500m"}
> }
> ```

### 9.2 `vo_middleware_instances`（中间件实例，空间下创建）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| catalog_id | BIGINT FK vo_middleware_catalog | 类型 |
| name | VARCHAR(64) | 实例名（空间内唯一） |
| display_name | VARCHAR(128) | |
| description | TEXT | |
| environment | VARCHAR(16) | `dev` / `test` / `staging` / `prod` |
| cluster_id | BIGINT FK vo_clusters | 目标集群 |
| namespace | VARCHAR(128) | 目标 Namespace |
| release_name | VARCHAR(128) | Helm Release 名 / Operator 实例名 |
| version | VARCHAR(64) | 部署的 Chart/Operator 版本 |
| replicas | INT | 副本数（如 MySQL 主从、Kafka broker） |
| architecture | VARCHAR(32) | `standalone` / `replication` / `cluster` |
| status | VARCHAR(16) | `pending` / `installing` / `running` / `updating` / `failed` / `stopped` / `deleting` |
| helm_release_secret | VARCHAR(255) | Helm release 存储的 secret 名 |
| access_info | JSONB | 访问信息（Service 名、端口、连接串模板） |
| current_params_id | BIGINT FK vo_middleware_params NULL | 当前生效参数版本 |
| current_release_id | BIGINT FK vo_middleware_releases NULL | 最近一次成功变更 |
| storage_class | VARCHAR(128) | 使用的 StorageClass |
| persistence_size | VARCHAR(16) | 如 `50Gi` |
| backup_enabled | BOOLEAN DEFAULT false | 是否启用备份 |
| release_requires_approval | BOOLEAN DEFAULT false | 变更需审批 |
| owner_id | BIGINT FK vo_users | 实例 Owner |
| labels | JSONB DEFAULT '{}' | |
| metadata | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, name) WHERE deleted = false | | |

### 9.3 `vo_middleware_params`（参数配置，版本化）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| instance_id | BIGINT FK vo_middleware_instances | |
| version | INT | 实例内版本号 |
| version_label | VARCHAR(64) | 可选标签 |
| change_summary | VARCHAR(255) | |
| change_type | VARCHAR(16) | `params` / `version` / `scaling` / `mixed` |
| values | JSONB | Helm values / Operator spec 完整快照 |
| values_hash | VARCHAR(64) | SHA-256，用于 diff |
| is_current | BOOLEAN DEFAULT false | |
| is_draft | BOOLEAN DEFAULT true | |
| applied_at | TIMESTAMPTZ | |
| applied_by | BIGINT FK vo_users | |
| applied_release_id | BIGINT FK vo_middleware_releases NULL | |
| parent_params_id | BIGINT FK vo_middleware_params NULL | 基于哪版修改 |
| + 通用审计字段 | | |
| UNIQUE(instance_id, version) WHERE deleted = false | | |

> `values` 为完整 Helm values 或 Operator CR spec；Secret 类参数（密码、Token）加密存储。

### 9.4 `vo_middleware_releases`（中间件变更记录：安装/升级/扩缩容/回滚）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| release_number | INT | 实例内自增号 |
| instance_id | BIGINT FK vo_middleware_instances | |
| type | VARCHAR(16) | `install` / `upgrade` / `scale` / `rollback` / `config_only` / `uninstall` |
| target_version | VARCHAR(64) NULL | 升级到的 Chart 版本 |
| target_params_id | BIGINT FK vo_middleware_params NULL | 目标参数版本 |
| target_replicas | INT NULL | 扩缩容目标副本 |
| previous_version | VARCHAR(64) | |
| previous_params_id | BIGINT FK vo_middleware_params NULL | |
| previous_replicas | INT | |
| helm_revision | INT | Helm release revision |
| status | VARCHAR(16) | `draft` / `pending_approval` / `approved` / `running` / `succeeded` / `failed` / `rolled_back` / `canceled` |
| progress_percent | INT DEFAULT 0 | |
| change_summary | VARCHAR(255) | |
| message | TEXT | |
| approval_instance_id | BIGINT FK vo_approval_instances NULL | |
| started_by | BIGINT FK vo_users | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| duration_ms | BIGINT | |
| is_rollback | BOOLEAN DEFAULT false | |
| rollback_from_release_id | BIGINT FK vo_middleware_releases NULL | |
| metadata | JSONB | |
| + 通用审计字段 | | |

### 9.5 `vo_middleware_backups`（备份记录）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| instance_id | BIGINT FK vo_middleware_instances | |
| type | VARCHAR(16) | `full` / `incremental` / `snapshot` |
| method | VARCHAR(16) | `velero` / `pg_dump` / `mysqldump` / `xtrabackup` / `rdb` / `snapshot` |
| trigger | VARCHAR(16) | `manual` / `schedule` |
| storage_location | VARCHAR(512) | 备份存储位置（S3 key 等） |
| size_bytes | BIGINT | |
| status | VARCHAR(16) | `pending` / `running` / `succeeded` / `failed` |
| retention_until | TIMESTAMPTZ | 保留到期 |
| restore_info | JSONB | 恢复用元数据 |
| started_by | BIGINT FK vo_users NULL | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| message | TEXT | |
| + 通用审计字段 | | |

### 9.6 `middleware_backup_policies`（备份策略）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| instance_id | BIGINT FK vo_middleware_instances UNIQUE | |
| enabled | BOOLEAN DEFAULT true | |
| schedule_cron | VARCHAR(64) | 如 `0 2 * * *` |
| type | VARCHAR(16) | `full` / `incremental` / `snapshot` |
| retention_count | INT DEFAULT 7 | 保留份数 |
| + 通用审计字段 | | |

### 9.7 `vo_middleware_connections`（应用与中间件的连接关系，便于拓扑与影响面分析）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| application_id | BIGINT FK vo_applications NULL | |
| group_id | BIGINT FK vo_groups NULL | 具体到分组 |
| instance_id | BIGINT FK vo_middleware_instances | |
| credential_id | BIGINT FK vo_credentials NULL | 连接凭证 |
| alias | VARCHAR(64) | 如 `order-db` |
| created_at | TIMESTAMPTZ | |
| UNIQUE(group_id, instance_id) | | |

---

## 10. 大模型服务

> 大模型（LLM/多模态）部署与中间件/应用不同：模型权重巨大、推理框架专一、显存调度苛刻、需 Token 计量与配额。平台把「模型仓库 → 模型版本 → 推理服务 → 适配器 → Token 计量」作为独立域，挂在空间下，与中间件平级。详见 [大模型部署](model-serving.md) 与 [K8s 集成 §大模型推理工作负载](kubernetes.md#大模型推理工作负载)。

### 10.1 `vo_model_registries`（模型仓库，平台级）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| name | VARCHAR(64) UNIQUE | 如 `huggingface` / `内部模型库` |
| type | VARCHAR(16) | `huggingface` / `oss` / `s3` / `nexus` / `custom` |
| endpoint | VARCHAR(255) | |
| credential_id | BIGINT FK vo_credentials NULL | |
| is_default | BOOLEAN DEFAULT false | |
| status | VARCHAR(16) | `active` / `disabled` |
| + 通用审计字段 | | |

### 10.2 `vo_models`（模型，空间级）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| registry_id | BIGINT FK vo_model_registries NULL | 来源仓库（外部模型） |
| name | VARCHAR(128) | 如 `qwen2-72b` |
| display_name | VARCHAR(128) | |
| modality | VARCHAR(16) | `text` / `multimodal` / `embedding` / `image` / `audio` |
| framework | VARCHAR(16) | `vllm` / `tgi` / `triton` / `sglang` / `ollama` / `custom` |
| description | TEXT | |
| owner_id | BIGINT FK vo_users | |
| labels | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, name) WHERE deleted = false | | |

### 10.3 `vo_model_versions`（模型版本/权重，版本化）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| model_id | BIGINT FK vo_models | |
| version | VARCHAR(64) | 如 `1.0` / `instruct-v2` |
| version_number | INT | 模型内自增号 |
| source_ref | VARCHAR(512) | 仓库引用（HF repo@rev / S3 key） |
| weight_storage_key | VARCHAR(512) NULL | 平台缓存到对象存储的 key |
| weight_size_bytes | BIGINT | 权重大小 |
| quantization | VARCHAR(32) NULL | `none` / `int8` / `int4` / `awq` / `gptq` / `fp8` |
| precision | VARCHAR(16) | `fp16` / `bf16` / `fp32` |
| context_length | INT NULL | 上下文长度 |
| params_billion | NUMERIC(8,2) NULL | 参数量（B） |
| download_status | VARCHAR(16) | `pending` / `downloading` / `ready` / `failed` |
| checksum | VARCHAR(128) | 权重校验和 |
| base_model_version_id | BIGINT FK vo_model_versions NULL | 基座模型（适配器场景） |
| + 通用审计字段 | | |
| UNIQUE(model_id, version) WHERE deleted = false | | |

### 10.4 `vo_model_adapters`（LoRA/QLoRA 适配器，可挂载到基座模型）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| model_id | BIGINT FK vo_models | 所属模型 |
| base_model_version_id | BIGINT FK vo_model_versions | 基座权重 |
| name | VARCHAR(128) | 适配器名 |
| adapter_storage_key | VARCHAR(512) | 适配器权重 key |
| rank | INT NULL | LoRA rank |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

### 10.5 `vo_inference_services`（推理服务，空间级，对外提供推理 API）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| name | VARCHAR(64) | 服务名（空间内唯一） |
| display_name | VARCHAR(128) | |
| model_id | BIGINT FK vo_models | |
| current_model_version_id | BIGINT FK vo_model_versions NULL | 当前权重版本 |
| framework | VARCHAR(16) | `vllm` / `tgi` / `triton` / `sglang` / `custom` |
| cluster_id | BIGINT FK vo_clusters | |
| namespace | VARCHAR(128) | |
| replicas | INT | 副本数 |
| gpu_per_replica | INT | 单副本 GPU 卡数（多卡张量并行） |
| gpu_type | VARCHAR(64) | |
| tensor_parallel_size | INT | 张量并行度 |
| pipeline_parallel_size | INT DEFAULT 1 | 流水线并行度 |
| max_model_len | INT NULL | |
| extra_args | JSONB | 框架参数（如 vLLM `--gpu-memory-utilization`） |
| autoscaling_enabled | BOOLEAN DEFAULT false | 按 QPS/显存自动伸缩 |
| autoscaling_metrics | JSONB NULL | `[{type:"qps",target:50},{type:"gpu_memory",target:85}]` |
| autoscaling_min | INT NULL | |
| autoscaling_max | INT NULL | |
| status | VARCHAR(16) | `pending` / `loading` / `running` / `updating` / `failed` / `stopped` |
| endpoint_url | VARCHAR(255) NULL | 推理访问地址（ClusterIP/Ingress） |
| api_key_required | BOOLEAN DEFAULT true | 是否需 API Key 鉴权 |
| current_release_id | BIGINT FK vo_inference_releases NULL | |
| release_requires_approval | BOOLEAN DEFAULT false | 模型切换/扩缩容需审批 |
| owner_id | BIGINT FK vo_users | |
| labels | JSONB DEFAULT '{}' | |
| + 通用审计字段 | | |
| UNIQUE(workspace_id, name) WHERE deleted = false | | |

### 10.6 `vo_inference_releases`（推理服务变更：部署/切模型/扩缩容/回滚）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| release_number | INT | 服务内自增 |
| service_id | BIGINT FK vo_inference_services | |
| type | VARCHAR(16) | `deploy` / `switch_model` / `scale` / `config` / `rollback` / `stop` |
| target_model_version_id | BIGINT FK vo_model_versions NULL | 切模型目标 |
| target_replicas | INT NULL | |
| target_extra_args | JSONB NULL | |
| previous_model_version_id | BIGINT FK vo_model_versions NULL | |
| previous_replicas | INT | |
| strategy | VARCHAR(16) | `rolling` / `blue_green`（蓝绿切模型）/ `canary` |
| status | VARCHAR(16) | `draft` / `pending_approval` / `approved` / `running` / `succeeded` / `failed` / `rolled_back` |
| progress_percent | INT DEFAULT 0 | |
| approval_instance_id | BIGINT FK vo_approval_instances NULL | |
| started_by | BIGINT FK vo_users | |
| started_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ NULL | |
| message | TEXT | |
| + 通用审计字段 | | |

### 10.7 `vo_inference_api_keys`（推理服务 API Key）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| service_id | BIGINT FK vo_inference_services | |
| name | VARCHAR(128) | |
| key_hash | VARCHAR(255) | SHA-256 |
| key_prefix | VARCHAR(16) | 展示前缀 `sk-xxx...` |
| rate_limit_per_min | INT NULL | 每分钟请求上限 |
| token_quota_per_day | BIGINT NULL | 每日 Token 配额 |
| expires_at | TIMESTAMPTZ NULL | |
| last_used_at | TIMESTAMPTZ NULL | |
| status | VARCHAR(16) | `active` / `revoked` |
| + 通用审计字段 | | |

### 10.8 `vo_inference_usage`（Token 用量计量，分区表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL | |
| uuid | UUID | |
| service_id | BIGINT FK vo_inference_services | |
| workspace_id | BIGINT | 冗余分片 |
| api_key_id | BIGINT FK vo_inference_api_keys NULL | |
| caller | VARCHAR(128) | 调用方（用户/应用） |
| prompt_tokens | INT | |
| completion_tokens | INT | |
| total_tokens | INT | |
| model_version_id | BIGINT FK vo_model_versions | |
| latency_ms | INT | |
| status_code | INT | |
| request_id | VARCHAR(64) | |
| created_at | TIMESTAMPTZ | |

### 10.9 `vo_inference_routes`（多模型路由/负载，可选高级）

> 把多个推理服务组成路由组，按权重/规则分发（A/B、按 prompt 路由、故障转移）。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | |
| name | VARCHAR(64) | 路由组名 |
| strategy | VARCHAR(16) | `weighted` / `header` / `failover` |
| targets | JSONB | `[{serviceUuid, weight:80},{serviceUuid, weight:20}]` |
| rules | JSONB NULL | header/prompt 路由规则 |
| endpoint_url | VARCHAR(255) | 路由组统一入口 |
| + 通用审计字段 | | |

---

## 11. 审批、通知、协作

### 11.1 `vo_approval_flows`（审批流定义）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `platform` / `workspace` / `application` / `group` |
| scope_id | BIGINT | |
| name | VARCHAR(128) | |
| trigger_action | VARCHAR(64) | `release.rolling` / `release.canary` / `config.apply` |
| environment_filter | VARCHAR(16)[] | 如 `{prod}` 仅生产需审批 |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

### 9.2 `approval_flow_steps`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| flow_id | BIGINT FK vo_approval_flows | |
| step_order | INT | |
| name | VARCHAR(128) | 如「应用负责人审批」 |
| approver_type | VARCHAR(16) | `role` / `user` / `group_owner` |
| approver_role_id | BIGINT FK vo_roles NULL | |
| approver_user_id | BIGINT FK vo_users NULL | |
| require_all | BOOLEAN DEFAULT false | 会签/或签 |
| timeout_hours | INT DEFAULT 24 | |
| + 通用审计字段 | | |

### 9.3 `vo_approval_instances`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| flow_id | BIGINT FK vo_approval_flows | |
| resource_type | VARCHAR(32) | `release` / `config` |
| resource_id | BIGINT | |
| current_step | INT | |
| status | VARCHAR(16) | `pending` / `approved` / `rejected` / `expired` / `canceled` |
| submitted_by | BIGINT FK vo_users | |
| submitted_at | TIMESTAMPTZ | |
| finished_at | TIMESTAMPTZ | |
| + 通用审计字段 | | |

### 9.4 `approval_records`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| instance_id | BIGINT FK vo_approval_instances | |
| step_order | INT | |
| approver_id | BIGINT FK vo_users | |
| action | VARCHAR(16) | `approve` / `reject` / `delegate` |
| comment | TEXT | |
| acted_at | TIMESTAMPTZ | |

### 9.5 `vo_notifications`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| user_id | BIGINT FK vo_users | 接收人 |
| category | VARCHAR(32) | `build` / `release` / `approval` / `system` / `mention` |
| title | VARCHAR(255) | |
| content | TEXT | |
| link_type | VARCHAR(32) | `build` / `release` / `approval` |
| link_id | VARCHAR(64) | 资源 uuid |
| level | VARCHAR(16) | `info` / `success` / `warning` / `error` |
| is_read | BOOLEAN DEFAULT false | |
| read_at | TIMESTAMPTZ | |
| created_at | TIMESTAMPTZ | |

### 9.6 `vo_notification_deliveries`（多渠道投递记录）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| notification_id | BIGINT FK vo_notifications | |
| channel | VARCHAR(16) | `in_app` / `email` / `webhook` / `dingtalk` / `feishu` |
| status | VARCHAR(16) | `pending` / `sent` / `failed` |
| sent_at | TIMESTAMPTZ | |
| error_message | TEXT | |

### 9.7 `vo_webhooks`（出站 Webhook）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| scope | VARCHAR(16) | `workspace` / `application` |
| scope_id | BIGINT | |
| name | VARCHAR(128) | |
| url | VARCHAR(512) | |
| secret_hash | VARCHAR(255) | 签名密钥 |
| events | VARCHAR(64)[] | `build.success` / `release.succeeded` 等 |
| enabled | BOOLEAN DEFAULT true | |
| last_triggered_at | TIMESTAMPTZ | |
| + 通用审计字段 | | |

### 9.8 `user_favorites`（收藏）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| user_id | BIGINT FK vo_users | |
| resource_type | VARCHAR(32) | `workspace` / `application` / `group` |
| resource_id | BIGINT | |
| sort_order | INT DEFAULT 0 | |
| created_at | TIMESTAMPTZ | |
| UNIQUE(user_id, resource_type, resource_id) | | |

### 9.9 `vo_activity_feeds`（动态流，提升协作感知）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| workspace_id | BIGINT FK vo_workspaces | |
| application_id | BIGINT FK vo_applications NULL | |
| actor_id | BIGINT FK vo_users | |
| verb | VARCHAR(32) | `created` / `built` / `released` / `commented` |
| object_type | VARCHAR(32) | |
| object_id | BIGINT | |
| object_uuid | UUID | |
| summary | VARCHAR(512) | 如「Alice 发布了 payment-prod v1.4.0」 |
| detail | JSONB | |
| created_at | TIMESTAMPTZ | |

**索引**：`(workspace_id, created_at DESC)`、`(application_id, created_at DESC)`

### 9.10 `comments`（资源评论，发布/构建可讨论）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| resource_type | VARCHAR(32) | `release` / `build` / `config` |
| resource_id | BIGINT | |
| parent_id | BIGINT FK comments NULL | 回复 |
| author_id | BIGINT FK vo_users | |
| content | TEXT | 支持 @mention |
| mentions | BIGINT[] | 被 @ 的用户 id |
| + 通用审计字段 | | |

### 9.11 `announcements`（系统公告）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| title | VARCHAR(255) | |
| content | TEXT | Markdown |
| level | VARCHAR(16) | `info` / `warning` / `critical` |
| target_scope | VARCHAR(16) | `all` / `platform` / `workspace` |
| target_scope_id | BIGINT NULL | |
| publish_at | TIMESTAMPTZ | |
| expire_at | TIMESTAMPTZ | |
| pinned | BOOLEAN DEFAULT false | |
| + 通用审计字段 | | |

---

## 12. 告警与运维观测

> 通知是被动的（事件发生后推送），告警是主动的（基于规则持续评估，满足条件触发）。告警规则由平台评估或对接 Prometheus Alertmanager。

### 12.1 `vo_alert_rules`（告警规则）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | 分片键 |
| scope | VARCHAR(16) | `workspace` / `application` / `group` / `middleware_instance` |
| scope_id | BIGINT NULL | |
| name | VARCHAR(128) | |
| metric | VARCHAR(64) | `pod_restart_high` / `release_failed` / `build_failed_rate` / `cpu_high` / `memory_high` / `pod_pending` / `middleware_down` / `backup_failed` / `hpa_maxed` / `custom` |
| condition | JSONB | `{op:">",threshold:5,for:"5m"}` |
| source | VARCHAR(16) | `builtin`（平台自评估，如发布失败/重启）/ `prometheus`（PromQL）/ `k8s_event` |
| promql | TEXT NULL | source=prometheus 时的查询表达式 |
| severity | VARCHAR(16) | `info` / `warn` / `critical` |
| notify_channels | JSONB | `[{"type":"in_app"},{"type":"email","target":"oncall@x.com"},{"type":"dingtalk","target":"webhook-uuid"}]` |
| cooldown_minutes | INT DEFAULT 30 | 同规则重复告警冷却 |
| enabled | BOOLEAN DEFAULT true | |
| + 通用审计字段 | | |

### 12.2 `vo_alert_events`（告警事件，分区表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL | |
| uuid | UUID | |
| rule_id | BIGINT FK vo_alert_rules | |
| workspace_id | BIGINT | 冗余分片 |
| scope | VARCHAR(16) | |
| scope_id | BIGINT | |
| scope_name | VARCHAR(128) | 冗余展示名 |
| severity | VARCHAR(16) | |
| status | VARCHAR(16) | `firing` / `resolved` |
| message | TEXT | |
| detail | JSONB | 指标值、Pod 名等 |
| fired_at | TIMESTAMPTZ | |
| resolved_at | TIMESTAMPTZ NULL | |
| notification_sent | BOOLEAN DEFAULT false | |
| + 通用审计字段 | | |

### 12.3 `silenced_alerts`（告警静默）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| uuid | UUID UNIQUE | |
| workspace_id | BIGINT FK vo_workspaces | |
| rule_id | BIGINT FK vo_alert_rules NULL | 静默某规则（空=按 match 静默） |
| match | JSONB | 静默匹配条件 `{scope,scopeId,label}` |
| reason | VARCHAR(255) | |
| expires_at | TIMESTAMPTZ | 静默到期 |
| created_by | BIGINT FK vo_users | |
| created_at | TIMESTAMPTZ | |

---

## 13. 审计与回收站

### 13.1 `vo_audit_logs`（分区表）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL | |
| uuid | UUID | |
| actor_id | BIGINT FK vo_users NULL | |
| actor_name | VARCHAR(128) | 冗余，用户删除后仍可读 |
| actor_type | VARCHAR(16) | `user` / `system` / `api_token` |
| action | VARCHAR(64) | `build.create` / `release.rolling` |
| resource_type | VARCHAR(32) | |
| resource_id | VARCHAR(64) | uuid |
| resource_name | VARCHAR(128) | 冗余 |
| workspace_id | BIGINT NULL | |
| application_id | BIGINT NULL | |
| request_id | VARCHAR(64) | TraceId |
| ip | VARCHAR(64) | |
| user_agent | VARCHAR(512) | |
| before | JSONB | |
| after | JSONB | |
| diff | JSONB | 预计算 diff |
| status | VARCHAR(16) | `success` / `failed` |
| message | TEXT | |
| duration_ms | INT | |
| created_at | TIMESTAMPTZ | |

**分区**：`PARTITION BY RANGE (created_at)` 按月；保留 2 年。

### 13.2 `recycle_bin`（回收站，软删除资源统一视图）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | BIGSERIAL PK | |
| resource_type | VARCHAR(32) | `application` / `group` / `image` / `git_source` |
| resource_id | BIGINT | |
| resource_uuid | UUID | |
| resource_name | VARCHAR(128) | |
| workspace_id | BIGINT FK vo_workspaces | |
| application_id | BIGINT NULL | |
| deleted_by | BIGINT FK vo_users | |
| deleted_at | TIMESTAMPTZ | |
| expire_at | TIMESTAMPTZ | 默认 deleted_at + 30 天自动清理 |
| restored_at | TIMESTAMPTZ NULL | |
| restored_by | BIGINT FK vo_users NULL | |
| status | VARCHAR(16) | `deleted` / `restored` / `purged` |

> 软删除时同步写入回收站；恢复时清除原表 `deleted` 标记。

---

## 14. 索引与约束策略

### 14.1 通用

- 所有 FK 建索引。
- 业务唯一约束加 `WHERE deleted = false` 部分唯一索引。
- 列表查询主索引：`(scope_id, created_at DESC) WHERE deleted = false`。
- 乐观锁更新：`UPDATE ... SET version = version + 1, updated_at = now(), updated_by = $1 WHERE id = $2 AND version = $3`。

### 14.2 热点表

| 表 | 索引 |
| --- | --- |
| vo_builds | `(application_id, status)`, `(triggered_by, created_at DESC)` |
| vo_releases | `(group_id, release_number DESC)`, `(status) WHERE status IN ('running','pending_approval')` |
| vo_middleware_releases | `(instance_id, release_number DESC)`, `(status) WHERE status IN ('running','pending_approval')` |
| vo_middleware_instances | `(workspace_id, status)`, `(cluster_id, namespace)` |
| vo_middleware_backups | `(instance_id, created_at DESC)`, `(status) WHERE status IN ('pending','running')` |
| vo_pipeline_runs | `(pipeline_id, run_number DESC)`, `(status) WHERE status IN ('running','paused')` |
| vo_pipeline_stage_runs | `(pipeline_run_id, seq)` |
| vo_promotions | `(application_id, status)`, `(status) WHERE status IN ('deploying','verifying')` |
| vo_inference_services | `(workspace_id, status)`, `(cluster_id, namespace)` |
| vo_inference_releases | `(service_id, release_number DESC)`, `(status) WHERE status IN ('running','pending_approval')` |
| vo_inference_usage | `(service_id, created_at DESC)`, `(api_key_id, created_at DESC)` |
| vo_external_api_call_logs | `(token_id, created_at DESC)`, `(workspace_id, created_at DESC)`, `(operation, status_code)` |
| vo_alert_events | `(workspace_id, fired_at DESC)`, `(status) WHERE status='firing'` |
| vo_alert_rules | `(workspace_id, enabled)`, `(scope, scope_id)` |
| vo_notifications | `(user_id, is_read, created_at DESC)` |
| vo_activity_feeds | `(workspace_id, created_at DESC)` |
| vo_audit_logs | `(workspace_id, created_at DESC)`, `(resource_type, resource_id)` |

### 14.3 数据迁移

- 工具：Atlas 或 golang-migrate。
- 每次 schema 变更附带 seed：`vo_permissions`、`vo_menus`、内置 `vo_roles`、`vo_role_permissions`、`vo_sys_dictionaries`、`vo_base_images`、`vo_middleware_catalog`、内置 `vo_alert_rules`（如发布失败、重启过多、推理服务异常）。

---

## 15. 配置快照结构（configs.snapshot）

```json
{
  "command": ["java", "-jar", "/app/app.jar"],
  "args": ["--spring.profiles.active=prod"],
  "env": [
    {"name": "LOG_LEVEL", "value": "INFO", "secret": false},
    {"name": "DB_PASSWORD", "valueRef": "secret/db-password", "secret": true}
  ],
  "envFrom": [
    {"configMapRef": "app-cm"},
    {"secretRef": "app-secret"}
  ],
  "files": [
    {
      "path": "/etc/app/application.yml",
      "mode": "0644",
      "secret": false,
      "contentHash": "sha256:abc...",
      "fileId": 1001
    }
  ],
  "healthCheck": {
    "liveness": {"httpGet": {"path": "/health", "port": 8080}, "initialDelaySeconds": 30},
    "readiness": {"httpGet": {"path": "/ready", "port": 8080}}
  }
}
```

大文件内容存 `config_files` + 对象存储；`snapshot` 只存引用与 hash。

---

## 16. 大规模适配（10 万+应用、单分组 1 万+副本）

> 详细扩展性设计见 [扩展性设计](scalability.md)。本节列数据模型层面的关键决策。

### 16.1 分片键：`workspace_id`

- 主分片键为 `workspace_id`，绝大多数查询带空间上下文，使空间内事务/join 落单分片。
- 实现采用 **Citus**（`workspace_id` 作分布列）或应用层路由。
- 全局表（不分片）：`vo_users`、`vo_platform_roles`、`vo_permissions`、`vo_menus`、`vo_clusters`、`vo_registries`、`vo_jenkins_instances`、`vo_credentials`、`vo_system_settings`、`vo_sys_dictionaries`。

### 16.2 冗余分片字段

以下表为便于按 `workspace_id` 分片，冗余该字段（虽可经 join 推导，但分片场景必须冗余）：

| 表 | 冗余字段 | 来源 |
| --- | --- | --- |
| vo_applications | workspace_id | 自身 |
| vo_groups | workspace_id | application.workspace_id |
| vo_git_sources | workspace_id | application.workspace_id |
| vo_images | workspace_id | application.workspace_id |
| vo_image_version_tags | workspace_id | application.workspace_id |
| vo_builds | workspace_id | application.workspace_id |
| vo_build_templates | workspace_id | 按 scope 填充 |
| vo_configs | workspace_id | group → application |
| vo_config_sets | workspace_id | 自身 |
| vo_config_set_versions | workspace_id | config_set.workspace_id |
| vo_config_set_bindings | workspace_id | group → application |
| vo_releases | workspace_id | group → application |
| vo_release_events | workspace_id | release → group |
| vo_middleware_instances | workspace_id | 自身 |
| vo_middleware_params | workspace_id | instance.workspace_id |
| vo_middleware_releases | workspace_id | instance.workspace_id |
| vo_middleware_backups | workspace_id | instance.workspace_id |
| vo_middleware_connections | workspace_id | 自身 |
| vo_pipelines | workspace_id | 自身 |
| vo_pipeline_runs | workspace_id | pipeline.workspace_id |
| vo_pipeline_stage_runs | workspace_id | pipeline_run.workspace_id |
| vo_promotions | workspace_id | application.workspace_id |
| vo_models | workspace_id | 自身 |
| vo_model_versions | workspace_id | model.workspace_id |
| vo_model_adapters | workspace_id | model.workspace_id |
| vo_inference_services | workspace_id | 自身 |
| vo_inference_releases | workspace_id | service.workspace_id |
| vo_inference_usage | workspace_id | service.workspace_id |
| vo_inference_routes | workspace_id | 自身 |
| vo_external_api_call_logs | workspace_id | 调用资源所属空间（无则空） |
| vo_alert_rules | workspace_id | 自身 |
| vo_alert_events | workspace_id | 自身（冗余） |
| vo_release_presets | workspace_id | 按 scope 填充 |
| vo_release_windows | workspace_id | 按 scope 填充 |
| vo_audit_logs | workspace_id | 已有 |
| vo_activity_feeds | workspace_id | 已有 |

### 16.3 Pod 不落库

- Pod 数量可达千万级且变更频繁，**不在平台 DB 持久化**。
- Pod 实时态来自 `client-go` Informer，缓存于按集群分片的 Redis（`cluster:{id}:pods:{namespace}`）。
- 平台仅记录发布时刻相关的 Pod 快照到 `release_events.detail`（非全量 Pod 表）。

### 16.4 时间二级分区

高写入表在分片基础上按 `created_at` 月度分区：

| 表 | 分区 | 保留 |
| --- | --- | --- |
| vo_builds | 月 | 1 年热 / 2 年归档 |
| vo_releases | 月 | 1 年热 / 2 年归档 |
| vo_release_events | 月 | 1 年热 / 归档 |
| vo_pipeline_runs | 月 | 1 年热 / 归档 |
| vo_pipeline_stage_runs | 月 | 1 年热 / 归档 |
| vo_inference_releases | 月 | 1 年热 / 归档 |
| vo_inference_usage | 月 | 3 月热 / 1 年归档（高写入） |
| vo_external_api_call_logs | 月 | 3 月热 / 1 年归档 |
| vo_audit_logs | 月 | 2 年热 / 归档 |
| vo_activity_feeds | 月 | 1 年热 / 归档 |
| vo_notifications | 月 | 6 月热 |
| vo_alert_events | 月 | 1 年热 / 归档 |

```sql
CREATE TABLE builds (...) PARTITION BY RANGE (created_at);
CREATE TABLE builds_2026_06 PARTITION OF builds
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
```

### 16.5 热点表优化

| 表 | 优化 |
| --- | --- |
| vo_releases | 进行中发布索引 `(status) WHERE status IN ('running','pending_approval','paused')`，部分索引仅含热数据 |
| vo_builds | 同上，`WHERE status IN ('pending','queued','running')` |
| vo_notifications | `(user_id, is_read, created_at DESC) WHERE is_read = false` 部分索引 |
| vo_refresh_tokens | 定期清理过期/已撤销 |

### 16.6 索引补充

- 所有分片表索引前置含 `workspace_id`（Citus 分布列需在索引中以便本地化查询）。
- 大表 `EXPLAIN` 校验，避免跨分片 broadcast join。

### 16.7 容量参考

见 [扩展性设计 §3 容量估算](scalability.md#3-容量估算)。
