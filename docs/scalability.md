# 扩展性设计

面向**超大 K8s 集群、超多应用**场景的容量与性能设计。本文档定义平台在以下规模下的可用性目标与实现策略，所有规模相关的横切设计统一在此。

---

## 1. 目标规模

| 维度 | 目标 | 极限 |
| --- | --- | --- |
| 接入业务 K8s 集群数 | 1,000+ | 10,000 |
| 平台管理应用数 | 100,000+ | 500,000 |
| 单应用分组数 | 数百 | 数千 |
| 单分组副本数（Pod） | 1,000 | 10,000+ |
| 单集群节点数 | 5,000 | 50,000 |
| 平台注册用户数 | 100,000+ | 1,000,000 |
| 日构建数 | 100,000+ | 1,000,000 |
| 日发布数 | 10,000+ | 100,000 |
| 在线 WebSocket 连接 | 50,000+ | 200,000 |

> 典型大客户画像：单个大应用（如电商交易核心）跨多集群、单分组副本 1 万+、单集群节点 5000+，要求发布进度实时、Pod 列表秒级返回、日志查询不卡顿。

## 2. 设计原则

1. **水平优先**：所有无状态组件可水平扩展；有状态组件分片。
2. **就近计算**：实时数据（Pod/日志/事件）从集群就近缓存或代理，不在平台 DB 聚合全量。
3. **写少读多分级**：热数据内存/缓存，温数据 DB，冷数据归档。
4. **异步解耦**：构建/发布/通知/审计走消息队列，API 不阻塞等待。
5. **分而治之**：按集群/空间分片（Shard），单分片故障不影响其他。
6. **降级预案**：核心链路（发布、构建）优先保障；观测类（日志/指标）可降级。

## 3. 容量估算

### 3.1 数据库容量（PostgreSQL）

| 表 | 行数估算（100k 应用） | 单行 | 总量 | 策略 |
| --- | --- | --- | --- | --- |
| vo_applications | 100k | 2KB | 0.2GB | 单库可承载 |
| vo_groups | 1M（10/应用） | 3KB | 3GB | 单库可承载 |
| vo_middleware_instances | 200k（2/空间，假设 100k 空间） | 3KB | 0.6GB | 单库可承载 |
| vo_middleware_releases | 2M/年 | 4KB | 8GB/年 | 按月分区 + 归档 |
| vo_middleware_backups | 10M/年（定时备份） | 2KB | 20GB/年 | 按月分区 + 归档 |
| vo_builds | 10M/年（日 30k） | 5KB | 50GB/年 | 按月分区 + 1 年归档 |
| vo_releases | 5M/年 | 4KB | 20GB/年 | 按月分区 + 归档 |
| vo_audit_logs | 100M/年 | 2KB | 200GB/年 | 按月分区 + 2 年热 |
| vo_release_events | 50M/年 | 1KB | 50GB/年 | 按月分区 + 归档 |
| vo_images | 5M（保留 50/应用） | 3KB | 15GB | 单库可承载 |
| vo_inference_services | 50k | 3KB | 0.15GB | 单库可承载 |
| vo_inference_releases | 500k/年 | 4KB | 2GB/年 | 按月分区 + 归档 |
| vo_inference_usage | 1B/年（高频调用） | 1KB | 1TB/年 | 按月分区 + 3 月热 |
| vo_pipeline_runs | 10M/年 | 4KB | 40GB/年 | 按月分区 + 归档 |
| vo_pipeline_stage_runs | 50M/年（5 阶段/run） | 2KB | 100GB/年 | 按月分区 + 归档 |
| vo_promotions | 1M/年 | 3KB | 3GB/年 | 按月分区 + 归档 |
| vo_pod_snapshots | **不落库** | — | — | 见 §6.1 |

> Pod **不**在平台 DB 持久化（数量可达千万级，变更频繁），仅在 Redis/集群本地缓存。详见 §6.1。

### 3.2 缓存容量（Redis）

