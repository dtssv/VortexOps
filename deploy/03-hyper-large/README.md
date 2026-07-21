# 03 — 超大规模集群部署

面向**平台型、超大客户、跨区域**的部署方案。覆盖 10 万+ 应用、500-10,000 个被管业务集群、单分组 1 万+ 副本、5 万+ 在线 WebSocket 的极限场景。

> 开发环境见 [`../01-dev-docker/`](../01-dev-docker/README.md)。
> 中型集群（物理机 / K8s）见 [`../02-mid-bare-k8s/`](../02-mid-bare-k8s/README.md)。
> 扩展性设计原理见 [`../../docs/scalability.md`](../../docs/scalability.md)。

---

## 1. 适用场景与规模

| 维度 | 量化 | 极限 |
| --- | --- | --- |
| 平台管理应用数 | 100,000+ | 500,000 |
| 接入业务 K8s 集群数 | 1,000+ | 10,000 |
| 单集群节点数 | 5,000 | 50,000 |
| 单分组副本数 | 1,000 | 10,000+ |
| 注册用户数 | 100,000+ | 1,000,000 |
| 日构建数 | 100,000+ | 1,000,000 |
| 在线 WebSocket 连接 | 50,000+ | 200,000 |
| 部署形态 | 多 Region，每 Region 一套完整管理集群 | — |

**与 02 层的边界**：从 02 升级到 03 时，**业务代码无需修改**——数据模型分片键（`workspace_id` / `cluster_id` / `application_id`）从 M1 起已落实。只需扩容依赖组件（Citus / Redis Cluster / Kafka / ES）+ 调整 Helm values。

---

## 2. 拓扑总览

```
                                ┌────────────── LB / CDN ──────────────┐
                                │  apiserver ×N (HPA)                   │
                                │  ws-gateway ×N (HPA)                  │
                                │  syncer ×N (按 namespace 分片)         │
                                │  ext-api-gw ×N                       │
                                │  pipeline-worker ×N                  │
                                └─────────────────┬───────────────────┘
                                                  │
        ┌─────────────────────────────────────────┼──────────────────────────────────┐
        ▼                                         ▼                                  ▼
  Citus 集群                              Redis Cluster                       Kafka 9+ broker
  (2 coordinator HA + 8+ worker)          (12+ 节点)                          (KRaft, RF=3)
  + 4+ 读副本                             Pod 缓存按 cluster_id 分片            Topic 按 workspace 分区
        │                                         │                                  │
        └────────── ES 6+ 数据节点 + 3 协调节点 ────┴──── S3 多区域复制 ──────────────┘
                                                  │
                                       ┌──────────┴───────────┐
                                       ▼                      ▼
                            syncer 集群（20+ 实例）        log-proxy（每被管集群 2+）
                            按 namespace 分片               边缘日志/exec 代理
                                       │
                                       ▼
                          ┌────────────────────────────┐
                          │  1000+ 被管业务 K8s 集群    │
                          └────────────────────────────┘

跨 Region 灾备：
  Region A (主)                            Region B (备)
  ├─ 管理集群（全量）                       ├─ 管理集群（standby）
  ├─ PG 主 + Citus worker                  ├─ PG 从（流复制 / 读副本）
  ├─ Redis Cluster                         ├─ Redis 从（异步复制）
  ├─ S3 Bucket A                           ├─ S3 Bucket B（CRR 跨区域复制）
  └─ 被管集群群 A                           └─ 被管集群群 B（可选双活）
  DNS 切换: vortexops.internal → Region B（故障时）
```

---

## 3. 资源规格与硬件清单

### 3.1 单 Region 管理集群（K8s）

| 节点角色 | 节点数 | 单节点规格 | 说明 |
| --- | --- | --- | --- |
| control-plane | 3 | 16C / 32G / 200G SSD | etcd 高可用 |
| worker（平台无状态层） | 10-20 | 32C / 64G / 500G SSD | apiserver / syncer / ws-gateway / pipeline-worker |
| worker（依赖有状态层） | 8-15 | 32C / 128G / 多盘 NVMe | Citus worker / Redis / Kafka / ES |
| GPU 节点（如平台托管推理） | 按需 | 8× A100 80G | 推理服务 |
| **合计** | **25-40+** | — | 单 Region |

