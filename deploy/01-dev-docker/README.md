# 01 — 开发环境 Docker 部署

面向**单机本地开发、PoC 验证、功能联调**的零依赖部署方案。在一台开发机（Windows / macOS / Linux）上用 `docker compose` 一键拉起 VortexOps 全栈：平台后端、前端、数据库、缓存、消息队列、对象存储、搜索引擎、Jenkins、镜像仓库、k3s 业务集群、Prometheus 监控。

> 中型集群（多节点物理机 / K8s）部署见 [`../02-mid-bare-k8s/`](../02-mid-bare-k8s/README.md)。
> 超大规模集群（10 万+应用 / 多区域）部署见 [`../03-hyper-large/`](../03-hyper-large/README.md)。

---

## 1. 适用场景与规模

| 维度 | 量化 |
| --- | --- |
| 应用规模 | < 500 个应用 / < 50 人团队 |
| 部署形态 | 单台开发机（物理机或 VM），全部服务以容器形式运行在 docker compose 中 |
| 业务 K8s 集群 | 1 个内嵌 k3s（容器化），NodePort 范围 30000-30099 |
| HA | 无（单点，所有组件单副本） |
| 数据持久化 | docker volume（位于 Docker Desktop VM / 宿主机 Docker 数据目录） |
| 网络 | docker bridge（默认）或 host-net（underlay 验证） |

**不适用**：生产环境、性能压测、多用户并发、跨集群联调。上述场景请升级到 02 / 03 层。

---

## 2. 前置条件

### 2.1 硬件

| 项 | 最低 | 推荐 |
| --- | --- | --- |
| CPU | 4 核 | 8 核 |
| 内存 | 8 GB | 16 GB（k3s + Jenkins + ES 占用较高） |
| 磁盘 | 30 GB 可用 | 50 GB SSD |
| 网络 | 可访问公网拉镜像 | 国内可配 registry mirror |

### 2.2 软件

| 软件 | 版本 | 说明 |
| --- | --- | --- |
| Docker Desktop | ≥ 4.34（含 Docker Engine 24+、Compose v2） | Windows/macOS 推荐；Linux 用 docker-ce + docker-compose-plugin |
| Git | 任意 | 拉取代码 |
| curl / wget | 任意 | 验证健康检查 |
| 代码仓库 | — | VortexOps 源码（含 `Dockerfile`、`frontend/Dockerfile`、`deploy/`） |

> Docker Desktop（Windows/macOS）需在 Settings → Resources 中分配 ≥ 8 GB 内存、≥ 4 CPU、≥ 64 GB 磁盘。
> Linux 主机若用 rootless docker，需保证 `subuid/subgid` 配置正确。

### 2.3 端口占用

宿主机端口映射（见 `docker-compose.dev.yml`），启动前确保未被占用：

| 端口 | 服务 |
| --- | --- |
| 8080 | apiserver（VortexOps 后端 API） |
| 8081 | ws-gateway（WebSocket） |
| 8082 | Jenkins Web UI |
| 8083 | Docker Registry |
| 8088 | frontend（nginx） |
| 8090 | JumpServer Web |
| 2222 / 3389 | JumpServer koko / lion |
| 5432 | PostgreSQL |
| 6379 | Redis |
| 8070 | JumpServer core |
| 8443 | webhook（稳定 IP 注入，仅 `--profile full`） |
| 9000 / 9001 | MinIO API / Console |
| 9090 | Prometheus |
| 9200 | OpenSearch |
| 6443 | k3s API server |
| 30000-30099 | k3s NodePort（业务服务暴露） |

---

## 3. 目录结构

本目录是开发部署的工作区，**引用**仓库根的 `Dockerfile` / `frontend/Dockerfile`，并补充本目录新增的便捷脚本与配置模板。

```
deploy/01-dev-docker/
├── README.md                       # 本文档
├── scripts/
│   ├── up.sh                       # 启动全栈（bridge 模式，默认）
│   ├── up-host-net.sh              # 启动全栈（host-net 模式，验证 underlay）
│   ├── up-external.sh              # 连接外部 k3s 集群模式
│   ├── down.sh                     # 停止并保留数据
│   ├── reset.sh                    # 停止并清空所有 volume（危险）
│   ├── migrate.sh                  # 触发 schema 迁移
│   ├── seed.sh                     # 注入 Mock 用户 + 默认 Jenkins/Registry 集成
│   ├── logs.sh                     # 跟踪 apiserver 日志
│   └── healthcheck.sh              # 全栈健康巡检
├── config/
│   ├── dev.env.template            # 开发环境变量模板（无敏感数据，可直接复制）
│   ├── prometheus.yml              # 开发 Prometheus 抓取配置（覆盖 deploy/prometheus/）
│   └── external-kubeconfig.example # external 模式 kubeconfig 模板
└── verify/
    └── smoke.sh                    # 冒烟测试：登录 → 建空间 → 建应用 → 触发构建 → 发布
```

