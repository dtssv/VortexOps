# 大模型部署

涵盖大模型（LLM/多模态/embedding）的权重管理、推理框架适配、GPU 调度、推理服务生命周期、Token 计量与监控。大模型作为独立域挂在空间下，与中间件/应用平级，复用权限、审批、通知、审计、告警。数据模型见 [数据模型 §10](data-model.md#10-大模型服务)，K8s 映射见 [K8s 集成 §大模型推理工作负载](kubernetes.md#大模型推理工作负载)。

---

## 1. 与普通应用/中间件的差异

| 维度 | 普通应用 | 中间件 | 大模型推理 |
| --- | --- | --- | --- |
| 工作负载 | Deployment | StatefulSet/Operator | 专用推理框架（vLLM/TGI/Triton） |
| 镜像/产物 | 容器镜像 | Chart/Operator | 模型权重 + 推理引擎镜像 |
| 资源 | CPU/内存为主 | CPU/内存/存储 | GPU 显存为核心，多卡并行 |
| 启动 | 秒级 | 秒~分钟 | 分钟级（权重加载） |
| 变更 | 滚动镜像 | 升级/扩缩容 | 切模型权重/扩缩容/热切换适配器 |
| 伸缩 | HPA(CPU/内存) | 副本数 | HPA(QPS/显存/队列) |
| 计量 | 不涉及 | 不涉及 | Token 用量、请求延迟 |
| 访问 | Service | Service | 推理 API（OpenAI 兼容）+ API Key |

---

## 2. 模型仓库与权重管理

### 2.1 模型仓库（`vo_model_registries`）

- 平台级配置模型来源：HuggingFace / OSS / S3 / Nexus / 自定义。
- 支持私有仓库（凭证加密存储）。
- 可设默认仓库。

### 2.2 模型与版本（`vo_models` / `vo_model_versions`）

- **模型**：空间级，如 `qwen2-72b`，标注 modality（text/multimodal/embedding）与首选 framework。
- **版本**：每个模型可有多个权重版本，含：
  - 来源引用（HF repo@rev / S3 key）
  - 量化（none/int8/int4/awq/gptq/fp8）
  - 精度（fp16/bf16/fp32）
  - 上下文长度、参数量（B）
  - 权重大小、校验和
- **权重缓存**：大权重文件不进 Git，平台从仓库下载后缓存到对象存储（`weight_storage_key`），部署时挂载或拉取到 Pod，避免重复下载。
- **下载状态**：`pending`/`downloading`/`ready`/`failed`，便于追踪大文件下载进度。
- **版本回退**：推理服务可切回历史权重版本（`inference_releases.type=switch_model`）。

### 2.3 适配器（`vo_model_adapters`，LoRA/QLoRA）

- 在基座模型上挂载轻量适配器（LoRA/QLoRA），实现领域微调低成本切换。
- 多适配器可挂载到同一基座，运行时按请求选择（vLLM/Triton 支持 multi-LoRA）。
- 适配器权重小（MB~GB级），切换快，适合多租户/多场景共用基座。

---

## 3. 推理框架适配

| 框架 | 适用 | 平台封装 |
| --- | --- | --- |
| **vLLM**（推荐） | 高吞吐 LLM 推理，PagedAttention | 渲染 vLLM 启动参数，支持张量并行、continuous batching |
| **TGI**（Text Generation Inference） | HuggingFace 生态 LLM | 渲染 TGI 参数 |
| **Triton Inference Server** | 多框架（TensorRT/PyTorch/TF）、多模态、embedding | 渲染 model repository 配置 |
| **SGLang** | 结构化生成、高吞吐 | 渲染 SGLang 参数 |
| **Ollama** | 小模型、本地/边缘 | 简化部署 |
| **custom** | 自研推理引擎 | 自定义镜像 + 启动命令 |

平台通过 `InferenceFramework` 适配器抽象，每种框架封装为：参数 schema（动态表单）→ 渲染启动命令 → 健康检查 → 就绪观察。新增框架实现适配器即可。

---

## 4. 推理服务生命周期（`vo_inference_services` / `vo_inference_releases`）

### 4.1 部署推理服务

1. 选模型 + 版本 + 推理框架。
2. 选集群/Namespace、副本数、GPU 卡数与型号、张量并行度。
3. 填框架参数（max_model_len、gpu_memory_utilization 等，按 schema 渲染）。
4. 是否启用自动伸缩（QPS/显存）、是否需 API Key、是否需审批。
5. 提交 → `deploy` release → 拉权重 → 启动推理 Pod → 健康检查 → running → 生成 endpoint_url。

### 4.2 变更类型

| 变更 | type | 说明 |
| --- | --- | --- |
| 部署 | `deploy` | 首次部署 |
| 切模型 | `switch_model` | 切换权重版本/量化/适配器，可蓝绿/灰度 |
| 扩缩容 | `scale` | 改副本数（增减 GPU） |
| 配置变更 | `config` | 改框架参数（如 max_model_len） |
| 回滚 | `rollback` | 回到历史 release（权重+配置） |
| 停止 | `stop` | 缩到 0/卸载（释放 GPU） |

### 4.3 切模型策略

| 策略 | 说明 |
| --- | --- |
| `rolling` | 滚动切，新副本加载新权重，旧副本逐步下线（期间两版本并存） |
| `blue_green` | 蓝绿：先起绿组（新权重）验证，再切流量、下蓝组（权重加载慢时推荐，避免滚动期双版本） |
| `canary` | 灰度：少量副本跑新权重，按流量比例验证再全量 |

> 切模型权重加载耗时长（大模型数分钟），蓝绿/灰度优于滚动，避免长时间半就绪。

### 4.4 健康检查与就绪

- 推理框架通常提供 `/health` 或 `/v1/models` 端点。
- 就绪条件：Pod ready + 模型加载完成（框架就绪端点 200）。
- 超时按模型大小自适应（如 72B 默认 15min）。
- 失败按变更类型处理（切模型失败自动回滚到 previous）。

---

## 5. GPU 调度

### 5.1 GPU 资源映射

| 平台字段 | K8s 字段 |
| --- | --- |
| `gpu_per_replica` + `gpu_resource_name` | `containers[].resources.limits.nvidia.com/gpu` |
| `gpu_type` | `nodeSelector`（GPU 型号）+ `tolerations`（GPU 污点） |
| `tensor_parallel_size` | 框架 `--tensor-parallel-size` 参数 + 多卡共享 |
| `pipeline_parallel_size` | 框架 `--pipeline-parallel-size` |

### 5.2 多卡张量并行

- 大模型单卡装不下（如 72B 需 2~4 张 A100），用张量并行（TP）切分到多卡。
- `gpu_per_replica = tensor_parallel_size`，单副本占多卡。
- 平台校验：GPU 卡数 = TP * PP（张量并行 * 流水线并行）。
- 多卡 Pod 需调度到同节点（或 RDMA 跨节点），平台自动加亲和性约束。

### 5.3 显存调度

- `gpu_memory_utilization`（vLLM）：控制预分配显存比例，留余量给 KV cache。
- 平台不直接管显存分配，由框架处理；但监控显存利用率，触发告警（>95% 可能 OOM）。
- 量化（int4/int8/awq/gptq/fp8）降低显存占用，平台在模型版本标注，部署时按版本自动传参。

### 5.4 GPU 共享（可选高级）

- 小模型可多副本共享单卡（MIG / 时间片），提高利用率。
- 平台支持配 `gpu_fraction`（如 0.5 卡），映射到 `nvidia.com/gpu.shared`。
- 适合 embedding/小模型场景，大模型不建议共享。

---

## 6. 推理服务访问与 API Key

### 6.1 访问端点

- 默认 ClusterIP（集群内调用）。
- 可选 Ingress（外部访问，需鉴权）。
- `endpoint_url` 记录访问地址。

### 6.2 OpenAI 兼容 API

- vLLM/TGI/SGLang 默认提供 OpenAI 兼容 `/v1/chat/completions`、`/v1/embeddings`。
- 平台不重写协议，透传到框架；调用方用标准 OpenAI SDK 即可。

### 6.3 API Key（`vo_inference_api_keys`）

- 每个推理服务可签发多个 API Key（`sk-xxx`）。
- 限流：每分钟请求数（`rate_limit_per_min`）。
- 配额：每日 Token 上限（`token_quota_per_day`）。
- 可设过期、撤销、查看最后使用时间。
- 鉴权由平台网关层校验（或框架原生鉴权），统一计量。

---

## 7. Token 计量与配额（`vo_inference_usage`）

- 每次推理请求记录：prompt_tokens / completion_tokens / total_tokens / 延迟 / 状态码 / 调用方 / 模型版本。
- 用途：
  - 按 API Key / 调用方 / 服务统计用量，出账与成本分摊。
  - 配额超限拒绝（429）。
  - 监控异常用量（如某 Key 突然激增）。
- 高写入表，按月分区，热数据 3 个月。
- 采集方式：平台网关层计量（推荐，统一）或框架日志解析。

---

## 8. 自动伸缩

推理服务自动伸缩与普通应用不同，按推理指标：

| 指标 | 说明 |
| --- | --- |
| QPS | 每秒请求数，超阈值扩容 |
| 队列长度 | 待处理请求积压（如 vLLM `num_requests_waiting`） |
| GPU 显存 | 显存利用率（防 OOM，非主要扩缩指标） |
| 延迟 P99 | 延迟升高扩容 |

- 用 HPA + Prometheus Adapter（自定义指标）。
- `autoscaling_min/max` 限制范围。
- 缩容需谨慎（权重加载慢，频繁缩扩代价高），配稳定窗口（默认缩容稳定 10min）。
- 到达 max 仍高负载 → 告警（`inference_overload`）。

---

## 9. 推理监控

| 指标 | 说明 |
| --- | --- |
| 吞吐 | tokens/s、requests/s |
| 延迟 | TTFT（首 token 延迟）、P50/P95/P99 |
| 队列 | 等待请求数、排队时长 |
| 显存 | 每卡显存利用率、KV cache 占用 |
| GPU 利用率 | 计算利用率 |
| 错误率 | 5xx 比例、OOM 次数 |
| 用量 | Token 消耗趋势、按调用方分布 |

- 集成 Prometheus（vLLM/Triton 原生暴露 metrics）。
- 告警：延迟突增、错误率升高、显存接近上限、服务不可用、到顶（max replicas 仍过载）。

---

## 10. 多模型路由（`vo_inference_routes`，可选）

- 把多个推理服务组成路由组，统一入口。
- 策略：
  - `weighted`：按权重分流（A/B 测试、新旧模型对比）。
  - `header`：按请求头路由（如 `X-Model: v2` 走新模型）。
  - `failover`：主故障自动切备。
- 用途：模型迭代对比、灾备、按场景路由。

---

## 11. 安全与合规

- 模型权重属敏感资产，下载/缓存加密，访问需权限。
- 推理 API Key 妥善保管，撤销即失效。
- 输入/输出审计（可选）：记录请求摘要（不含敏感内容）用于合规与滥用排查。
- 内容安全（可选）：集成内容审核，违规输入/输出拦截。
- GPU 资源昂贵，配额管控（空间级 GPU 卡数上限）。

---

## 12. 失败与重试

| 场景 | 处理 |
| --- | --- |
| 权重下载失败 | release failed；可重试下载；缓存断点续传 |
| 权重校验失败 | 拒绝部署（校验和不匹配，可能损坏/篡改） |
| Pod OOM | 健康检查失败 → release failed；提示降量化或加 GPU |
| 模型加载超时 | 按模型大小自适应超时；超时 failed，可重试或换量化版本 |
| 切模型失败 | 自动回滚到 previous 权重版本 |
| GPU 不足调度 | Pod Pending；提示空间 GPU 配额不足或集群无可用 GPU |
| 推理框架崩溃 | Pod 重启；持续崩溃 → release failed 告警 |

---

## 13. 典型场景

### 13.1 部署 Qwen2-72B（vLLM，4 卡张量并行）

1. 模型仓库接入 HuggingFace → 添加模型 `qwen2-72b` → 版本 `instruct-v1`（bf16）。
2. 平台下载权重到对象存储（约 140GB），状态 ready。
3. 新建推理服务：framework=vllm，gpu_per_replica=4，tensor_parallel_size=4，gpu_type=nvidia-a100，max_model_len=32768。
4. 提交 deploy → 起 Pod（4 卡）→ 加载权重 → 就绪 → endpoint_url 生成。
5. 签发 API Key，用 OpenAI SDK 调用 `/v1/chat/completions`。

### 13.2 切换到 int4 量化版本（省显存）

1. 模型新增版本 `instruct-v1-int4`（awq 量化，约 40GB）。
2. 推理服务 → 切模型 → 选 int4 版本 → 蓝绿策略。
3. 起绿组（2 卡即可装下）→ 验证 → 切流量 → 下蓝组。
4. GPU 占用从 4 卡降到 2 卡/副本，吞吐略降但成本大降。

### 13.3 多租户 LoRA 适配器

1. 基座 `llama3-8b` 部署推理服务（1 卡）。
2. 为不同业务上传 LoRA 适配器（客服/代码/摘要）。
3. 调用时按 `model` 参数选适配器，共享基座，省显存。

---

如需了解 K8s 映射细节见 [Kubernetes 集成 §大模型推理工作负载](kubernetes.md#大模型推理工作负载)，权限见 [权限体系](permissions.md)，API 见 [API 设计](api.md)。