### 3.2 关键组件规模

| 组件 | 规模 | 说明 |
| --- | --- | --- |
| apiserver | 8+ 副本，HPA CPU 70% | 无状态水平扩展 |
| syncer | 20+ 实例，每实例管 ≤50 集群 | Informer 按 Namespace 分片，Lease 选举分片主 |
| ws-gateway | 10+ 副本，每实例 ≤5000 连接 | Redis Pub/Sub 路由，LB 粘性会话 |
| Citus coordinator | 2 核 4Gi ×2（HA） | 协调节点，Patroni 管理 |
| Citus worker | 8 核 32Gi ×8+ | 按 workspace_id 分片 |
| Redis Cluster | 12+ 节点（6 主 6 从） | Pod 缓存分片，每分片 <8GB |
| Kafka | 9+ broker × 8C16G | 审计 / Token 计量 / 构建事件，RF=3 |
| OpenSearch | 6+ 数据节点 + 3 协调节点 | ILM 冷热分层 |
| log-proxy | 每被管集群 2+ 副本 | 按 namespace 分片 |
| 对象存储 | 多区域复制 | 模型权重 PB 级冷热分层 |
| Jenkins | 多 Jenkins 实例 + 大规模 agent 池 | 排队 + 优先级 |

### 3.3 容量规划

| 规模 | PG 磁盘 | Redis 内存 | 对象存储 | ES 磁盘 |
| --- | --- | --- | --- | --- |
| XL | 10 Ti+（Citus 多 worker） | 200 Gi+ | PB 级 | 2 Ti+ |

详见 [`../../docs/scalability.md`](../../docs/scalability.md) §3 容量估算。

---

## 4. 目录结构

```
deploy/03-hyper-large/
├── README.md                              # 本文档
├── manifests/
│   ├── citus-cluster.yaml                 # Citus 8+ worker 集群
│   ├── redis-cluster.yaml                 # Redis Cluster 12 节点
│   ├── kafka-cluster.yaml                 # Kafka 9 broker KRaft
│   ├── opensearch-cluster.yaml            # OpenSearch 6+ 数据 + 3 协调
│   ├── helm-values/
│   │   ├── values-xl-region-a.yaml        # 主 Region 完整 values
│   │   └── values-xl-region-b-standby.yaml # 备 Region standby values
│   ├── syncer-shard/
│   │   ├── syncer-shard-configmap.yaml    # Namespace 分片配置
│   │   └── syncer-lease.yaml              # K8s Lease 分片主选举
│   ├── ws-gateway/
│   │   └── ws-session-affinity.yaml       # LB 粘性会话 + Redis Pub/Sub
│   ├── log-proxy/
│   │   └── log-proxy-daemonset.yaml       # 边缘日志代理 DaemonSet
│   ├── gpu/
│   │   ├── nvidia-device-plugin.yaml      # NVIDIA GPU 设备插件
│   │   ├── gpu-node-pool.yaml             # GPU 节点池标签 + 污点
│   │   └── shared-weights-pvc.yaml        # 模型权重 RWX 共享 PVC
│   ├── monitoring/
│   │   ├── prometheus-federation.yaml     # Prometheus 联邦
│   │   ├── thanos.yaml                    # Thanos 长期存储
│   │   └── grafana-datasources.yaml       # 多 Region 数据源
│   └── dr/
│       ├── pg-citus-patroni.yaml          # Patroni HA + 跨 Region 复制
│       ├── redis-cross-region-replica.yaml # Redis 跨 Region 异步复制
│       └── dns-failover.yaml              # DNS 故障切换
└── scripts/
    ├── bootstrap-region.sh                # 初始化新 Region
    ├── enable-citus-sharding.sh           # 启用 Citus 分片
    ├── add-redis-shard.sh                 # Redis Cluster 扩容
    ├── add-kafka-broker.sh                # Kafka 扩容 broker
    ├── register-managed-cluster-batch.sh  # 批量接入业务集群
    ├── deploy-log-proxy.sh                # 批量部署 log-proxy
    ├── failover-to-region-b.sh            # 故障切换到备 Region
    ├── sync-cross-region.sh               # 触发跨 Region 数据同步
    └── capacity-check.sh                  # 容量巡检
```

