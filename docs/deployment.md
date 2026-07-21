# 部署文档

VortexOps 平台自身的部署、依赖组件安装、多规模拓扑、高可用与运维。平台采用 Helm Chart 部署到 Kubernetes，支持从**小规模 PoC**到**超大规模（10 万+ 应用）**的渐进式演进。

> 架构见 [架构设计](architecture.md)（含 §5 元数据与协调存储决策：不引入 etcd/zk），扩展性见 [扩展性设计](scalability.md)，建表语句见 [schema.sql](../schema.sql)。

---

## 1. 部署总览

### 1.1 组件分层

```
┌─────────────────────────────────────────────────────────────────┐
│ 接入层：Ingress / LB / CDN                                       │
├─────────────────────────────────────────────────────────────────┤
│ 平台无状态层：frontend │ apiserver │ ext-api-gw │ ws-gateway     │
│              syncer（按集群/Namespace 分片）│ log-proxy（边缘）   │
├─────────────────────────────────────────────────────────────────┤
│ 平台有状态/外部依赖：                                              │
│   PostgreSQL（元数据事实源）│ Redis（缓存/锁/限流/Pod 运行态）     │
│   Kafka/NATS（异步）│ Elasticsearch（搜索，可选）                 │
│   对象存储 MinIO/S3（日志/权重/SBOM/备份）                        │
├─────────────────────────────────────────────────────────────────┤
│ 外部集成（非平台内置，需单独部署）：                                │
│   Jenkins │ Harbor/Registry │ 被管业务 K8s 集群群                 │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 存储决策速查

| 组件 | 是否必需 | 小规模 | 中规模 | 大规模 | 超大规模 |
| --- | --- | --- | --- | --- | --- |
| PostgreSQL | **必需** | 单实例 | 主从 + 读副本 | Citus 分片 | Citus 多 worker |
| Redis | **必需** | 单实例 | 主从/Sentinel | Cluster 6 节点 | Cluster 12+ 节点 |
| 对象存储 | **必需** | MinIO 单节点 | MinIO 分布式 | S3/OSS 托管 | S3 多区域 |
| Kafka/NATS | 可选 | 不启用 | NATS 单集群 | Kafka 3 broker | Kafka 9+ broker |
| Elasticsearch | 可选 | 不启用 | 单节点 | 3 节点集群 | 6+ 节点 |
| Jenkins | 可选 | 单实例 | 主从 agent | K8s agent 池 | 多 Jenkins + agent 池 |
| etcd/zk | **不需要** | — | — | — | — |

> 平台**不单独引入 etcd/ZooKeeper**。Leader 选举用 K8s Lease，元数据用 PostgreSQL，缓存用 Redis。详见 [架构设计 §5](architecture.md#5-元数据与协调存储决策)。

---

## 2. 规模分级与选型

### 2.1 规模定义

| 级别 | 画像 | 应用数 | 集群数 | 日构建 | 在线 WS |
| --- | --- | --- | --- | --- | --- |
| **S 小规模** | PoC / 小团队 / 单集群 | <500 | 1-5 | <500 | <500 |
| **M 中规模** | 部门级 / 多团队 | 500-5k | 5-50 | 500-5k | 500-5k |
| **L 大规模** | 企业级 / 多 BU | 5k-50k | 50-500 | 5k-50k | 5k-20k |
| **XL 超大规模** | 超大客户 / 平台型 | 50k-500k | 500-10k | 50k+ | 20k+ |

### 2.2 S 小规模 — PoC / 开发验证

**拓扑**

```
┌──────────── 管理 K8s 集群（单 AZ）────────────┐
│  Ingress                                       │
│  ├─ frontend ×1                                │
│  ├─ apiserver ×2                               │
│  ├─ syncer ×1                                  │
│  ├─ ws-gateway ×1                              │
│  ├─ ext-api-gw ×1                              │
│  ├─ PostgreSQL ×1（Chart 内置或外部单实例）     │
│  ├─ Redis ×1（单实例）                         │
│  └─ MinIO ×1（单节点，100Gi）                  │
└────────────────────────────────────────────────┘
         │ kubeconfig
         ▼
   1-5 个被管业务集群
