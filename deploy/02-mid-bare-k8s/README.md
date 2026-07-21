# 02 — 中型集群部署（物理机 / K8s）

面向**部门级生产、多团队、需要 HA**的部署方案。涵盖两种部署形态：

- **2A 物理机裸机部署**：以 systemd + Nginx 直接在 3-5 台物理机 / VM 上运行，依赖组件（PG/Redis/...）外部托管或自建
- **2B Kubernetes 部署**：以 Helm Chart 部署到中型 K8s 集群（3-10 节点），依赖组件独立 namespace 管理

> 开发环境（单机 docker）见 [`../01-dev-docker/`](../01-dev-docker/README.md)。
> 超大规模（10 万+应用 / 多区域）见 [`../03-hyper-large/`](../03-hyper-large/README.md)。

---

## 1. 适用场景与规模

| 维度 | 量化 |
| --- | --- |
| 应用规模 | 500 - 5,000 个应用 / 50 - 1,000 人团队 |
| 被管业务 K8s 集群 | 5 - 50 个 |
| 日构建数 | 500 - 5,000 |
| 在线 WS 连接 | 500 - 5,000 |
| HA | 关键组件多副本，PG 主从 + PgBouncer，Redis Sentinel |
| RPO 目标 | < 1 分钟（PG 同步复制） |
| RTO 目标 | < 5 分钟 |

**与 03 层（超大规模）的边界**：应用数 > 5k、单集群节点 > 1000、需 Citus 分片 / Redis Cluster / Kafka / 跨 Region 灾备时，升级到 03 层。

---

## 2. 拓扑总览

### 2A 物理机裸机部署

```
                ┌────── Nginx / HAProxy (TLS) ──────┐
                │   vortexops.internal             │
                │   api.vortexops.internal         │
                └──────┬───────────────┬───────────┘
                       │               │
            ┌──────────▼───┐   ┌───────▼────────┐
            │ apiserver-01 │   │ ws-gateway-01  │
            │ apiserver-02 │   │ ws-gateway-02  │   systemd 服务
            │ frontend-01  │   │ syncer-01      │   keepalived VIP
            └──────┬───────┘   └───────┬────────┘
                   │                   │
        ┌──────────┴───────────────────┴──────────┐
        │                                          │
   ┌────▼────────────┐   ┌──────────────▼─────┐   ┌──────────────┐
   │ PostgreSQL 主从  │   │ Redis Sentinel     │   │ MinIO 4 节点  │
   │ (1 主 1 从)      │   │ (1 主 2 从 + 3 哨兵)│   │ (EC:2 纠删码) │
   │ + PgBouncer ×2  │   │                    │   │               │
   └─────────────────┘   └────────────────────┘   └──────────────┘

   独立部署：Jenkins (主从 agent) / Harbor / NATS (3 节点) / OpenSearch (3 节点，可选)
   被管业务集群群：5-50 个外部 K8s 集群（kubeconfig 接入）
```

### 2B Kubernetes 部署

```
                ┌────── Ingress (nginx, TLS) ──────┐
                │   vortexops.internal             │
                └──────┬───────────────────────────┘
                       │
            ┌──────────┴───────────────────────────┐
            │  namespace: vortexops                 │
            │  ├─ apiserver ×2 (HPA 2-6)            │
            │  ├─ ws-gateway ×2 (HPA 2-4)           │
            │  ├─ syncer ×2 (Lease 分片)            │
            │  ├─ ext-api-gw ×2                     │
            │  ├─ pipeline-worker ×2                │
            │  ├─ webhook ×2                        │
            │  └─ frontend ×2                       │
            └──────┬───────────────────────────────┘
                   │
        ┌──────────┴───────────────────────────────┐
        │  namespace: vortexops-infra               │
        │  ├─ postgresql (Bitnami, replication)    │
        │  ├─ pgbouncer ×2                          │
        │  ├─ redis (Bitnami, sentinel)             │
        │  ├─ minio ×4 (distributed)                │
        │  ├─ nats ×3 (jetstream)                   │
        │  ├─ opensearch ×3 (可选)                  │
        │  └─ prometheus + grafana                  │
        └───────────────────────────────────────────┘
                   │ kubeconfig
                   ▼
            5-50 个被管业务集群（独立部署 log-proxy）
```