---

## 5. 前置条件

### 5.1 多 Region 基础设施

- **Region 数量**：≥ 2（主备）或 ≥ 3（多活）
- **跨 Region 网络**：专线或 VPN，延迟 < 50ms，带宽 ≥ 10 Gbps
- **跨 Region DNS**：基于 GeoDNS / Route 53 故障切换
- **跨 Region 对象存储**：S3 CRR / OSS 跨区域复制

### 5.2 单 Region 集群要求

- K8s 集群 ≥ 1.27（多节点池支持）
- 集群规模 ≥ 25 节点
- 多 StorageClass（NVMe for DB / SSD for cache / HDD for cold）
- CNI 支持 NetworkPolicy 与大数据包（Calico 或 Cilium）
- 集群内 Ingress + cert-manager + External Secrets Operator

### 5.3 外部依赖

- 私有 Harbor 多区域同步
- Jenkins 多实例 + agent 池（多集群）
- 企业 KMS / Vault（密钥信封加密）
- 监控：Prometheus 联邦 + Thanos 长期存储

---

## 6. 部署流程

### 6.1 总体流程

```
1. 准备多 Region 基础设施（K8s 集群 + 网络 + 对象存储）
2. 在主 Region 部署依赖组件（Citus / Redis Cluster / Kafka / ES）
3. 在备 Region 部署 standby 依赖组件（PG 从 / Redis 从 / S3 CRR）
4. 构建并推送 VortexOps 镜像到多 Region 镜像仓库
5. Helm 部署 VortexOps 平台组件到主 Region
6. 启用 Citus 分片（应用层分片键已在 M1 落实）
7. 配置 syncer 分片（按 namespace shard）
8. 批量接入业务集群 + 部署 log-proxy
9. 配置跨 Region 复制与 DNS 故障切换
10. 容量巡检 + 监控告警接入
```

### 6.2 步骤 1：初始化新 Region

```bash
./scripts/bootstrap-region.sh \
  --region us-east-1 \
  --kubeconfig /path/to/region-a.kubeconfig \
  --mode primary
```

脚本完成：
- 创建 namespace（`vortexops` / `vortexops-infra` / `vortexops-builds`）
- 部署 cert-manager + External Secrets Operator
- 部署 nginx-ingress
- 配置 StorageClass（`nvme-retain` / `ssd-retain` / `ssd` / `hdd`）

### 6.3 步骤 2：部署依赖组件

```bash
# Citus 集群（替代 02 层的 PG 主从）
kubectl apply -f manifests/citus-cluster.yaml -n vortexops-infra

# Redis Cluster（替代 02 层的 Sentinel）
kubectl apply -f manifests/redis-cluster.yaml -n vortexops-infra

# Kafka 9 broker KRaft
kubectl apply -f manifests/kafka-cluster.yaml -n vortexops-infra

# OpenSearch 6+ 数据节点
kubectl apply -f manifests/opensearch-cluster.yaml -n vortexops-infra
```

### 6.4 步骤 3：启用 Citus 分片

```bash
./scripts/enable-citus-sharding.sh \
  --coordinator citus-coordinator.vortexops-infra.svc \
  --worker-count 8
```

执行 `create_distributed_table`，按 `workspace_id` 分片业务表。详见 [`../../schema.sql`](../../schema.sql) §14.3。

### 6.5 步骤 4：构建并推送镜像

```bash
../02-mid-bare-k8s/scripts/build-images.sh \
  --target k8s \
  --registry registry.us-east-1.vortexops.io \
  --version 1.0.0 \
  --push --multi-arch

# 同步镜像到备 Region
./scripts/sync-cross-region.sh --to-region us-west-2
```