| key 类型 | 数量 | TTL | 内存 |
| --- | --- | --- | --- |
| 用户权限集 | 100k | 5min | <1GB |
| Pod 缓存（按集群分片） | 千万级 | 5min | 分片集群，每分片 <8GB |
| 集群健康/资源摘要 | 10k 集群 | 60s | <1GB |
| 发布/构建进行中状态 | <10k | 实时 | <100MB |
| 限流计数器 | — | 1min | <500MB |

Pod 缓存是内存大头，采用 **Redis Cluster 分片** + **冷热分离**（只缓存活跃分组）。

### 3.3 对象存储

- 构建日志：单构建 ~10MB，日 30k 构建 = 300GB/天，归档到 S3/OSS，热日志保留 30 天。
- 配置大文件：稀疏，单文件 <10MB。
- 模型权重：单版本 10GB~300GB（大模型），按版本缓存；冷版本归档到低成本存储，热版本 PVC 共享挂载。10k 模型版本估算 PB 级，需生命周期管理（冷热分层 + 按需回源）。
- SBOM/签名：稀疏，单文件 <1MB。

## 4. 数据库分片策略

### 4.1 分片键

主分片键：**`workspace_id`**（空间）。绝大多数查询带空间上下文，按空间分片使空间内事务/查询落在单分片。

| 表 | 分片方式 | 说明 |
| --- | --- | --- |
| applications / groups | 按 `workspace_id` | 空间内聚合 |
| builds / releases / configs | 按 `workspace_id`（冗余字段） | 跨表 join 同分片 |
| vo_images | 按 `workspace_id`（冗余） | 同上 |
| audit_logs / vo_activity_feeds | 按 `workspace_id` + 按月二级分区 | 双维度 |
| vo_release_events | 按 `release_id` hash | 高写入表独立分片 |

### 4.2 全局表（不分片）

- `vo_users`、`vo_platform_role_bindings`、`vo_platform_roles`、`vo_permissions`、`vo_menus`：全局，读写集中但量小（<100 万行），单库 + 读副本。
- `vo_clusters`、`vo_registries`、`vo_jenkins_instances`、`vo_credentials`：全局基础设施元数据，量小。

### 4.3 分片实现

- 工具：**Citus**（PostgreSQL 原生分布式扩展）或 **应用层路由**（Vitess 风格，按 workspace_id 路由到对应 DB 实例）。
- 推荐 Citus：对应用透明，分布式 join/聚合原生支持；`workspace_id` 作为分布列。
- 中小规模（<10k 应用）单库 + 读副本即可；超大规模启用 Citus 分片。

### 4.4 跨分片查询

- 全局搜索（如「所有应用的构建」）：走 Elasticsearch（见 §7），不扫 DB。
- 平台管理报表：异步预聚合到 OLAP（如 ClickHouse），T+1 或准实时。

## 5. 表分区（时间维度）

高写入表按时间二级分区，配合分片：

```sql
-- builds: 按 workspace_id 分片 + 按月分区
CREATE TABLE builds (...) PARTITION BY RANGE (created_at);
CREATE TABLE builds_2026_06 PARTITION OF builds
  FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

-- audit_logs: 同上，保留 2 年热，超期归档到冷存储
-- release_events: 按 release_id hash 分片 + 按月分区
```

归档策略：
- 1 年内：主库热查询。
- 1-2 年：只读副本可查。
- 2 年+：导出到对象存储（Parquet），按需导入查询。

### 5.1 构建日志查询性能

构建/发布日志单次可达数十 MB~数百 MB，需专项处理：

