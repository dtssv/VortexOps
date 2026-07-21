# 集群网络方案（Network Profile）部署指南

VortexOps 支持 4 种集群网络方案，按集群规模与场景选择。方案由集群配置 `cluster.metadata.network_profile` 指定，平台据此决定 IPAM 策略与 CNI annotation 注入方式。

## 方案总览

| Profile | 场景 | CNI | Pod IP | 跨集群 | 平台全局 IPAM |
|---------|------|-----|--------|--------|---------------|
| `dev-single` | 开发环境单集群 | kindnet/flannel | Overlay 虚拟网段 | 不涉及 | 否 |
| `medium-overlay` | 中型生产 | Flannel/Calico VXLAN | Overlay 虚拟网段 | 独立 CIDR 或网关 | 否 |
| `large-underlay` | 大型生产/办公直连 | Macvlan/IPVLAN/Kube-OVN | **物理局域网 IP** | 共享超网 | **是** |
| `xlarge-bgp` | 超大型 | Calico BGP-only | 独立 Pod CIDR（BGP 宣告） | BGP Route Reflector | 是（账本） |

---

## 1. dev-single（开发环境）

**适用**：本地 kind/k3s，单机开发。

**CNI 安装**：无需，kind/k3s 自带 kindnet/flannel。

**集群配置**：
```json
{ "network_profile": { "profile": "dev-single" } }
```
或不配置（默认）。

**端口暴露**：NodePort（30000-30099 映射到宿主机，见 `deploy/k8s-init`）。

---

## 2. medium-overlay（中型集群 Overlay）

**适用**：中小规模生产，多集群独立 CIDR 互不通信或经网关。

**CNI 安装（Calico VXLAN 示例）**：
```bash
kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml
# ippool 默认 192.168.0.0/16，按需修改 CALICO_IPV4POOL_CIDR
```

**集群配置**：
```json
{ "network_profile": { "profile": "medium-overlay", "cni": "calico", "cidr": "192.168.0.0/16" } }
```

**多集群**：每集群独立 CIDR（如 A: 192.168.0.0/16, B: 192.168.1.0/16），经网关或 VPN 互联。

---

## 3. large-underlay（大型集群 Underlay 直连）★

**适用**：Pod 拿物理局域网 IP，与办公 PC 同网段直连；多集群共享超网。

### 3.1 地址规划

```
10.0.0.0/8                    # 超网，VortexOps 全局 IPAM 独占
├── 10.0.0.0/16   平台基础设施
├── 10.1.0.0/16   集群A        # 每集群 /16
│   ├── 10.1.0.0/24   节点+PC（Underlay L2 同网段）
│   └── 10.1.1.0/24   Macvlan Pod 池
├── 10.2.0.0/16   集群B
└── 10.254.0.0/16 预留
```

### 3.2 物理网络前置

- 节点在同一 L2 广播域（或 VLAN Trunk 可达）
- 交换机开启对应 VLAN，注意 MAC 学习上限（Macvlan 每 Pod 一 MAC）
- 网关 ARP 限制实测（IPVLAN 共享 MAC 场景）

### 3.3 CNI 安装

#### 方式 A：Macvlan + Whereabouts + Multus（推荐）

```bash
# 1. Multus
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/v4.1.0/deployments/multus-daemonset.yml

# 2. Whereabouts IPAM
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/v0.6.3/manifests/whereabouts.yaml

# 3. 默认 CNI（保留 Service/ClusterIP 通信，Multus 双网卡场景）
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml
```

#### NetworkAttachmentDefinition（Macvlan NAD）

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: macvlan-100            # 名字约定：<cni>-<vlan_id>，与平台注入的 networks annotation 对应
  namespace: default
spec:
  config: |
    {
      "cniVersion": "0.4.0",
      "name": "macvlan-100",
      "type": "macvlan",
      "master": "eth0",          # parent_interface，与集群配置一致
      "mode": "bridge",
      "ipam": {
        "type": "whereabouts",
        "range": "10.1.1.0/24",  # 集群 CIDR 的 Pod 池
        "gateway": "10.1.0.1"
      }
    }