### 6.6 步骤 5：部署 VortexOps 平台

```bash
../02-mid-bare-k8s/scripts/deploy-k8s.sh \
  --namespace vortexops \
  --tag 1.0.0 \
  --values manifests/helm-values/values-xl-region-a.yaml
```

### 6.7 步骤 6：配置 syncer 分片

```bash
kubectl apply -f manifests/syncer-shard/syncer-shard-configmap.yaml -n vortexops
kubectl apply -f manifests/syncer-shard/syncer-lease.yaml -n vortexops
kubectl -n vortexops rollout restart deploy/vortexops-syncer
```

### 6.8 步骤 7：批量接入业务集群

```bash
./scripts/register-managed-cluster-batch.sh \
  --clusters-file clusters.csv \
  --kubeconfig-dir /path/to/kubeconfigs/
```

`clusters.csv` 格式：`cluster_id,api_server,kubeconfig_path,network_profile`

### 6.9 步骤 8：批量部署 log-proxy

```bash
./scripts/deploy-log-proxy.sh \
  --clusters-file clusters.csv \
  --log-proxy-image registry.vortexops.io/vortexops/log-proxy:1.0.0
```

### 6.10 步骤 9：跨 Region 灾备配置

```bash
# 在主 Region 配置 PG 流复制到备 Region
kubectl apply -f manifests/dr/pg-citus-patroni.yaml -n vortexops-infra

# Redis 跨 Region 异步复制
kubectl apply -f manifests/dr/redis-cross-region-replica.yaml -n vortexops-infra

# S3 CRR（在云厂商控制台配置，或用 mc mirror 定时同步）
# DNS 故障切换
kubectl apply -f manifests/dr/dns-failover.yaml -n vortexops
```

### 6.11 步骤 10：容量巡检

```bash
./scripts/capacity-check.sh --region us-east-1
```

输出：PG 连接数 / Redis 内存 / Kafka lag / ES 索引大小 / Pod 调度延迟。

---

## 7. 关键组件部署细节

### 7.1 Citus 分片集群

详见 [`manifests/citus-cluster.yaml`](manifests/citus-cluster.yaml)。

**分片键**：`workspace_id`（绝大多数查询带空间上下文，按空间分片使空间内事务/查询落在单分片）

| 表 | 分片方式 | 说明 |
| --- | --- | --- |
| applications / groups | 按 `workspace_id` | 空间内聚合 |
| builds / releases / configs | 按 `workspace_id`（冗余字段） | 跨表 join 同分片 |
| images | 按 `workspace_id`（冗余） | 同上 |
| audit_logs / activity_feeds | 按 `workspace_id` + 按月二级分区 | 双维度 |
| release_events | 按 `release_id` hash | 高写入表独立分片 |

**全局表（不分片）**：`vo_users`、`vo_platform_role_bindings`、`vo_clusters`、`vo_registries` 等小表，单库 + 读副本。

**跨分片查询**：
- 全局搜索走 Elasticsearch（不扫 DB）
- 平台管理报表异步预聚合到 OLAP（ClickHouse），T+1 或准实时

### 7.2 Redis Cluster

详见 [`manifests/redis-cluster.yaml`](manifests/redis-cluster.yaml)。

- 12+ 节点（6 主 6 从），按 `cluster_id` hash 分片 Pod 缓存
- 每分片内存 < 8GB
- Key 前缀：
  - `rt:pod:{clusterId}:{namespace}:{podName}` — Pod 缓存，TTL 5min
  - `rt:ip:{clusterId}:{podIP}` — IP 反查索引
  - `rt:group:{clusterId}:...` — 分组运行态
  - `lock:` — 分布式锁
  - `ratelimit:` — 限流计数

### 7.3 Kafka 集群

详见 [`manifests/kafka-cluster.yaml`](manifests/kafka-cluster.yaml)。