---

## 3. 资源规格与硬件清单

### 3.1 物理机方案硬件清单

| 角色 | 节点数 | CPU | 内存 | 磁盘 | 网卡 |
| --- | --- | --- | --- | --- | --- |
| 负载均衡（Nginx + keepalived） | 2 | 4 核 | 8 GB | 100 GB SSD | 双千兆 |
| apiserver + ws-gateway + frontend | 2 | 16 核 | 32 GB | 200 GB SSD | 千兆 |
| syncer + ext-api-gw + webhook | 1-2 | 8 核 | 16 GB | 200 GB SSD | 千兆 |
| PostgreSQL 主 + 从 | 2 | 16 核 | 64 GB | 1 TB NVMe + 200 GB SSD（WAL） | 万兆 |
| PgBouncer | 2 | 4 核 | 4 GB | 50 GB | 千兆 |
| Redis 主 + 从 ×2 + Sentinel ×3 | 3-6 | 8 核 | 32 GB | 200 GB SSD | 千兆 |
| MinIO ×4 | 4 | 8 核 | 16 GB | 2 TB ×4（EC:2） | 万兆 |
| Jenkins controller + agent | 2 | 8 核 | 16 GB | 500 GB SSD | 千兆 |
| Harbor | 1 | 4 核 | 8 GB | 2 TB | 千兆 |
| NATS ×3（可选） | 3 | 4 核 | 8 GB | 100 GB | 千兆 |
| OpenSearch ×3（可选） | 3 | 8 核 | 32 GB | 500 GB SSD | 千兆 |
| **合计** | **17-22 台** | — | — | — | — |

### 3.2 K8s 方案集群规格

| 维度 | 推荐 |
| --- | --- |
| 集群节点数 | 5-10 节点（3 control-plane + 3-7 worker） |
| 单节点规格 | 16C / 64G / 500GB SSD |
| StorageClass | `ssd-retain`（PG/Redis）、`ssd`（MinIO/ES）、`hdd`（冷数据） |
| 网络 CNI | Calico（支持静态 IP）或 Cilium |
| Ingress Controller | nginx-ingress（推荐）或 traefik |
| 集群版本 | K8s ≥ 1.26 |

资源分配参考：

| 组件 | 副本 | CPU 请求/上限 | 内存 请求/上限 | 存储 |
| --- | --- | --- | --- | --- |
| apiserver | 2-3 | 1/2 | 1/2 Gi | — |
| ws-gateway | 2 | 0.5/1 | 1/2 Gi | — |
| syncer | 2 | 1/2 | 1/2 Gi | — |
| ext-api-gw | 2 | 0.25/0.5 | 256/512 Mi | — |
| pipeline-worker | 2 | 0.5/1 | 512Mi/1Gi | — |
| webhook | 2 | 0.25/0.5 | 128/256 Mi | — |
| frontend | 2 | 0.1/0.25 | 64/128 Mi | — |
| PostgreSQL 主 | 1 | 4/8 | 8/16 Gi | 200Gi SSD |
| PostgreSQL 从 | 1 | 2/4 | 4/8 Gi | 200Gi SSD |
| PgBouncer | 2 | 0.25/0.5 | 128/256 Mi | — |
| Redis Sentinel（3 节点） | 3 | 0.5/1 | 1/2 Gi | 20Gi |
| MinIO（4 节点） | 4 | 1/2 | 2/4 Gi | 500Gi/节点 |
| NATS（3 节点） | 3 | 0.5/1 | 512Mi/1Gi | 20Gi |
| OpenSearch（可选，3 节点） | 3 | 2/4 | 4/8 Gi | 100Gi |

---

## 4. 目录结构