- **归档分离**：实时日志走 WS（内存/边缘代理），完成后归档对象存储（S3/OSS），DB 只存 `log_storage_key` + offset + `error_line`，不存日志正文。
- **范围拉取**：历史日志用 HTTP `Range` 按字节拉取，前端虚拟滚动只渲染可视区，避免百万行全量加载。
- **搜索**：小日志按行号索引内存搜索；超长日志（>50MB）建 ES 索引（按 build_id 分片），搜索走 ES 返回命中行号再回源拉上下文。
- **冷热分层**：30 天内热日志存标准存储，超期转低频/归档存储，按需回源（延迟可接受）。
- **预计算**：构建完成时异步提取 `error_line`、失败阶段、耗时分布，列表页直接展示无需读日志。

## 6. K8s 数据治理（核心难点）

### 6.1 Pod 不落库

**原则**：Pod 是 K8s 实时态，数量巨大（10 万应用 × 平均 10 Pod = 100 万 Pod，单大应用可达 1 万+），变更频繁，**不在平台 DB 持久化**。

- 实时来源：`client-go` Informer 缓存（内存）。
- 平台缓存：**按集群分片的 Redis**，key `rt:pod:{clusterId}:{namespace}:{podName}`（见 §6.6），TTL 5min，Informer 增量更新；并维护 `rt:ip:*` 反查索引。
- 前端查询：`GET /groups/{uuid}/pods` → 走集群就近的 Pod 缓存，按标签过滤。
- 历史快照（可选）：发布时刻的 Pod 列表快照存 `release_events.detail`（仅记录发布相关 Pod 状态，非全量）。

### 6.2 Informer 分片与协调

单集群节点 5000+、Pod 15 万+ 时，单 Informer 内存压力大、List 全量慢。

**分片策略**：

- **按 Namespace 分片**：每个 Informer 实例只 Watch 一组 Namespace（`NamespaceShard`），如每 50 个 Namespace 一个 Informer worker。
- **平台侧 Shard 调度器**：将集群的 Namespace 划分到多个 `k8s-syncer` Pod（平台组件），每个 syncer 负责若干 Namespace 的 Informer。
- **Leader 选举粒度**：从「集群级」细化到「(cluster, namespace_shard) 级」，多 syncer 并行。
- **ListOptions 限制**：Initial List 用 `ResourceVersion=0` + 分页（`limit=500`），避免 API Server 大列表阻塞。

```
                VortexOps syncer 集群（水平扩展）
   ┌──────────┬──────────┬──────────┬──────────┐
   │ syncer-0 │ syncer-1 │ syncer-2 │ syncer-N │  ← 按 namespace 分片
   │ ns[0:50] │ ns[50:100]│ns[100:150]│  ...     │
   └────┬─────┴────┬─────┴────┬─────┴────┬─────┘
        │ Informer  │ Informer  │ Informer  │
        ▼          ▼          ▼          ▼
   ┌─────────────────────────────────────────┐
   │       业务 K8s 集群 API Server            │
   └─────────────────────────────────────────┘
```

### 6.3 大副本分组（1 万+ Pod）的发布观察

整组发布 1 万副本时，`Watch` Deployment 事件量大、Ready 计数频繁。

- **采样推送**：不逐 Pod 推送，按进度百分比（每 5% 或每 100 Pod 推一次）推送 `vo_release_events`。
- **就绪计数走 K8s status**：读 `Deployment.status.readyReplicas`，不自行聚合 Pod 列表。
- **超时拉长**：大副本分组发布超时按副本数自适应（如 `max(10min, replicas/1000 * 2min)`）。
- **并行度限制**：K8s 滚动 `maxSurge/maxUnavailable` 由平台按规模建议（如大副本用 `25%/10%` 而非小副本的 `1/0`）。

### 6.4 日志/事件流（就近代理）

大集群 Pod 日志流并发高，不能全回平台apiserver。

