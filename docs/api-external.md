# 对外 API（External API）

VortexOps 除 UI 外，提供对外 REST API，供外部系统（CI 系统、运维平台、自研控制台、定时任务、ChatOps）程序化调用：部署、扩缩容、增减配置、构建、回滚、查询状态等。与内部 API 同源但独立网关入口、独立鉴权、独立限流与审计。

> 内部 API 设计见 [API 设计](api.md)，权限见 [权限体系 §对外 API](permissions.md)，调用日志表见 [数据模型 §3.7](data-model.md#37-external_api_call_logs对外-api-调用日志分区表)。

---

## 1. 设计原则

- **与内部 API 一致语义**：对外接口复用内部领域模型与用例，仅入口、鉴权、限流、审计不同，避免双套实现。
- **稳定优先**：对外 API 走独立版本前缀 `/api/v1/ext/`，变更走版本化，保证外部集成不被破坏性改动影响。
- **最小授权**：Token 绑定 scope（细粒度操作）+ 空间/应用范围 + IP 白名单 + 限流，防滥用。
- **幂等**：所有写接口接受 `Idempotency-Key` 头，重复请求返回首次结果。
- **可观测**：每次调用落 `vo_external_api_call_logs`，含 token、操作、资源、状态、耗时、错误，便于外部排查与计费。
- **异步友好**：长任务（构建/部署）返回任务 ID + 状态查询接口 + 可选 webhook 回调，外部不必轮询密集。

---

## 2. 鉴权

- **Token 类型**：`external` 类型 API Token（`api_tokens.token_type='external'`），格式 `voe_xxxxxxxxxxxx`。
- **请求头**：`Authorization: Bearer voe_xxxx`。
- **scope 授权**：Token 创建时勾选允许的 scope（见 §3），调用时网关校验 scope ⊆ Token.scopes。
- **范围限定**：`allowed_workspaces` / `allowed_apps` 限定可操作资源；空=该用户可见全部（仍受 RBAC 约束）。
- **IP 白名单**：`ip_allowlist` 非 空 时仅允许列表内 CIDR 调用。
- **RBAC 叠加**：Token 对应用户仍受其角色权限约束（Token 是用户身份的代理），scope 是额外收紧，不能越权。
- **限流**：按 Token `rate_limit_per_min` 限流，超限 429。

---

## 3. Scope 定义

| Scope | 说明 |
| --- | --- |
| `ext:workspace:read` | 查询空间/应用/分组 |
| `ext:deploy` | 部署/发布（整组/灰度/回滚） |
| `ext:scale` | 扩缩容分组/推理服务 |
| `ext:config` | 增删改配置版本、应用配置 |
| `ext:configset` | 配置集管理 |
| `ext:build` | 触发构建、查询构建 |
| `ext:image` | 镜像/制品版本查询、设别名、回退 |
| `ext:middleware` | 中间件实例管理 |
| `ext:inference` | 推理服务部署/切模型/扩缩容/回滚 |
| `ext:pipeline` | 流水线触发与查询 |
| `ext:status` | 查询运行时状态（Pod/事件/日志摘要） |
| `ext:rollback` | 回滚（发布/配置/制品/推理） |

---

## 4. 核心接口

所有路径前缀 `/api/v1/ext`。响应统一 `{"code":0,"data":...,"requestId":"..."}` 或 `{"code":<非0>,"message":"...","requestId":"..."}`。

### 4.1 部署与发布

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:deploy` | 部署指定制品版本到分组 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:canary` | 灰度发布 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:promote-canary` | 灰度提升 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:abort-canary` | 放弃灰度 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:rollback` | 回滚到指定 release/制品版本 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:restart` | 重启分组 |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/releases/current` | 当前发布状态 |

#### 部署请求示例

```http
POST /api/v1/ext/workspaces/{wsUuid}/groups/{groupUuid}:deploy
Authorization: Bearer voe_xxxx
Idempotency-Key: 9b1c2d3e-4f5a
Content-Type: application/json

{
  "imageUuid": "...",            // 制品版本
  "configVersion": 12,           // 可选，配置版本
  "strategy": "rolling",         // rolling/canary
  "canaryPercent": 20,           // canary 时
  "changeSummary": "外部 CI 触发部署 v1.2.3",
  "callbackUrl": "https://ci.internal/callback"  // 可选，结果回调
}
```

```json
{"code":0,"data":{"releaseUuid":"...","status":"running","releaseNumber":15},"requestId":"..."}
```

### 4.2 扩缩容

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}:scale` | 改分组副本数 |
| POST | `/workspaces/{wsUuid}/inference-services/{svcUuid}:scale` | 推理服务扩缩容 |
| PUT | `/workspaces/{wsUuid}/groups/{groupUuid}/autoscaling` | 配置 HPA |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/autoscaling` | 查询 HPA 配置 |

```json
{ "replicas": 5, "changeSummary": "外部扩容应对峰值" }
```

### 4.3 配置管理

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/configs` | 配置版本列表 |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/configs/current` | 当前生效配置 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}/configs` | 新建配置版本（文件/命令参数） |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}/configs/{version}:apply` | 应用某配置版本 |
| POST | `/workspaces/{wsUuid}/groups/{groupUuid}/configs:rollback` | 回滚配置版本 |
| POST | `/workspaces/{wsUuid}/config-sets` | 创建配置集 |
| POST | `/workspaces/{wsUuid}/config-sets/{csUuid}:bind` | 关联分组 |

#### 新建配置版本示例

```json
{
  "files": [
    {"path":"/etc/app/application.yml","content":"server:\n  port: 8080","mode":"0644"}
  ],
  "command":["java","-jar","app.jar"],
  "args":["--spring.profiles.active=prod"],
  "env":{"LOG_LEVEL":"INFO"},
  "changeSummary":"外部更新生产配置"
}
```

### 4.4 构建

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/applications/{appUuid}:build` | 触发构建 |
| GET | `/workspaces/{wsUuid}/builds/{buildUuid}` | 构建详情与状态 |
| GET | `/workspaces/{wsUuid}/builds/{buildUuid}/logs` | 构建日志（支持 `Range`/`?search=`） |
| GET | `/workspaces/{wsUuid}/images` | 制品版本列表 |
| POST | `/workspaces/{wsUuid}/images/{imgUuid}:tag` | 设版本别名 |
| POST | `/workspaces/{wsUuid}/images/{imgUuid}:rollback-target` | 标记可回退 |