```
deploy/02-mid-bare-k8s/
├── README.md                          # 本文档
├── manifests/                         # K8s 部署用清单
│   ├── helm-values/
│   │   ├── values-bare-prod.yaml      # 2A 物理机：apiserver 连外部 PG/Redis
│   │   ├── values-k8s-mid.yaml        # 2B K8s：完整中型规模 values
│   │   └── values-k8s-staging.yaml    # 预发环境 values（资源略小）
│   ├── deps/                          # 依赖组件 Helm 安装参数
│   │   ├── postgres-replication.yaml
│   │   ├── pgbouncer.yaml
│   │   ├── redis-sentinel.yaml
│   │   ├── minio-distributed.yaml
│   │   ├── nats-jetstream.yaml
│   │   ├── opensearch-3node.yaml
│   │   ├── jenkins-k8s-agent.yaml
│   │   └── harbor.yaml
│   ├── network/
│   │   ├── networkpolicy-apiserver.yaml
│   │   ├── networkpolicy-ext-api.yaml
│   │   └── networkpolicy-syncer.yaml
│   └── system/
│       ├── nginx-ingress-tls.yaml     # Ingress + cert-manager
│       └── service-rbac.yaml          # 平台 ServiceAccount + RBAC
├── bare/                              # 2A 物理机方案专用
│   ├── nginx/
│   │   ├── vortexops.conf             # Nginx 反向代理（SSE / WS 支持）
│   │   └── tls.conf                   # TLS 配置片段
│   ├── systemd/
│   │   ├── vortexops-apiserver.service
│   │   ├── vortexops-ws-gateway.service
│   │   ├── vortexops-syncer.service
│   │   ├── vortexops-webhook.service
│   │   ├── vortexops-pipeline-worker.service
│   │   └── vortexops-frontend.service
│   ├── keepalived/
│   │   └── keepalived.conf            # VIP 配置
│   └── postgres/
│       ├── postgresql.conf            # PG 主配置（含 pgvector）
│       └── pg_hba.conf                # 客户端访问控制
└── scripts/
    ├── build-images.sh                # 构建并推送所有镜像
    ├── deploy-k8s.sh                  # 2B: Helm 部署 / 升级 / 卸载
    ├── install-bare.sh                # 2A: 安装单个 systemd 服务
    ├── install-deps-k8s.sh            # 2B: 一键安装依赖组件（PG/Redis/MinIO/...）
    ├── install-deps-bare.sh           # 2A: 物理机安装 PG/Redis/MinIO
    ├── init-db.sh                     # 初始化 schema + admin 用户
    ├── rotate-certs.sh                # 证书轮换
    ├── backup-db.sh                   # PG 逻辑备份
    └── restore-db.sh                  # PG 恢复
```

---

## 5. 前置条件

### 5.1 软件版本

| 软件 | 版本 | 用途 |
| --- | --- | --- |
| Docker / containerd | ≥ 24 / 1.7 | 镜像构建与运行 |
| Helm | ≥ 3.12 | K8s 部署 |
| kubectl | 与集群版本匹配 | K8s 操作（2B） |
| Go | 1.22+（构建机） | 后端编译 |
| Node.js | 20+（构建机） | 前端构建 |
| PostgreSQL | 16 + pgvector | 元数据 |
| Redis | 7+ | 缓存 |
| Nginx | 1.24+ | 反向代理（2A） |
| keepalived | 2.2+ | VIP（2A） |

### 5.2 网络与域名

- **管理网段**：内部组件互通，建议万兆
- **业务网段**：被管 K8s 集群接入，可经专线 / VPN
- **域名**（生产示例）：
  - `vortexops.internal` — 前端 UI
  - `api.vortexops.internal` — 内部 API
  - `ext.vortexops.internal` — 对外 API（独立域名）
  - `ws.vortexops.internal` — WebSocket
- **TLS 证书**：生产强制 TLS 1.2+，推荐 cert-manager + Let's Encrypt 或企业 CA

### 5.3 镜像仓库

- 私有 Harbor 或 ACR，存放 VortexOps 镜像与业务镜像
- 推荐开 CVE 扫描与签名（cosign）

---

## 6. 2A — 物理机裸机部署

### 6.1 总体流程