```

**资源规格（管理集群节点建议 ≥3，每节点 8C16G）**

| 组件 | 副本 | CPU 请求/上限 | 内存 请求/上限 | 存储 |
| --- | --- | --- | --- | --- |
| apiserver | 2 | 0.5/1 | 512Mi/1Gi | — |
| frontend | 1 | 0.1/0.5 | 64Mi/256Mi | — |
| syncer | 1 | 0.5/1 | 512Mi/1Gi | — |
| ws-gateway | 1 | 0.25/0.5 | 256Mi/512Mi | — |
| ext-api-gw | 1 | 0.25/0.5 | 256Mi/512Mi | — |
| PostgreSQL | 1 | 1/2 | 2Gi/4Gi | 50Gi SSD |
| Redis | 1 | 0.25/0.5 | 512Mi/1Gi | 10Gi（可选） |
| MinIO | 1 | 0.5/1 | 1Gi/2Gi | 100Gi |

**不启用**：Kafka、Elasticsearch、log-proxy（直连被管集群 API）、Citus。

**适用场景**：功能验证、≤50 人团队、单业务集群。

---

### 2.3 M 中规模 — 部门级生产

**拓扑**

```
                    ┌──────── Ingress（TLS）────────┐
                    │  frontend×2  apiserver×2       │
                    │  ws-gateway×2  ext-api-gw×2    │
                    │  syncer×2                      │
                    └───────────────┬────────────────┘
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        ▼                           ▼                           ▼
  PostgreSQL 主从              Redis Sentinel               MinIO 4 节点
  (1 主 1 从 + PgBouncer)      (1 主 2 从)                  (纠删码 EC:2)
        │                           │
        └──────── NATS（可选）────────┘
                                    │
                          5-50 被管集群 + log-proxy（边缘）
```

**资源规格**

| 组件 | 副本/节点 | CPU | 内存 | 存储 |
| --- | --- | --- | --- | --- |
| apiserver | 2-3 | 1/2 核 | 1/2 Gi | — |
| syncer | 2 | 1/2 核 | 1/2 Gi | — |
| ws-gateway | 2 | 0.5/1 核 | 1/2 Gi | — |
| PostgreSQL 主 | 1 | 4/8 核 | 8/16 Gi | 200Gi SSD |
| PostgreSQL 从 | 1 | 2/4 核 | 4/8 Gi | 200Gi SSD |
| PgBouncer | 2 | 0.25/0.5 核 | 128/256 Mi | — |
| Redis Sentinel | 3 节点 | 0.5/1 核 | 1/2 Gi | 20Gi |
| MinIO | 4 节点 | 1/2 核 | 2/4 Gi | 500Gi/节点 |
| NATS | 3 节点 | 0.5/1 核 | 512Mi/1Gi | 20Gi |
| ES（可选） | 1 节点 | 2/4 核 | 4/8 Gi | 100Gi |

**启用**：log-proxy 边缘代理、PgBouncer 连接池、NATS 异步事件、可选 ES 全局搜索。

---

### 2.4 L 大规模 — 企业级

**拓扑**

```
                         ┌────── LB / CDN ──────┐
                         │  apiserver ×4+       │
                         │  ws-gateway ×4+      │
                         │  syncer ×N（分片）    │
                         └──────────┬───────────┘
                                    │
     ┌──────────────────────────────┼──────────────────────────────┐
     ▼                              ▼                              ▼
 Citus 集群                    Redis Cluster                   Kafka 3 broker
 (1 coordinator + 4 worker)    (6 节点，3 主 3 从)              + ZooKeeper/KRaft
 + 2 读副本                                                              │
     │                              │                              ▼
     └──────────── ES 3 节点 ────────┴──────── S3/OSS 托管 ──────────┘
                                    │
                          50-500 被管集群 + log-proxy 分片
```

**资源规格（关键组件）**

| 组件 | 规模 | 说明 |
| --- | --- | --- |
| apiserver | 4+ 副本，HPA CPU 70% | 无状态水平扩展 |
| syncer | 按 `(cluster, ns_shard)` 分片，每实例管 ≤50 集群 | Lease 选举分片主 |
| ws-gateway | 4+ 副本，每实例 ≤5000 连接 | Redis Pub/Sub 路由 |
| Citus coordinator | 2 核 4Gi ×2（HA） | 协调节点 |
| Citus worker | 8 核 32Gi ×4+ | 按 workspace_id 分片 |
| Redis Cluster | 6 节点起 | Pod 缓存分片，每分片 <8GB |
| Kafka | 3 broker × 8C16G | 审计/Token 计量/构建事件 |
| ES | 3 节点 × 4C8G | 全局搜索、超长日志索引 |
| 对象存储 | S3/OSS 托管 | 构建日志 30 天热 + 归档 |

**启用**：Citus 分片、Redis Cluster、Kafka、ES、log-proxy 全量、PgBouncer 池化。

---

### 2.5 XL 超大规模 — 平台型

在 L 基础上进一步扩展：

| 维度 | XL 方案 |
| --- | --- |
| PostgreSQL | Citus 8+ worker，coordinator HA（Patroni），读副本 4+ |
| Redis | Cluster 12+ 节点，按 cluster_id hash 分片 Pod 缓存 |
| syncer | 20+ 实例，Informer 按 Namespace 分片，Lease 协调 |
| ws-gateway | 10+ 实例，LB 粘性会话 + Redis Pub/Sub |
| Kafka | 9+ broker，Topic 按 workspace 分区 |
| ES | 6+ 数据节点 + 3 协调节点，ILM 冷热分层 |
| log-proxy | 每被管集群 2+ 副本，按 namespace 分片 |
| 对象存储 | 多区域复制，模型权重 PB 级冷热分层 |

详见 [扩展性设计 §13 部署拓扑](scalability.md#13-部署拓扑大规模) 与 [§17 规模演进路线](scalability.md#17-规模演进路线)。

---

## 3. 依赖组件部署

以下示例均部署到**管理集群**的 `vortexops-infra` 命名空间（或与平台同集群独立 namespace）。生产建议依赖组件与平台应用分 namespace，便于独立升级与备份。

### 3.1 PostgreSQL

#### 3.1.1 S 规模 — 单实例（Bitnami Helm）

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami

helm install vortexops-pg bitnami/postgresql -n vortexops-infra --create-namespace \
  --set auth.database=vortexops \
  --set auth.username=vortexops \
  --set auth.password='CHANGE_ME' \
  --set primary.persistence.size=50Gi \
  --set primary.resources.requests.cpu=1 \
  --set primary.resources.requests.memory=2Gi
```