### 4.5 中间件

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/middleware` | 创建并安装 |
| POST | `/workspaces/{wsUuid}/middleware/{mwUuid}:scale` | 扩缩容 |
| POST | `/workspaces/{wsUuid}/middleware/{mwUuid}:upgrade` | 升级 |
| POST | `/workspaces/{wsUuid}/middleware/{mwUuid}:backup` | 手动备份 |
| GET | `/workspaces/{wsUuid}/middleware/{mwUuid}` | 详情 |

### 4.6 推理服务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/inference-services` | 部署推理服务 |
| POST | `/workspaces/{wsUuid}/inference-services/{svcUuid}:switch-model` | 切模型版本 |
| POST | `/workspaces/{wsUuid}/inference-services/{svcUuid}:scale` | 扩缩容 |
| POST | `/workspaces/{wsUuid}/inference-services/{svcUuid}:rollback` | 回滚 |
| GET | `/workspaces/{wsUuid}/inference-services/{svcUuid}` | 状态与 endpoint |

### 4.7 流水线

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/pipelines/{pipelineUuid}:trigger` | 触发流水线 |
| GET | `/workspaces/{wsUuid}/pipeline-runs/{runUuid}` | 执行状态 |

### 4.8 状态查询

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/workspaces/{wsUuid}/groups` | 分组列表 |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}` | 分组详情（副本/资源/状态） |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/pods` | Pod 列表与状态 |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/pods/logs/search` | 跨 Pod 日志搜索（`?keyword=&since=1h`） |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/events` | K8s 事件 |
| GET | `/workspaces/{wsUuid}/groups/{groupUuid}/metrics` | 资源指标 |

### 4.9 异步任务与回调

- 长任务接口（部署/构建/切模型）返回 `releaseUuid`/`buildUuid` 等任务 ID，外部可：
  1. **轮询**：`GET` 对应状态接口。
  2. **回调**：请求带 `callbackUrl`，任务完成/失败时平台 POST 回调（带签名 `X-VortexOps-Signature`）。
- 回调请求体：`{"event":"release.finished","resourceUuid":"...","status":"succeeded","requestId":"..."}`。

### 4.10 批量操作

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/workspaces/{wsUuid}/groups:batch-deploy` | 多分组批量部署（≤20） |
| POST | `/workspaces/{wsUuid}/groups:batch-scale` | 批量扩缩容 |

---

## 5. 错误码

| HTTP | code | 含义 |
| --- | --- | --- |
| 400 | 40000 | 参数错误 |
| 401 | 40100 | Token 无效/过期 |
| 403 | 40300 | scope 不足 / RBAC 拒绝 / IP 不允许 |
| 404 | 40400 | 资源不存在 |
| 409 | 40900 | 状态冲突（如分组已有进行中发布） |
| 422 | 42200 | 业务校验失败（如配额超限、GPU 不足） |
| 429 | 42900 | 限流 |
| 500 | 50000 | 服务异常 |

错误体含 `message`（人类可读）+ `requestId`（排查用）+ 可选 `details`。

---

## 6. SDK 与集成示例

- **OpenAPI Spec**：平台暴露 `/api/v1/ext/openapi.json`（OpenAPI 3.1），外部可生成各语言 SDK（Java/Python/Go/Node）。
- **示例（curl）**：
  ```bash
  curl -X POST https://vortexops/api/v1/ext/workspaces/{ws}/groups/{g}:deploy \
    -H "Authorization: Bearer voe_xxxx" \
    -H "Idempotency-Key: $(uuidgen)" \
    -H "Content-Type: application/json" \
    -d '{"imageUuid":"...","strategy":"rolling"}'
  ```
- **示例（Python）**：
  ```python
  import requests
  API = "https://vortexops/api/v1/ext"
  H = {"Authorization":"Bearer voe_xxxx"}
  r = requests.post(f"{API}/workspaces/{ws}/groups/{g}:deploy",
      headers=H, json={"imageUuid":img,"strategy":"rolling"})
  print(r.json())
  ```
- **CI 集成**：Jenkins/GitLab CI 构建完成后调用 `:deploy` 完成自动发布；或用流水线 `ext:pipeline` 触发完整流水线。

---

## 7. 安全与治理

- Token 一次创建显示明文，后续只存哈希，丢失需重建。
- 高危操作（生产部署/回滚/中间件升级）即使有 scope，仍走平台审批/发布保护策略（与 UI 一致）。
- 所有调用审计入库，平台管理员可按 Token/操作/资源/时间查询对外调用记录。
- Token 可随时撤销；接近过期平台通知。
- 建议外部系统使用独立服务账号 + 独立 Token，按需配 scope，定期轮换。

如需了解完整内部 API 见 [API 设计](api.md)，部署平台见 [部署文档](deployment.md)。