> 本目录的 `scripts/up.sh` 实际调用仓库根 `deploy/docker-compose.dev.yml`，不做复制，保证与主线 compose 文件同步升级。

---

## 4. 快速开始（5 分钟拉起全栈）

### 4.1 准备环境变量

```bash
cd deploy/01-dev-docker
cp config/dev.env.template config/dev.env
# 开发环境默认值无需修改；如需切换 CNI / underlay，编辑 config/dev.env
```

### 4.2 启动

```bash
./scripts/up.sh
```

脚本内部执行：

```bash
docker compose \
  --env-file config/dev.env \
  -f ../docker-compose.dev.yml \
  up -d
```

首次启动会拉镜像并构建本地镜像，预计 5-15 分钟（取决于网络）。关键依赖（postgres / redis / kafka / minio / elasticsearch / jenkins / k8s）通过 `healthcheck` 串行等待就绪后，`apiserver` 才启动。

### 4.3 初始化数据库 schema

```bash
./scripts/migrate.sh
```

等价于：

```bash
docker compose -f ../docker-compose.dev.yml run --rm migrate
```

### 4.4 注入 Mock 用户与默认集成

```bash
./scripts/seed.sh
```

等价于执行 `db-seed`（admin/admin123）+ `integration-seed`（自动登记 compose 内的 Jenkins、Registry）。完成后即可登录系统。

### 4.5 访问

| 入口 | 地址 | 凭证 |
| --- | --- | --- |
| 前端 | http://localhost:8088 | admin / admin123 |
| API | http://localhost:8080/api/v1 | — |
| Swagger | http://localhost:8080/swagger | — |
| Jenkins | http://localhost:8082 | admin / vortexops_dev |
| MinIO Console | http://localhost:9001 | admin / vortexops_dev |
| OpenSearch | http://localhost:9200 | 无认证 |
| Prometheus | http://localhost:9090 | — |
| k3s kubeconfig | `deploy/export/kubeconfig-localhost.yaml`（host-net 模式为 `kubeconfig-vortexops.yaml`） | — |
| JumpServer | http://localhost:8090 | admin / change-me |

### 4.6 验证

```bash
./scripts/healthcheck.sh
```

输出每个组件的健康状态，全部 `OK` 即部署成功。

```bash
./scripts/logs.sh apiserver    # 跟踪某服务日志
./scripts/logs.sh k8s          # k3s 日志
```

---

## 5. 部署模式切换

`docker-compose.dev.yml` 默认 **bridge** 模式；可通过 override 文件切换到 **host-net** 或 **external** 模式。三种模式对应不同网络验证场景（详见 [`../DEPLOY-MODES.md`](../DEPLOY-MODES.md)）。

### 5.1 bridge 模式（默认）

```bash
./scripts/up.sh
```

- k3s 跑在 docker bridge 网络
- Pod 使用 Calico（默认）/ Flannel overlay
- Pod IP 仅容器内可达
- 适用于：流程验证、不关心 Pod 网络可达性

### 5.2 host-net 模式（underlay 验证）

k3s 容器共享宿主机网络栈，叠加 Multus + Macvlan，让带 underlay 注解的 Pod 直接拿到物理局域网 IP。

```bash
# 编辑 config/dev.env 覆盖默认网络参数
export K3S_UNDERLAY_PARENT_IFACE=eth0
export K3S_UNDERLAY_SUBNET=192.168.1.0/24
export K3S_UNDERLAY_GATEWAY=192.168.1.1
export K3S_UNDERLAY_RANGE_START=192.168.1.200
export K3S_UNDERLAY_RANGE_END=192.168.1.250

./scripts/up-host-net.sh

# 首次启动后，初始化集群网络画像与 IP 池
docker compose -f ../docker-compose.dev.yml -f ../docker-compose.host-net.yml \
  run --rm underlay-seed
```