连接串（供平台 values.yaml）：

```yaml
postgresql:
  external:
    host: vortexops-pg-postgresql.vortexops-infra.svc
    port: 5432
    database: vortexops
    username: vortexops
    existingSecret: pg-creds
```

#### 3.1.2 M 规模 — 主从 + PgBouncer

```bash
# 主从（Bitnami replication 模式）
helm install vortexops-pg bitnami/postgresql -n vortexops-infra \
  --set architecture=replication \
  --set auth.database=vortexops \
  --set auth.username=vortexops \
  --set auth.password='CHANGE_ME' \
  --set primary.persistence.size=200Gi \
  --set readReplicas.replicaCount=1

# PgBouncer（连接池，减少 apiserver 直连 PG 连接数）
helm install pgbouncer bitnami/pgbouncer -n vortexops-infra \
  --set postgresql.host=vortexops-pg-postgresql \
  --set postgresql.database=vortexops \
  --set postgresql.username=vortexops \
  --set postgresql.password='CHANGE_ME' \
  --set replicaCount=2
```

平台 `dbPoolMax` 建议：apiserver 50、syncer 30，经 PgBouncer 汇聚到 PG 实际连接 ≤100。

#### 3.1.3 L/XL 规模 — Citus 分片

```bash
# 使用 Citus 官方 Helm（或自建 StatefulSet）
helm repo add citusdata https://charts.citusdata.com

helm install vortexops-citus citusdata/citus -n vortexops-infra \
  --set worker.replicas=4 \
  --set worker.resources.requests.cpu=4 \
  --set worker.resources.requests.memory=16Gi \
  --set worker.persistence.size=500Gi
```

分片表与命令见 [schema.sql §14.3](../schema.sql)（`create_distributed_table` 注释块）。分片键：`workspace_id` / `cluster_id` / `application_id`。

#### 3.1.4 初始化 schema

```bash
# 方式一：psql 直接执行
psql -h <pg-host> -U vortexops -d vortexops -f schema.sql

# 方式二：平台迁移 Job（推荐，升级时自动执行）
kubectl -n vortexops exec deploy/vortexops-apiserver -- vortexops migrate up
```

验证：

```bash
psql -h <pg-host> -U vortexops -d vortexops -c \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';"
# 期望 ~117 张表
```

---

### 3.2 Redis

#### 3.2.1 S 规模 — 单实例

```bash
helm install vortexops-redis bitnami/redis -n vortexops-infra \
  --set auth.password='CHANGE_ME' \
  --set master.persistence.size=10Gi \
  --set replica.replicaCount=0
```

#### 3.2.2 M 规模 — Sentinel 高可用

```bash
helm install vortexops-redis bitnami/redis -n vortexops-infra \
  --set architecture=replication \
  --set auth.password='CHANGE_ME' \
  --set sentinel.enabled=true \
  --set replica.replicaCount=2
```

#### 3.2.3 L/XL 规模 — Cluster 模式

```bash
helm install vortexops-redis bitnami/redis-cluster -n vortexops-infra \
  --set cluster.nodes=6 \
  --set cluster.replicas=1 \
  --set redis.password='CHANGE_ME' \
  --set persistence.size=20Gi
```

**Redis 用途与 key 前缀**（平台配置无需改 key，仅供运维排查）：

| 前缀 | 用途 | TTL |
| --- | --- | --- |
| `rt:pod:` | Pod 运行态缓存 | 5min |
| `rt:group:` | 分组运行态摘要 | 5min |
| `rt:ip:` | IP→Pod 反查 | 5min |
| `lock:` | 分布式锁（发布/流水线） | 30s-5min |
| `ratelimit:` | 限流计数 | 1min |
| `ws:route:` | WS 订阅路由 | 实时 |

---

### 3.3 对象存储（MinIO / S3）

#### 3.3.1 S/M 规模 — MinIO 单节点或分布式

```bash
helm repo add minio https://charts.min.io/

# 单节点（S）
helm install minio minio/minio -n vortexops-infra \
  --set mode=standalone \
  --set rootUser=admin \
  --set rootPassword='CHANGE_ME' \
  --set persistence.size=100Gi \
  --set buckets[0].name=vortexops \
  --set buckets[0].policy=none

# 分布式 4 节点（M）
helm install minio minio/minio -n vortexops-infra \
  --set mode=distributed \
  --set replicas=4 \
  --set persistence.size=500Gi \
  --set rootUser=admin \
  --set rootPassword='CHANGE_ME'
```