```
1. 在所有节点创建 vortexops 系统用户与目录
2. 部署依赖：PostgreSQL 主从、Redis Sentinel、MinIO、Jenkins、Harbor
3. 构建并分发二进制（或镜像）
4. 部署 systemd 服务（apiserver / ws-gateway / syncer / ...）
5. 配置 Nginx + keepalived
6. 初始化数据库 schema + admin 用户
7. 健康检查 + 接入业务集群
```

### 6.2 构建二进制

在构建机执行：

```bash
./scripts/build-images.sh --target bare --version 1.0.0
# 产物：
#   dist/vortexops-1.0.0-linux-amd64
#   dist/vortexops-syncer-1.0.0-linux-amd64
#   dist/vortexops-ws-gateway-1.0.0-linux-amd64
#   dist/vortexops-webhook-1.0.0-linux-amd64
#   dist/vortexops-pipeline-worker-1.0.0-linux-amd64
#   dist/frontend-1.0.0.tar.gz  (nginx 静态资源)
```

### 6.3 安装依赖组件

执行 `./scripts/install-deps-bare.sh`，自动完成：
- PostgreSQL 16 + pgvector 主从部署（含 `postgresql.conf` / `pg_hba.conf`）
- Redis 7 主从 + Sentinel 3 节点
- MinIO 4 节点分布式（纠删码 EC:2）
- Jenkins controller + agent
- 可选：NATS / OpenSearch

> 物理机方案 PG 主配置与 `pg_hba.conf` 模板见 [`bare/postgres/`](bare/postgres/)。

### 6.4 安装平台服务

将二进制与 systemd unit 分发到对应节点后执行：

```bash
# 在 apiserver 节点
sudo ./scripts/install-bare.sh \
  --binary /tmp/vortexops-1.0.0-linux-amd64 \
  --service apiserver \
  --env-file /etc/vortexops/vortexops.env

# 在 ws-gateway 节点
sudo ./scripts/install-bare.sh \
  --binary /tmp/vortexops-ws-gateway-1.0.0-linux-amd64 \
  --service ws-gateway \
  --env-file /etc/vortexops/vortexops.env

# syncer / webhook / pipeline-worker 同理
```

systemd unit 模板见 [`bare/systemd/`](bare/systemd/)。`install-bare.sh` 会：
1. 创建 `/opt/vortexops/{bin,conf,logs}` 目录
2. 复制二进制到 `/opt/vortexops/bin/`
3. 安装 systemd unit 到 `/etc/systemd/system/`
4. `systemctl daemon-reload && systemctl enable <service>`

### 6.5 配置 Nginx + keepalived

将 [`bare/nginx/vortexops.conf`](bare/nginx/vortexops.conf) 复制到 `/etc/nginx/conf.d/`，修改 `server_name` 与 upstream 地址。

keepalived 配置 [`bare/keepalived/keepalived.conf`](bare/keepalived/keepalived.conf) 提供VIP，主备切换保证入口高可用。

### 6.6 初始化数据库

```bash
./scripts/init-db.sh \
  --pg-host 192.168.1.20 \
  --pg-user vortexops \
  --pg-password 'CHANGE_ME' \
  --admin-password 'CHANGE_ME_ADMIN'
```

执行 schema.sql + 创建 admin 用户。

### 6.7 启动与验证

```bash
sudo systemctl start vortexops-apiserver vortexops-ws-gateway vortexops-syncer
sudo systemctl status vortexops-apiserver
curl -sf https://api.vortexops.internal/api/v1/healthz
```

---

## 7. 2B — Kubernetes 部署

### 7.1 总体流程

```
1. 准备 K8s 集群（已安装 Ingress、CSI、cert-manager）
2. 创建 namespace：vortexops / vortexops-infra
3. 安装依赖组件到 vortexops-infra
4. 构建 VortexOps 镜像并推送到镜像仓库
5. 准备 Secret（DB/Redis/S3 凭证、JWT、加密 key）
6. Helm install vortexops 到 vortexops namespace
7. 等 rollout 就绪
8. 初始化数据库 + admin
9. 接入业务集群
```