- 适用于：验证 VIP、直连 Pod IP、稳定 IP 注入
- 注意：Docker Desktop（WSL2）下 macvlan 父接口默认是 VM 内 `eth0`，物理二层可能仍隔离；真正 LAN 可达需用 external 模式对接物理机 k3s

### 5.3 external 模式（连接物理机 k3s）

不在 docker 内启动 k3s，而是连接已存在的物理机 k3s 集群。

```bash
# 1. 准备 kubeconfig
cp config/external-kubeconfig.example ../external-kubeconfig.yaml
# 编辑：填入外部集群 server 地址与证书

# 2. 启动
export VORTEXOPS_K8S_API_SERVER=https://192.168.1.10:6443
./scripts/up-external.sh
```

- 适用于：物理机多节点 k3s、真实 underlay 网络、生产前完整联调
- 外部集群需提前安装 Multus + Macvlan NAD（如需 underlay）

### 5.4 切换模式注意

切换模式前必须**完全停止当前栈并清空 k3s 状态卷**（不同模式间 k3s 状态不通用）：

```bash
./scripts/reset.sh     # 危险：清空所有 volume
# 再启动另一种模式
./scripts/up.sh
```

---

## 6. 依赖组件清单

开发环境的依赖组件全部内嵌在 `docker-compose.dev.yml` 中，由 docker compose 统一编排：

| 组件 | 镜像 | 角色 | 端口 | volume |
| --- | --- | --- | --- | --- |
| PostgreSQL 16 + pgvector | `pgvector/pgvector:pg16` | 业务元数据事实源 | 5432 | `pgdata` |
| Redis 7 | `redis:7-alpine` | 缓存 / 锁 / 限流 / Pub/Sub | 6379 | — |
| Kafka 3.7 (KRaft) | `apache/kafka:3.7.1` | 异步事件 | 9092 | — |
| MinIO | `minio/minio:RELEASE.2024-12-18` | 对象存储 | 9000 / 9001 | `miniodata` |
| OpenSearch 2.18 | `opensearchproject/opensearch:2.18.0` | 全文检索 | 9200 | `esdata` |
| Jenkins | 本地构建 `Dockerfile.jenkins` | 构建执行器 | 8082 / 50000 | `jenkinsdata` |
| Docker-in-Docker Builder | `docker:24-dind` | Jenkins 远程 docker daemon | — | `builderdata`, `buildworkspace` |
| Docker Registry v2 | `registry:2` | 镜像仓库 | 8083→5000 | `registrydata` |
| k3s | 本地构建 `Dockerfile.k3s-dev` | 业务 K8s 集群 | 6443 / 30000-30099 | `k8sstate`, `k8sdata` |
| Prometheus | `prom/prometheus:v2.55.1` | 监控后端 | 9090 | `prometheusdata` |
| JumpServer（可选） | `jumpserver/jms_*:v3.10.9` | 堡垒机 | 8070 / 8090 / 2222 / 3389 | `jmsdata`, `jmsmysqldata` |
| JumpServer MySQL | `mysql:8.0` | JumpServer 专用 DB（共享平台 Redis） | — | `jmsmysqldata` |

**可选 profile（`--profile full`）**：syncer（K8s 状态同步）、webhook（稳定 IP 注入）、pipeline-worker。生产或联调时启用。

---

## 7. 配置参数

开发环境所有参数集中在 `config/dev.env`，关键项：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `K3S_NETWORK_MODE` | `bridge` | 网络模式：`bridge` / `host-net` |
| `K3S_CNI` | `calico` | CNI 插件：`calico`（支持静态 IP）/ `flannel` / `cilium` |
| `K3S_UNDERLAY_PARENT_IFACE` | `eth0` | macvlan 父接口（host-net） |
| `K3S_UNDERLAY_SUBNET` | `192.168.1.0/24` | underlay 子网 CIDR |
| `K3S_UNDERLAY_GATEWAY` | `192.168.1.1` | underlay 网关 |
| `K3S_UNDERLAY_RANGE_START` | `192.168.1.200` | IPAM 分配起始 |
| `K3S_UNDERLAY_RANGE_END` | `192.168.1.250` | IPAM 分配结束 |
| `VORTEXOPS_K8S_API_SERVER` | 模式相关 | k8s API server 地址（external 需显式指定） |
| `VORTEXOPS_JWT_SIGNING_KEY` | `dev-jwt-signing-key-...` | JWT 签名密钥（仅开发） |
| `VORTEXOPS_SECURITY_ENCRYPTION_KEY` | `0123...` | 凭证加密 AES key（仅开发，32 字节十六进制） |