| Topic | 用途 | 分区 |
| --- | --- | --- |
| `build.events` | 构建状态变更 | 24 |
| `release.events` | 发布进度 | 24 |
| `audit.async` | 审计异步落库 | 48 |
| `ext-api.audit` | 对外 API 审计 | 24 |
| `inference.usage` | Token 计量 | 48 |
| `notification.dispatch` | 通知分发 | 12 |
| `cdc.dml` | Debezium → ES 索引同步 | 24 |

### 7.4 syncer 分片

详见 [`manifests/syncer-shard/`](manifests/syncer-shard/)。

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

- 每个 syncer 实例只 Watch 一组 Namespace（`NamespaceShard`，每 50 个 Namespace 一个 worker）
- Leader 选举粒度：从「集群级」细化到「(cluster, namespace_shard) 级」
- Initial List 用 `ResourceVersion=0` + 分页（`limit=500`），避免 API Server 大列表阻塞

### 7.5 ws-gateway 扩展

详见 [`manifests/ws-gateway/`](manifests/ws-gateway/)。

- 10+ 副本，每副本 ≤5000 连接（单副本内存 <4GB）
- LB 粘性会话（cookie 或 IP hash）
- 订阅路由经 Redis Pub/Sub 转发到后端发布者
- 日志流背压：100ms 窗口批量发送，避免压垮前端
- 断线重连 + 续传：日志带 offset

### 7.6 log-proxy 边缘代理

详见 [`manifests/log-proxy/`](manifests/log-proxy/)。

- 每个被管业务集群部署 2+ 副本（Deployment 或 DaemonSet）
- 平台转发日志请求到对应集群 proxy，proxy 直接 `kubectl logs` 流式返回
- 平台仅做路由 + 透传，不缓存全量日志
- 限流：单用户日志并发 20、单集群总并发 500，超额排队/拒绝

### 7.7 大模型推理工作负载

详见 [`manifests/gpu/`](manifests/gpu/) 与 [`../../docs/model-serving.md`](../../docs/model-serving.md)。

- GPU 节点池：`nodeSelector: accelerator: nvidia-a100` + 容忍污点
- 模型权重 PVC 共享只读（RWX），避免多副本重复下载
- 多卡张量并行：TP 同节点（多卡）+ PP 跨节点（需 RDMA）
- 蓝绿切模型：双 Deployment 并存，绿组就绪后切流量，零停机
- HPA 按 QPS / 队列 / 延迟自定义指标（需 Prometheus Adapter）

---

## 8. 网络设计

### 8.1 流量路径

| 流量 | 路径 | 说明 |
| --- | --- | --- |
| 用户 UI / API | GeoDNS → 最近 Region LB → Ingress → frontend/apiserver | TLS 1.2+ |
| WebSocket | LB（粘性）→ ws-gateway → Redis Pub/Sub → apiserver/syncer | WSS 长连接 |
| 平台 → 被管集群 | syncer → K8s API Server（专线/VPN） | mTLS 推荐 |
| 日志/exec | ws-gateway → log-proxy（业务集群内）→ kubelet | 边缘代理，不汇聚平台 |
| 跨 Region | PG 流复制 / Redis 异步复制 / S3 CRR | RPO < 1min |

### 8.2 NetworkPolicy

在 02 层 NetworkPolicy 基础上：
- syncer 出站白名单按被管集群 CIDR 细化（禁止 0.0.0.0/0）
- ws-gateway 仅允许 Ingress 入站 + Redis Pub/Sub 出站
- Citus worker 仅允许 coordinator 入站
- 跨 Region 流量走专线，独立网络策略

### 8.3 DNS 与流量调度

- 对外：`vortexops.internal` → GeoDNS 多 Region A 记录
- 故障切换：健康检查失败 → 自动摘除主 Region 记录
- 内部：`*.vortexops-infra.svc.cluster.local`

---

## 9. 高可用与灾备

### 9.1 组件 HA 矩阵