平台 values.yaml：

```yaml
objectStorage:
  type: s3
  endpoint: http://minio.vortexops-infra.svc:9000
  bucket: vortexops
  region: us-east-1
  existingSecret: s3-creds   # accessKey + secretKey
  pathStyle: true            # MinIO 需开启
```

**Bucket 目录规划**

| 前缀 | 内容 | 生命周期 |
| --- | --- | --- |
| `build-logs/` | 构建日志归档 | 30 天热 → 90 天归档 → 删除 |
| `pipeline-artifacts/` | 流水线产物 | 90 天 |
| `model-weights/` | 模型权重缓存 | 热版本常驻，冷版本转低频 |
| `sbom/` | 制品 SBOM | 与镜像同生命周期 |
| `config-snapshots/` | 配置快照 | 365 天 |
| `backups/` | DB 备份 | 30 天全量 + WAL |

#### 3.3.2 L/XL 规模 — 托管 S3/OSS

直接使用云厂商 S3/OSS，开启版本化与跨区域复制。模型权重 PB 级需配置 Intelligent-Tiering / 归档存储。

---

### 3.4 Kafka / NATS（M 规模起可选，L 规模推荐）

#### 3.4.1 NATS（M 规模，轻量）

```bash
helm repo add nats https://nats-io.github.io/k8s/helm/charts/

helm install nats nats/nats -n vortexops-infra \
  --set nats.jetstream.enabled=true \
  --set cluster.enabled=true \
  --set cluster.replicas=3
```

#### 3.4.2 Kafka（L/XL 规模）

```bash
helm repo add strimzi https://strimzi.io/charts/

helm install strimzi strimzi/strimzi-kafka-operator -n vortexops-infra

# Kafka 集群 CR（3 broker）
kubectl apply -f - <<'EOF'
apiVersion: kafka.strimzi.io/v1beta2
kind: Kafka
metadata:
  name: vortexops-kafka
  namespace: vortexops-infra
spec:
  kafka:
    version: 3.7.0
    replicas: 3
    listeners:
      - name: plain
        port: 9092
        type: internal
        tls: false
    storage:
      type: persistent-claim
      size: 100Gi
  zookeeper:
    replicas: 3
    storage:
      type: persistent-claim
      size: 10Gi
EOF
```

**Topic 规划**

| Topic | 用途 | 分区（L） | 分区（XL） |
| --- | --- | --- | --- |
| `build.events` | 构建状态变更 | 6 | 24 |
| `release.events` | 发布进度 | 6 | 24 |
| `audit.async` | 审计异步落库 | 12 | 48 |
| `ext-api.audit` | 对外 API 审计 | 6 | 24 |
| `inference.usage` | Token 计量 | 12 | 48 |
| `notification.dispatch` | 通知分发 | 3 | 12 |

---

### 3.5 Elasticsearch（M 规模可选，L 规模推荐）

```bash
helm repo add elastic https://helm.elastic.co

helm install elasticsearch elastic/elasticsearch -n vortexops-infra \
  --set replicas=3 \
  --set minimumMasterNodes=2 \
  --set resources.requests.cpu=2 \
  --set resources.requests.memory=4Gi \
  --set volumeClaimTemplate.resources.requests.storage=100Gi
```

**索引规划**

| 索引 | 用途 | 保留 |
| --- | --- | --- |
| `vortexops-audit-*` | 审计全文检索 | 2 年 |
| `vortexops-build-logs-*` | 构建日志全文 | 30 天 |
| `vortexops-apps-*` | 应用/构建中心搜索 | 实时 |
| `vortexops-pipeline-*` | 流水线 run 搜索 | 1 年 |

