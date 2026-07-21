# 中间件开放 API 部署指南

中间件实例通过开放 API（`/api/v1/ext`）以**普通应用**方式部署，平台仅承载 K8s 部署；构建、镜像、发布、扩缩容、停止/启动、删除、成员、状态等所有操作均由中间件团队通过 API 完成。

## 前置条件

1. 在平台创建 **external API Token**，勾选 scope `ext:middleware`。
   - `ext:middleware` 涵盖全生命周期：部署、更新、扩缩容、停止/启动、删除、状态、Pod、日志、发布历史、回滚、成员、镜像管理。
   - 如需查询通用分组状态（非中间件专用端点），可额外勾选 `ext:status`。
2. 在平台登记**镜像仓库**（含公共仓库，公共仓库 `CredentialID=0`），记录 `registryUuid`。
3. 登记 **K8s 集群**，记录 `clusterUuid` 或 `clusterId`。
4. 目标工作空间 UUID：`wsUuid`。

所有端点均要求 `Authorization: Bearer voe_<token>`，写操作建议带 `Idempotency-Key` 头。

## 端点总览

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/workspaces/{wsUuid}/middleware-deployments` | 创建并部署中间件应用 |
| PATCH | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}` | 更新配置/资源/换镜像重发 |
| POST | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}:scale` | 扩缩容 |
| POST | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}:stop` | 停止（关机，scale 到 0，保留原副本数） |
| POST | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}:start` | 启动（恢复关机前副本数） |
| POST | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}:rollback` | 回滚到上一成功发布 |
| DELETE | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}` | 删除应用（先删分组再删应用） |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/status` | 查询主分组状态 + 运行态 |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods` | 列出主分组 Pod |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods/{pod}/logs` | 拉取 Pod 日志（`?container=&tail=`） |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases` | 发布历史（`?status=&page=&size=`） |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases/current` | 当前发布 |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members` | 列出成员 |
| POST | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members` | 添加成员 |
| PUT | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}` | 更新成员角色 |
| DELETE | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}` | 移除成员 |
| GET | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/images` | 列出已登记镜像 |
| DELETE | `/workspaces/{wsUuid}/middleware-deployments/{appUuid}/images/{imageId}` | 退役镜像 |

所有 `{appUuid}` 键端点内部会自动解析到应用的主分组，调用方无需关心 `groupUuid`。

## 部署中间件

```http
POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments
Authorization: Bearer voe_<token>
Content-Type: application/json
Idempotency-Key: deploy-redis-001

{
  "name": "redis-cache",
  "displayName": "Redis 缓存",
  "groupName": "default",
  "imageRef": "docker.io/library/redis:7",
  "registryUuid": "<平台登记的仓库 UUID>",
  "clusterUuid": "<集群 UUID>",
  "namespace": "middleware",
  "replicas": 1,
  "managingTeam": "middleware-team",
  "env": [
    { "name": "REDIS_PASSWORD", "value": "secret", "is_secret": true }
  ],
  "resources": {
    "cpu_m": 500,
    "memory_bytes": 536870912
  }
}
```

响应：

```json
{
  "success": true,
  "data": {
    "applicationUuid": "...",
    "groupUuid": "...",
    "imageUuid": "...",
    "releaseId": 42
  }
}
```

应用 `metadata.managed_by` 为 `ext_api`，平台 UI 显示「外部托管」并隐藏构建/镜像/发布等操作入口。

## 扩缩容 / 停止 / 启动

```http
POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}:scale
{ "replicas": 3 }

POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}:stop
# 无 body；scale 到 0，原副本数存入 metadata.shutdown_replicas

POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}:start
# 无 body；从 metadata.shutdown_replicas 恢复副本数
```

> 推荐用 `:stop` / `:start` 而非 `scale:0`，前者保留原副本数便于恢复。

## 更新配置 / 镜像

```http
PATCH /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}
{
  "replicas": 2,
  "env": [{ "name": "FOO", "value": "bar" }],
  "files": [{ "path": "/etc/redis/redis.conf", "content": "..." }],
  "command": ["redis-server"],
  "args": ["--appendonly", "yes"],
  "imageRef": "docker.io/library/redis:7.2",
  "registryUuid": "<仓库 UUID>",
  "version": 1
}
```

`version` 为分组乐观锁版本号（可选，默认使用当前值）。设置 `imageRef` 时会登记新外部镜像并触发标准发布流程。

## 回滚

```http
POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}:rollback
# 无 body
```

## 删除

```http
DELETE /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}
# 先逐个删除分组，再删除应用
```

## 状态 / Pod / 日志

```http
GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/status

GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods

GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/pods/{pod}/logs?container=redis&tail=1000
# 返回 text/plain 流式日志
```

## 发布历史

```http
GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases?status=succeeded&page=1&size=20

GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/releases/current
```

## 成员管理

```http
GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members?page=1&size=50

POST /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members
{ "userId": 10, "roleId": 5 }

PUT /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}
{ "roleId": 6 }

DELETE /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/members/{userId}
```

> 鉴权：所有写操作（添加/改角色/移除成员、删除、停止、启动）要求 token 用户为应用 owner。

## 镜像管理

```http
GET /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/images?page=1&size=50

DELETE /api/v1/ext/workspaces/{wsUuid}/middleware-deployments/{appUuid}/images/{imageId}
# 退役镜像（软删除，已发布的引用不受影响）
```

## 与普通应用发布的关系

- 外部镜像通过 `RegisterExternalImage`（`Source=manual`）写入 `vo_images`，发布走标准 `releaseapp` → K8s renderer/applier。
- 不再使用 Helm 中间件子系统（`vo_middleware_*` 表已移除）。
- WebSSH 不在平台提供（按设计），如需排查 Pod 可用 Pod 日志端点。

## Scope 说明

- `ext:middleware`：中间件应用全生命周期（推荐中间件团队使用）。
- `ext:configset` / `ext:image`：预留 scope，当前中间件场景下配置与镜像均由 `ext:middleware` 端点统一处理，未来如需独立粒度可启用。