| 组件 | HA 方案 | RPO | RTO |
| --- | --- | --- | --- |
| apiserver / ws-gateway / syncer | 多副本 + HPA + PDB + 跨 AZ | 0 | <1min |
| Citus | coordinator HA（Patroni） + worker 多副本 | <1min（同步复制） | <5min |
| Redis Cluster | 6 主 6 从，自动 failover | 秒级（AOF） | <2min |
| Kafka | 9+ broker + RF=3 + KRaft | 0 | <5min |
| OpenSearch | 6+ 数据节点 + 3 协调节点 | 秒级 | <5min |
| 对象存储（S3） | 多 AZ + 跨区域复制 | 0 | <15min |

### 9.2 跨 Region 灾备

```
Region A（主）                              Region B（备）
├─ 管理集群（平台全量）                      ├─ 管理集群（平台 standby）
├─ PG 主 + Citus worker                     ├─ PG 从（流复制 / Citus 读副本）
├─ Redis Cluster                            ├─ Redis 从（异步复制）
├─ S3 Bucket A                              ├─ S3 Bucket B（CRR 复制）
└─ 被管集群群 A                              └─ 被管集群群 B（可选双活）

DNS 切换：vortexops.internal → Region B（故障时）
```

- **RPO 目标**：PG 同步复制 RPO≈0；Redis/ES 异步 RPO<1min
- **RTO 目标**：DNS 切换 + PG promote <15min
- syncer 在备 Region 预热但不连被管集群，故障时手动/自动 promote PG 并启动 syncer

### 9.3 备份策略

| 数据 | 方式 | 频率 | 保留 |
| --- | --- | --- | --- |
| Citus | coordinator 备份 + worker 并行 pg_dump | 日全量 | 30 天 |
| Redis | RDB 快照（限流/锁 key） | 每小时 | 7 天 |
| 对象存储 | 版本化 + 跨区域复制 | 持续 | 按生命周期 |
| Chart values / Secret | Git + Sealed Secrets | 每次变更 | 永久 |

### 9.4 降级预案

| 故障 | 影响 | 降级 |
| --- | --- | --- |
| 某业务集群不可达 | 该集群操作不可用 | 标记离线，其他集群正常 |
| Redis Pod 缓存失效 | Pod 列表延迟 | 回源 Informer；提示「数据可能过期」 |
| ES 故障 | 全局搜索不可用 | 降级 `pg_trgm` 或关闭搜索 |
| Kafka 故障 | 异步审计/计量延迟 | 同步写 PG（性能下降但不丢） |
| Citus worker 故障 | 对应 shard 不可写 | 其他 shard 正常；读副本可读 |
| ws-gateway 单点故障 | LB 摘除，前端重连其他副本 | — |
| 备份存储（S3）故障 | 备份任务失败重试 | 已成功备份仍可查；恢复任务暂停 |
| Operator 故障 | 中间件本身不受影响 | 变更操作暂停并提示运维 |
| 主 Region 故障 | 整个 Region 不可用 | DNS 切换到备 Region，promote PG |

---

## 10. 监控与告警

### 10.1 Prometheus 联邦 + Thanos

详见 [`manifests/monitoring/`](manifests/monitoring/)。

- 每 Region 独立 Prometheus 集群（2 副本 HA）
- Thanos Receive + Store + Compactor 做长期存储与跨 Region 查询
- Grafana 配置多 Region 数据源（见 [`manifests/monitoring/grafana-datasources.yaml`](manifests/monitoring/grafana-datasources.yaml)）

### 10.2 关键指标

| 指标 | 阈值 | 说明 |
| --- | --- | --- |
| `vortexops_api_request_duration_seconds` P99 | > 300ms | API 延迟 |
| `vortexops_ws_connections` | 接近 maxConnectionsPerInstance | WS 容量 |
| `vortexops_informer_lag_seconds` | > 30s | Informer 同步延迟 |
| `vortexops_release_progress_percent` | 推送延迟 > 5s | 发布进度 |
| `vortexops_k8s_api_duration_seconds` P99 | > 2s | 被管集群 API 延迟 |
| `vortexops_db_query_duration_seconds` | > 500ms | DB 查询延迟 |
| `vortexops_mq_lag` | > 10000 | 消息队列消费滞后 |
| `vortexops_es_query_duration_seconds` | > 500ms | ES 查询延迟 |
| Citus worker 连接数 | > 80% | 分片负载 |
| Redis 内存 | > 85% | 缓存容量 |
| 磁盘使用 | > 85% | 容量预警 |

