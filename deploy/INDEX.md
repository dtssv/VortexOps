# VortexOps 部署文档索引

VortexOps 的部署按**规模与形态**分为三层，覆盖从个人开发到 10 万级应用托管的全场景。请根据目标规模选择对应层级，每一层都包含完整的：应用部署、依赖组件部署、配置要求、网络设计、运维脚本。

> 📌 历史文档 `docs/deployment.md` 与 `deploy/DEPLOY-MODES.md` 保留作为参考。本索引下的三层文档为**正式部署指南**，推荐优先使用。

---

## 📊 层级总览

| 层级 | 目录 | 形态 | 规模 | 目标读者 |
|------|------|------|------|---------|
| **L1 开发环境** | [`01-dev-docker/`](./01-dev-docker/) | 单机 Docker Compose | 单租户、< 100 应用 | 开发、联调、POC |
| **L2 中型集群** | [`02-mid-bare-k8s/`](./02-mid-bare-k8s/) | 物理机 / 中型 K8s | 多租户、< 5,000 应用、单 Region | 企业生产、中型团队 |
| **L3 超大规模** | [`03-hyper-large/`](./03-hyper-large/) | 多 Region K8s + 分片 | 100k+ 应用、多 Region、多 AZ | 大规模 SaaS、集团级 |

---

## 🚀 快速选型

```
你的目标规模是？
├─ 开发联调 / 演示 POC …………………………… → L1  01-dev-docker
├─ 单团队生产 (< 1000 应用, 单 Region) ……… → L2  02-mid-bare-k8s
├─ 多团队生产 (1000 ~ 10000 应用, 单 Region) → L2  02-mid-bare-k8s (扩容副本)
├─ 集团级 / SaaS (10000 ~ 100000 应用) ……… → L3  03-hyper-large
└─ 超大规模 (100000+ 应用, 多 Region) ……… → L3  03-hyper-large (多 Region + 分片)
```

**升级路径**：L1 → L2 → L3，每层升级条件在各层 README 末尾"升级判断"章节定义。不可跨层直接升级，需按序执行数据迁移。

---

## 📁 目录结构

```
deploy/
├── INDEX.md                          ← 本文件
├── DEPLOY-MODES.md                   ← （历史）Docker Compose 部署模式说明
├── docker-compose.dev.yml            ← （历史）开发环境 compose
├── docker-compose.host-net.yml       ← （历史）host-net override
├── docker-compose.external.yml       ← （历史）external k8s override
│
├── 01-dev-docker/                    ← L1: 开发环境
│   ├── README.md                     完整部署指南
│   ├── config/                       环境配置模板（dev.env / prometheus / kubeconfig）
│   ├── scripts/                      up/down/reset/migrate/seed/healthcheck
│   └── verify/                       端到端冒烟测试
│
├── 02-mid-bare-k8s/                  ← L2: 中型集群（物理机 / K8s）
│   ├── README.md                     完整部署指南（含 bare 与 k8s 两套）
│   ├── manifests/
│   │   ├── helm-values/              K8s 三套环境 values（prod/staging/mid）
│   │   ├── deps/                     依赖组件 Helm values（PG/Redis/MinIO/NATS/OS/Jenkins/Harbor）
│   │   ├── network/                  NetworkPolicy
│   │   └── system/                   Ingress TLS / ServiceAccount RBAC
│   ├── bare/                         物理机部署资源
│   │   ├── systemd/                  各组件 systemd unit
│   │   ├── nginx/                    Nginx 反代 + TLS
│   │   ├── keepalived/               VIP 高可用
│   │   └── postgres/                 PG 物理机配置
│   └── scripts/                      build/deploy/init/backup/restore/rotate-certs
│
└── 03-hyper-large/                   ← L3: 超大规模（多 Region + 分片）
    ├── README.md                     完整部署指南
    ├── manifests/
    │   ├── helm-values/              多 Region values（region-a active / region-b standby）
    │   ├── citus-cluster.yaml        Citus 分布式 PG（8 worker + 2 coord HA）
    │   ├── redis-cluster.yaml        Redis Cluster（12 节点）
    │   ├── kafka-cluster.yaml        Strimzi Kafka（9 broker KRaft）
    │   ├── opensearch-cluster.yaml   OpenSearch（coord + data 分离）
    │   ├── syncer-shard/             syncer 分片 ConfigMap + Lease
    │   ├── ws-gateway/               WebSocket 会话亲和 + 背压
    │   ├── log-proxy/                业务集群边缘日志 DaemonSet
    │   ├── gpu/                      GPU 设备插件 / 节点池 / 权重 PVC
    │   ├── monitoring/               Prometheus 联邦 / Thanos / Grafana
    │   ├── network/                  跨 Region Ingress / NetworkPolicy / DNS 故障转移
    │   └── dr/                       跨 Region 复制 / 备份策略 / 配置漂移检测
    └── scripts/                      install-deps-xl / deploy-xl / init-citus-sharding /
                                      syncer-shard / bootstrap-region / onboard-clusters /
                                      model-weights / dr-failover / capacity-check
```

