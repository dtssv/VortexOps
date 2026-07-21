# 架构设计

## 1. 架构总览

VortexOps 采用**前后端分离 + 后端单体（模块化）**架构，后端使用 Go 实现，遵循 DDD（领域驱动设计）分层。外部依赖 Jenkins（构建）与镜像仓库（Harbor 等）通过适配器接入。

```
┌─────────────────────────────────────────────────────────────────┐
│                       前端 (React + TS)                          │
│  工作台 │ 动态菜单 │ 构建发布 │ 配置 │ 审批 │ 通知 │ 运维观测   │
└──────────────────────────────┬──────────────────────────────────┘
                               │ HTTPS / WebSocket
┌──────────────────────────────┴──────────────────────────────────┐
│                         API Gateway (Chi)                        │
│ AuthN │ AuthZ(菜单+动作+数据) │ Audit │ RateLimit │ Trace        │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────┴──────────────────────────────────┐
│                     Application Services (用例层)                │
│ WorkspaceSvc │ BuildSvc │ ReleaseSvc │ PermissionSvc │ ApprovalSvc│
│ NotificationSvc │ DashboardSvc │ RecycleBinSvc │ MiddlewareSvc │ ...  │
│ ModelServingSvc │ PipelineEngine │ ...                                  │
└──────┬──────────────┬──────────────┬──────────────┬─────────────┘
       │              │              │              │
┌──────┴──────┐ ┌─────┴─────┐ ┌──────┴──────┐ ┌─────┴──────┐
│ Domain      │ │ K8s       │ │ Jenkins     │ │ Git        │
│ (聚合根/实体)│ │ Adapter   │ │ Adapter     │ │ Adapter    │
│             │ │ client-go │ │ REST API    │ │ Provider   │
│             │ │ + Helm    │ │             │ │            │
└──────┬──────┘ └───────────┘ └─────────────┘ └────────────┘
       │
┌──────┴──────────────────────────────────────────────────────────┐
│                  Infrastructure / Persistence                   │
│   PostgreSQL │ Redis │ Object Storage(配置文件) │ KMS(凭证加密)  │
└─────────────────────────────────────────────────────────────────┘
```

## 2. 分层职责

### 2.1 Interfaces（接口层）

- **HTTP Handlers**：接收 REST 请求，参数校验，调用 Application Service。
- **WebSocket**：实时推送构建日志、发布进度、Pod 日志流。
- **Middlewares**：认证（JWT）、授权（RBAC）、审计、限流、链路注入。

### 2.2 Application（应用服务层）

- 编排领域对象与外部适配器，实现用例（如「创建构建并发起发布」）。
- 不含业务规则，只做事务边界与协调。
- 一个用例对应一个 Service 方法，便于权限粒度控制。

### 2.3 Domain（领域层）

- 纯领域模型，无框架依赖：`Workspace`、`Application`、`Group`、`Image`、`Build`、`Release`、`Config`、`User`、`Role`。
- 包含领域不变量与业务规则（如「灰度发布 Pod 数不能超过副本数」「访客不能触发构建」）。

### 2.4 Infrastructure（基础设施层）

- 适配器实现：`KubernetesClient`、`JenkinsClient`、`GitProvider`、`RegistryClient`、`UserRepository` 等。
- 凭证加密、对象存储、消息发布等横切能力。

## 3. 核心模块

### 3.1 身份与权限模块

- 认证：JWT（Access 短期 + Refresh 长期），可选 OIDC 接入企业 SSO；个人 API Token。
- 授权：**三维 RBAC**——菜单（`menu:*`）、动作（`action:*`）、数据范围（`data:*`）。
- 角色：平台/空间/应用三级；内置角色 + 自定义角色；显式拒绝优先。
- 动态菜单：登录后按有效权限过滤菜单树，无权限菜单不返回。
- 详见 [权限体系](permissions.md)。

### 3.1.1 协作与体验模块

- **工作台**：待审批、进行中任务、收藏、最近访问、趋势图（见 [协作与体验](collaboration.md)）。
- **通知**：站内信 + 邮件/IM/Webhook 多渠道；用户偏好可配置。
- **审批流**：生产发布/配置变更多级审批，与工作台待办联动。
- **回收站**：软删除资源 30 天可恢复。
- **全局搜索**：Command Palette 跨资源检索。
- **动态流/评论**：构建/发布单协作讨论，@mention 通知。

