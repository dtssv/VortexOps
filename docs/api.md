# API 设计

## 1. 通用约定

### 1.1 基础

- Base URL: `/api/v1`
- 编码：UTF-8 JSON
- 时间：ISO 8601 带时区（`2026-06-20T07:42:00+08:00`）
- ID：对外使用 UUID（字符串）；分页游标使用 base64 字符串。
- 幂等：写接口接受可选 `Idempotency-Key` 头。

### 1.2 认证

- 头：`Authorization: Bearer <accessToken>`
- accessToken 有效期 15min；refreshToken 7d。
- WebSocket 鉴权：连接时通过 `?token=` 或 `Sec-WebSocket-Protocol` 传递。

### 1.3 统一响应

成功：

```json
{
  "code": 0,
  "message": "ok",
  "data": { ... },
  "traceId": "a1b2c3"
}
```

分页：

```json
{
  "code": 0,
  "data": {
    "items": [...],
    "nextCursor": "eyJpZCI6MTIzfQ==",
    "hasMore": true
  }
}
```

错误：

```json
{
  "code": "FORBIDDEN",
  "message": "无权执行此操作",
  "details": {"requiredRole": "developer", "yourRole": "viewer"},
  "traceId": "a1b2c3"
}
```

### 1.4 HTTP 状态码

| 状态码 | 场景 |
| --- | --- |
| 200 | 成功（GET/PUT/PATCH） |
| 201 | 创建成功（POST） |
| 202 | 已接受异步任务（构建/发布） |
| 204 | 无内容（DELETE） |
| 400 | 参数错误 |
| 401 | 未认证 |
| 403 | 无权限 |
| 404 | 资源不存在 |
| 409 | 冲突（如重复名、状态不允许） |
| 422 | 业务校验失败 |
| 429 | 限流 |
| 500 | 服务器错误 |

### 1.5 错误码表（部分）

| code | HTTP | 含义 |
| --- | --- | --- |
| `UNAUTHENTICATED` | 401 | 未登录或 token 失效 |
| `FORBIDDEN` | 403 | 权限不足 |
| `NOT_FOUND` | 404 | 资源不存在 |
| `VALIDATION_FAILED` | 400 | 参数校验失败 |
| `CONFLICT` | 409 | 资源冲突 |
| `INVALID_STATE` | 422 | 资源状态不允许操作（如已归档） |
| `CLUSTER_UNREACHABLE` | 503 | K8s 集群不可达 |
| `JENKINS_UNAVAILABLE` | 503 | Jenkins 不可达 |
| `RELEASE_IN_PROGRESS` | 409 | 已有发布进行中 |
| `BUILD_ALREADY_RUNNING` | 409 | 同分支已有构建运行中 |

## 2. 认证与用户

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/auth/login` | 本地账号登录，返回 access+refresh |
| POST | `/auth/refresh` | 刷新 access token |
| POST | `/auth/logout` | 撤销 refresh token |
| GET | `/auth/oidc/start` | OIDC 跳转 |
| GET | `/auth/oidc/callback` | OIDC 回调 |
| GET | `/me` | 当前用户信息 + 有效角色汇总 |

### POST `/auth/login`

请求：
```json
{"username": "alice", "password": "******"}
```
响应：
```json
{
  "accessToken": "eyJ...",
  "refreshToken": "eyJ...",
  "expiresIn": 900,
  "user": {"uuid":"...","username":"alice","displayName":"Alice"}
}
```

## 3. 用户管理（platform_admin）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/users` | 用户列表（分页、搜索） |
| POST | `/users` | 创建用户 |
| GET | `/users/{uuid}` | 用户详情 |
| PUT | `/users/{uuid}` | 更新用户 |
| POST | `/users/{uuid}/disable` | 禁用 |
| POST | `/users/{uuid}/enable` | 启用 |
| POST | `/users/{uuid}/reset-password` | 重置密码 |

## 4. 集群管理（platform_admin）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/clusters` | 列表 |
| POST | `/clusters` | 创建（上传 kubeconfig） |
| GET | `/clusters/{uuid}` | 详情 |
| PUT | `/clusters/{uuid}` | 更新 |
| DELETE | `/clusters/{uuid}` | 删除（需无空间引用） |
| POST | `/clusters/{uuid}/check` | 触发健康检查 |
| GET | `/clusters/{uuid}/namespaces` | 列举集群 Namespace |