- **边缘日志代理**：每个业务集群部署 `vortexops-log-proxy`（DaemonSet 或 Deployment），平台转发日志请求到对应集群的 proxy，proxy 直接 `kubectl logs` 流式返回。
- **平台侧仅做路由**：`GET /groups/{uuid}/pods/{pod}/logs` → 解析 pod 所在集群 → 代理到该集群 log-proxy → 透传流。
- **跨 Pod 日志搜索**：小规模（<50 Pod）由 log-proxy 并行拉取聚合；大规模接入集群日志系统（Loki/ES），log-proxy 转发查询而非拉全文，平台不持久化日志。
- **限流**：单用户日志并发数、单集群总并发数限制，超额排队或拒绝。
- **事件流**：同理，`EventInformer` 在 syncer 侧运行，按 namespace 过滤后推平台，平台按 (workspace, group) 路由到前端。

### 6.5 Pod exec / 端口转发就近代理

- exec 与端口转发同样经集群边缘代理（log-proxy 复用或独立 `vortexops-tunnel`）：平台 WS → 边缘代理 → kubelet/exec API，避免长连接全汇聚平台 apiserver。
- exec 会话状态存 Redis（会话 ID、用户、Pod、起止时间），便于审计与并发控制。
- 限流：单用户 exec/端口转发并发上限；生产 Pod exec 默认需审批或 admin。
- 大集群（5000+ 节点）下，边缘代理按节点分片部署（DaemonSet），就近接入同节点 Pod 的 exec，降低跨节点流量。

### 6.6 运行态缓存与 IP 反查