### 7.2 安装依赖组件

```bash
# 一键安装所有依赖到 vortexops-infra namespace
./scripts/install-deps-k8s.sh --all

# 或单独安装
./scripts/install-deps-k8s.sh --component postgres
./scripts/install-deps-k8s.sh --component redis
./scripts/install-deps-k8s.sh --component minio
./scripts/install-deps-k8s.sh --component nats
./scripts/install-deps-k8s.sh --component opensearch
./scripts/install-deps-k8s.sh --component jenkins
./scripts/install-deps-k8s.sh --component harbor
```

各组件的 Helm values 见 [`manifests/deps/`](manifests/deps/)。安装后产出连接串：

```yaml
# 连接串（供 values-k8s-mid.yaml 填入）
postgresql:
  host: vortexops-pg-postgresql.vortexops-infra.svc
  port: 5432
  database: vortexops
  username: vortexops
  existingSecret: pg-creds       # 含 password

redis:
  host: vortexops-redis-master.vortexops-infra.svc
  existingSecret: redis-creds   # 含 password

objectStorage:
  endpoint: http://minio.vortexops-infra.svc:9000
  bucket: vortexops
  existingSecret: s3-creds      # 含 accessKey + secretKey
  pathStyle: true
```

### 7.3 构建并推送镜像

```bash
./scripts/build-images.sh --target k8s \
  --registry registry.vortexops.io \
  --version 1.0.0
```

构建以下镜像（多架构 amd64/arm64）：

| 镜像 | Dockerfile |
| --- | --- |
| `registry.vortexops.io/vortexops/apiserver:1.0.0` | `Dockerfile` |
| `registry.vortexops.io/vortexops/syncer:1.0.0` | `Dockerfile.syncer` |
| `registry.vortexops.io/vortexops/ws-gateway:1.0.0` | `Dockerfile.ws-gateway` |
| `registry.vortexops.io/vortexops/webhook:1.0.0` | `Dockerfile.webhook` |
| `registry.vortexops.io/vortexops/pipeline-worker:1.0.0` | `Dockerfile.pipeline-worker` |
| `registry.vortexops.io/vortexops/frontend:1.0.0` | `frontend/Dockerfile` |

### 7.4 准备 Secret

```bash
kubectl -n vortexops create secret generic pg-creds \
  --from-literal=password='CHANGE_ME_PG_PASSWORD' \
  --from-literal=username=vortexops

kubectl -n vortexops create secret generic redis-creds \
  --from-literal=password='CHANGE_ME_REDIS_PASSWORD'

kubectl -n vortexops create secret generic s3-creds \
  --from-literal=accessKey='admin' \
  --from-literal=secretKey='CHANGE_ME_S3_SECRET'

kubectl -n vortexops create secret generic jwt-secret \
  --from-literal=key="$(openssl rand -base64 32)"

kubectl -n vortexops create secret generic kms-key \
  --from-literal=key="$(openssl rand -hex 16)"   # 32 字节 AES 密钥
```

生产推荐用 External Secrets Operator + Vault / KMS 注入，密钥不入 Git。

### 7.5 部署 VortexOps

```bash
./scripts/deploy-k8s.sh \
  --namespace vortexops \
  --tag 1.0.0 \
  --values manifests/helm-values/values-k8s-mid.yaml
```

脚本内部执行：

```bash
helm upgrade --install vortexops ../../helm \
  --namespace vortexops --create-namespace \
  --set image.tag=1.0.0 \
  -f manifests/helm-values/values-k8s-mid.yaml

kubectl -n vortexops rollout status deploy/vortexops-apiserver --timeout=300s
```

values 文件见 [`manifests/helm-values/values-k8s-mid.yaml`](manifests/helm-values/values-k8s-mid.yaml)。

### 7.6 初始化数据库

```bash
./scripts/init-db.sh --k8s --namespace vortexops
# 等价于：
# kubectl -n vortexops exec deploy/vortexops-apiserver -- \
#   vortexops migrate up
# kubectl -n vortexops exec deploy/vortexops-apiserver -- \
#   vortexops bootstrap-admin --username admin --password 'CHANGE_ME' --email admin@corp
```