### 3.2 资源管理模块

- Workspace → Application → Group 三层聚合。
- Workspace 绑定 K8s 集群与目标 Namespace（一对一或一对多映射）。
- Group 对应一个 K8s 工作负载（按 `workload_type`：Deployment / StatefulSet / CronJob / Job），承载镜像、副本、配置、资源规格、网络、弹性伸缩。
- 资源规格（CPU/内存/磁盘/GPU）+ 网络（模式/稳定 IP/公网访问/Ingress/NetworkPolicy）+ HPA 弹性伸缩，统一作为分组规格版本化管理，随发布生效。

### 3.3 Git 集成模块

- 抽象 `GitProvider` 接口，实现：GitHub / GitLab / Gitea / 通用 Git。
- 能力：列举分支、列举 Tag、获取 Commit、Webhook 回调、浅克隆凭证透传。
- 凭证存储：用户名密码 / Deploy Token / SSH Key，KMS 加密落库。

### 3.4 构建模块

- 构建策略：Java(Maven/Gradle) / Python(pip/poetry) / Go(go mod) / Node(npm/yarn/pnpm) / 自定义 Shell。
- 基础镜像目录：系统预置（如 `eclipse-temurin:17-jdk`、`python:3.12`、`golang:1.22`、`node:20`），可扩展。
- Dockerfile 来源：基础镜像 + 模板渲染 / 用户自定义。
- 流水线：通过 Jenkins Job DSL 动态生成 Pipeline，回调更新构建状态。
- 详见 [构建与发布](build-release.md)。

### 3.5 镜像模块

- 镜像来源：构建产物（自动） / 手动登记（外部镜像）。
- 镜像仓库适配：Harbor（首选）/ Docker Registry / ACR。
- 镜像元数据：仓库地址、Tag、Digest、构建 ID、Git Commit、大小、扫描结果（可选）。

### 3.6 发布模块

- 发布类型：
  - **整组发布（RollingUpdate）**：替换 Deployment 全部 Pod。
  - **部分 Pod 灰度发布（Canary by Pod）**：通过修改 Pod Template + 选择器或独立 Canary Deployment 实现。
- 发布动作：发布、暂停、继续、回滚、终止。
- 发布历史：每次发布记录镜像、配置版本、操作人、状态、事件流。
- 详见 [构建与发布](build-release.md)。

### 3.7 配置管理模块