## 5. 空间管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces` | 我可见的空间列表 |
| POST | `/workspaces` | 创建（自助建空间，创建者自动 admin；受 `vo_workspace_creation_policies` 约束） |
| GET | `/workspaces/creation-policy` | 当前用户的建空间策略（是否允许、上限、默认配额） |
| GET | `/workspaces/{uuid}` | 详情 |
| PUT | `/workspaces/{uuid}` | 更新（admin） |
| POST | `/workspaces/{uuid}/archive` | 归档（admin） |
| POST | `/workspaces/{uuid}/transfer-ownership` | 转让 Owner（admin） |
| GET | `/workspaces/{uuid}/members` | 成员列表 |
| POST | `/workspaces/{uuid}/members` | 添加成员 |
| POST | `/workspaces/{uuid}/members:batch` | 批量添加成员 |
| PUT | `/workspaces/{uuid}/members/{userUuid}` | 修改角色 |
| DELETE | `/workspaces/{uuid}/members/{userUuid}` | 移除成员 |
| GET | `/workspaces/{uuid}/clusters` | 空间绑定的集群 |
| POST | `/workspaces/{uuid}/clusters` | 绑定集群+Namespace |
| DELETE | `/workspaces/{uuid}/clusters/{bindingUuid}` | 解绑 |

### POST `/workspaces`（自助建空间）

```json
{
  "name": "team-svc",
  "displayName": "交易服务团队",
  "description": "...",
  "clusterUuid": "...",          // 可选，指定绑定集群；不填用策略默认
  "namespace": "team-svc"        // 可选
}
```

平台按当前用户匹配的 `vo_workspace_creation_policies` 校验：是否允许自助创建、是否超 `max_workspaces_per_user`、是否需审批。通过则创建空间、自动加创建者为 admin、应用默认配额与默认集群绑定、自动绑定平台默认中间件目录/基础镜像（`auto_bind_catalog`）。

### POST `/workspaces/{uuid}/members`

```json
{"userUuid": "...", "role": "developer"}
```

## 6. 应用管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/applications` | 应用列表 |
| POST | `/workspaces/{wsUuid}/applications` | 创建应用 |
| GET | `/applications/{uuid}` | 应用详情 |
| PUT | `/applications/{uuid}` | 更新（admin） |
| DELETE | `/applications/{uuid}` | 删除（admin） |
| POST | `/applications/{uuid}/archive` | 归档 |
| GET | `/applications/{uuid}/members` | 应用成员 |
| POST | `/applications/{uuid}/members` | 添加成员 |
| PUT | `/applications/{uuid}/members/{userUuid}` | 改角色 |
| DELETE | `/applications/{uuid}/members/{userUuid}` | 移除 |
| GET | `/applications/{uuid}/git-sources` | Git 源列表 |
| POST | `/applications/{uuid}/git-sources` | 添加 Git 源 |
| PUT | `/git-sources/{uuid}` | 更新 |
| DELETE | `/git-sources/{uuid}` | 删除 |
| GET | `/git-sources/{uuid}/branches` | 拉取分支 |
| GET | `/git-sources/{uuid}/tags` | 拉取 Tag |
| GET | `/git-sources/{uuid}/commits?branch=main` | 分支提交历史 |

### GET `/git-sources/{uuid}/branches`

响应：
```json
{
  "code": 0,
  "data": {
    "items": [
      {"name":"main","lastCommitSha":"abc123","lastCommitAt":"2026-06-19T10:00:00Z","author":"bob"}
    ]
  }
}
```

## 7. 分组管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/applications/{appUuid}/groups` | 分组列表 |
| POST | `/applications/{appUuid}/groups` | 创建分组（含目标集群/Namespace） |
| GET | `/groups/{uuid}` | 详情（含当前镜像、配置、Pod 概览、HPA 状态） |
| PUT | `/groups/{uuid}` | 更新（副本/资源/策略/工作负载类型/HPA） |
| DELETE | `/groups/{uuid}` | 删除（admin，需无运行 Pod 或强制） |
| POST | `/groups/{uuid}:clone` | 克隆分组（复制规格/配置/配置集绑定到新分组） |
| GET | `/groups/{uuid}/yaml` | 分组渲染后的 K8s 工作负载 YAML（只读预览） |
| GET | `/groups/{uuid}/pods` | Pod 列表 |
| WS | `/groups/{uuid}/pods/{pod}/logs` | 日志流 |
| GET | `/groups/{uuid}/logs/search` | 跨 Pod 日志聚合搜索 |
| WS | `/groups/{uuid}/pods/{pod}/exec` | Pod 终端（exec） |
| POST | `/groups/{uuid}/pods/{pod}/portforward` | 端口转发（返回会话） |
| DELETE | `/groups/{uuid}/pods/{pod}/portforward/{sessionId}` | 关闭端口转发 |
| GET | `/groups/{uuid}/events` | K8s 事件列表 |
| GET/PUT | `/groups/{uuid}/autoscaling` | HPA 配置（启用/指标/范围） |
| GET | `/groups/{uuid}/autoscaling/events` | 伸缩事件历史 |

### POST `/applications/{appUuid}/groups`