### 10.3 SLO

- API P99 < 300ms
- 发布进度推送延迟 < 5s
- Pod 列表 P99 < 500ms
- 审计落库滞后 < 1s
- 推理 API P99 按模型大小分级（小模型 < 500ms，大模型 TTFT < 2s）
- Token 计量落库滞后 < 30s

---

## 11. 安全加固（在 02 层基础上增强）

### 11.1 多 Region 密钥管理

- 每 Region 独立 KMS / Vault 实例
- 跨 Region 复制使用信封加密（Region 内 KMS 解密 → 跨 Region 传输 → 对端 KMS 加密）
- 密钥轮换自动化：JWT 90 天 / 加密 key 180 天（需 re-encrypt）

### 11.2 网络隔离

- 跨 Region 流量走专线，独立 VPC peering
- 被管业务集群 API Server 白名单（仅允许 syncer Pod CIDR）
- mTLS：apiserver ↔ syncer ↔ log-proxy 双向证书（推荐）

### 11.3 数据合规

- 跨 Region 数据流动需符合 GDPR / 数据出境法规
- 审计日志按 Region 独立存储，按法规保留
- 敏感字段（Secret/Token）默认掩码

---

## 12. 升级与运维

### 12.1 滚动升级

```bash
# 主 Region
helm diff upgrade vortexops ../../helm -n vortexops -f manifests/helm-values/values-xl-region-a.yaml
helm upgrade vortexops ../../helm -n vortexops -f manifests/helm-values/values-xl-region-a.yaml

# 备 Region（standby，升级后保持 standby）
helm upgrade vortexops ../../helm -n vortexops -f manifests/helm-values/values-xl-region-b-standby.yaml
```

升级注意：
- Chart 含迁移 Job，升级时自动执行 schema 变更
- **升级前备份 Citus**（coordinator + worker 并行）
- syncer 逐实例滚动，避免全量重连被管集群 API Server
- 跨 Region 升级：先备后主，验证后再切换

### 12.2 扩容

```bash
# Citus 加 worker
./scripts/enable-citus-sharding.sh --add-worker

# Redis Cluster 加节点
./scripts/add-redis-shard.sh --shard-count 8

# Kafka 加 broker
./scripts/add-kafka-broker.sh --broker-count 12

# syncer 加实例
kubectl -n vortexops scale deploy/vortexops-syncer --replicas=30
# 重新分片 namespace
kubectl -n vortexops rollout restart deploy/vortexops-syncer
```

### 12.3 容量巡检

```bash
./scripts/capacity-check.sh --region us-east-1
```

输出示例：

```
============================================
 VortexOps Capacity Check (us-east-1)
============================================
 Citus coordinator:
   connections: 156/200 (78%)
   disk:        1.2T/2T (60%)
 Citus workers:
   worker-0: connections 89/200, disk 850G/1T (85%)  ⚠
   worker-1: connections 67/200, disk 720G/1T (72%)
 Redis Cluster:
   shard-0: memory 5.8G/8G (72%)
   shard-1: memory 6.2G/8G (77%)
 Kafka:
   audit.async lag: 2345 (OK)
   build.events lag: 12 (OK)
 OpenSearch:
   vortexops-audit-2026-07: 45G (OK)
 syncer:
   20 instances, max lag 18s (OK)
 ws-gateway:
   10 instances, max connections 4100/5000 (82%)
============================================
```

---

## 13. 关键场景验证

详见 [`../../docs/scalability.md`](../../docs/scalability.md) §14 关键场景验证。

### 13.1 单大应用 1 万副本发布