### 7.7 接入业务集群

1. 在被管集群创建 ServiceAccount + RBAC（最小权限，见 [`manifests/system/service-rbac.yaml`](manifests/system/service-rbac.yaml)）。
2. VortexOps 系统管理 → 集群 → 接入：上传 kubeconfig，选 standard/edge 模式。
3. syncer 建立 Informer，同步节点池 / IP 池。
4. 空间绑定集群 + Namespace 即可发布。

**M 规模起在被管集群部署 log-proxy**：

```bash
helm install vortexops-log-proxy vortexops/log-proxy \
  -n vortexops-edge --create-namespace \
  --set platformEndpoint=https://api.vortexops.internal \
  --set clusterId=<cluster-uuid>
```

---

## 8. 网络设计

### 8.1 流量路径

| 流量 | 路径 | 端口 / TLS |
| --- | --- | --- |
| 用户 UI | LB/Nginx → frontend | 443 TLS |
| 内部 API | LB/Nginx → apiserver | 443 TLS |
| 对外 API | LB → ext-api-gw（独立域名） | 443 TLS |
| WebSocket | LB/Nginx → ws-gateway（sticky session） | 443 WSS |
| 平台 → 被管集群 | syncer → K8s API Server | 6443 TLS |
| 日志/exec | ws-gateway → log-proxy → K8s API | 443 mTLS（边缘） |
| 平台 → Jenkins | apiserver → Jenkins REST | 443/8080 |
| 平台 → Harbor | apiserver → Registry API | 443 |

### 8.2 NetworkPolicy（K8s 方案）

- [`manifests/network/networkpolicy-apiserver.yaml`](manifests/network/networkpolicy-apiserver.yaml)：apiserver 仅允许 Ingress + 内部组件入站
- [`manifests/network/networkpolicy-ext-api.yaml`](manifests/network/networkpolicy-ext-api.yaml)：ext-api-gw 独立策略
- [`manifests/network/networkpolicy-syncer.yaml`](manifests/network/networkpolicy-syncer.yaml)：syncer 出站到所有被管集群 API Server 白名单
- 依赖组件（PG/Redis/Kafka）仅允许 vortexops namespace 入站

### 8.3 DNS

- 平台内部（K8s）：`*.vortexops-infra.svc.cluster.local`
- 对外：`vortexops.internal`、`api.vortexops.internal`、`ext.vortexops.internal`、`ws.vortexops.internal`

### 8.4 Nginx 反向代理关键配置

SSE / WebSocket 长连接需关闭 buffer、增大 timeout：

```nginx
location /api/v1/ {
    proxy_pass http://apiserver_upstream;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    # SSE
    proxy_buffering off;
    proxy_read_timeout 120s;
    proxy_http_version 1.1;
    proxy_set_header Connection '';
    chunked_transfer_encoding off;
}
location /ws {
    proxy_pass http://ws_gateway_upstream;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;       # WS 长连接
    ip_hash;                        # sticky session
}
```

完整配置见 [`bare/nginx/vortexops.conf`](bare/nginx/vortexops.conf)。

---

## 9. 存储设计

### 9.1 StorageClass（K8s）

| 用途 | StorageClass | 说明 |
| --- | --- | --- |
| PostgreSQL | `ssd-retain` | Retain 策略，高性能 SSD |
| Redis | `ssd-retain` | 低延迟 |
| MinIO | `ssd` 或 `hdd` | 按 IOPS 需求 |
| OpenSearch | `ssd` | 检索性能 |
| 备份 | 对象存储 | 不用 PV |

### 9.2 数据分层

| 层 | 存储 | 数据 |
| --- | --- | --- |
| 热 | Redis | Pod 运行态、限流、WS 路由 |
| 温 | PostgreSQL | 业务元数据、期望态 |
| 冷 | 对象存储 | 构建日志、SBOM、配置快照 |
| 检索 | OpenSearch | 审计 / 日志全文索引（可选） |