- **文件级配置**：上传/编辑配置文件，挂载为 ConfigMap 或 Secret，覆盖容器内指定路径。
- **命令参数配置**：修改容器 `command`、`args`、`env`、`envFrom`。
- **版本化**：每次配置变更生成版本号，支持 diff 与一键回滚。
- 详见 [K8s 集成](kubernetes.md#配置注入)。

### 3.7.1 中间件管理模块

- **中间件目录**：预置 MySQL/Redis/Kafka/ES/RabbitMQ/MongoDB/PostgreSQL/MinIO 等，支持扩展。
- **部署方式**：Helm（推荐）/ Operator / Manifest，通过适配器抽象。
- **生命周期**：安装/升级/扩缩容/参数变更/回滚/卸载，每次变更有 release 记录。
- **备份恢复**：定时 + 手动，支持卷快照与逻辑备份，可恢复。
- **连接拓扑**：应用↔中间件依赖关系，影响面分析。
- **发布保护**：生产中间件变更默认审批，升级前自动备份。
- 详见 [构建与发布 §13](build-release.md#中间件部署)、[K8s 集成 §11](kubernetes.md#中间件资源映射)。

### 3.7.2 大模型服务模块（ModelServingSvc）

- **模型仓库与权重**：对接 HuggingFace/OSS/S3 等，下载并缓存权重到对象存储/PVC，版本化与校验。
- **推理框架适配**：`InferenceFramework` 适配器抽象（vLLM/TGI/Triton/SGLang/Ollama/custom），封装参数 schema → 启动命令 → 健康检查。
- **推理服务生命周期**：部署/切模型/扩缩容/回滚/停止，支持 rolling/blue_green/canary 切模型策略。
- **GPU 调度**：多卡张量并行（TP）/流水线并行（PP），GPU 型号选择与亲和约束，显存利用监控。
- **适配器（LoRA）**：基座模型挂载轻量适配器，多租户共用。
- **Token 计量与配额**：网关层计量 prompt/completion tokens，按 Key/调用方统计，配额限流。
- **自动伸缩**：按 QPS/队列/延迟自定义指标 HPA，缩容稳定窗口。
- **多模型路由**：加权/header/故障转移路由组（可选）。
- 详见 [大模型部署](model-serving.md)、[K8s 集成 §14](kubernetes.md#大模型推理工作负载)。

### 3.7.3 CI/CD 流水线模块（PipelineEngine）

- **流水线编排器**：按阶段（build/test/scan/image/deploy/verify/promote）调度，阶段内任务并行，阶段间按门禁推进；任务下发 Jenkins/K8s Job，编排器不直接跑任务。
- **门禁评估器**：评估测试通过率/覆盖率/CVE/签名/人工审批，结果写 `pipeline_stage_runs.gate_result`。
- **环境晋升器**：管理 dev→staging→prod 晋升链，支持 auto/canary/manual，部署后验证自动推进或回滚。
- **制品签名**：cosign/notation 签名验签，SBOM 生成与存储。
- **GitOps 同步器**（可选）：声明式配置双向同步，drift detection。
- **可观测**：流水线看板、MTTR、DORA 指标、瓶颈分析。
- 详见 [构建与发布 §14](build-release.md#14-cicd-流水线)。

### 3.7.4 对外 API 网关（ExternalAPIGateway）

- 独立入口 `/api/v1/ext/`，复用内部领域用例，仅入口/鉴权/限流/审计不同。
- **鉴权链**：external Token → 校验哈希 → scope ⊇ 所需 → RBAC（代理用户身份）→ IP 白名单 → 限流。
- **审计**：每次调用落 `vo_external_api_call_logs`（token、操作、资源、状态、耗时、错误）。
- **幂等**：`Idempotency-Key` 去重，缓存首次结果。
- **异步回调**：长任务支持 `callbackUrl` + HMAC 签名回调。
- **OpenAPI**：暴露 `/api/v1/ext/openapi.json` 供外部生成 SDK。
- 详见 [对外 API](api-external.md)。

### 3.7.5 空间自助服务模块（WorkspaceProvisioningSvc）

- **自助建空间**：普通用户（有 `action:workspace:create`）可自建空间，无需管理员。
- **策略驱动**：`vo_workspace_creation_policies` 控制是否允许、单用户上限、默认配额、默认集群、是否审批、是否自动绑定目录。
- **流程**：校验策略 → 创建空间 → 加创建者为 admin → 应用默认配额 → 绑定默认集群 → 自动绑定中间件目录/基础镜像（`auto_bind_catalog`）→ 若需审批则挂起待审。
- **配额管控**：新建空间按策略默认配额，空间内可再调整（受平台上限约束）。

### 3.8 审计与观测模块

- 审计：操作日志表，记录 who/what/when/where/before/after/traceId；敏感操作（exec、公网开关、PVC 删除、中间件密码查看等）高亮。
- 指标：Prometheus（构建数、发布数、失败率、K8s API 延迟、HPA 副本数）。
- 链路：OpenTelemetry，TraceId 贯穿 HTTP → Jenkins → K8s 调用。
- 运维操作：Pod 日志/跨 Pod 日志搜索/Pod exec/端口转发，经集群边缘代理（log-proxy/tunnel），避免长连接汇聚平台。
- **日志服务增强**：构建/发布日志归档到对象存储，支持范围拉取（`Range`）、全文搜索（行号定位/ES）、错误行自动提取（`build_steps.error_line`）、日志对比、分享链接；实时流带 offset 续传。
- 告警：规则化告警引擎（内置 + PromQL + K8s Event），触发 `vo_alert_events`，多通道通知，支持静默与事件追踪。
- 弹性观测：HPA 实时副本数、伸缩事件、到顶告警。

## 4. 关键流程时序

### 4.1 构建 → 镜像产出

```
前端                API Server           Jenkins          Registry        K8s(可选)
 │  POST /builds     │                     │                │               │
 │──────────────────>│                     │                │               │
 │                   │ 校验权限/参数        │                │               │
 │                   │ 创建 Build 记录      │                │               │
 │                   │ POST /job/build     │                │               │
 │                   │────────────────────>│                │               │
 │                   │<─────queueId────────│                │               │
 │  WS 订阅日志       │                     │                │               │
 │──────────────────>│ 订阅 Jenkins log    │                │               │
 │                   │────────────────────>│                │               │
 │                   │                     │ mvn package    │               │
 │                   │                     │ docker build   │               │
 │                   │                     │ docker push──────>              │
 │                   │                     │<─────digest────│               │
 │                   │<───回调(buildId,────│                │               │
 │                   │     image, digest)  │                │               │
 │                   │ 更新 Build=success  │                │               │
 │                   │ 创建 Image 记录      │                │               │
 │<────WS 通知────────│                     │                │               │
```

### 4.2 整组发布

```
前端                API Server              K8s API
 │  POST /releases  │                        │
 │─────────────────>│                        │
 │                  │ 校验权限（release）     │
 │                  │ 加载 Group 当前配置版本 │
 │                  │ 计算目标 PodTemplate    │
 │                  │ PATCH Deployment        │
 │                  │───────────────────────>│
 │                  │<─────revision──────────│
 │                  │ 创建 Release 记录       │
 │                  │ 启动发布观察器（异步）   │
 │<────202 releaseId─│                        │
 │                  │ 观察器轮询 Rollout状态  │
 │                  │───────────────────────>│
 │                  │ WS 推送进度             │
 │<────WS 进度────────│                        │
 │                  │ 完成/失败 → 更新 Release│
```

### 4.3 部分 Pod 灰度发布

- 方案 A（推荐）：创建独立 Canary Deployment（命名 `{group}-canary`），副本数 = 灰度 Pod 数，共享 Service 选择器但加 `canary: "true"` 标签隔离；通过修改 Service 选择器或独立 Canary Service 控制流量。
- 方案 B：修改主 Deployment 副本数 + 临时注入灰度镜像到部分 Pod（不推荐，K8s 不原生支持）。
- 平台采用方案 A，灰度完成后可「提升为整组」（替换主 Deployment 镜像并删除 Canary）或「放弃」（删除 Canary Deployment）。

## 5. 元数据与协调存储决策

> **结论：VortexOps 不单独引入 etcd / ZooKeeper。** 元数据主存储用 PostgreSQL，缓存与锁用 Redis，Leader 选举用 K8s 原生 Lease，服务发现用 K8s Service/DNS。下表给出各存储职责的最终分工。

### 5.1 为什么不用 etcd / zk

| 关注点 | etcd / zk 方案 | VortexOps 现有方案 | 结论 |
| --- | --- | --- | --- |
| 业务元数据存储 | etcd 强一致 KV，但单 value 有大小限制，不适合存大量结构化数据与多表关联 | PostgreSQL：关系模型、事务、外键、JSONB、全文检索、Citus 分片 | PostgreSQL 更合适，etcd/zk 不胜任 |
| 分布式锁 | zk 临时节点 / etcd lease | Redis Redlock（如分组发布并发锁、流水线触发去重锁） | Redis 已覆盖，更轻量 |
| 缓存（Pod 运行态、IP 反查、限流计数） | etcd 不适合做高频读缓存 | Redis Cluster + Informer 本地缓存 | Redis 更合适 |
| Leader 选举（syncer 分片主、流水线执行器主） | etcd/zk 选举 | K8s `coordination.k8s.io/v1` Lease + client-go leader election | 平台跑在 K8s 上，K8s 自身用 etcd 实现，已隐式提供，无需再引一套 |
| 服务发现 | zk 注册中心 | K8s Service + CoreDNS | 平台组件部署在 K8s，原生即可 |
| 异步事件流 | 不适用 | Kafka / NATS | 已有，非协调场景 |
| 配置中心 | 不适用（平台自有 configs/config_sets 表） | PostgreSQL + Redis 缓存 | 已有 |

**唯一例外**：若平台要部署在**裸机/非 K8s**环境，则 Leader 选举需要 etcd/zk 或 Redis。但本设计以「平台自身部署在 K8s」为前提，故不引入。

### 5.2 各存储职责一览

| 存储 | 角色 | 存什么 | 不存什么 |
| --- | --- | --- | --- |
| **PostgreSQL** | 事实源（desired state + 业务元数据） | 空间/应用/分组/构建/发布/配置/模型/中间件/审批/审计等所有业务实体；稳定 IP 分配（`vo_cluster_ip_allocations`）；流水线定义与运行记录 | Pod 实时状态、Pod IP→Pod 映射、实时副本数（这些是运行态） |
| **Redis** | 缓存 + 锁 + 限流 + Pub/Sub | Pod 运行态缓存（`rt:pod:`）、分组运行态摘要（`rt:group:`）、IP 反查索引（`rt:ip:`）、Service/HPA 摘要、会话、限流计数、WS 路由 Pub/Sub、分布式锁 | 不存任何「事实」——所有可重建，宕机后由 Informer 重新填充 |
| **K8s etcd（被管集群自带）** | 被管集群的运行态事实源 | Pod/Deployment/Service/Ingress 等 K8s 资源（actual state） | 平台业务元数据（这些在 PostgreSQL） |
| **K8s Lease（平台所在集群）** | Leader 选举协调 | syncer 分片主、pipeline 执行器主、edge proxy 主的租约 | 业务数据 |
| **对象存储（MinIO/S3）** | 大对象 | 构建日志归档、流水线产物、SBOM、模型权重缓存、配置快照 | 结构化元数据 |
| **Elasticsearch** | 检索 | 审计/日志全文检索（可选，小规模可退化到 PostgreSQL `pg_trgm`） | 事实源 |
| **Kafka / NATS** | 异步事件 | Pod 事件流、构建/发布事件、Token 计量、审计异步落库 | 协调状态 |

### 5.3 状态分层（desired vs actual）

```
┌─────────────────────────────────────────────────────────────┐
│ Desired State（期望态）—— PostgreSQL                         │
│  groups.replicas / applications.lifecycle / configs / ...    │
│  用户「想要」的样子，事务一致，可审计可回滚                    │
└─────────────────────────────────────────────────────────────┘
                          │ reconcile（syncer）
                          ▼
┌─────────────────────────────────────────────────────────────┐
│ Actual State（运行态）—— K8s etcd + Redis 缓存               │
│  Pod 实际副本数 / Pod IP / 就绪状态 / HPA 当前副本           │
│  Informer 监听 K8s → 写 Redis 缓存（不落库）                 │
│  稳定 IP 分配例外：写入 PostgreSQL cluster_ip_allocations    │
└─────────────────────────────────────────────────────────────┘
```

- **Pod 不入库**：Pod 是高频变更的运行态，全量入库会压垮 PostgreSQL。运行态走 Redis 缓存（详见 [K8s 集成 §11.7 运行态缓存与 IP 反查](kubernetes.md#117-运行态缓存与-ip-反查设计)、[扩展性 §6.6](scalability.md#66-运行态缓存与-ip-反查)）。
- **稳定 IP 是例外**：`keep_pod_ip=true` 的分组，其 IP 分配需要跨重启持久，故写入 `vo_cluster_ip_allocations`（PostgreSQL），Pod 重建时平台查表注入同 IP。
- **一致性**：期望态变更走 PostgreSQL 事务；运行态由 Informer 最终一致同步到 Redis；两者偏差由 reconciler 周期校正。

### 5.4 与扩展性的关系

- 小规模：单 PostgreSQL + 单 Redis 即可，无需 Citus / Redis Cluster。
- 中大规模：PostgreSQL 读写分离 + Citus 按 `workspace_id` 分片；Redis Cluster 分片缓存。
- 超大规模（10 万+应用）：Informer 按 Namespace 分片到多个 syncer 实例（Lease 选举分片主），Pod 缓存分片到 Redis Cluster；PostgreSQL 进一步分片。详见 [扩展性设计](scalability.md)。

> 详见部署方案：[部署文档](deployment.md)（多规模拓扑与依赖组件部署）。

## 6. 部署架构

```
┌──────────────── K8s 集群（平台自身）────────────────┐
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐      │
│  │ apiserver│  │controller│  │ frontend     │      │
│  │ (Deploy) │  │ (Deploy) │  │ (Deployment) │      │
│  └────┬─────┘  └────┬─────┘  └──────────────┘      │
│       │              │                              │
│  ┌────┴──────────────┴──────────────┐              │
│  │  PostgreSQL │ Redis │ OTLP Collector │           │
│  └───────────────────────────────────┘              │
└─────────────────────────────────────────────────────┘
         │                          │
         │ client-go (kubeconfig)   │ REST
         ▼                          ▼
   被管理的业务 K8s 集群         Jenkins / Harbor
```

- 平台自身以 Helm Chart 部署到「管理集群」。
- 通过 kubeconfig 接入多个「业务集群」（Multi-Cluster）。
- Jenkins 与 Harbor 为外部组件，平台仅做客户端集成。

## 7. 跨切面设计

### 7.1 多集群接入

- `Cluster` 实体：名称、API Server、kubeconfig（加密）、默认 Namespace 前缀。
- `Workspace` 绑定一个或多个 Cluster + Namespace。
- `KubernetesClientPool` 按 Cluster 缓存 `client-go` Clientset，支持 Leader 选举的 Informer。

### 7.2 凭证管理

- 所有敏感凭证（kubeconfig、Git Token、Registry Password）通过 KMS 加密后落库。
- 运行时解密注入到 Jenkins Pipeline 与 K8s Secret。

### 7.3 异步与可靠性

- 构建回调可能丢失：定时任务（Reconciler）拉取 Jenkins 状态对账。
- 发布观察器：基于 `Watch` + 兜底轮询，超时失败自动标记。
- 所有外部调用幂等：通过唯一 ID（buildId/releaseId）去重。

### 7.4 可扩展点

| 扩展点 | 接口 | 默认实现 |
| --- | --- | --- |
| GitProvider | `ListBranches / ListTags / GetCommit` | GitHub / GitLab / Gitea / Generic |
| BuildStrategy | `GeneratePipeline(build) string` | Java / Python / Go / Node / Custom |
| RegistryClient | `Push / List / Delete / Exist` | Harbor / Docker Registry |
| ReleaseStrategy | `Apply(group, image, opts)` | Rolling / Canary-by-Pod |
| ConfigInjector | `Inject(deploy, config)` | FileConfig / CommandArgs |
| AuthProvider | `Authenticate / UserInfo` | Local / OIDC / LDAP |
| NotificationChannel | `Send(notification, channel)` | InApp / Email / Webhook / DingTalk |
| ApprovalEngine | `Submit / Approve / Reject` | 内置多级审批 |
| MiddlewareInstaller | `Install / Upgrade / Rollback / Scale` | Helm / Operator / Manifest |
| BackupProvider | `Backup / Restore` | VolumeSnapshot / pg_dump / mysqldump / Velero |
| WorkloadAdapter | `Render(group) -> YAML / Observe` | Deployment / StatefulSet / CronJob / Job |
| AlertEngine | `Evaluate(rule) -> events` | Builtin（发布失败/重启） / Prometheus / K8s Event |
| ImageScanPolicy | `Gate(image) -> pass/block` | off / warn / block_critical / block_high |
| ConfigMerger | `Merge(sets[], groupConfig)` | overlay / prepend / append |
| InferenceFramework | `Render(svc,model) -> cmd / Probe / Metrics` | vLLM / TGI / Triton / SGLang / Ollama / custom |
| ModelRegistry | `List / Download(version) -> storage_key` | HuggingFace / OSS / S3 / Nexus / custom |
| PipelineExecutor | `Execute(run) / EvaluateGate(stage)` | Jenkins + K8s Job + 内置门禁 |
| ArtifactSigner | `Sign(image) / Verify(image)` | cosign / notation |
| PromotionStrategy | `Apply(promotion)` | auto / canary / manual |

## 8. 错误处理与一致性

- **错误模型**：统一 `AppError{Code, Message, HTTPStatus, Details}`，前端按 Code 决定提示与跳转。
- **事务边界**：应用服务层用数据库事务包裹领域变更；外部副作用（K8s/Jenkins）通过 Outbox 模式或补偿事务保证最终一致。
- **幂等键**：所有写接口接受 `Idempotency-Key` 头，避免重复提交。

## 9. 安全设计要点

- 输入校验：所有外部输入经 `validator` 校验，禁止裸拼接 SQL/Shell。
- Dockerfile 自定义：构建在隔离 Builder Pod 中执行，禁止访问平台网络。
- K8s RBAC：平台使用的 ServiceAccount 遵循最小权限，按 Namespace 限定。
- 日志脱敏：Git Token、镜像仓库密码等不得入日志。

---

## 10. 大规模扩展架构

> 完整扩展性设计见 [扩展性设计](scalability.md)。本节列架构层面的关键拓扑。

### 10.1 组件分片拓扑

面向 10 万+应用、单分组 1 万+副本规模，架构从单体演进为分片化：

```
                          ┌──────────────┐
                          │  LB / CDN    │
                          └──────┬───────┘
           ┌─────────────────────┼─────────────────────┐
           ▼                     ▼                     ▼
    ┌────────────┐       ┌────────────┐        ┌────────────┐
    │ frontend   │       │ apiserver  │ (多副本)│ ws-gateway │ (多副本)
    │ (静态)     │       │  ×N (HPA)  │        │  ×N        │
    └────────────┘       └──────┬─────┘        └──────┬─────┘
                                │                     │
           ┌────────────────────┼─────────────────────┘
           ▼                     ▼                     ▼
    ┌────────────┐       ┌────────────┐        ┌────────────┐
    │ PostgreSQL │       │  Redis     │        │ Kafka/NATS │
    │ Citus 集群 │       │  Cluster   │        │  集群      │
    │ (分片+读副本)│      │ (Pod缓存)  │        │            │
    └────────────┘       └────────────┘        └────────────┘
           │                     │                     │
           ▼                     ▼                     ▼
    ┌────────────┐       ┌────────────┐        ┌────────────┐
    │ ES 集群    │       │ syncer 集群│        │ log-proxy  │
    │ (搜索)     │       │(按ns分片)  │        │ (每业务集群)│
    └────────────┘       └──────┬─────┘        └──────┬─────┘
                                 │                     │
                          ┌──────┴─────────────────────┴──────┐
                          │      业务 K8s 集群群（1000+）       │
                          └───────────────────────────────────┘
```

### 10.2 新增组件

| 组件 | 职责 | 扩展方式 |
| --- | --- | --- |
| **syncer** | 按 `(cluster, namespace_shard)` 运行 Informer，缓存 Pod/Event，健康检查 | 多副本，按分片调度 |
| **ws-gateway** | 承接 WebSocket 连接，订阅路由到后端发布者 | 按连接数 HPA |
| **log-proxy** | 部署于业务集群，就近代理 Pod 日志流 | 每集群一组 |
| **audit-writer / activity-writer / notifier** | 异步消费者，落审计/动态/通知 | 独立消费者组 |

### 10.3 消息队列解耦

写接口主流程只写 outbox + 投递 MQ，异步落库：

| Topic | 用途 |
| --- | --- |
| `build.events` | Jenkins 回调 → 状态更新/通知 |
| `release.events` | 发布进度 → 通知/WS |
| `audit.events` | 异步落审计（不阻塞主流程） |
| `activity.events` | 动态流异步写 |
| `notification.events` | 多渠道通知投递 |
| `k8s.events` | Pod/Deployment 状态变更广播 |
| `cdc.dml` | Debezium 订阅 WAL → ES 索引同步 |

### 10.4 读写分离与缓存

- 写主库（分片主节点），列表/详情读走读副本。
- 实时态（进行中构建/发布）走主库或缓存，避免副本延迟。
- 热查询（权限、菜单、集群健康、分组概览）Redis 前置。
- Citus 自动路由查询到对应 shard。

### 10.5 演进路线

| 阶段 | 应用规模 | 架构 |
| --- | --- | --- |
| 小 | <1k | 单库 + 单 apiserver + 单 syncer |
| 中 | 1k-10k | 主从 + 读副本 + Redis + 单 ES |
| 大 | 10k-100k | Citus 分片 + syncer 多副本 + ES 集群 + Kafka |
| 超 | 100k+ | 多 Region + 跨 Region 同步 + 独立 ws-gateway/log-proxy 集群 |

> 关键决策（分片键 `workspace_id`、Pod 不落库、审计异步化）从 M1 落实，避免后期重构。
