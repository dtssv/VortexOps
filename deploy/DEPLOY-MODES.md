# VortexOps 部署模式

VortexOps 通过 docker compose override 文件支持三种部署模式，**启动时通过 `-f` 参数选择走哪一套**。

## 模式总览

| 模式 | 启动命令 | k3s 位置 | Pod 网络 | 适用场景 |
|------|---------|---------|---------|---------|
| **bridge**（默认） | `docker compose -f docker-compose.dev.yml up -d` | 容器内（docker bridge） | flannel overlay，仅容器内可达 | 日常开发、流程验证 |
| **host-net** | `docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml up -d` | 容器内（host network） | flannel + multus macvlan，Pod 直拿物理局域网 IP | 验证 underlay / VIP / 直连 Pod IP |
| **external** | `docker compose -f docker-compose.dev.yml -f docker-compose.external.yml up -d` | 外部物理机 k3s 集群 | 由外部集群决定 | 真实多节点联调、生产前验证 |

## 1. bridge 模式（默认）

最简开发环境。k3s 跑在 docker bridge 网络，Pod 使用 flannel vxlan overlay，IP 在 10.42.0.0/16，**仅容器内可达**，外部无法 ping Pod IP。

```bash
docker compose -f docker-compose.dev.yml up -d
```

适用：纯流程验证，不关心网络可达性。

## 2. host-net 模式（underlay 开发验证）

k3s 容器共享宿主机网络栈，叠加 Multus + Macvlan。带 underlay 注解的 Pod 会附加一张 macvlan 网卡，直接从物理局域网子网拿 IP，**局域网内可直连 Pod IP**，用于验证 VIP 场景。

```bash
# 按需覆盖网络参数（默认 eth0 / 192.168.1.0/24）
export K3S_UNDERLAY_PARENT_IFACE=eth0
export K3S_UNDERLAY_SUBNET=192.168.1.0/24
export K3S_UNDERLAY_GATEWAY=192.168.1.1
export K3S_UNDERLAY_RANGE_START=192.168.1.200
export K3S_UNDERLAY_RANGE_END=192.168.1.250

docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml up -d
```

启动后自动：
- 安装 Multus CNI（meta-plugin）
- 安装 Whereabouts IPAM
- 在 `vortexops-dev` / `vortexops` 命名空间创建 `macvlan` NetworkAttachmentDefinition（与平台注入的 NAD 名一致）

配置集群与 IP 池（首次 host-net 启动后执行一次）：

```bash
docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml run --rm underlay-seed
```

该脚本会：将集群 `network_profile` 设为 `large-underlay` + `macvlan`、创建物理网段 IP 池、释放旧的 Overlay 稳定 IP 分配。**随后需重新发布分组**，Pod 才会拿到物理 IP。

> **注意**：Docker Desktop（WSL2）环境下 macvlan 父接口默认为 `eth0`（VM 内网卡），实际二层可能仍隔离于宿主机物理网络。如需真正 LAN 可达，建议使用 external 模式对接物理机 k3s。

## 3. external 模式（连接物理机 k3s 集群）

不在 docker 内启动 k3s，而是连接已存在的物理机 k3s 集群。VortexOps 的 `apiserver` 容器通过挂载的 kubeconfig 访问外部集群。

```bash
# 1. 准备 kubeconfig
cp external-kubeconfig.yaml.example external-kubeconfig.yaml
# 编辑 external-kubeconfig.yaml：填入外部集群 server 地址与证书

# 2. 启动（如需指定 server，覆盖环境变量）
export VORTEXOPS_K8S_API_SERVER=https://192.168.1.10:6443
docker compose -f docker-compose.dev.yml -f docker-compose.external.yml up -d
```

适用：物理机多节点 k3s、真实 underlay 网络、生产前完整联调。

## 切换模式

切换模式前需**完全停止当前栈**（k3s 状态卷在不同模式间不通用）：

```bash
docker compose -f docker-compose.dev.yml -f docker-compose.host-net.yml down -v
# 再启动另一种模式
docker compose -f docker-compose.dev.yml up -d
```

## 环境变量速查

| 变量 | 默认值 | 作用 |
|------|-------|------|
| `K3S_NETWORK_MODE` | `bridge` | 网络模式：`bridge` / `host-net` |
| `K3S_UNDERLAY_PARENT_IFACE` | `eth0` | macvlan 父接口（host-net） |
| `K3S_UNDERLAY_SUBNET` | `192.168.1.0/24` | underlay 子网 CIDR |
| `K3S_UNDERLAY_GATEWAY` | `192.168.1.1` | underlay 网关 |
| `K3S_UNDERLAY_RANGE_START` | `192.168.1.200` | IPAM 分配起始 |
| `K3S_UNDERLAY_RANGE_END` | `192.168.1.250` | IPAM 分配结束 |
| `VORTEXOPS_K8S_API_SERVER` | 模式相关 | k8s API server 地址（external 需显式指定） |