ES 不可用时平台降级到 PostgreSQL `pg_trgm` 模糊搜索（限小结果集）。详见 [扩展性 §15 降级](scalability.md#15-降级与容灾)。

---

### 3.6 Jenkins（构建 executor）

Jenkins 通常部署在**独立集群或管理集群**，与平台通过 REST API 集成。

```bash
helm repo add jenkins https://charts.jenkins.io

helm install jenkins jenkins/jenkins -n vortexops-infra \
  --set controller.resources.requests.cpu=1 \
  --set controller.resources.requests.memory=2Gi \
  --set persistence.size=20Gi \
  --set agent.enabled=true \
  --set agent.resources.requests.cpu=1 \
  --set agent.resources.requests.memory=2Gi
```

**K8s 动态 Agent（L 规模推荐）**

- 安装 Jenkins Kubernetes Plugin。
- Agent Pod 在被管集群或专用构建集群中动态创建，构建完销毁。
- 平台 `vo_jenkins_instances` 表登记 URL + 凭证，构建时下发 Job 参数。

**规模建议**

| 规模 | Jenkins 方案 | 并发构建 |
| --- | --- | --- |
| S | 单 controller + 2 静态 agent | ≤5 |
| M | controller HA + K8s 动态 agent | ≤20 |
| L | 多 controller + agent 池（按空间隔离） | ≤100 |
| XL | 多 Jenkins 实例 + 大规模 agent 池 | 排队 + 优先级 |

---

### 3.7 Harbor（镜像仓库）

```bash
helm repo add harbor https://helm.goharbor.io

helm install harbor harbor/harbor -n vortexops-infra \
  --set expose.type=ingress \
  --set expose.ingress.hosts.core=harbor.internal \
  --set persistence.persistentVolumeClaim.registry.size=200Gi \
  --set persistence.persistentVolumeClaim.jobservice.size=10Gi \
  --set persistence.persistentVolumeClaim.database.size=5Gi
```

平台系统管理 → 镜像仓库 → 登记 Harbor URL + 凭证。CVE 扫描结果回写 `images.scan_status`。

---

## 4. VortexOps 平台部署（Helm）

### 4.1 前置条件

- 管理 K8s 集群 ≥ 1.26，已安装 Ingress Controller（nginx/traefik）。
- 依赖组件就绪：PostgreSQL（schema 已初始化）、Redis、对象存储。
- 可选：Kafka/NATS、Elasticsearch、Jenkins、Harbor 已部署并可达。

#### 4.1.1 构建后端镜像

仓库根目录提供 `Dockerfile`（多阶段构建：`golang:1.22-alpine` 编译 → `gcr.io/distroless/static-debian12:nonroot` 运行，CGO 关闭、纯静态二进制）。

```bash
# 单架构构建并推送至私有仓库
docker build -t registry.vortexops.io/vortexops/apiserver:1.0.0 \
  --build-arg VERSION=1.0.0 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -f Dockerfile .
docker push registry.vortexops.io/vortexops/apiserver:1.0.0

# 多架构（amd64/arm64，需 buildx）
docker buildx build --platform linux/amd64,linux/arm64 \
  -t registry.vortexops.io/vortexops/apiserver:1.0.0 \
  --build-arg VERSION=1.0.0 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --push -f Dockerfile .
```

构建产物运行时仅依赖环境变量配置（见 `internal/config`），镜像内 `ENTRYPOINT` 为 `/app/vortexops serve`，监听 `:8080`。版本号通过 `-ldflags -X` 注入 `internal/version` 包，可用 `vortexops version` 查看。

### 4.2 添加 Chart 仓库

```bash
helm repo add vortexops https://charts.vortexops.io
helm repo update
```

### 4.3 values.yaml（按规模选 profile）

**S 规模 profile**

```yaml
global:
  imageRegistry: registry.vortexops.io
  imageTag: "1.0.0"

postgresql:
  enabled: false
  external:
    host: vortexops-pg-postgresql.vortexops-infra.svc
    database: vortexops
    existingSecret: pg-creds

redis:
  enabled: false
  external:
    host: vortexops-redis-master.vortexops-infra.svc
    existingSecret: redis-creds

objectStorage:
  type: s3
  endpoint: http://minio.vortexops-infra.svc:9000
  bucket: vortexops
  existingSecret: s3-creds
  pathStyle: true

apiserver:
  replicas: 2
  config:
    dbPoolMax: 20
    jwtSecretRef: jwt-secret
    encryptionKeyRef: kms-key

syncer:
  replicas: 1

wsGateway:
  replicas: 1

extApiGateway:
  replicas: 1

kafka:
  enabled: false
elasticsearch:
  enabled: false
logProxy:
  enabled: false

systemSettings:
  workspaceCreation:
    allowSelfCreate: true
    maxPerUser: 5
```

**L 规模 profile 差异**

```yaml
apiserver:
  replicas: 4
  autoscaling:
    enabled: true
    minReplicas: 4
    maxReplicas: 12
    targetCPU: 70

syncer:
  replicas: 4
  config:
    clustersShardPerInstance: 50
    namespaceShardCount: 16

wsGateway:
  replicas: 4
  config:
    maxConnectionsPerInstance: 5000

kafka:
  enabled: true
  bootstrapServers: vortexops-kafka-kafka-bootstrap.vortexops-infra.svc:9092

elasticsearch:
  enabled: true
  hosts: ["http://elasticsearch-master.vortexops-infra.svc:9200"]

logProxy:
  enabled: true    # 在被管集群单独 helm install log-proxy
```

### 4.4 安装步骤

```bash
# 1. 创建命名空间
kubectl create namespace vortexops

# 2. 创建 Secret
kubectl -n vortexops create secret generic pg-creds \
  --from-literal=password='CHANGE_ME' \
  --from-literal=username=vortexops
kubectl -n vortexops create secret generic redis-creds \
  --from-literal=password='CHANGE_ME'
kubectl -n vortexops create secret generic s3-creds \
  --from-literal=accessKey='admin' \
  --from-literal=secretKey='CHANGE_ME'
kubectl -n vortexops create secret generic jwt-secret \
  --from-literal=key='$(openssl rand -base64 32)'
kubectl -n vortexops create secret generic kms-key \
  --from-literal=key='$(openssl rand -hex 16)'   # 32 字节 AES 密钥

# 3. 部署平台
helm install vortexops vortexops/vortexops -n vortexops -f values.yaml

# 4. 等待就绪
kubectl -n vortexops rollout status deploy/vortexops-apiserver --timeout=300s

# 5. 初始化管理员
kubectl -n vortexops exec deploy/vortexops-apiserver -- \
  vortexops bootstrap-admin --username admin --password 'CHANGE_ME' --email admin@corp
```

### 4.5 集群接入

1. 在被管集群创建 ServiceAccount + RBAC（见 [K8s 集成](kubernetes.md)）。
2. 系统管理 → 集群 → 接入：上传 kubeconfig，选 standard/edge 模式。
3. syncer 建立 Informer 连接，同步节点池/IP 池。
4. 空间绑定集群 + Namespace 后即可部署。

**边缘模式（M 规模起）**：在被管集群部署 log-proxy：

```bash
helm install vortexops-log-proxy vortexops/log-proxy \
  -n vortexops-edge --create-namespace \
  --set platformEndpoint=https://vortexops.internal \
  --set clusterId=<cluster-uuid>
```

---

## 5. 网络设计

### 5.1 流量路径

| 流量 | 路径 | 端口/TLS |
| --- | --- | --- |
| 用户 UI | Ingress → frontend | 443 TLS |
| 内部 API | Ingress → apiserver | 443 TLS |
| 对外 API | Ingress → ext-api-gw（独立域名） | 443 TLS |
| WebSocket | Ingress → ws-gateway（sticky session） | 443 WSS |
| 平台 → 被管集群 | syncer → K8s API Server | 6443 TLS |
| 日志/exec | ws-gateway → log-proxy → K8s API | 443 mTLS（边缘） |
| 平台 → Jenkins | apiserver → Jenkins REST | 443/8080 |
| 平台 → Harbor | apiserver → Registry API | 443 |

### 5.2 NetworkPolicy 建议

```yaml
# apiserver 仅允许来自 Ingress 和内部组件
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: apiserver-ingress-only
  namespace: vortexops
spec:
  podSelector:
    matchLabels:
      app: vortexops-apiserver
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              name: ingress-nginx
    - from:
        - podSelector:
            matchLabels:
              app.kubernetes.io/part-of: vortexops
```

- **ext-api-gw** 独立 NetworkPolicy，仅允许 Ingress + 出站到 apiserver/Redis。
- **syncer** 允许出站到所有被管集群 API Server（按 cluster CIDR 白名单）。
- **依赖组件**（PG/Redis/Kafka）仅允许 vortexops namespace 入站。

### 5.3 DNS

- 平台内部：`*.vortexops-infra.svc.cluster.local`
- 对外：`vortexops.internal`（UI）、`api.vortexops.internal`（内部 API）、`ext.vortexops.internal`（对外 API）

---

## 6. 存储设计

### 6.1 StorageClass 建议

| 用途 | StorageClass | 说明 |
| --- | --- | --- |
| PostgreSQL / Citus | `ssd-retain` | Retain 策略，高性能 SSD |
| Redis | `ssd-retain` | 低延迟 |
| MinIO / Kafka / ES | `ssd` 或 `hdd`（冷数据） | 按 IOPS 需求 |
| 模型权重 PVC | `shared-readonly`（RWX） | 多 Pod 共享只读挂载 |
| 备份 | 对象存储 | 不用 PV |

### 6.2 数据分层

| 层级 | 存储 | 数据 |
| --- | --- | --- |
| 热 | Redis | Pod 运行态、限流、WS 路由 |
| 温 | PostgreSQL | 业务元数据、期望态 |
| 冷 | 对象存储 | 构建日志、SBOM、模型权重冷版本 |
| 检索 | Elasticsearch | 审计/日志全文索引 |

### 6.3 容量规划速查

| 规模 | PG 磁盘 | Redis 内存 | 对象存储 | ES 磁盘 |
| --- | --- | --- | --- | --- |
| S | 50Gi | 1Gi | 100Gi | — |
| M | 200Gi | 6Gi | 2Ti | 100Gi |
| L | 2Ti（Citus） | 48Gi | 20Ti | 500Gi |
| XL | 10Ti+ | 200Gi+ | PB 级 | 2Ti+ |

---

## 7. 高可用与灾备

### 7.1 组件 HA 矩阵

| 组件 | HA 方案 | RPO | RTO |
| --- | --- | --- | --- |
| apiserver / frontend / ws-gateway | 多副本 + HPA + PDB | 0 | <1min |
| syncer | 多副本 + Lease 分片 + PDB | 0（缓存可重建） | <5min |
| PostgreSQL | 主从 / Citus + Patroni | <1min（同步复制） | <5min |
| Redis | Sentinel / Cluster | 秒级（AOF） | <2min |
| Kafka | 3+ broker + RF=3 | 0 | <5min |
| MinIO | 纠删码 EC:2/EC:4 | 0 | <5min |
| 对象存储（S3） | 多 AZ / 跨区域复制 | 0 | <15min |

### 7.2 备份策略

| 数据 | 方式 | 频率 | 保留 |
| --- | --- | --- | --- |
| PostgreSQL | pg_dump 逻辑 / pgBackRest 物理 + WAL | 日全量 + WAL 持续 | 30 天 |
| Redis | RDB 快照（限流/锁 key） | 每小时 | 7 天 |
| 对象存储 | 版本化 + 跨区域复制 | 持续 | 按生命周期 |
| Chart values / Secret | Git + Sealed Secrets | 每次变更 | 永久 |
| Citus | coordinator 备份 + worker 并行 | 日全量 | 30 天 |

恢复示例：

```bash
# PostgreSQL 逻辑恢复
pg_restore -h <pg-host> -U vortexops -d vortexops --clean /backup/vortexops.dump

# 恢复后重启平台组件
kubectl -n vortexops rollout restart deploy/vortexops-apiserver deploy/vortexops-syncer
```

### 7.3 灾备拓扑（跨 Region）

```
Region A（主）                          Region B（备）
├─ 管理集群（平台全量）                  ├─ 管理集群（平台 standby）
├─ PG 主 + Citus worker                 ├─ PG 从（流复制 / Citus 读副本）
├─ Redis Cluster                        ├─ Redis 从（异步复制）
├─ S3 Bucket A                          ├─ S3 Bucket B（CRR 复制）
└─ 被管集群群 A                          └─ 被管集群群 B（可选双活）

DNS 切换：vortexops.internal → Region B（故障时）
```

- **RPO 目标**：PG 同步复制 RPO≈0；Redis/ES 异步 RPO<1min。
- **RTO 目标**：DNS 切换 + PG promote <15min。
- syncer 在备 Region 预热但不连被管集群，故障时手动/自动 promote PG 并启动 syncer。

### 7.4 降级预案

| 故障 | 影响 | 降级 |
| --- | --- | --- |
| 某被管集群不可达 | 该集群操作不可用 | 标记离线，其他集群正常 |
| Redis 不可用 | Pod 列表延迟 | 回源 Informer 或提示「数据可能过期」 |
| ES 不可用 | 全局搜索不可用 | 降级 pg_trgm 或关闭搜索 |
| Kafka 不可用 | 异步审计/计量延迟 | 同步写 PG（性能下降） |
| PG 从库不可用 | 读延迟上升 | 读切主库 |

详见 [扩展性 §15](scalability.md#15-降级与容灾)。

---

## 8. 安全加固

### 8.1 密钥与凭证

| 密钥 | 存储 | 轮换 |
| --- | --- | --- |
| JWT signing key | K8s Secret / Vault | 90 天 |
| 凭证加密 key（kms-key） | K8s Secret / KMS | 180 天（需 re-encrypt） |
| DB/Redis/S3 密码 | K8s Secret / External Secrets | 90 天 |
| kubeconfig（被管集群） | PG 加密存储 | 按需 |
| API Token（voe_） | PG 哈希存储 | 用户自助轮换 |

生产建议对接 **Vault / AWS KMS** 做信封加密，密钥不入 Git。

### 8.2 TLS 与 mTLS

- Ingress 强制 TLS 1.2+，推荐 cert-manager 自动签发。
- 内部组件 mTLS（L 规模起推荐）：apiserver ↔ syncer ↔ log-proxy 双向证书。
- 被管集群 kubeconfig 使用 client cert 或 token，最小 RBAC。

### 8.3 审计与合规

- 平台操作全量审计 → `vo_audit_logs`（按月分区）。
- 对外 API 独立审计 → `vo_external_api_call_logs`。
- 敏感字段（Secret/Token）默认掩码，查看需额外权限 + 审计。

### 8.4 镜像安全

- 平台自身镜像 CI 扫描（Trivy），Critical CVE 阻断发布。
- 业务镜像 CVE 准入策略：`systemSettings.cve.defaultPolicy`（warn/block_critical/block_high）。

---

## 9. 规模对照总表

| 维度 | S | M | L | XL |
| --- | --- | --- | --- | --- |
| 应用数 | <500 | 500-5k | 5k-50k | 50k-500k |
| 被管集群 | 1-5 | 5-50 | 50-500 | 500-10k |
| PostgreSQL | 单实例 | 主从+PgBouncer | Citus 4 worker | Citus 8+ worker |
| Redis | 单实例 | Sentinel 3 节点 | Cluster 6 节点 | Cluster 12+ |
| Kafka/NATS | 否 | NATS 可选 | Kafka 3 broker | Kafka 9+ |
| Elasticsearch | 否 | 单节点可选 | 3 节点 | 6+ 节点 |
| apiserver | 2 | 2-3 | 4+ HPA | 8+ HPA |
| syncer | 1 | 2 | 4+ 分片 | 20+ 分片 |
| ws-gateway | 1 | 2 | 4+ | 10+ |
| log-proxy | 否 | 是 | 全量 | 分片 |
| 对象存储 | MinIO 单节点 | MinIO 4 节点 | S3 托管 | S3 多区域 |
| Jenkins | 单实例 | HA + K8s agent | 多实例 | 多实例+池 |
| 管理集群节点 | 3×8C16G | 5×16C32G | 10×32C64G | 按需 |
| 预估月成本（云） | ¥5k-1w | ¥2-5w | ¥10-30w | ¥50w+ |

---

## 10. 升级与运维

### 10.1 升级

```bash
helm repo update
helm diff upgrade vortexops vortexops/vortexops -n vortexops -f values.yaml
helm upgrade vortexops vortexops/vortexops -n vortexops -f values.yaml
# 回滚
helm rollback vortexops <REVISION> -n vortexops
```

升级注意：

- Chart 含迁移 Job，升级时自动执行 schema 变更。
- **升级前备份 PG**（`pg_dump` 或 pgBackRest）。
- syncer 逐实例滚动，避免全量重连被管集群 API Server。
- DB 迁移可能不可逆，应用层可 helm rollback，但 schema 需配套回滚脚本。

### 10.2 监控

- 平台暴露 Prometheus 指标（`vortexops_*`），见 [扩展性 §16](scalability.md#16-监控指标)。
- ServiceMonitor 接入集群 Prometheus + Grafana 看板。
- 关键告警：API P99 >300ms、Informer lag >30s、PG 连接池 >80%、Redis 内存 >85%、Kafka lag >10000。

### 10.3 健康检查

| 端点 | 说明 |
| --- | --- |
| `/healthz` | 进程存活 |
| `/readyz` | DB + Redis 就绪 |
| syncer `/healthz` | 被管集群连接状态 |

### 10.4 常见运维命令

```bash
# 查看 apiserver 日志
kubectl -n vortexops logs -f deploy/vortexops-apiserver

# 调整日志级别
kubectl -n vortexops exec deploy/vortexops-apiserver -- \
  vortexops config set log.level debug

# 手动触发空间配额重算
kubectl -n vortexops exec deploy/vortexops-apiserver -- \
  vortexops admin recompute-quota --workspace <uuid>

# 清理过期构建日志
kubectl -n vortexops exec deploy/vortexops-apiserver -- \
  vortexops admin gc-logs --before 30d

# 查看 syncer 分片状态
kubectl -n vortexops exec deploy/vortexops-syncer -- \
  vortexops syncer status
```

---

## 11. 故障排查

| 现象 | 可能原因 | 排查 |
| --- | --- | --- |
| apiserver CrashLoop | DB 未就绪 / schema 未迁移 | 查日志；`vortexops migrate version` |
| syncer 集群离线 | kubeconfig 过期 / RBAC 不足 / 网络不通 | 测试 `kubectl --kubeconfig=... get ns` |
| Pod 列表空白 | Redis 缓存 miss + Informer 未同步 | 查 syncer Informer lag；Redis key `rt:pod:*` |
| 发布卡住 | K8s API 延迟 / 资源不足 / 并发锁 | 查 release 事件 + K8s Deployment status |
| 日志不显示 | log-proxy 未部署 / 对象存储不可达 | 查 log-proxy pod；测试 S3 endpoint |
| 对外 API 403 | Token 过期 / scope 不足 / IP 白名单 | 查 `vo_external_api_call_logs` |
| 推理 Pod Pending | GPU 不足 / 配额超限 / 无匹配节点 | 查 `kubectl describe pod`；空间 GPU 配额 |
| WS 频繁断连 | ws-gateway 副本不足 / Redis Pub/Sub 异常 | 查 `vortexops_ws_connections` 指标 |
| PG 连接耗尽 | 未用 PgBouncer / pool 配置过大 | 查 PG `pg_stat_activity`；调低 `dbPoolMax` |
| Citus 分片不可用 | worker 节点故障 | `SELECT * FROM citus_get_active_worker_nodes();` |

---

## 12. 从 S 到 XL 的演进路径

推荐渐进式演进，避免一步到位：

```
S（PoC 验证）
  │  应用 >500 / 多团队 / 需 HA
  ▼
M（部门生产）
  │  PG 主从 + PgBouncer + Redis Sentinel + log-proxy + NATS
  │  应用 >5k / 多 BU / 需搜索
  ▼
L（企业级）
  │  Citus 分片 + Redis Cluster + Kafka + ES + syncer 分片
  │  应用 >50k / 平台型
  ▼
XL（超大规模）
     Citus 多 worker + syncer 20+ + ws-gateway 10+ + 多区域灾备
```

每步演进只需修改 Helm values + 依赖组件扩容，**无需改业务代码**。分片启用步骤见 [schema.sql §14.3](../schema.sql) 与 [扩展性 §17](scalability.md#17-规模演进路线)。