### 9.3 容量规划

| 规模 | PG 磁盘 | Redis 内存 | 对象存储 | ES 磁盘 |
| --- | --- | --- | --- | --- |
| M（中型） | 200 Gi | 6 Gi | 2 Ti | 100 Gi |

---

## 10. 高可用与灾备

### 10.1 组件 HA 矩阵

| 组件 | HA 方案 | RPO | RTO |
| --- | --- | --- | --- |
| apiserver / ws-gateway / frontend | 多副本 + HPA + PDB | 0 | <1min |
| syncer | 多副本 + Lease 分片 + PDB | 0（缓存可重建） | <5min |
| PostgreSQL | 主从（同步复制） + Patroni | <1min | <5min |
| Redis | Sentinel 自动主从切换 | 秒级（AOF） | <2min |
| MinIO | 纠删码 EC:2 | 0 | <5min |
| Jenkins | controller HA + agent 池 | 0 | <10min |

### 10.2 备份策略

| 数据 | 方式 | 频率 | 保留 |
| --- | --- | --- | --- |
| PostgreSQL | pg_dump 逻辑 + pgBackRest 物理 + WAL | 日全量 + WAL 持续 | 30 天 |
| Redis | RDB 快照（限流/锁 key） | 每小时 | 7 天 |
| 对象存储 | 版本化 + 跨区域复制（如跨机房） | 持续 | 按生命周期 |
| Chart values / Secret | Git + Sealed Secrets | 每次变更 | 永久 |

备份脚本：

```bash
./scripts/backup-db.sh --pg-host 192.168.1.20 --backup-dir /backups
# 或 K8s：用 pgBackRest sidecar / CronJob
```

恢复：

```bash
./scripts/restore-db.sh --pg-host 192.168.1.20 --dump-file /backups/vortexops_20260716.dump
# 重启平台组件
sudo systemctl restart vortexops-apiserver vortexops-syncer
# 或 K8s：
kubectl -n vortexops rollout restart deploy/vortexops-apiserver deploy/vortexops-syncer
```

### 10.3 降级预案

| 故障 | 影响 | 降级 |
| --- | --- | --- |
| 某被管集群不可达 | 该集群操作不可用 | 标记离线，其他集群正常 |
| Redis 不可用 | Pod 列表延迟 | 回源 Informer 或提示「数据可能过期」 |
| OpenSearch 不可用 | 全局搜索不可用 | 降级 `pg_trgm` 或关闭搜索 |
| PG 从库不可用 | 读延迟上升 | 读切主库 |

---

## 11. 安全加固

### 11.1 密钥与凭证

| 密钥 | 存储 | 轮换 |
| --- | --- | --- |
| JWT signing key | K8s Secret / Vault | 90 天 |
| 凭证加密 key（kms-key） | K8s Secret / KMS | 180 天（需 re-encrypt） |
| DB/Redis/S3 密码 | K8s Secret / External Secrets | 90 天 |
| kubeconfig（被管集群） | PG 加密存储 | 按需 |
| API Token（voe_） | PG 哈希存储 | 用户自助轮换 |

生产建议对接 **Vault / AWS KMS** 做信封加密，密钥不入 Git。

### 11.2 TLS 与 mTLS

- Ingress 强制 TLS 1.2+，cert-manager 自动签发
- 内部组件 mTLS（K8s 方案推荐）：apiserver ↔ syncer ↔ log-proxy 双向证书
- 被管集群 kubeconfig 使用 client cert 或 token，最小 RBAC

### 11.3 审计与合规

- 平台操作全量审计 → `vo_audit_logs`（按月分区）
- 对外 API 独立审计 → `vo_external_api_call_logs`
- 敏感字段（Secret/Token）默认掩码，查看需额外权限 + 审计

### 11.4 镜像安全

- 平台镜像 CI 扫描（Trivy），Critical CVE 阻断发布
- 业务镜像 CVE 准入策略：`systemSettings.cve.defaultPolicy`（warn/block_critical/block_high）

---

## 12. 监控与运维

### 12.1 监控

