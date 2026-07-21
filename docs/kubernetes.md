# Kubernetes 集成

涵盖多集群接入、资源映射、配置注入、Pod/日志/事件流、灰度发布的 K8s 实现与漂移检测。所有 K8s 集成相关内容统一在此文档。

---

## 1. 接入模型

### 1.1 多集群管理

- `vo_clusters` 表存储每个业务集群的 kubeconfig（KMS 加密）。
- `KubernetesClientPool`：
  - 启动时加载所有 enabled 集群，构建 `client-go` Clientset（含 Informer）。
  - 按 cluster_id 缓存，热更新（cluster 配置变更后重建）。
  - 健康检查：每 60s `GET /healthz`，更新 `clusters.status`。
- Leader Election：Informer 与 Watcher 仅在 leader 实例运行，避免多副本重复。

### 1.2 Namespace 映射

- `vo_workspace_clusters` 定义空间在集群中的目标 Namespace。
- 创建分组时选定集群与 Namespace；Namespace 不存在时可选「自动创建」（带 ResourceQuota/LimitRange 默认值）。

### 1.3 平台 ServiceAccount 权限

平台在每个业务 Namespace 使用一个 ServiceAccount（如 `vortexops`），通过 K8s RBAC 授予**最小权限**：

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: <ns>
  name: vortexops