```json
{
  "name": "prod",
  "clusterUuid": "...",
  "namespace": "team-svc",
  "deploymentName": "payment-prod",
  "replicas": 3,
  "resourceTemplateUuid": "...",
  "resources": {
    "cpuMillis": 4000,
    "cpuLimitMillis": 8000,
    "memoryBytes": 8589934592,
    "memoryLimitBytes": 17179869184,
    "gpu": 1,
    "gpuType": "nvidia-a100",
    "storageSizeBytes": 107374182400,
    "storageClass": "fast-ssd",
    "ephemeralStorageRequestBytes": 10737418240,
    "ephemeralStorageLimitBytes": 21474836480
  },
  "network": {
    "mode": "loadbalancer",
    "servicePorts": [
      {"name": "http", "port": 80, "targetPort": 8080, "protocol": "TCP"}
    ],
    "keepPodIp": true,
    "allowEgressInternet": false,
    "egressAllowlist": [
      {"cidr": "10.0.0.0/8"},
      {"host": "npm.registry.com", "port": 443}
    ],
    "networkPolicyEnabled": true,
    "ingressEnabled": true,
    "ingressHost": "payment.example.com",
    "ingressPath": "/"
  },
  "scheduling": {
    "nodeSelector": {"accelerator": "nvidia-a100"},
    "tolerations": [{"key": "nvidia.com/gpu", "effect": "NoSchedule"}],
    "priorityClass": "high"
  },
  "strategy": "rolling",
  "maxSurge": 1,
  "maxUnavailable": 0
}
```