- 发布请求 → apiserver PATCH Deployment → release watcher 读 `status.readyReplicas`，每 5% 推一次进度
- 1 万副本约 10-30min 完成，前端进度条平滑
- 并发：单分组仅 1 发布；日志/事件走就近代理不压平台

### 13.2 跨空间构建中心列表（10 万应用）

- 列表查询走 ES（按 status/time/workspace 过滤），分页 20 条，P99 < 200ms
- DB 不参与该查询

### 13.3 5 万 WS 在线

- ws-gateway 10 副本，每副本 5000 连接
- 订阅路由经 Redis Pub/Sub；日志流采样
- 单副本内存 < 4GB

### 13.4 大型中间件集群部署（100+ broker Kafka）

- 用 Operator（Strimzi），平台仅提交 `Kafka` CR
- 100 broker 升级超时默认 2 小时，滚动观察期间进度按 broker 就绪数推送
- 备份走卷快照（CSI），按 broker PV 并行快照

---

## 14. 常见问题

| 现象 | 原因 | 解决 |
| --- | --- | --- |
| Citus 分片不可用 | worker 节点故障 | `SELECT * FROM citus_get_active_worker_nodes();`；故障 shard 标记只读 |
| syncer 集群离线 | kubeconfig 过期 / RBAC 不足 / 网络不通 | 测试 `kubectl --kubeconfig=... get ns`；批量续期 |
| Pod 列表空白 | Redis 缓存 miss + Informer 未同步 | 查 syncer Informer lag；Redis key `rt:pod:*` |
| 发布卡住 | K8s API 延迟 / 资源不足 / 并发锁 | 查 release 事件 + Deployment status |
| WS 频繁断连 | ws-gateway 副本不足 / Redis Pub/Sub 异常 | 查 `vortexops_ws_connections`；扩容 |
| PG 连接耗尽 | 未用 PgBouncer / pool 配置过大 | 查 `pg_stat_activity`；调低 `dbPoolMax` |
| Kafka lag 持续增长 | 消费者处理慢 / 分区不均 | 扩容消费者；增加分区数 |
| 跨 Region 复制延迟 | 网络抖动 / 大事务 | 检查专线带宽；拆分大事务 |
| 推理 Pod Pending | GPU 不足 / 配额超限 | 查 `kubectl describe pod`；空间 GPU 配额 |
| 主 Region 故障 | 整 Region 不可用 | `./scripts/failover-to-region-b.sh` DNS 切换 + PG promote |

---

## 15. 从 02 层升级到 03 层的路径

### 15.1 前提

- 数据模型分片键（`workspace_id` / `cluster_id` / `application_id`）从 M1 起已落实，无需重构
- Pod 不落库原则已落实（Pod 运行态走 Redis 缓存）

### 15.2 升级步骤

1. **扩容依赖组件**：
   - PG 主从 → Citus 多 worker（`./scripts/enable-citus-sharding.sh`）
   - Redis Sentinel → Redis Cluster（`./scripts/add-redis-shard.sh`）
   - NATS → Kafka 9 broker（`kubectl apply -f manifests/kafka-cluster.yaml`）
   - OpenSearch 3 节点 → 6+ 数据节点

2. **修改 Helm values**：从 `values-k8s-mid.yaml` 切换到 `values-xl-region-a.yaml`

3. **配置 syncer 分片**：从单实例 → 20+ 实例按 namespace 分片

4. **部署 ws-gateway 集群**：从 2 副本 → 10+ 副本

5. **批量部署 log-proxy**：在被管业务集群部署边缘代理

6. **启用跨 Region 灾备**（如需）

7. **验证**：执行 [`../../docs/scalability.md`](../../docs/scalability.md) §14 关键场景验证

### 15.3 数据迁移

- PG → Citus：执行 `create_distributed_table` 后，Citus 自动分布已有数据（耗时取决于数据量，建议低峰执行）
- Redis Sentinel → Redis Cluster：用 `redis-cli --cluster import` 迁移数据
- NATS → Kafka：双写过渡，逐步切流量

**无需改业务代码**。所有规模演进只需修改 Helm values + 依赖组件扩容。