```

> 平台注入的 annotation：`k8s.v1.cni.cncf.io/networks: [{"name":"macvlan-100","ips":["10.1.1.5"]}]`
> Whereabouts 读 `ips` 字段分配指定 IP（固定 IP）。

#### 方式 B：Kube-OVN Underlay

参考 [Kube-OVN Underlay 文档](https://kubeovn.github.io/docs/v1.12/advance/underlay/)。

### 3.4 集群配置

```json
{
  "network_profile": {
    "profile": "large-underlay",
    "cni": "macvlan",
    "cidr": "10.1.0.0/16",
    "supernet_cidr": "10.0.0.0/8",
    "vlan_id": 100,
    "parent_interface": "eth0",
    "gateway": "10.1.0.1",
    "multus_enabled": true
  }
}
```

### 3.5 平台 IP 池

为该集群创建 Underlay IP 池（provider=macvlan），CIDR 为 Pod 池段：
```bash
POST /api/v1/clusters/{id}/ip-pools
{ "name": "underlay-pod-pool", "cidr": "10.1.1.0/24", "gateway": "10.1.0.1", "provider": "macvlan", "metadata": { "vlan_id": 100, "parent_interface": "eth0" } }
```

### 3.6 分组使用

分组网络模式选 `underlay` → 平台分配物理 IP → 注入 CNI annotation → Pod 拿物理 IP → 办公 PC 直连。

### 3.7 跨集群互联

核心交换机静态路由（每集群 /16 网关指向核心交换机）：
```
10.1.0.0/16 → 集群A 网关
10.2.0.0/16 → 集群B 网关
```

---

## 4. xlarge-bgp（超大型 BGP）

**适用**：超大规模，Pod 路由自动宣告，跨集群经 BGP 互联。

### 4.1 CNI 安装（Calico BGP-only）

```bash
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.27.0/manifests/calico.yaml
# 关闭 IPIP/VXLAN，设 BGP mode
kubectl patch ippool default-ipv4-ippool --type='merge' -p '{"spec":{"ipipMode":"Never","vxlanMode":"Never"}}'
```

### 4.2 BGP 配置

```yaml
# BGPConfiguration：与核心交换机/Route Reflector 建立 peer
apiVersion: crd.projectcalico.org/v1
kind: BGPConfiguration
metadata:
  name: default
spec:
  logSeverityScreen: Info
  asNumber: 64513            # local_asn，与集群配置一致
  peers:
    - peerIP: 10.0.0.1       # bgp_peer_ip，核心交换机/RR
      asNumber: 64512        # bgp_peer_asn
```

### 4.3 集群配置

```json
{
  "network_profile": {
    "profile": "xlarge-bgp",
    "cni": "calico",
    "cidr": "10.10.0.0/16",
    "bgp_peer_ip": "10.0.0.1",
    "bgp_peer_asn": 64512,
    "local_asn": 64513
  }
}
```

### 4.4 跨集群

各集群 ASN 不同，CIDR 不重叠，均 peer 到同一 Route Reflector → 路由自动学习 → 跨集群 Pod 互通。

---

## 平台侧改动（本次实现）

| 层 | 改动 |
|----|------|
| DB（迁移 0003） | `vo_cluster_ip_pools` 加 `metadata` 列；provider 约束加 macvlan/ipvlan；`uq_ip_allocations_ip_active` 跨集群全局唯一索引 |
| domain | 新增 `networkprofile` 包（4 profile + ProfileConfig + 校验）；`NetworkMode` 加 `underlay`；`IPPoolProvider` 加 macvlan/ipvlan；`IPPool.Metadata` |
| clusterapp | `GetNetworkProfile`/`SupportsUnderlay`/`ParseNetworkProfile`；`AllocateForGroup` profile 感知（Underlay 优先 macvlan/ipvlan 池 + 全局冲突重试） |
| renderer | `RenderInput.NetworkProfile`；按 profile+CNI 注入 annotation（whereabouts/calico/kube-ovn/macvlan+Multus）；underlay 模式不建 Service |
| releaseapp | 调 renderer 前注入 cluster profile |
| applicationapp | `NetworkProfileResolver` 接口 + `validateUnderlay`（underlay 模式需集群支持） |
| HTTP | IP 池创建接受 metadata；IP 池 DTO 返回 metadata |
| 前端 | 集群注册表单加网络方案 Card（4 profile 联动 CNI/Underlay/BGP 字段）；列表展示网络方案列；分组网络模式加 underlay 选项 |

## CNI annotation 注入映射

| CNI | annotation key | 格式 |
|-----|----------------|------|
| Whereabouts | `k8s.v1.cni.cncf.io/ipAddrs` | `["10.1.1.5"]` |
| Calico | `cni.projectcalico.org/ipAddrs` | `["10.1.1.5"]` |
| Kube-OVN | `ovn.kubernetes.io/ip_address` | `10.1.1.5` |
| Macvlan/IPVLAN (Multus) | `k8s.v1.cni.cncf.io/networks` | `[{"name":"macvlan-100","ips":["10.1.1.5"]}]` |