---

## 🧩 各层关键差异

### 依赖组件矩阵

| 组件 | L1 dev-docker | L2 mid-bare-k8s | L3 hyper-large |
|------|--------------|-----------------|----------------|
| PostgreSQL | 单机容器 | 主从复制 + PgBouncer | **Citus 分布式**（8 worker 分片） |
| Redis | 单机容器 | Sentinel 主备 | **Redis Cluster**（12 节点 6 主 6 从） |
| 消息队列 | 单机 Kafka / NATS | NATS JetStream 集群 | **Kafka 9 broker KRaft**（Strimzi） |
| 对象存储 | 单机 MinIO | MinIO 4 节点 EC | MinIO 多站点 + 跨 Region 复制 |
| 搜索引擎 | 单机 OpenSearch | OpenSearch 3 节点 | OpenSearch coord+data 分离 + ILM |
| CI/CD | 单机 Jenkins + DinD | Jenkins + K8s 动态 agent | Jenkins 多 Region + GPU agent |
| 镜像仓库 | — | Harbor + Trivy | Harbor 多 Region + 镜像同步 |
| 监控 | 单机 Prometheus | Prometheus + Grafana | **Prometheus 联邦 + Thanos** 长期存储 |

### 应用层差异

| 维度 | L1 | L2 | L3 |
|------|----|----|-----|
| apiserver 副本 | 1 | 2~3（HA） | 6+（多 AZ 拓扑分布） |
| syncer | 单进程 | 单进程（HA lease） | **分片**（16~64 shard，lease 选举） |
| ws-gateway | 1 | 2（HA） | 10+（sticky session + 背压） |
| log-proxy | — | — | 业务集群 **DaemonSet** 边缘采集 |
| 大模型推理 | — | — | GPU 节点池 + 权重共享 PVC |
| 高可用 | 无 | 主备 + VIP / PDB | 多 Region 主备 + DNS 故障转移 |
| 容灾 RTO/RPO | — | 小时级 | **RTO < 5min, RPO < 1min** |

---

## 📖 推荐阅读顺序

**新手（首次部署）**：
1. 本 INDEX.md（选型）
2. [`01-dev-docker/README.md`](./01-dev-docker/README.md)（理解组件与流程）
3. [`02-mid-bare-k8s/README.md`](./02-mid-bare-k8s/README.md)（生产部署）

**生产运维**：
1. [`02-mid-bare-k8s/README.md`](./02-mid-bare-k8s/README.md)（HA/DR/备份/监控）
2. [`03-hyper-large/README.md`](./03-hyper-large/README.md)（分片/多 Region/容灾）

**大规模扩容**：
1. [`03-hyper-large/README.md`](./03-hyper-large/README.md)（升级路径）
2. [`03-hyper-large/scripts/`](./03-hyper-large/scripts/)（自动化脚本）
3. [`03-hyper-large/manifests/`](./03-hyper-large/manifests/)（依赖组件配置）

---

## 🔗 相关文档

- 架构设计：[`docs/architecture.md`](../docs/architecture.md)
- 原始部署文档（参考）：[`docs/deployment.md`](../docs/deployment.md)
- Docker 部署模式（参考）：[`deploy/DEPLOY-MODES.md`](./DEPLOY-MODES.md)
- 可扩展性设计：[`docs/scalability.md`](../docs/scalability.md)
- Kubernetes 集成：[`docs/kubernetes.md`](../docs/kubernetes.md)

---

## ❓ FAQ

**Q: 我可以跳过 L2 直接从 L1 到 L3 吗？**
A: 不建议。L3 假设你已具备 L2 的运维能力（Helm、PG 主备、监控告警等）。建议先在 L2 完成至少一次生产部署与容灾演练，再升级到 L3。

**Q: 物理机部署（bare metal）还能用吗？**
A: 可以。L2 同时覆盖物理机（systemd + Nginx + keepalived）与 K8s 两种形态。L3 不再推荐物理机，因分片与多 Region 难以在物理机实现。

**Q: L3 的 syncer 分片数怎么定？**
A: 初始 16 个 shard（每 shard 管理约 600 个业务集群）。当单 shard CPU 持续 > 60% 或事件延迟 > 5s 时扩容。详见 [`03-hyper-large/scripts/syncer-shard.sh`](./03-hyper-large/scripts/syncer-shard.sh)。

**Q: 多 Region 是必须的吗？**
A: 不是。L3 单 Region 已能支撑 100k 应用。多 Region 用于**同城双活**或**异地容灾**，RTO/RPO 要求严格时才启用。

**Q: GPU 推理服务必须部署在平台集群吗？**
A: 不是。GPU 节点池可以部署在业务集群，平台通过 `log-proxy` 同款机制下发推理 Deployment。L3 文档中的 GPU manifests 适用于"平台托管推理"场景。