应用/分组的「期望态」存 DB（`applications.lifecycle`、`groups.replicas`），「实际运行态」（Pod 就绪、某 IP 属于谁、分组运行状态）由 K8s 维护，平台不落 DB，靠 syncer Informer 订阅后缓存 Redis。详见 [K8s 集成 §11.7](kubernetes.md#117-运行态缓存与-ip-反查设计)。

**缓存规模与分片**：

| key 类型 | 数量（10 万应用） | 内存 | 策略 |
| --- | --- | --- | --- |
| Pod 摘要 `rt:pod:*` | 百万级 | 分片集群，每分片 <8GB | TTL 5min，按集群分片 |
| IP 反查 `rt:ip:*` | 百万级（1:1 Pod） | 与 Pod 同分片 | 同 Pod 生命周期 |
| 分组状态 `rt:group:*:status` | 百万级分组 | <2GB | TTL 30s |
| HPA 状态 `rt:hpa:*` | 十万级 | <500MB | TTL 30s |

**IP 反查索引性能**：

- 写：Informer 事件同步维护，O(1)。
- 读：`rt:ip:{clusterId}:{ip}` → O(1) 命中。
- miss 回源：List namespace Pod 过滤（慢路径），限流（每集群 1 并发）并提示「正在查询」，结果回填缓存。
- 大规模下 IP 索引随 Pod 同步生灭，不积累；集群下线时清理该集群前缀。

**降级**：集群不可达 → IP 反查返回「集群离线」；Redis 分片故障 → 回源 K8s（限流）+ 告警。

## 7. 搜索（Elasticsearch）

DB `LIKE` 查询在 10 万应用规模不可用。

- **索引范围**：applications、groups、images、builds（近期）、releases（近期）、workspaces、users。
- **同步**：CDC（Debezium 订阅 PostgreSQL WAL）→ Kafka → ES；准实时（秒级）。
- **查询**：全局搜索、构建/发布中心跨空间列表、标签筛选均走 ES。
- **分片**：ES 按时间（builds/releases）+ 按 workspace hash 双维度分片。

## 8. 消息队列（Kafka / NATS）

解耦异步链路，削峰填谷：

| Topic | 生产者 | 消费者 | 用途 |
| --- | --- | --- | --- |
| `build.events` | Jenkins 回调 | apiserver | 构建状态更新、通知 |
| `release.events` | release watcher | apiserver/notify | 发布进度、通知 |
| `audit.events` | 所有写接口 | audit-writer | 异步落审计（不阻塞主流程） |
| `activity.events` | 业务服务 | activity-writer | 动态流异步写 |
| `notification.events` | 业务服务 | notifier | 多渠道通知投递 |
| `k8s.events` | syncer | apiserver | Pod/Deployment 状态变更广播 |
| `cdc.dml` | Debezium | ES indexer | 搜索索引同步 |

- 写接口主流程只写 outbox 表 + 投递 MQ，保证最终一致。
- 审计/动态/通知的写入延迟（<1s）可接受，换来主接口低延迟。

## 9. 读写分离

- **写主库**：所有写走主库（分片主节点）。
- **读副本**：列表/详情读走读副本；实时态（进行中构建/发布）走主库或缓存避免副本延迟。
- **缓存前置**：热查询（用户权限、菜单、集群健康、分组概览）先 Redis，未命中再 DB。
- **Citus 场景**：自动路由查询到对应 shard。

## 10. API 分页与批量

- **游标分页**：所有列表强制游标分页（基于 `id` 或 `(created_at, id)`），禁止无 limit 全量。
- **默认页大小**：20，最大 100；构建/发布历史等大表最大 50。
- **导出**：大数据导出走异步任务（生成 CSV 到对象存储，通知下载链接），不阻塞 API。
- **批量接口**：成员批量加、镜像批量删等提供 batch 端点，单次 ≤100。

## 11. 实时流（WebSocket）扩展

5 万+ WS 连接单 apiserver 不可承载。

- **WS 网关层**：独立 `ws-gateway` 集群，水平扩展，前端连最近网关。
- **订阅路由**：网关将订阅按 `topic`（如 `release:{uuid}`）映射到后端发布者（apiserver/syncer），通过 Redis Pub/Sub 或 Kafka 转发。
- **背压**：日志流高频，网关侧合并/采样（如 100ms 窗口批量发送），避免压垮前端。
- **断线重连 + 续传**：日志带 offset，重连续传不丢。

```
前端 ──WS──> ws-gateway（多副本，LB）──> Redis Pub/Sub / Kafka ──> apiserver / syncer / log-proxy
```

## 12. 限流与配额

### 12.1 接口限流

| 维度 | 限制 |
| --- | --- |
| 全局 | 每 IP 1000 req/s（按规模上调） |
| 单用户 | 100 req/s |
| 构建 API | 每用户 5 并发，每空间可配（默认 10，大空间 50） |
| 发布 API | 每分组 1 并发发布 |
| 日志 WS | 每用户 20 连接，每集群总并发 500 |
| 搜索 | 每用户 10 req/s |
| 对外 API | 按 Token `rate_limit_per_min`（默认 60/min），全局外部入口 5000 req/s |
| 对外 API 写操作 | 每 Token 部署/扩缩容并发受发布/分组并发约束复用 |

### 12.2 对外 API 治理

- **独立网关**：对外入口独立部署（可水平扩容），与内部 API 物理或逻辑隔离，外部突发流量不影响 UI。
- **限流**：Token 级令牌桶 + 全局令牌桶双层；超限 429 + `Retry-After`。
- **审计异步落库**：`vo_external_api_call_logs` 高频写入，走 Kafka 异步批量落分区表，不阻塞请求。
- **配额计费**：按 Token/操作统计调用量，可作为多租户计费依据。
- **熔断**：某 Token 异常高频或大量失败，自动熔断该 Token 并告警。

### 12.3 资源配额

`vo_workspace_quotas` 按空间配置；超大客户单独提高配额。平台级熔断：当某集群 API Server 延迟 P99 > 2s，自动降级该集群的 Pod 列表/日志为缓存态。

**GPU 配额**（大模型场景关键）：

| 维度 | 说明 |
| --- | --- |
| 空间 GPU 卡数上限 | 防止单空间占用过多昂贵 GPU |
| 推理服务副本/GPU 卡数 | 单服务上限 |
| 模型权重存储 | 单空间权重缓存上限 |
| 推理 API 限流 | 按 API Key 每分钟请求数 + 每日 Token 配额 |
| 推理并发 | 每推理服务部署/切模型并发上限（权重加载耗资源） |

### 12.4 流水线并发

| 维度 | 限制 |
| --- | --- |
| 流水线触发 | 每空间并发 run 上限（默认 20），超额排队 |
| 阶段任务 | 下发 Jenkins/K8s Job 受其并发上限约束 |
| 晋升 | 同应用同环境 1 个进行中晋升 |
| 部署阶段 | 复用「每分组 1 并发发布」约束 |

## 13. 部署拓扑（大规模）

```
                          ┌──────────────┐
                          │  LB / CDN    │
                          └──────┬───────┘
           ┌─────────────────────┼─────────────────────┐
           ▼                     ▼                     ▼
    ┌────────────┐       ┌────────────┐        ┌────────────┐
    │ frontend   │       │ apiserver  │ (多副本)│ ws-gateway │ (多副本)
    │ (静态)     │       │  ×N        │        │  ×N        │
    └────────────┘       └──────┬─────┘        └──────┬─────┘
                                │                     │
           ┌────────────────────┼─────────────────────┘
           ▼                     ▼                     ▼
    ┌────────────┐       ┌────────────┐        ┌────────────┐
    │ PostgreSQL │       │  Redis     │        │ Kafka/NATS │
    │ Citus 集群 │       │  Cluster   │        │  集群      │
    │ (主+读副本)│       │ (Pod缓存)  │        │            │
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

- **apiserver**：无状态，按 CPU 水平扩展（HPA）。
- **syncer**：按 `(cluster, namespace_shard)` 调度，独立扩缩容。
- **ws-gateway**：按连接数扩缩容。
- **Citus**：coordinator + 多 worker 节点。
- **ES / Kafka / Redis Cluster**：各自集群化。

## 14. 关键场景验证

### 14.1 单大应用 1 万副本发布

- 发布请求 → apiserver 校验 → PATCH Deployment（K8s 自己滚动）→ release watcher 读 `status.readyReplicas`，每 5% 推一次进度 → 1 万副本约 10-30min 完成，前端进度条平滑。
- 并发：单分组仅 1 发布；日志/事件走就近代理不压平台。

### 14.2 跨空间构建中心列表（10 万应用）

- 列表查询走 ES（按 status/time/workspace 过滤），分页 20 条，P99 <200ms。
- DB 不参与该查询。

### 14.3 全平台 Pod 总览

- **不提供**「全平台所有 Pod」列表（无业务意义且不可承载）。
- 提供「分组内 Pod」「集群内 Pod（按 namespace）」「发布中 Pod」等有界查询，走缓存/Informer。

### 14.4 5 万 WS 在线

- ws-gateway 10 副本，每副本 5000 连接；订阅路由经 Redis Pub/Sub；日志流采样。单副本内存 <4GB。

### 14.5 大型中间件集群部署（100+ broker Kafka）

- 用 Operator（Strimzi），平台仅提交 `Kafka` CR，Operator 负责分片与滚动。
- 观察器读 CR `.status.conditions`，不自行编排 Pod。
- 100 broker 升级超时默认 2 小时，滚动观察期间进度按 broker 就绪数推送。
- 备份走卷快照（CSI），按 broker PV 并行快照。
- 平台不聚合该 Operator 管理的全量 Pod，仅缓存就绪计数。

### 14.6 中间件故障影响面分析

- 某 MySQL 实例故障 → 通过 `vo_middleware_connections` 反查所有连接的应用/分组 → 工作台/通知推送受影响团队。
- 拓扑图可视化应用↔中间件依赖，便于故障定位。

### 14.7 大模型推理服务大规模

- **多副本高并发**：单推理服务 10+ 副本（如 QPS 高的在线服务），HPA 按 QPS 扩容，权重 PVC 共享只读避免重复下载，扩容后秒级就绪（权重已在 PV）。
- **切模型蓝绿**：72B 权重加载 5-10min，蓝绿双 Deployment 并存，绿组就绪后切流量，零停机；权重加载期间平台不把它算入就绪副本，HPA 不误判。
- **Token 计量高写入**：万级 QPS 推理 → inference_usage 高频写入，走 Kafka 异步批量落库（网关层先记录到 MQ，消费者批量写分区表），避免压 DB。
- **GPU 调度**：大模型需多卡 TP，nodeSelector 选多卡节点；集群 GPU 资源紧张时排队（PriorityClass），空间 GPU 配额管控。
- **权重存储**：PB 级权重走对象存储冷热分层，热版本 PVC 缓存，冷版本按需回源下载。

### 14.8 流水线高并发

- 10 万应用 × 平均流水线 → 日均数万 run，PipelineEngine 水平扩容多副本，run 状态机驱动，任务下发 Jenkins/K8s Job。
- 高并发构建阶段受 Jenkins agent 池上限约束，超额排队，平台显示排队位次。
- 部署阶段复用分组发布并发约束（每分组 1 并发），避免冲突。

## 15. 降级与容灾

| 故障 | 降级 |
| --- | --- |
| 某业务集群不可达 | 该集群 Pod/日志返回「离线」；构建/发布到该集群拒绝；其他集群正常 |
| Redis Pod 缓存失效 | Pod 列表回源 Informer（若 syncer 在线）；否则返回「数据可能过期」 |
| ES 故障 | 搜索降级到 DB `LIKE`（限小结果集）或关闭搜索 |
| Kafka 故障 | 审计/通知降级同步写主库（性能下降但不丢） |
| Citus worker 故障 | 对应 shard 不可写，其他 shard 正常；读副本可读 |
| ws-gateway 单点故障 | LB 摘除，前端重连其他副本 |
| 备份存储（S3）故障 | 备份任务失败重试；已成功备份仍可查；恢复任务暂停 |
| Operator 故障 | 中间件实例本身不受影响（已部署的资源自洽）；变更操作暂停并提示运维 |

## 16. 监控指标

平台自身 Prometheus 指标（关键）：

- `vortexops_api_request_duration_seconds{route,method}` P99
- `vortexops_ws_connections` 当前连接数
- `vortexops_informer_lag_seconds{cluster,shard}` Informer 同步延迟
- `vortexops_release_progress_percent` 发布进度
- `vortexops_k8s_api_duration_seconds{cluster,verb}` K8s API 延迟
- `vortexops_db_query_duration_seconds{table}` DB 查询延迟
- `vortexops_mq_lag{topic}` 消息队列消费滞后
- `vortexops_es_query_duration_seconds` ES 查询延迟
- `vortexops_pipeline_run_duration_seconds{stage}` 流水线阶段耗时
- `vortexops_pipeline_runs_active` 活跃流水线数
- `vortexops_inference_tokens_total{service,api_key}` Token 吞吐
- `vortexops_inference_latency_seconds{service,quantile}` 推理延迟
- `vortexops_gpu_memory_utilization{service,gpu}` 显存利用率
- `vortexops_model_weight_download_seconds` 权重下载耗时

SLO：API P99 <300ms；发布进度推送延迟 <5s；Pod 列表 P99 <500ms；审计落库滞后 <1s；推理 API P99 按模型大小分级（小模型 <500ms，大模型 TTFT <2s）；Token 计量落库滞后 <30s。

## 17. 规模演进路线

| 阶段 | 应用规模 | 架构形态 |
| --- | --- | --- |
| 小 | <1k | 单库 + 单 apiserver + 单 syncer |
| 中 | 1k-10k | 主从 + 读副本 + Redis + 单 ES |
| 大 | 10k-100k | Citus 分片 + syncer 多副本 + ES 集群 + Kafka |
| 超 | 100k+ | 多 Region 部署 + 跨 Region 同步 + 独立 ws-gateway/log-proxy 集群 |

> 平台从「小」起步，架构按阶段渐进演进，避免过早优化；但**数据模型分片键**与**Pod 不落库**等关键决策从 M1 起落实，避免后期重构。