rules:
  - apiGroups: ["apps"]
    resources: ["deployments", "replicasets"]
    verbs: ["get","list","watch","create","update","patch","delete"]
  - apiGroups: [""]
    resources: ["pods","pods/log","configmaps","secrets","services","events","namespaces"]
    verbs: ["get","list","watch","create","update","patch","delete"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get","list","watch","create","update","patch","delete"]
```

> 平台不直接操作节点、ClusterRole 等高危资源；如需高级能力，单独申请。

## 2. 资源映射

### 2.0 工作负载映射

| `groups.workload_type` | K8s 资源 | 关系 | 发布策略 |
| --- | --- | --- | --- |
| `deployment`（默认，无状态） | Deployment | 1:1（灰度时 1:2） | 滚动/灰度/重建 |
| `statefulset`（有状态） | StatefulSet | 1:1 | 滚动（有序），默认稳定网络 |
| `cronjob`（定时任务） | CronJob | 1:1 | 改 schedule/jobTemplate；不涉及副本滚动 |
| `job`（一次性任务） | Job | 1:1 | 重新运行 = 新建 Job |

| VortexOps 实体 | K8s 资源 | 关系 |
| --- | --- | --- |
| Group | Deployment / StatefulSet / CronJob / Job（按 workload_type） | 1:1（主），灰度时 Deployment 1:2 |
| Config（文件级） | ConfigMap + Secret | 1:N（按文件分组：明文→ConfigMap，secret→Secret） |
| Config（命令参数） | workload.spec.template.spec.containers（或 jobTemplate） | 修改 command/args/env/envFrom |
| Release | workload 更新 + ControllerRevision/ReplicaSet | 历史由 revision 保留 |
| Service | Service（可选自动创建） | Group 可选关联 Service |
| HPA | HorizontalPodAutoscaler | autoscaling_enabled 时 1:1 |

**StatefulSet 注意**：
- 自动启用稳定网络（Headless Service + 有序 Pod 名），`keep_pod_ip` 默认 true。
- 发布为有序滚动（`podManagementPolicy`，默认 `OrderedReady`，可配 `Parallel`）。
- 灰度不适用（StatefulSet 无独立灰度 Deployment 范式）；变更走滚动 + 可选分区（`partition`）。

**CronJob/Job 注意**：
- 不设 `replicas`（CronJob 按 schedule 触发，Job 按 completions）。
- 「发布」= 更新 jobTemplate 后，CronJob 下次触发生效；Job 可「重新运行」生成新 Job 实例。
- `job_policy` 控制 completions/parallelism/backoffLimit/ttlSecondsAfterFinished/concurrencyPolicy。

### 2.1 Group → 工作负载模板

平台创建/更新的 Deployment 由「分组定义 + 当前镜像 + 当前配置版本」渲染：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {group.deployment_name}
  namespace: {group.namespace}
  labels:
    vortexops.io/workspace: {ws.name}
    vortexops.io/application: {app.name}
    vortexops.io/group: {group.name}
    vortexops.io/managed: "true"
  annotations:
    vortexops.io/image-uuid: "{image.uuid}"
    vortexops.io/config-version: "{config.version}"
    vortexops.io/release-uuid: "{release.uuid}"
spec:
  replicas: {group.replicas}
  strategy:
    type: RollingUpdate / Recreate
    rollingUpdate: {maxSurge, maxUnavailable}
  selector:
    matchLabels:
      vortexops.io/group: {group.name}
  template:
    metadata:
      labels:
        vortexops.io/group: {group.name}
    spec:
      containers:
        - name: app
          image: {image.full_reference}
          command: [...]   # 来自 config
          args: [...]       # 来自 config
          env: [...]        # 来自 config
          envFrom: [...]    # 来自 config
          resources:
            requests: {cpu, memory}
            limits: {cpu, memory}
          volumeMounts:     # 来自文件级配置
            - name: config-files
              mountPath: /etc/app/application.yml
              subPath: application.yml
      volumes:
        - name: config-files
          configMap: {name}  # 或 projected 多 ConfigMap/Secret
```

`vortexops.io/managed: "true"` 标签用于区分平台管理资源与外部资源，避免误操作。

## 3. 配置注入

### 3.1 文件级配置

- 每个文件条目：`{path, mode, secret, content/contentRef}`。
- 渲染策略：使用 `projected volume` 聚合多个 ConfigMap/Secret，按 `subPath` 挂载到指定路径。
  - 明文文件 → ConfigMap（命名 `{deployment}-cm-{hash}`）
  - Secret 文件 → Secret（命名 `{deployment}-secret-{hash}`）
- 文件权限：通过 `projected.volume.sources[].configMap.items[].mode` 设置。
- 大文件（>1MB）：存对象存储，构建镜像时拷入；运行时配置不再挂载（避免 ConfigMap 限制，1MB 上限）。

### 3.2 命令参数配置

- `command`：覆盖容器 entrypoint（数组）。
- `args`：覆盖容器 cmd（数组）。
- `env`：键值对，`secret=true` 的来自 Secret。
- `envFrom`：引用外部 ConfigMap/Secret 全量注入。

### 3.3 配置版本与 Deployment 协调

配置变更流程：

1. 用户编辑配置 → 校验 → 生成新 `configs.version`（分组内自增）。
2. 新版本默认 `is_current=false`（草稿）。
3. 用户「应用此配置」→ 触发一次 `config_only` release（见 [构建与发布](build-release.md#发布与配置的关系)）：
   - 用新配置版本渲染 PodTemplate。
   - PATCH Deployment，`annotations.vortexops.io/config-version` 更新。
   - Group.current_config_id 更新。
4. 回滚配置：选择历史 version，重复「应用」流程。

> 配置与发布统一走 release 记录，保证可审计可回滚。

### 3.4 配置集（ConfigSet）合并

配置集是独立于分组的可复用配置，可关联多个分组共享。数据模型见 [数据模型 §8.3-8.6](data-model.md#83-config_sets配置集可关联多个分组共享)。

**合并时机**：发布（含 config_only）时，平台计算分组最终生效配置：

```
最终配置 = 按 priority 升序依次合并各配置集版本（merge_strategy）
         → 最后叠加分组自身 configs.snapshot（自身优先级最高）
```

- `merge_strategy=overlay`（默认）：配置集作为底座，分组自身配置覆盖同 path/key。
- `prepend`/`append`：env 列表前后插入。
- `vo_config_set_bindings.pinned=true`：锁定该配置集特定版本，不随配置集升级而变；`pinned=false` 跟随 `config_sets.current_version_id`。
- 合并产物记入 release 的 target_config 快照，可审计可回滚（回滚 release 即回到合并前状态）。

**K8s 映射**：合并后的最终配置按 §3.1/§3.2 注入 ConfigMap/Secret/command/env，配置集本身不单独生成 K8s 资源（它只是合并源）。

**版本同步**：配置集升级新版本并设为 current 时，关联的非 pinned 分组下次发布自动采用新版本；不影响已在运行的 Pod（直到下次发布）。

### 3.5 Secret 处理

- 平台存储的 Secret 值 KMS 加密落库（`configs.snapshot` 中标记 `secret:true`，值加密）。
- 推送到 K8s 时解密为 K8s Secret（K8s 侧 etcd 加密由集群保证）。
- 列表/详情接口：Secret 值默认不返回（返回 `***`），仅 admin 可显式查询（且记审计）。
- 配置集中的 Secret 同样加密存储，合并时按 path 合并到最终 Secret。

## 4. 资源与网络映射

创建分组时选择的硬件（CPU/内存/磁盘/GPU）与网络（Service 类型、稳定 IP、公网访问、Ingress）由平台转换为 K8s 资源字段。数据模型见 [数据模型 §6.6 groups](data-model.md#66-groups) 与 [§5.5-5.8](data-model.md#55-cluster_node_pools集群节点池画像缓存)。

### 4.1 硬件资源映射

| 平台字段 | K8s 字段 | 说明 |
| --- | --- | --- |
| `resources_cpu_m` | `containers[].resources.requests.cpu` | 毫核，如 `4000m`=4 核 |
| `resources_cpu_limit_m` | `containers[].resources.limits.cpu` | 空=不设上限 |
| `resources_memory_bytes` | `containers[].resources.requests.memory` | |
| `resources_memory_limit_bytes` | `containers[].resources.limits.memory` | 空=不设上限 |
| `resources_gpu` + `gpu_resource_name` | `containers[].resources.limits.{gpu_resource_name}` | 如 `nvidia.com/gpu: 1` |
| `gpu_type` | `nodeSelector` + `tolerations` | 自动选对应 GPU 节点并容忍污点 |
| `ephemeral_storage_request_bytes` | `containers[].resources.requests.ephemeral-storage` | 临时存储 |
| `ephemeral_storage_limit_bytes` | `containers[].resources.limits.ephemeral-storage` | |
| `storage_size_bytes` + `storage_class` | `emptyDir` + `sizeLimit` 或独立 `PVC` | 临时盘 |
| `node_selector` / `node_affinity` | `spec.nodeSelector` / `spec.affinity.nodeAffinity` | |
| `tolerations` | `spec.tolerations` | |
| `priority_class` | `spec.priorityClassName` | |

**GPU 处理**：
- 选 GPU 型号（如 `nvidia-a100`）→ 平台自动写入 `nodeSelector: {accelerator: nvidia-a100}`（或集群约定的标签）+ 容忍该 GPU 节点污点（如 `nvidia.com/gpu`）。
- `gpu_resource_name` 默认 `nvidia.com/gpu`，昇腾为 `huawei.com/ascend-910`，按集群节点池画像填充。
- GPU 为整数卡（不支持分数），`resources_gpu=0` 时不生成 GPU 字段。

**资源模板**：`vo_resource_templates` 预设规格，创建分组时可「一键套用」，再微调。

### 4.2 网络映射

| `network_mode` | K8s 资源 | 适用 |
| --- | --- | --- |
| `clusterip`（默认） | Service(ClusterIP) | 集群内访问 |
| `nodeport` | Service(NodePort) | 测试环境 |
| `loadbalancer` | Service(LoadBalancer) | 外部访问，需云 LB 或 metallb |
| `hostnetwork` | Pod `hostNetwork: true` + `dnsPolicy` | 特殊网络需求 |

`service_port_info` 转换为 Service `ports`：
```yaml
ports:
  - name: http
    port: 80
    targetPort: 8080
    protocol: TCP
```

`ingress_enabled=true` 时额外创建 Ingress（host/path 来自 `ingress_host`/`ingress_path`），需集群已装 Ingress Controller。

`network_policy_enabled=true` 时生成入流量 NetworkPolicy，默认仅放行同 Namespace + 指定来源，收紧分组入流量。

**公网访问控制**（`allow_egress_internet`）：
- `allow_egress_internet=false`（默认，更安全）：生成出方向 NetworkPolicy，默认拒绝所有出公网流量，仅放行：
  - 集群内 CIDR（同 Namespace / kube-system DNS）
  - `egress_allowlist` 中显式列出的 CIDR/域名+端口（如镜像仓库、npm registry、外部 API）
- `allow_egress_internet=true`：不生成出方向限制 NetworkPolicy，Pod 可自由访问公网。
- 适用：大部分业务分组默认禁止公网出访以降低数据外泄与攻击面；需要拉外部依赖的分组按白名单放行。
- 域名级放行需集群 CNI 支持（如 Calico FQDN policy）；不支持时降级为 IP/CIDR。

### 4.3 稳定 IP（keep_pod_ip）

`keep_pod_ip=true` 让分组 Pod 在重新部署、滚动更新、Pod 重启/迁移后 **IP 保持不变**。用户无需指定具体 IP，平台自动保留。

适用场景：
- 数据库/中间件客户端 IP 白名单（IP 不变，白名单无需改）
- 有状态服务需稳定身份
- 外部系统回调地址固定
- 日志/监控按 IP 聚合

**实现方案**（按集群 CNI 选择，记录于 `cluster_ip_pools.provider`）：

| 方案 | 说明 |
| --- | --- |
| Calico IPAM 保留 | Pod annotation `cni.projectcalico.org/ipAddrs` 指定原 IP；Pod 删除后 IP 保留 |
| Whereabouts（IPAM） | `k8s.v1.cni.cncf.io/ipAddrs` annotation 指定原 IP |
| Kube-OVN 固定 IP | Pod annotation `ovn.kubernetes.io/ip_address` 指定原 IP |
| MetalLB（LoadBalancer 场景） | LoadBalancer Service 的 `loadBalancerIP` 固定 |
| StatefulSet 稳定网络（有状态推荐） | Headless Service + 有序 Pod，配合 IP 保留 |

**保留流程**（无需用户指定 IP）：
1. 分组 `keep_pod_ip=true` 且绑定 `vo_cluster_ip_pools`。
2. Pod 首次调度拿到 IP → 平台记录到 `vo_cluster_ip_allocations`（`status=allocated`，记 `replica_index`）。
3. Pod 重建/滚动更新时，平台从 `vo_cluster_ip_allocations` 查回该副本原 IP，通过 CNI annotation 注入，使新 Pod 复用同 IP。
4. IP 在分组存活期间一直保留（Pod 删除不释放）。
5. 分组删除时释放 IP（`status=released`）。

> 与「显式指定 IP」不同：用户只勾选「保持 IP 不变」开关，IP 由 IPAM 自动分配并保留，简化使用。多副本时按 `replica_index` 一一对应保留。

### 4.4 资源校验

创建/更新分组时平台校验：
- CPU/内存/GPU 不超过节点池单节点上限（避免无法调度）。
- GPU 型号在该集群存在（查 `vo_cluster_node_pools`）。
- `keep_pod_ip=true` 时校验集群已配置稳定 IP 池且 CNI 支持保留。
- Namespace ResourceQuota 不超限（空间配额）。
- `hostNetwork` / `keep_pod_ip` / `allow_egress_internet=false` 等高级网络选项需 admin 权限（`action:group:advanced-network`）。

校验失败返回明确错误（如「GPU 型号 nvidia-a100 在该集群不存在」「该集群未配置稳定 IP 池，无法启用 keep_pod_ip」）。

### 4.5 变更影响

- 改 `resources_*` / `gpu`：触发滚动更新（Pod 重建以应用新 resources）。
- 改 `network_mode` / `keep_pod_ip` / `allow_egress_internet`：重建 Service/Pod/NetworkPolicy，可能短暂断连，生产需审批。
- 改 `node_selector`：滚动调度到新节点池。
- 资源/网络/配置集绑定变更默认走配置版本 + 发布流程（非即时生效），可审计可回滚。

## 5. 实时数据：Pod / 日志 / 事件

### 5.1 Pod 列表与状态

- 通过 Informer 缓存 Pod（按 namespace），按 Group 标签过滤。
- 字段：名称、状态、镜像、IP、节点、启动时间、重启次数、就绪状态。
- 灰度 Pod 标记 `vortexops.io/canary=true`，前端区分展示。

### 5.2 日志流

- 接口：`GET /groups/{uuid}/pods/{pod}/logs?follow=true&container=app`（WebSocket）。
- 实现：调用 `CoreV1().Pods(ns).GetLogs(name, opts).Stream(ctx)`，逐行转发。
- 权限：viewer 及以上可读日志（按 [权限体系](permissions.md)）。
- 限制：单连接最长 30min；并发数按用户限流。

### 5.3 跨 Pod 日志聚合与搜索

- 接口：`GET /groups/{uuid}/logs/search?q=keyword&since=1h&pod=&level=`（分页）。
- 实现：
  - 小规模（单分组 Pod 数 < 50）：并行调用各 Pod logs 接口，在内存聚合 + 关键词过滤。
  - 大规模：经集群 log-proxy（Loki/Vector agent）查询，平台转发查询结果（见 [扩展性 §日志/事件就近代理](scalability.md)）。
- 能力：按 Pod/容器/时间/关键词过滤；正则搜索；高亮匹配；导出；「跳转到该日志所在 Pod 的实时流」。
- 不持久化日志全文（仅缓存近期查询结果），长期留存依赖集群日志系统。

### 5.4 Pod 终端（exec）

- 接口：`WS /groups/{uuid}/pods/{pod}/exec?container=app&command=/bin/sh`。
- 实现：`CoreV1().Pods(ns).GetExec(name, opts).URL()`，WebSocket 双向转发 stdin/stdout/stderr + 终端尺寸（resize）。
- 权限：developer 及以上（`action:group:pod:exec`）；生产 Pod exec 默认需 admin 或审批（可配置）。
- 审计：每次 exec 记审计（操作人、Pod、命令、时长）；可选录屏（记录 tty 输出到对象存储，供回放）。
- 安全：默认 `/bin/sh`，可限制可用命令；超时自动断开；并发会话数限制。

### 5.5 端口转发

- 接口：`POST /groups/{uuid}/pods/{pod}/portforward`（返回会话 ID + 本地端口）+ `WS .../portforward/{sessionId}`。
- 实现：`CoreV1().Pods(ns).PortForward(name, opts)`，把平台端口转发到 Pod 端口。
- 用途：临时调试——访问 Pod 内服务（如 JMX、调试端口、管理后台）而无需暴露 Service/Ingress。
- 权限：developer 及以上（`action:group:pod:portforward`）；会话最长 2h，超时自动断；记审计。
- 限制：生产环境默认禁用，需 admin 开启；平台侧监听端口受网络策略保护。

### 5.6 事件流

- `EventInformer` 按命名空间订阅 K8s Event，过滤与该 namespace 资源相关事件，推送到前端「分组详情 → 事件」面板。
- 历史事件可查（保留期按集群 etcd 配置，平台不另存）。

### 5.7 弹性伸缩（HPA）

`autoscaling_enabled=true` 时平台生成并管理 HPA：

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {group.deployment_name}
  namespace: {group.namespace}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment   # 或 StatefulSet
    name: {group.deployment_name}
  minReplicas: {hpa_min_replicas}
  maxReplicas: {hpa_max_replicas}
  metrics:
    - type: Resource
      resource: {name: cpu, target: {type: Utilization, averageUtilization: 70}}
    - type: Resource
      resource: {name: memory, target: {type: Utilization, averageUtilization: 80}}
    # 自定义指标需集群已装 Prometheus Adapter
    - type: Pods
      pods: {metric: {name: qps}, target: {type: AverageValue, averageValue: 1000}}
  behavior: {hpa_behavior}
```

- `replicas` 字段在 HPA 启用时转为初始/期望下限，实际副本数由 HPA 控制；平台读 `Deployment.status.replicas` 展示实时副本。
- 自定义指标（QPS/队列长度等）需集群部署 Prometheus Adapter，`hpa_metrics` 中 `type=custom`。
- 关闭 HPA = 删除 HPA 资源，副本数回退到 `replicas` 字段。
- 伸缩事件（scale up/down）写入 `vo_activity_feeds` + 可触发告警（`hpa_maxed`：已达 maxReplicas 仍高负载）。
- VPA（垂直伸缩）为可选高级，默认不启用（重建 Pod 影响大）。

## 6. 灰度发布的 K8s 实现

### 6.1 Canary Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-canary
  namespace: team-svc
  labels:
    vortexops.io/group: payment
    vortexops.io/canary: "true"
    vortexops.io/release: {release.uuid}
spec:
  replicas: 1   # canary_replicas
  selector:
    matchLabels:
      vortexops.io/group: payment
      vortexops.io/canary: "true"
  template:
    metadata:
      labels:
        vortexops.io/group: payment
        vortexops.io/canary: "true"
    spec:
      containers:
        - name: app
          image: {canary_image}
          ...
```

### 6.2 流量切分

- **基础（默认）**：Canary Pod 与主 Pod 共享 Service 选择器 `vortexops.io/group=payment`，流量按副本数比例自然分配。
- **精确比例（v2）**：集成 Nginx Ingress 或服务网格（Istio），按权重路由，平台写入 VirtualService/Ingress 注解。
- **会话保持**：可在 Canary Service 上加 `sessionAffinity`，便于定向测试。

### 6.3 提升为整组

1. 用 canary 镜像 + 配置触发整组 Rolling release。
2. release 成功后删除 Canary Deployment 与其 ReplicaSet。
3. 主 Deployment 即为新版本。

### 6.4 放弃灰度

1. 删除 Canary Deployment（`foregroundDeletion` 确保清理 Pod）。
2. release.status=`rolled_back`。
3. 主 Deployment 未受影响。

## 7. 控制器（可选高级）

为支持「自愈」与「漂移检测」，平台可运行一个 controller-runtime 控制器：

- **Watch** 平台管理的 Deployment（按 `vortexops.io/managed=true` 标签）。
- **漂移检测**：若 Deployment 被 VortexOps 之外修改（如 kubectl 改了镜像），记录告警，可选择「拉回平台状态」或「采纳外部变更」。
- **状态同步**：Deployment 实际副本数、镜像与 `vo_groups` 表同步。
- **自愈**：若副本被手动缩到 0 而平台期望非 0，按策略决定是否恢复（默认告警不自动改，避免误操作）。

> 控制器为 M5+ 可选项，初期用轮询 + Watch 实现核心功能即可。

## 8. 集群健康与资源视图

- 集群详情页：版本、节点数、CPU/内存总量与分配（通过 `metrics-server` 或 Prometheus）。
- Namespace 视图：ResourceQuota 使用率、Pod 数。
- 这些数据只读展示，不提供修改（修改走 K8s 原生或运维流程）。

## 9. 错误处理

| 场景 | 处理 |
| --- | --- |
| 集群不可达 | 客户端操作返回 503；缓存数据仍可展示（标注「数据可能过期」） |
| Namespace 不存在 | 创建分组时报错；可选自动创建 |
| Deployment 不存在 | 发布时自动创建；纯配置变更若不存在则报错 |
| 权限不足（K8s RBAC） | 返回 403，提示运维检查 ServiceAccount 权限 |
| Pod 日志接口超时 | WebSocket 关闭，前端自动重连（指数退避） |

## 10. 安全约束

- 平台不向用户暴露原始 kubeconfig。
- Pod exec 接口需额外权限（developer+），且记审计 + 录屏（可选，通过 tty 记录）。
- 删除资源操作必须二次确认 + 审计。
- 镜像拉取 Secret：由平台在创建 Deployment 时自动注入（按 namespace 配置的 `imagePullSecrets`）。

---

## 11. 大规模集群适配（单集群 5000+ 节点、单分组 1 万+副本）

> 详细扩展性设计见 [扩展性设计](scalability.md)。本节列 K8s 集成层面的关键策略。

### 11.1 Informer 按 Namespace 分片

单集群 Pod 15 万+时，单 Informer 内存压力大、Initial List 慢。

- 每个 syncer 实例只 Watch 一组 Namespace（`NamespaceShard`，如每 50 个 Namespace 一个 worker）。
- 平台 `k8s-syncer` 组件多副本，调度器将 `(cluster, namespace_shard)` 分配到不同 syncer。
- Leader 选举粒度从「集群级」细化到「(cluster, namespace_shard) 级」。
- Initial List 用 `ResourceVersion=0` + `limit=500` 分页，避免 API Server 大列表阻塞。

### 11.2 Pod 缓存（不落库）

- Pod 不入平台 DB；缓存于按集群分片的 Redis：`cluster:{id}:pods:{namespace}`，TTL 5min。
- Informer 增量更新缓存；前端 `GET /groups/{uuid}/pods` 走缓存 + 标签过滤。
- 缓存失效回源 Informer（若 syncer 在线），否则标注「数据可能过期」。

### 11.3 大副本分组发布观察

1 万+副本整组发布时：

- 就绪计数读 `Deployment.status.readyReplicas`，不自行聚合 Pod。
- 采样推送：每 5% 或每 100 Pod 推一次 `vo_release_events`，不逐 Pod 推。
- 超时自适应：`max(10min, replicas/1000 × 2min)`。
- 滚动参数建议：大副本用 `maxSurge=25%`、`maxUnavailable=10%`（而非小副本的 `1/0`），避免滚动过慢。

### 11.4 日志/事件就近代理

大集群日志并发高，不全回平台 apiserver。

- 每个业务集群部署 `vortexops-log-proxy`，平台转发日志请求到对应集群 proxy，proxy 直接 `kubectl logs` 流式返回。
- 平台仅做路由 + 透传，不缓存全量日志。
- 限流：单用户日志并发 20、单集群总并发 500，超额排队/拒绝。
- 事件流：`EventInformer` 在 syncer 侧运行，按 namespace 过滤后推平台，按 (workspace, group) 路由到前端。

### 11.5 集群健康检查分摊

- 健康检查（`GET /healthz`）由该集群对应的 syncer 承担，更新 `clusters.status`。
- 平台 apiserver 不直接轮询所有集群，避免 N 集群时连接爆炸。

### 11.6 降级

- 集群 API Server 延迟 P99 > 2s：该集群 Pod 列表/日志降级为缓存态，发布/构建拒绝并提示。
- 集群不可达：Pod/日志返回「离线」，其他集群不受影响。

### 11.7 运行态缓存与 IP 反查设计

> 应用/分组的「期望状态」存 PostgreSQL（`applications.lifecycle`、`groups.replicas` 等，可审计可回滚）；「实际运行态」（Pod 是否就绪、某 IP 属于哪个 Pod、分组运行状态）由 K8s 维护，平台**不落 DB**，通过 syncer Informer 订阅后缓存到 Redis。详见 [扩展性设计 §6.2](scalability.md#62-运行态缓存与-ip-反查)。

#### 11.7.1 状态层次

| 状态 | 维护方 | 存储 | 说明 |
| --- | --- | --- | --- |
| 应用是否可用（平台层） | 用户配置 | PostgreSQL `applications.lifecycle` | `active`/`frozen`/`archived`，配置态 |
| 分组是否启动 | 用户配置 | PostgreSQL `groups.replicas` | 0=停止，配置态 |
| 分组运行状态 | K8s + syncer 摘要 | Redis（缓存 Deployment.status） | running/degraded/failed |
| Pod 就绪/实时列表 | K8s | K8s etcd（Pod.status）→ Informer → Redis | 不落 DB |
| Pod IP → Pod 反查 | syncer 索引 | Redis（反向索引） | 见 11.7.3 |
| 稳定 IP 绑定 | 平台预留 | PostgreSQL `vo_cluster_ip_allocations` + K8s | 需持久才能重建复用 |
| 发布进度 | syncer watch | Redis（实时）+ `vo_release_events`(DB 历史) | |

#### 11.7.2 Redis 缓存结构

| key | value | TTL | 更新方式 | 用途 |
| --- | --- | --- | --- | --- |
| `rt:pod:{clusterId}:{namespace}:{podName}` | Pod 摘要 JSON（name/ip/phase/ready/containerStatuses/labels/node） | 5min | Informer Add/Update | Pod 详情、分组 Pod 列表 |
| `rt:group:{clusterId}:{namespace}:{deploymentName}:status` | Deployment/StatefulSet status 摘要（replicas/readyReplicas/updatedReplicas/conditions） | 30s | Informer + 定时刷新 | 分组运行态、发布进度 |
| `rt:svc:{clusterId}:{namespace}:{svcName}` | Service 摘要（type/clusterIP/externalIP/ports） | 5min | Informer | 访问信息展示 |
| `rt:hpa:{clusterId}:{namespace}:{hpaName}` | HPA status（currentReplicas/desiredReplicas/metrics） | 30s | Informer | 弹性伸缩观测 |

- 分组 Pod 列表：按 `vortexops.io/group` 标签从 `rt:pod:*` 过滤（namespace 内）。
- 缓存 miss → 回源 Informer（syncer 在线时秒级返回）；syncer 离线 → 标注「数据可能过期」并回源 K8s API（限流）。
- Pod 缓存按集群分片 Redis，避免单分片过大（见 [扩展性设计](scalability.md)）。

#### 11.7.3 Pod IP 反查索引

「某个 IP 对应哪个 Pod/应用/分组」是排障与网络拓扑高频需求，K8s 原生不支持按 IP 查询（FieldSelector 不支持 podIP），平台用 Redis 反向索引解决：

| key | value | TTL | 更新方式 |
| --- | --- | --- | --- |
| `rt:ip:{clusterId}:{podIP}` | `{podName, namespace, groupUuid, applicationUuid, nodeName, phase}` | 同 Pod 生命周期 | Informer Add/Update/Delete 同步维护 |

- Pod 分配/释放 IP 时，syncer 同步写/删该索引。
- 查询接口 `GET /clusters/{uuid}/ip-lookup?ip=10.0.1.5` → Redis 反查 → 命中返回所属分组/应用/节点；miss 回源（List namespace Pod 过滤，慢路径，限流并提示）。
- 用途：排障（某 IP 报错定位来源）、网络拓扑、稳定 IP 校验、安全审计。

#### 11.7.4 分组运行状态推导

分组「running/degraded/failed」由 syncer 摘要推导，缓存到 `rt:group:*:status`：

| 条件 | 状态 |
| --- | --- |
| readyReplicas == replicas 且 conditions Available=true | `running` |
| readyReplicas == 0 且 replicas > 0 | `failed` |
| 0 < readyReplicas < replicas | `degraded`（滚动中或部分不健康） |
| replicas == 0 | `stopped` |
| Deployment 不存在 | `not_deployed` |

发布进行中叠加 `progressing` 状态。前端分组列表/详情读此缓存，不实时聚合 Pod。

#### 11.7.5 稳定 IP 的特殊性

`keep_pod_ip=true` 的分组，其 IP 绑定**必须落 DB**（`vo_cluster_ip_allocations`，status=allocated/released）：

- 原因：Pod 重建时需从 DB 找回原 IP 并注入，否则无法保证「IP 不变」。
- 与普通 Pod IP 区别：普通 Pod IP 是 K8s 动态分配、随重建变化，不需持久；稳定 IP 是平台预留并持久绑定。
- 查法：DB `WHERE ip_address=?` 拿分组与副本序号；运行态是否真正生效仍看 K8s（Redis 缓存）。

---

## 12. 中间件资源映射

中间件作为有状态工作负载，K8s 资源映射与应用（Deployment）不同。部署流程见 [构建与发布 §中间件部署](build-release.md#中间件部署)。

### 12.1 安装方式

| 方式 | 适用 | 实现 | 平台封装 |
| --- | --- | --- | --- |
| **Helm**（推荐） | 大多数标准中间件 | `helm install/upgrade/rollback` | Helm SDK（如 `helm.sh/helm/v3`）嵌入 Go 后端 |
| **Operator** | 复杂有状态中间件（Kafka/ES/Cassandra） | 创建 Operator CR（如 `Kafka` CR） | CR schema 来自目录，apply 后由 Operator 调谐 |
| **Manifest** | 简单单实例 | 直接 apply StatefulSet+Service+PVC YAML | 模板渲染 |

平台优先用 Helm（bitnami 等成熟 Chart），复杂场景用 Operator（如 Strimzi Kafka、Elastic Cloud on K8s）。

### 12.2 资源对应

| VortexOps 实体 | K8s 资源 | 说明 |
| --- | --- | --- |
| MiddlewareInstance | Helm Release（secret 存储） | `sh.helm.release.v1.{name}` secret |
| | 或 Operator CR | 如 `kafka.strimzi.io/v1b2` |
| | 或 StatefulSet + Service + PVC | manifest 方式 |
| MiddlewareParams | Helm values / CR spec | 版本化快照 |
| MiddlewareRelease | Helm revision / CR generation | 历史可回滚 |
| MiddlewareBackup | VolumeSnapshot / Job（dump） | 按 method 不同 |

### 12.3 Helm Release 管理

- 平台通过 Helm SDK 直接操作（不依赖 helm CLI），与 kubeconfig 复用。
- Release 存储于业务集群 Namespace 的 Secret（`sh.helm.release.v1.{name}.v{N}`）。
- 升级 = `helm upgrade --version {chartVersion} -f values.yaml`。
- 回滚 = `helm rollback {name} {revision}`。
- 平台记录 `helm_revision` 到 `vo_middleware_releases`，与 Helm release history 对齐。

### 12.4 持久化与存储

- 中间件必有 PVC，部署时必选 StorageClass 与容量。
- 平台校验集群存在对应 StorageClass，容量不超 Namespace ResourceQuota。
- 卷快照备份：用 `VolumeSnapshot`（需 CSI 驱动支持），最快。
- 卸载时默认保留 PVC（`--keep-history` + 不删 PVC），防止数据丢失；显式「连同数据删除」才清 PVC。

### 12.5 网络暴露

| 类型 | Service | 说明 |
| --- | --- | --- |
| 集群内访问 | ClusterIP | 默认 |
| Headless | Headless Service | StatefulSet 稳定网络名（如 `mysql-0.mysql-h`） |
| 节点端口 | NodePort | 测试环境 |
| 外部访问 | Ingress / LoadBalancer | 按需，需权限校验 |

平台在 `access_info` 记录 Service 名、端口、连接串模板，供应用连接使用。

### 12.6 健康检查与就绪观察

- 安装/升级后观察器轮询：
  - StatefulSet：`status.readyReplicas == replicas`
  - Operator CR：读 CR `.status.conditions`（如 `Ready=True`）
  - Helm：`helm status` 的 `STATUS: deployed`
- 超时按中间件类型与规模自适应（如 ES 集群初始化慢，默认 30min）。
- 失败时按变更类型处理（升级自动 rollback，安装保留供排查）。

### 12.7 中间件目录与参数 schema

`middleware_catalog.schema_config`（JSON Schema）驱动前端动态表单：

```json
{
  "auth.rootPassword": {"type":"string","secret":true,"label":"root 密码","required":true},
  "architecture": {"type":"string","enum":["standalone","replication"],"default":"standalone","label":"架构"},
  "primary.persistence.size": {"type":"string","default":"8Gi","label":"主库存储"},
  "primary.persistence.storageClass": {"type":"string","label":"StorageClass","optionsFrom":"cluster.storageClasses"},
  "metrics.enabled": {"type":"boolean","default":false,"label":"启用指标"}
}
```

- `optionsFrom` 支持从集群动态拉取选项（如 StorageClass 列表）。
- Secret 字段提交加密存储，回显掩码。

### 12.8 备份的 K8s 实现

| method | 实现 |
| --- | --- |
| `snapshot` | 创建 `VolumeSnapshot` CR，等待 `ReadyToUse=true` |
| `pg_dump` / `mysqldump` / `mongoexport` | 创建 Job，挂载 PVC 或连 Service 执行导出，结果传对象存储 |
| `xtrabackup` | Job 执行物理备份 |
| `rdb`（Redis） | Job 执行 `BGSAVE` 后拷贝 rdb 文件 |
| `velero` | 调用 Velero API 创建 Backup（含 PV） |

恢复：
- snapshot：从 `VolumeSnapshot` 创建新 PVC，挂到新实例。
- dump：Job 从对象存储拉取并导入。
- 覆盖现有实例需审批 + 二次确认。

### 12.9 大规模中间件

- 超大集群（5000+ 节点）部署大型中间件集群（如 100+ broker 的 Kafka）：
  - 优先用 Operator（Strimzi 等），Operator 自身处理分片与滚动。
  - 平台仅提交 CR，观察器读 CR status，不自行编排 Pod。
  - 升级滚动观察超时拉长（如 100 broker 升级默认 2 小时）。
- 中间件实例数大时（万级），按 `workspace_id` 分片，Helm 操作走对应集群 kubeconfig，不在平台聚合全量 Pod。

### 12.10 安全约束

- 中间件密码随机生成（除非用户指定），加密存储。
- 默认仅集群内访问；外部暴露需 admin + 审计。
- 备份含敏感数据，存储加密（S3 SSE）。
- 卸载默认保留数据，删除数据需二次确认 + 审计。

---

## 13. CI/CD 流水线的 K8s 映射

流水线编排见 [构建与发布 §14](build-release.md#14-cicd-流水线)，数据模型见 [数据模型 §7.6-7.11](data-model.md#76-pipelinescicd-流水线定义)。

### 13.1 流水线执行与 K8s

| 流水线阶段 | K8s 实现 |
| --- | --- |
| build | 调度 Jenkins Job（Jenkins 自身可跑在 K8s，pod-template 动态 agent） |
| test / scan | Jenkins Job 步骤，或平台起 Job Pod 跑测试/扫描 |
| image | Jenkins 构建推送镜像 |
| deploy | 复用分组发布能力（PATCH Deployment/StatefulSet） |
| verify | 起 Job Pod 跑冒烟测试，或调服务健康端点 |
| promote | 触发目标环境分组发布 |

- 流水线编排器（平台组件）负责阶段调度与门禁评估，不直接跑任务，任务下发 Jenkins 或 K8s Job。
- 门禁评估调外部系统（SonarQube/Trivy 结果）或读 release/build 状态。

### 13.2 部署后验证的 K8s 实现

- 冒烟测试 Job：平台生成 `Job`（按 verify 阶段参数），跑测试镜像，结果回写 `pipeline_stage_runs.gate_result`。
- 金丝雀分析：灰度期间 scrape Prometheus 指标，对比基线，异常触发回滚 release。

---

## 14. 大模型推理工作负载

大模型部署专章见 [大模型部署](model-serving.md)，数据模型见 [数据模型 §10](data-model.md#10-大模型服务)。

### 14.1 推理服务 → K8s 资源

| VortexOps 实体 | K8s 资源 | 说明 |
| --- | --- | --- |
| InferenceService | Deployment（推理 Pod）+ Service | 推理框架跑在 Deployment（无状态，权重从挂载/下载） |
| | ConfigMap（框架配置） | 启动参数、模型路径 |
| | PVC 或 hostPath（权重缓存） | 大权重文件挂载，避免每次下载 |
| ModelVersion（权重） | 对象存储 + Pod 挂载 | 权重缓存到 PV，多副本共享只读 |
| InferenceRelease | Deployment 更新 / 蓝绿（双 Deployment） | 切模型/扩缩容 |
| HPA | HorizontalPodAutoscaler | 按 QPS/队列/延迟自定义指标 |

### 14.2 推理 Pod 模板（vLLM 示例）

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {service.name}
  namespace: {service.namespace}
  labels:
    vortexops.io/inference-service: {service.name}
    vortexops.io/managed: "true"
spec:
  replicas: {service.replicas}
  selector:
    matchLabels:
      vortexops.io/inference-service: {service.name}
  template:
    metadata:
      labels:
        vortexops.io/inference-service: {service.name}
    spec:
      nodeSelector:
        accelerator: {service.gpu_type}   # GPU 型号
      tolerations:
        - key: nvidia.com/gpu
          effect: NoSchedule
      containers:
        - name: vllm
          image: vllm/vllm-openai:latest
          command: ["python","-m","vllm.entrypoints.openai.api_server"]
          args:
            - --model={model.source_ref}
            - --tensor-parallel-size={service.tensor_parallel_size}
            - --gpu-memory-utilization=0.9
            - --max-model-len={service.max_model_len}
          resources:
            limits:
              nvidia.com/gpu: {service.gpu_per_replica}
          volumeMounts:
            - name: weights
              mountPath: /models
          readinessProbe:
            httpGet: {path: /health, port: 8000}
            initialDelaySeconds: 60
            periodSeconds: 10
      volumes:
        - name: weights
          persistentVolumeClaim:
            claimName: {model.weight_pvc}   # 只读共享权重
```

### 14.3 权重挂载策略

| 策略 | 说明 |
| --- | --- |
| PVC 共享只读 | 权重缓存到 PVC（RWX 或 ROX），多副本共享，避免重复下载 |
| initContainer 下载 | Pod 启动前 initContainer 从对象存储拉权重到 emptyDir/PVC |
| 框架直连仓库 | vLLM/HF 直接从 HF 仓库流式加载（适合首次，慢） |

推荐 PVC 共享：首次部署下载权重到 PVC，后续副本/重启直接挂载，秒级就绪（权重已在 PV）。

### 14.4 多卡张量并行的调度

- `gpu_per_replica = TP * PP`，单 Pod 多卡。
- 多卡需同节点（TP）或跨节点（PP，需 RDMA）：
  - TP：`pod.spec.nodeSelector` 选多卡节点 + `topologySpreadConstraints` 避免跨节点。
  - PP：跨节点需低延迟网络（InfiniBand/RoCE），平台标注并校验。
- 平台校验节点 GPU 数 ≥ `gpu_per_replica`（TP 场景）。

### 14.5 蓝绿切模型的 K8s 实现

权重加载慢，滚动切模型会有长时间半就绪，推荐蓝绿：

1. 当前版本 = 蓝组 Deployment（label `version=blue`），Service 指向蓝。
2. 切模型时起绿组 Deployment（label `version=green`，新权重），等绿组就绪。
3. 切 Service selector 到 green，流量切到绿组。
4. 验证 OK → 删蓝组；验证失败 → 切回蓝组、删绿组。
5. 记录 `vo_inference_releases`（switch_model，blue_green）。

### 14.6 推理服务 HPA

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {service.name}
spec:
  scaleTargetRef:
    kind: Deployment
    name: {service.name}
  minReplicas: {autoscaling_min}
  maxReplicas: {autoscaling_max}
  metrics:
    - type: Pods
      pods:
        metric: {name: vllm:num_requests_waiting}
        target: {type: AverageValue, averageValue: 10}
    - type: Pods
      pods:
        metric: {name: http_requests_per_second}
        target: {type: AverageValue, averageValue: 50}
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 600   # 缩容稳定 10min，避免频繁缩扩
```

- 需集群装 Prometheus Adapter 暴露自定义指标。
- vLLM/Triton 原生暴露 metrics，经 Prometheus 采集。

### 14.7 GPU 监控

- DCGM Exporter（NVIDIA）暴露 GPU 利用率/显存/温度。
- vLLM/Triton 暴露推理指标（吞吐/延迟/队列/KV cache）。
- 平台聚合展示，超阈告警（显存 >95%、延迟 P99 突增、到顶）。