- 平台暴露 Prometheus 指标（`vortexops_*`）
- ServiceMonitor 接入集群 Prometheus + Grafana 看板
- 关键告警：
  - API P99 > 300ms
  - Informer lag > 30s
  - PG 连接池 > 80%
  - Redis 内存 > 85%
  - PG 复制延迟 > 5s
  - 磁盘使用 > 85%

### 12.2 健康检查

| 端点 | 说明 |
| --- | --- |
| `/healthz` | 进程存活 |
| `/readyz` | DB + Redis 就绪 |
| syncer `/healthz` | 被管集群连接状态 |

### 12.3 常见运维命令

```bash
# 物理机方案
sudo journalctl -u vortexops-apiserver -f
sudo systemctl restart vortexops-apiserver

# K8s 方案
kubectl -n vortexops logs -f deploy/vortexops-apiserver
kubectl -n vortexops exec deploy/vortexops-apiserver -- vortexops config set log.level debug
kubectl -n vortexops exec deploy/vortexops-apiserver -- vortexops admin recompute-quota --workspace <uuid>
kubectl -n vortexops exec deploy/vortexops-apiserver -- vortexops admin gc-logs --before 30d
kubectl -n vortexops exec deploy/vortexops-syncer -- vortexops syncer status
```

### 12.4 升级流程

```bash
# K8s
helm repo update
helm diff upgrade vortexops ../../helm -n vortexops -f manifests/helm-values/values-k8s-mid.yaml
helm upgrade vortexops ../../helm -n vortexops -f manifests/helm-values/values-k8s-mid.yaml
# 回滚
helm rollback vortexops <REVISION> -n vortexops
```

升级注意：
- Chart 含迁移 Job，升级时自动执行 schema 变更
- **升级前备份 PG**
- syncer 逐实例滚动，避免全量重连被管集群 API Server
- DB 迁移可能不可逆，应用层可 helm rollback，但 schema 需配套回滚脚本

---

## 13. 常见问题

| 现象 | 原因 | 解决 |
| --- | --- | --- |
| apiserver CrashLoop | DB 未就绪 / schema 未迁移 | 查日志；`vortexops migrate version` |
| syncer 集群离线 | kubeconfig 过期 / RBAC 不足 / 网络不通 | `kubectl --kubeconfig=... get ns` |
| Pod 列表空白 | Redis 缓存 miss + Informer 未同步 | 查 syncer Informer lag；Redis key `rt:pod:*` |
| 发布卡住 | K8s API 延迟 / 资源不足 / 并发锁 | 查 release 事件 + Deployment status |
| 日志不显示 | log-proxy 未部署 / 对象存储不可达 | 查 log-proxy pod；测试 S3 endpoint |
| 对外 API 403 | Token 过期 / scope 不足 / IP 白名单 | 查 `vo_external_api_call_logs` |
| WS 频繁断连 | ws-gateway 副本不足 / Nginx timeout 太短 | 增加副本；调大 `proxy_read_timeout` |
| PG 连接耗尽 | 未用 PgBouncer / pool 配置过大 | 查 `pg_stat_activity`；调低 `dbPoolMax` |
| Nginx SSE 客户端断开 | `proxy_buffering` 未关 / `proxy_read_timeout` 太短 | 关 buffering，调大 timeout 到 ≥ 120s |

---

## 14. 升级到 03 层（超大规模）的判断标准

满足以下任一条件，应升级到 [`../03-hyper-large/`](../03-hyper-large/README.md)：

- 应用数 > 5,000
- 单分组副本 > 1,000（大副本发布观察需采样）
- 单被管集群节点 > 1,000（Informer 分片）
- 被管业务集群 > 50 个
- 在线 WS 连接 > 5,000
- 需要跨 Region 灾备
- PG 单库写入瓶颈（需 Citus 分片）
- 需要 Kafka 异步审计 / 计量

升级路径：**数据模型分片键从 M1 起已落实**（workspace_id / cluster_id），因此从 02 升级到 03 只需扩容依赖组件 + 修改 Helm values，无需改业务代码。详见 03 层文档。