> 资源规格、网络、调度变更非即时生效，会生成新的配置版本并触发发布（可审计可回滚）。`network_mode=hostnetwork`、`keepPodIp`、`allowEgressInternet=false` 等高级网络选项需 `action:group:advanced-network` 权限。校验规则见 [K8s 集成 §4.4 资源校验](kubernetes.md#44-资源校验)。

### 集群资源画像（创建分组时下拉用）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/clusters/{uuid}/node-pools` | 集群节点池画像（机型/CPU/内存/GPU/StorageClass） |
| GET | `/clusters/{uuid}/ip-pools` | 稳定 IP 池列表（剩余可用数） |
| GET | `/clusters/{uuid}/ip-pools/{poolUuid}/available` | 池内可用 IP |
| GET/POST/PUT/DELETE | `/clusters/{uuid}/ip-pools` | IP 池管理（admin） |
| GET | `/resource-templates` | 资源规格模板列表（平台+空间级） |
| POST | `/workspaces/{wsUuid}/resource-templates` | 创建空间级模板 |
| PUT/DELETE | `/resource-templates/{uuid}` | 更新/删除模板 |

## 8. 镜像与制品版本管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/base-images` | 基础镜像目录 |
| POST | `/base-images` | 新增基础镜像（platform_admin） |
| DELETE | `/base-images/{uuid}` | 删除（非系统内置） |
| GET | `/applications/{appUuid}/images` | 制品版本列表（含版本号、digest、git 来源、使用分组、扫描状态） |
| GET | `/applications/{appUuid}/images/{uuid}` | 制品版本详情 |
| POST | `/applications/{appUuid}/images:manual` | 手动登记外部镜像 |
| DELETE | `/images/{uuid}` | 软删除（被别名引用/近期回退的除外） |
| POST | `/applications/{appUuid}/images:cleanup` | 按保留策略清理旧制品版本 |
| GET/POST/PUT/DELETE | `/applications/{appUuid}/image-tags` | 制品版本别名（stable/production...） |
| PUT | `/applications/{appUuid}/image-tags/{name}` | 移动别名到指定版本 `{imageUuid}` |
| GET | `/groups/{uuid}/images:rollback` | 可回退的制品版本列表（排除当前） |
| POST | `/groups/{uuid}/releases:rollback-image` | 回退到指定制品版本 `{targetImageUuid, targetConfigUuid?}` |

## 9. 构建

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/applications/{appUuid}/builds` | 构建历史（分页） |
| POST | `/applications/{appUuid}/builds` | 触发构建 |
| GET | `/builds/{uuid}` | 构建详情 |
| POST | `/builds/{uuid}/cancel` | 取消构建 |
| WS | `/builds/{uuid}/logs?fromOffset=` | 实时日志（增量，支持 fromOffset 续传） |
| GET | `/builds/{uuid}/logs?offset=0&limit=` | 历史日志（分页） |
| GET | `/builds/{uuid}/logs:search?keyword=&step=` | 日志全文搜索（返回命中行号+上下文） |
| GET | `/builds/{uuid}/logs:download?step=` | 下载日志（原始/zip，支持 `Range`） |
| POST | `/builds/{uuid}/logs:share` | 生成带过期的日志分享链接 |
| GET | `/builds/{uuid}/steps/{seq}/error-line` | 步骤错误行摘要（快速定位失败） |
| GET | `/builds/{uuid}/logs:compare?otherBuildUuid=` | 与另一次构建日志对比 |

### POST `/applications/{appUuid}/builds`

请求（BuildSpec）：
```json
{
  "gitSourceUuid": "...",
  "refType": "branch",
  "refValue": "main",
  "buildStrategy": "java",
  "buildCommand": null,
  "contextPath": ".",
  "baseImageUuid": "...",
  "dockerfileSource": "template",
  "dockerfileContent": null,
  "targetImage": {
    "registry": "harbor.example.com",
    "repository": "team-svc/payment",
    "tag": null
  },
  "buildArgs": {"PROFILE":"prod"}
}
```

响应 202：
```json
{"code":0,"data":{"buildUuid":"...","status":"pending"}}
```

## 10. 配置管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/groups/{uuid}/configs` | 配置版本历史 |
| GET | `/groups/{uuid}/configs/current` | 当前生效配置（含合并后的最终配置 + 各配置集贡献） |
| POST | `/groups/{uuid}/configs` | 新建配置版本（草稿） |
| GET | `/configs/{uuid}` | 配置版本详情 |
| GET | `/groups/{uuid}/configs/diff?from=3&to=5` | 同分组版本对比 |
| POST | `/configs/{uuid}:apply` | 应用此配置（触发 config_only release） |
| POST | `/groups/{uuid}/configs:rollback` | 回滚到指定版本 `{targetVersion}` |
| GET | `/configs:compare?groupA={uuid}&groupB={uuid}` | 跨分组配置比对（默认各自当前生效；可 `?versionA=&versionB=` 指定） |
| POST | `/groups/{targetUuid}/configs:copy-from` | 从另一分组复制配置生成草稿 `{sourceGroupUuid, sourceConfigUuid?}` |
| GET | `/groups/{uuid}/resolved-config` | 解析后的最终生效配置（合并配置集 + 自身） |

### 配置集（ConfigSet）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/config-sets` | 配置集列表（按 scope 过滤） |
| POST | `/workspaces/{wsUuid}/config-sets` | 创建配置集 |
| GET | `/config-sets/{uuid}` | 配置集详情（含 current 版本） |
| PUT/DELETE | `/config-sets/{uuid}` | 更新/删除 |
| GET | `/config-sets/{uuid}/versions` | 配置集版本历史 |
| POST | `/config-sets/{uuid}/versions` | 新建版本（草稿） |
| GET | `/config-set-versions/{uuid}` | 版本详情 |
| GET | `/config-sets/{uuid}/versions/diff?from=2&to=4` | 配置集版本间对比 |
| POST | `/config-set-versions/{uuid}:set-current` | 设为当前版本 |
| POST | `/config-set-versions/{uuid}:rollback` | 回退配置集到该版本 |
| GET | `/config-sets/{uuid}/bindings` | 关联了哪些分组（影响面） |
| POST | `/groups/{groupUuid}/config-set-bindings` | 关联配置集 `{configSetUuid, pinned?, pinnedVersionUuid?, priority, mergeStrategy}` |
| PUT/DELETE | `/config-set-bindings/{uuid}` | 更新绑定（锁版本/优先级）/ 解绑 |

### POST `/groups/{uuid}/configs`

```json
{
  "changeSummary": "调高日志级别并新增配置文件",
  "snapshot": {
    "files": [
      {"path":"/etc/app/application.yml","mode":"0644","secret":false,"content":"server:\n  port: 8080\n"},
      {"path":"/etc/app/db-password","mode":"0600","secret":true,"content":"<plaintext-or-base64>"}
    ],
    "command": ["java","-jar","/app/app.jar"],
    "args": ["--spring.profiles.active=prod"],
    "env": [
      {"name":"LOG_LEVEL","value":"INFO","secret":false},
      {"name":"DB_PASSWORD","value":"s3cret","secret":true}
    ],
    "envFrom": [{"configMapRef":"app-cm"},{"secretRef":"app-secret"}]
  }
}
```

> Secret 值在传输与存储均加密；接口返回时 `secret:true` 的值替换为 `***`，仅 admin 可通过 `?reveal=true` 查询（记审计）。

## 11. 发布

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/groups/{uuid}/releases` | 发布历史 |
| POST | `/groups/{uuid}/releases:rolling` | 整组发布 |
| POST | `/groups/{uuid}/releases:canary` | 灰度发布 |
| POST | `/groups/{uuid}/releases:config-only` | 纯配置变更发布 |
| POST | `/groups/{uuid}/releases:rollback-image` | 回退到指定制品版本 |
| POST | `/groups/{uuid}/releases:retry` | 重试上次失败发布 |
| POST | `/releases:batch` | 批量发布（多分组同镜像/配置） |
| GET | `/releases/{uuid}` | 发布详情 |
| POST | `/releases/{uuid}:promote` | 灰度提升为整组 |
| POST | `/releases/{uuid}:abort` | 放弃灰度 |
| POST | `/releases/{uuid}:rollback` | 回滚到此版本 |
| POST | `/releases/{uuid}:cancel` | 终止发布 |
| WS | `/releases/{uuid}/events` | 发布事件流 |
| GET/POST/PUT/DELETE | `/release-presets` | 发布预设管理（按 scope） |
| GET/POST/PUT/DELETE | `/release-windows` | 发布窗口管理 |

### POST `/groups/{uuid}/releases:rolling`

```json
{
  "imageUuid": "...",
  "configVersion": 5,
  "presetUuid": "...",
  "changeSummary": "升级到 v1.4.0"
}
```

### POST `/releases:batch`

```json
{
  "targetGroupUuids": ["...", "..."],
  "imageUuid": "...",
  "configVersion": null,
  "changeSummary": "批量同步 v1.4.0",
  "sequential": true,
  "stopOnFailure": true
}
```

> `sequential=true` 串行发布（一组成功再发下一组），`stopOnFailure=true` 失败则停止后续。批量发布汇总为一张总览页，展示每组进度。

## 12. 审计

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/audit-logs` | 全局审计（platform_admin） |
| GET | `/workspaces/{uuid}/audit-logs` | 空间审计 |
| GET | `/applications/{uuid}/audit-logs` | 应用审计 |

过滤参数：`actor`、`action`、`resourceType`、`status`、`from`、`to`，均分页。

## 13. 权限、菜单与角色

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/me/menus` | 当前用户可见菜单树（已按权限过滤） |
| GET | `/me/permissions` | 当前用户有效权限码列表 |
| GET | `/permissions` | 全部权限码（角色配置页用，admin） |
| GET | `/menus` | 全部菜单树（admin） |
| GET | `/roles` | 角色列表（?scope=platform&scopeId=） |
| POST | `/roles` | 创建自定义角色 |
| GET | `/roles/{uuid}` | 角色详情含权限码 |
| PUT | `/roles/{uuid}` | 更新角色 |
| DELETE | `/roles/{uuid}` | 删除自定义角色 |
| PUT | `/roles/{uuid}/permissions` | 批量设置权限码 |
| POST | `/platform/users/{userUuid}/roles` | 绑定平台角色 |
| DELETE | `/platform/users/{userUuid}/roles/{roleUuid}` | 解绑 |

### GET `/me/menus` 响应示例

```json
{
  "code": 0,
  "data": [
    {
      "code": "build-release",
      "name": "构建与发布",
      "path": "/build-release",
      "icon": "RocketOutlined",
      "children": [
        {"code": "build-center", "name": "构建中心", "path": "/builds", "icon": "BuildOutlined"}
      ]
    }
  ]
}
```

## 14. 工作台与搜索

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/dashboard` | 工作台聚合数据 |
| GET | `/dashboard/todos` | 待办（审批、失败任务） |
| GET | `/search?q=&types=&workspaceUuid=` | 全局搜索（走 ES，支持类型过滤） |

### 大规模分页与导出约定（见 [扩展性设计](scalability.md)）

- 所有列表强制游标分页：`?cursor=&limit=`，默认 20，最大 100（大表 50）。
- 不支持 `page/size` 偏移分页（深翻页性能差）；前端只允许「下一页/上一页」。
- 大数据导出走异步任务：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/exports` | 创建导出任务 `{type, filters}`，返回 jobUuid |
| GET | `/exports/{uuid}` | 查询导出任务状态与下载链接 |
| GET | `/exports` | 我的导出任务列表 |

- 批量接口单次 ≤100：成员批量加 `POST /workspaces/{uuid}/members:batch`、镜像批量删 `POST /images:batch-delete`。

### GET `/dashboard` 响应摘要

```json
{
  "pendingApprovals": 2,
  "runningBuilds": 1,
  "runningReleases": 0,
  "recentFailures": 3,
  "favorites": [...],
  "recentResources": [...],
  "announcements": [...],
  "buildTrend": {"dates":[],"success":[],"failed":[]}
}
```

## 15. 告警

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/alert-rules` | 告警规则列表 |
| POST | `/workspaces/{wsUuid}/alert-rules` | 创建规则 |
| GET/PUT/DELETE | `/alert-rules/{uuid}` | 规则详情/更新/删除 |
| GET | `/workspaces/{wsUuid}/alert-events` | 告警事件列表（?status=firing&severity=） |
| GET | `/alert-events/{uuid}` | 事件详情 |
| POST | `/alert-events/{uuid}:resolve` | 手动标记已解决 |
| GET/POST/DELETE | `/workspaces/{wsUuid}/silenced-alerts` | 静默规则（创建/列表/删除） |
| GET | `/workspaces/{wsUuid}/alert-rules/builtin` | 内置规则模板（发布失败/重启过多等） |

## 16. 通知

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/notifications` | 通知列表（?isRead=false） |
| GET | `/notifications/unread-count` | 未读数 |
| POST | `/notifications/{uuid}/read` | 标记已读 |
| POST | `/notifications/read-all` | 全部已读 |
| GET | `/me/notification-settings` | 通知偏好 |
| PUT | `/me/notification-settings` | 更新偏好 |

## 17. 审批

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/approval-flows` | 审批流列表 |
| POST | `/approval-flows` | 创建审批流 |
| PUT | `/approval-flows/{uuid}` | 更新 |
| DELETE | `/approval-flows/{uuid}` | 删除 |
| GET | `/approvals` | 审批实例列表（?status=pending&mine=true） |
| GET | `/approvals/{uuid}` | 审批详情 |
| POST | `/approvals/{uuid}/approve` | 通过 |
| POST | `/approvals/{uuid}/reject` | 拒绝 |

## 18. 构建模板

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/applications/{appUuid}/build-templates` | 模板列表 |
| POST | `/applications/{appUuid}/build-templates` | 创建 |
| PUT | `/build-templates/{uuid}` | 更新 |
| DELETE | `/build-templates/{uuid}` | 删除 |

## 19. 收藏、回收站、评论

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/me/favorites` | 收藏列表 |
| POST | `/me/favorites` | 添加收藏 `{resourceType, resourceUuid}` |
| DELETE | `/me/favorites/{uuid}` | 取消收藏 |
| PUT | `/me/favorites/reorder` | 排序 |
| GET | `/recycle-bin` | 回收站列表 |
| POST | `/recycle-bin/{uuid}/restore` | 恢复 |
| DELETE | `/recycle-bin/{uuid}` | 彻底删除 |
| GET | `/comments?resourceType=release&resourceUuid=` | 评论列表 |
| POST | `/comments` | 发表评论（支持 @mention） |
| DELETE | `/comments/{uuid}` | 删除自己的评论 |

## 20. 用户偏好与 API Token

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/me/preferences` | 用户偏好 |
| PUT | `/me/preferences` | 更新（主题、布局、默认空间） |
| GET | `/me/api-tokens` | Token 列表 |
| POST | `/me/api-tokens` | 创建（返回明文一次） |
| DELETE | `/me/api-tokens/{uuid}` | 撤销 |

## 21. 动态流与公告

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{uuid}/activities` | 空间动态流 |
| GET | `/applications/{uuid}/activities` | 应用动态流 |
| GET | `/announcements` | 有效公告 |
| POST | `/announcements` | 创建公告（admin） |

## 22. 基础设施扩展

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST/PUT/DELETE | `/registries` | 镜像仓库实例 |
| GET/POST/PUT/DELETE | `/jenkins-instances` | Jenkins 实例 |
| GET/POST/PUT/DELETE | `/webhooks` | 出站 Webhook |
| GET/PUT | `/workspaces/{uuid}/quotas` | 空间配额 |

## 23. 中间件管理

中间件实例挂在空间下，与应用平级。数据模型见 [数据模型 §9](data-model.md#9-中间件)，部署流程见 [构建与发布 §13](build-release.md#中间件部署)。

### 22.1 中间件目录（平台级）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/middleware-catalog` | 中间件类型目录列表 |
| GET | `/middleware-catalog/{uuid}` | 详情（含 schema_config） |
| POST | `/middleware-catalog` | 新增类型（admin） |
| PUT | `/middleware-catalog/{uuid}` | 更新（admin） |
| DELETE | `/middleware-catalog/{uuid}` | 删除（非系统内置） |
| GET | `/middleware-catalog/{uuid}/versions` | 可用版本列表 |

### 22.2 中间件实例（空间级）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/middleware` | 实例列表（分页/筛选） |
| POST | `/workspaces/{wsUuid}/middleware` | 创建实例并安装 |
| GET | `/middleware/{uuid}` | 实例详情（含状态、access_info） |
| PUT | `/middleware/{uuid}` | 编辑实例元信息 |
| DELETE | `/middleware/{uuid}` | 卸载（默认保留数据） |
| POST | `/middleware/{uuid}:purge-data` | 连同 PVC 数据删除（admin，二次确认） |
| GET | `/middleware/{uuid}/pods` | 实例 Pod 列表 |
| WS | `/middleware/{uuid}/logs?pod=` | Pod 日志流 |
| GET | `/middleware/{uuid}/events` | K8s 事件 |

### 22.3 中间件参数（版本化）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/middleware/{uuid}/params` | 参数版本历史 |
| GET | `/middleware/{uuid}/params/current` | 当前生效参数 |
| POST | `/middleware/{uuid}/params` | 新建参数版本（草稿） |
| GET | `/middleware-params/{uuid}` | 参数版本详情 |
| GET | `/middleware/{uuid}/params/diff?from=2&to=4` | 版本对比 |
| POST | `/middleware-params/{uuid}:apply` | 应用此参数（触发 config_only release） |

### 22.4 中间件变更（安装/升级/扩缩容/回滚）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/middleware/{uuid}:upgrade` | 升级版本 |
| POST | `/middleware/{uuid}:scale` | 扩缩容 `{replicas}` |
| POST | `/middleware/{uuid}:rollback` | 回滚到历史 release |
| GET | `/middleware/{uuid}/releases` | 变更历史 |
| GET | `/middleware-releases/{uuid}` | 变更详情 |
| POST | `/middleware-releases/{uuid}:cancel` | 终止进行中变更 |
| WS | `/middleware-releases/{uuid}/events` | 变更事件流 |

### 22.5 中间件备份

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/middleware/{uuid}/backups` | 备份列表 |
| POST | `/middleware/{uuid}/backups` | 创建手动备份 |
| DELETE | `/middleware/backups/{uuid}` | 删除备份 |
| POST | `/middleware/backups/{uuid}:restore` | 恢复（可指定目标实例，覆盖现有需审批） |
| GET/PUT | `/middleware/{uuid}/backup-policy` | 备份策略 |

### 22.6 中间件连接关系

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/middleware/{uuid}/connections` | 哪些应用用了此中间件 |
| GET | `/groups/{groupUuid}/connections` | 此分组用了哪些中间件 |
| POST | `/groups/{groupUuid}/connections` | 建立连接 `{instanceUuid, credentialUuid, alias}` |
| DELETE | `/middleware-connections/{uuid}` | 解除连接 |
| GET | `/middleware/{uuid}/topology` | 依赖拓扑（实例↔应用） |

### POST `/workspaces/{wsUuid}/middleware` 请求示例

```json
{
  "catalogUuid": "...",
  "name": "order-mysql",
  "displayName": "订单库",
  "environment": "prod",
  "clusterUuid": "...",
  "namespace": "team-svc",
  "version": "8.0.36",
  "architecture": "replication",
  "replicas": 2,
  "storageClass": "fast-ssd",
  "persistenceSize": "100Gi",
  "params": {
    "auth.rootPassword": "<plaintext>",
    "primary.persistence.size": "100Gi",
    "architecture": "replication",
    "metrics.enabled": true
  },
  "backupEnabled": true,
  "changeSummary": "订单库生产实例"
}
```

响应 202：
```json
{"code":0,"data":{"instanceUuid":"...","releaseUuid":"...","status":"installing"}}
```

## 24. CI/CD 流水线与晋升

### 24.1 流水线定义

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/pipelines` | 流水线列表 |
| POST | `/workspaces/{wsUuid}/pipelines` | 创建流水线 |
| GET/PUT/DELETE | `/pipelines/{uuid}` | 详情/更新/删除 |
| GET/POST/PUT/DELETE | `/pipelines/{uuid}/stages` | 阶段管理 |
| POST | `/pipelines/{uuid}:trigger` | 手动触发 `{ref, commitSha?}` |

### 24.2 流水线执行

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/pipelines/{uuid}/runs` | 执行历史 |
| GET | `/pipeline-runs/{uuid}` | 执行详情（含各阶段状态） |
| WS | `/pipeline-runs/{uuid}/events` | 执行事件流 |
| POST | `/pipeline-runs/{uuid}:retry` | 重试失败阶段 |
| POST | `/pipeline-runs/{uuid}:cancel` | 取消执行 |
| POST | `/pipeline-runs/{uuid}/stages/{seq}:approve` | 人工门禁通过 |
| POST | `/pipeline-runs/{uuid}/stages/{seq}:reject` | 人工门禁拒绝 |

### 24.3 环境晋升

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/applications/{appUuid}/promotions` | 晋升历史 |
| POST | `/applications/{appUuid}/promotions` | 发起晋升 `{sourceEnv, targetEnv, imageUuid, targetGroupUuids, strategy}` |
| GET | `/promotions/{uuid}` | 晋升详情 |
| POST | `/promotions/{uuid}:approve` | 审批通过（prod） |
| POST | `/promotions/{uuid}:abort` | 终止晋升 |
| POST | `/promotions/{uuid}:rollback` | 晋升失败回滚 |

### 24.4 制品签名与 SBOM

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/images/{uuid}/signature` | 签名与 SBOM 信息 |
| POST | `/images/{uuid}/signature` | 触发签名（cosign/notation） |
| GET | `/images/{uuid}/sbom` | 下载 SBOM（CycloneDX/SPDX） |
| POST | `/images/{uuid}:verify` | 验签 |

### 24.5 CI/CD 看板

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/cicd/dashboard` | 流水线看板（成功率/MTTR/DORA） |
| GET | `/workspaces/{wsUuid}/cicd/metrics` | 指标统计 |

## 25. 大模型服务

### 25.1 模型仓库与模型

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST/PUT/DELETE | `/model-registries` | 模型仓库管理（admin） |
| GET | `/workspaces/{wsUuid}/models` | 模型列表 |
| POST | `/workspaces/{wsUuid}/models` | 创建模型 |
| GET/PUT/DELETE | `/models/{uuid}` | 模型详情/更新/删除 |
| GET | `/models/{uuid}/versions` | 模型版本列表 |
| POST | `/models/{uuid}/versions` | 新增版本（含下载权重） |
| GET | `/model-versions/{uuid}` | 版本详情 |
| POST | `/model-versions/{uuid}:download` | 触发权重下载 |
| POST | `/model-versions/{uuid}:set-current` | 设为当前版本 |
| GET/POST/DELETE | `/models/{uuid}/adapters` | LoRA 适配器管理 |

### 25.2 推理服务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/inference-services` | 推理服务列表 |
| POST | `/workspaces/{wsUuid}/inference-services` | 创建并部署推理服务 |
| GET | `/inference-services/{uuid}` | 详情（含状态、endpoint、GPU、用量概览） |
| PUT | `/inference-services/{uuid}` | 更新 |
| DELETE | `/inference-services/{uuid}` | 删除（释放 GPU） |
| POST | `/inference-services/{uuid}:switch-model` | 切模型版本 |
| POST | `/inference-services/{uuid}:scale` | 扩缩容 |
| POST | `/inference-services/{uuid}:rollback` | 回滚 |
| POST | `/inference-services/{uuid}:stop` | 停止（释放 GPU） |
| GET | `/inference-services/{uuid}/releases` | 变更历史 |
| GET/POST | `/inference-services/{uuid}/autoscaling` | HPA 配置 |
| GET | `/inference-services/{uuid}/pods` | Pod 列表 |
| WS | `/inference-services/{uuid}/pods/{pod}/logs` | 日志流 |

### 25.3 推理服务部署请求示例

```json
{
  "modelUuid": "...",
  "modelVersionUuid": "...",
  "framework": "vllm",
  "name": "qwen72b-prod",
  "clusterUuid": "...",
  "namespace": "ai-svc",
  "replicas": 1,
  "gpuPerReplica": 4,
  "gpuType": "nvidia-a100",
  "tensorParallelSize": 4,
  "maxModelLen": 32768,
  "extraArgs": {"gpu_memory_utilization": 0.9},
  "autoscalingEnabled": false,
  "apiKeyRequired": true,
  "changeSummary": "部署 Qwen72B 生产推理"
}
```

### 25.4 API Key 与用量

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST | `/inference-services/{uuid}/api-keys` | API Key 列表/签发 |
| DELETE | `/inference-api-keys/{uuid}` | 撤销 |
| GET | `/inference-services/{uuid}/usage` | 用量统计（?groupBy=apiKey&since=7d） |
| GET | `/inference-services/{uuid}/usage/timeseries` | 用量时序 |
| GET | `/inference-services/{uuid}/metrics` | 推理指标（吞吐/延迟/显存） |

### 25.5 多模型路由（可选）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST/PUT/DELETE | `/workspaces/{wsUuid}/inference-routes` | 路由组管理 |
| GET | `/inference-routes/{uuid}/stats` | 路由分发统计 |

## 26. 通用写操作字段

所有支持更新的资源响应均包含审计字段：

```json
{
  "uuid": "...",
  "version": 3,
  "createdAt": "...",
  "createdBy": {"uuid":"...","displayName":"Alice"},
  "updatedAt": "...",
  "updatedBy": {"uuid":"...","displayName":"Bob"},
  "deleted": false
}
```

更新请求需带 `version`（乐观锁），冲突返回 409 `VERSION_CONFLICT`。

## 27. WebSocket 协议

连接：`wss://host/api/v1/ws?token=...`

订阅消息（客户端 → 服务端）：
```json
{"type":"subscribe","channel":"build.logs","params":{"buildUuid":"..."}}
```

事件消息（服务端 → 客户端）：
```json
{"type":"event","channel":"build.logs","data":{"stream":"stdout","line":"...","ts":"..."}}
```

取消订阅：
```json
{"type":"unsubscribe","channel":"build.logs","params":{"buildUuid":"..."}}
```

心跳：服务端每 30s 发 `{"type":"ping"}`，客户端应回 `{"type":"pong"}`，60s 无响应断开。

## 28. 限流

大规模场景限流（见 [扩展性设计 §12](scalability.md#12-限流与配额)）：

- 全局：每 IP 1000 req/s。
- 单用户：100 req/s。
- 登录：每 IP 10 次/min，失败 5 次锁定 15min。
- 构建 API：每用户 5 并发，每空间可配（默认 10，大空间 50）。
- 发布 API：每分组同时仅 1 个发布（409 `RELEASE_IN_PROGRESS`）。
- 日志 WS：每用户 20 连接，每集群总并发 500。
- 搜索：每用户 10 req/s。
- 超限返回 429，Header `X-RateLimit-Remaining`。

## 29. 版本与兼容

- URL 带 `/v1` 前缀；破坏性变更升 `/v2`，旧版保留 6 个月。
- 字段新增不影响兼容；字段废弃先返回但标记 `deprecated`，3 个月后移除。
- Webhook/Jenkins 回调签名：`X-VortexOps-Signature: t=...,v1=...`（HMAC-SHA256 of body）。

## 30. 对外 API（External API）

平台提供独立对外 API（`/api/v1/ext/`），供外部系统程序化调用部署、扩缩容、配置、构建、回滚等。独立鉴权（external Token + scope + IP 白名单）、独立限流、独立审计（`vo_external_api_call_logs`）。详见 [对外 API](api-external.md)。OpenAPI Spec 暴露于 `/api/v1/ext/openapi.json`。