> 生产环境**严禁**使用 `dev.env` 中的默认密钥。请参考 [`../02-mid-bare-k8s/`](../02-mid-bare-k8s/README.md)。

完整模板见 [`config/dev.env.template`](config/dev.env.template)。

---

## 8. 日常运维

```bash
./scripts/down.sh                 # 停止全部，保留数据
./scripts/up.sh                   # 再次启动
./scripts/logs.sh apiserver       # 跟踪日志
./scripts/healthcheck.sh          # 健康巡检
./scripts/reset.sh                # 清空所有 volume（慎用，数据全丢）
```

### 8.1 重置某单个组件

```bash
docker compose -f ../docker-compose.dev.yml stop postgres
docker volume rm docker-compose_pgdata
./scripts/up.sh                   # 重新创建 postgres，需重新 migrate + seed
```

### 8.2 进入容器调试

```bash
docker compose -f ../docker-compose.dev.yml exec apiserver sh
docker compose -f ../docker-compose.dev.yml exec k8s /bin/sh
docker compose -f ../docker-compose.dev.yml exec postgres psql -U vortexops -d vortexops
```

> 注意：`apiserver` 镜像是 distroless（无 shell），调试需用 `distroless` 调试镜像或临时换 base。

### 8.3 查看 k3s 资源

```bash
# 使用导出的 kubeconfig
export KUBECONFIG=$(pwd)/../export/kubeconfig-localhost.yaml
kubectl get nodes
kubectl -n vortexops-dev get pods
```

---

## 9. 验证冒烟流程

`verify/smoke.sh` 自动执行端到端冒烟：

1. 登录获取 JWT token
2. 创建工作空间 / 应用 / 分组
3. 上传/登记一个测试镜像
4. 触发整组发布
5. 等待 Pod 就绪
6. 查询发布进度与 Pod 列表
7. 清理测试数据

```bash
./verify/smoke.sh
```

预期输出：所有步骤 `PASS`，发布状态 `succeeded`，Pod `running`。

---

## 10. 常见问题

| 现象 | 原因 | 解决 |
| --- | --- | --- |
| `apiserver` CrashLoop | 数据库 schema 未迁移 | 执行 `./scripts/migrate.sh` |
| `apiserver` 启动报 `vo_*` 表不存在 | 同上 | 同上 |
| k3s Pod 一直 Pending | k3s 容器未就绪或 CNI 安装失败 | 查 `docker logs <k3s-container>`；如 Calico 失败可切 `K3S_CNI=flannel` |
| Jenkins 启动慢 | 首次初始化插件 | 等待 3-5 分钟，查看 `docker logs` |
| OpenSearch 启动失败 `bootstrap.memory_lock` | Docker Desktop ulimit 不够 | Settings → Resources → sysctl `vm.max_map_count=262144` |
| `host-net` 模式 Pod 拿不到 LAN IP | macvlan 父接口错误 | 检查 `K3S_UNDERLAY_PARENT_IFACE`；Docker Desktop 下用 `eth0`（VM 内网卡） |
| 端口冲突 | 宿主机已占用 8080 等 | 修改 `docker-compose.dev.yml` 的端口映射 |
| MinIO Console 无法登录 | 默认密码已改 | 检查 `MINIO_ROOT_PASSWORD` 是否被 `dev.env` 覆盖 |
| Jenkins 构建镜像失败 push registry | registry 不可达或 TLS | 确认 `registry:5000` 可解析；builder 已配 `insecure-registry` |

---

## 11. 升级到 02 层（中型集群）的判断标准

满足以下任一条件，应升级到 [`../02-mid-bare-k8s/`](../02-mid-bare-k8s/README.md)：

- 用户数 > 50，需要并发隔离
- 应用数 > 500
- 需要多副本 HA / 数据备份
- 需要真实多节点 K8s 集群
- 有生产 SLA 要求
- 需要外部 OIDC SSO / 企业目录集成

升级路径：在物理机或中型 K8s 集群重新部署，**数据需手动迁移**（`pg_dump` + 对象存储同步），开发栈的数据卷与生产无兼容性。
