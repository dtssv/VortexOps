# VortexOps

> 面向研发团队的 Kubernetes 应用管理平台，覆盖「代码 → 构建 → 镜像 → 发布 → 运行时配置 → CI/CD 流水线 → 大模型推理」完整生命周期。以「空间 / 应用 / 分组」三层资源模型、**菜单+动作+数据三维 RBAC**、审批流与协作通知为核心，兼顾普通应用、中间件与大模型推理服务，追求功能完整与极致使用体验。

---

## 1. 项目定位

VortexOps 解决的核心问题：

- **多团队隔离**：空间 → 应用 → 分组，对应 K8s Namespace / 应用元数据 / Deployment。
- **从源码到镜像**：Git 分支 → 构建模板 → Jenkins 流水线 → 镜像入库可追溯。
- **从镜像到运行时**：整组发布 / 部分 Pod 灰度 / 回滚；配置版本化（文件 + 命令参数）。
- **精细权限治理**：不同角色看到**不同菜单**——运维看集群管理，发布专员只看构建发布，测试人员只能灰度。
- **企业级体验**：工作台、全局搜索、收藏/最近、审批流、通知、回收站、动态流协作。

## 2. 核心能力一览

| 能力域 | 说明 |
| --- | --- |
| **工作台** | 待审批、进行中任务、收藏、趋势图，可拖拽布局 |
| **动态菜单** | 按角色权限过滤菜单树，无权限不显示 |
| **空间/应用/分组** | 三层资源 CRUD，标签、配额、回收站；工作负载类型（Deployment/StatefulSet/CronJob/Job） |
| **资源与网络** | 分组创建选 CPU/内存/磁盘/GPU 型号；网络模式+稳定 IP+公网访问开关+Ingress+NetworkPolicy；资源模板；弹性伸缩 HPA |
| **Git 集成** | 多 Provider，分支/Tag/Commit，Webhook 自动构建 |
| **构建流水线** | Java/Python/Go/Node/自定义；构建模板复用；构建中心跨空间视图 |
| **CI/CD 流水线** | 多阶段编排（build/test/scan/image/deploy/verify/promote）、质量门禁、环境晋升（dev→staging→prod）、部署后验证、制品签名/SBOM、CI/CD 看板（MTTR/DORA） |
| **制品版本** | 制品版本化历史、版本别名、随时回退到任意制品版本、保留策略、签名验签 |
| **镜像管理** | 基础镜像目录、构建产物、手动登记、扫描 + CVE 准入策略 |
| **发布管理** | 整组/灰度/回滚；发布单；生产审批；发布中心；发布预设；批量发布；发布窗口 |
| **中间件管理** | 部署 MySQL/Redis/Kafka/ES 等；Helm/Operator；参数版本化；备份恢复；连接拓扑 |
| **大模型服务** | 模型仓库/版本/权重缓存、推理框架适配（vLLM/TGI/Triton）、多卡张量并行、推理服务生命周期（蓝绿切模型）、Token 计量与配额、推理监控、多模型路由 |
| **配置管理** | 文件级 + 命令参数，版本 diff、跨分组比对、版本回退、配置集共享 |
| **权限体系** | 平台/空间/应用三级；内置+自定义角色；menu/action/data 权限码 |
| **审批流** | 生产发布/配置变更多级审批，工作台待办 |
| **通知与告警** | 站内+邮件/IM/Webhook 通知；规则化告警中心（静默/事件追踪） |
| **运维观测** | Pod 日志/exec/端口转发、跨 Pod 日志搜索、K8s 事件、资源监控、告警、HPA 观测 |
| **协作** | 动态流、发布/构建评论、@mention、新手引导 |
| **审计** | 全操作审计，字段级 diff，敏感操作高亮 |
| **系统管理** | 用户、角色、集群、Jenkins、仓库、凭证、公告 |
| **超大规模** | 10万+应用、单分组1万+副本、1000+集群；分片/分区/就近代理；S/M/L/XL 分级部署 |

## 3. 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.22+ / Chi / sqlc + pgx |
| 数据库 | PostgreSQL 16 + Redis 7 |
| K8s | client-go + controller-runtime |
| 流水线 | Jenkins REST + Job DSL |
| 前端 | React 18 + TS 5 + Ant Design 5 + Vite 5 |
| 状态 | TanStack Query + Zustand |
| 部署 | Helm Chart（S/M/L/XL 多规模方案） |

## 4. 资源层次模型

```
Workspace（空间）
├── Members + Role（admin/developer/tester/viewer 或自定义）
├── Cluster Binding + Quota
├── Application（应用，无状态）
│   ├── Members + Role
│   ├── Git Source / Build Template / Webhook
│   ├── Image（构建产物）
│   └── Group（分组 → K8s Deployment）
│       ├── Config（版本化：文件 + command/args/env）
│       ├── Release（整组 / 灰度 / 审批）
│       └── Pod / Events / Logs
└── MiddlewareInstance（中间件，有状态，与应用平级）
    ├── Catalog（类型：MySQL/Redis/Kafka/...）
    ├── Params（版本化：Helm values / Operator spec）
    ├── Release（安装/升级/扩缩容/回滚）
    ├── Backup（定时 + 手动，可恢复）
    └── Connections（应用↔中间件连接，影响面分析）
└── InferenceService（大模型推理服务，与中间件/应用平级）
    ├── Model + Version（模型与权重版本，含量化/精度）
    ├── Adapter（LoRA/QLoRA 适配器）
    ├── Release（部署/切模型/扩缩容/回滚/停止，蓝绿/灰度）
    ├── API Key（鉴权 + 限流 + Token 配额）
    └── Usage（Token 用量计量）

CI/CD 流水线（Pipeline）横跨应用，编排构建→测试→扫描→镜像→部署→验证→晋升。
```

## 5. 权限与菜单（亮点）

| 角色示例 | 可见菜单 | 不可见 |
| --- | --- | --- |
| 平台运维 | 集群管理、运维观测、审计 | 构建、发布、配置编辑 |
| 发布专员 | 构建中心、发布中心、镜像 | 集群管理、用户管理 |
| 测试人员 | 应用、构建、灰度发布 | 整组发布、配置、系统管理 |
| 空间访客 | 应用/分组只读 | 构建、发布、成员管理 |

详见 [权限体系](docs/permissions.md)。

## 6. 数据模型要点

所有业务表统一包含：

| 字段 | 说明 |
| --- | --- |
| `version` | 乐观锁 |
| `created_at/by` | 创建时间/人 |
| `updated_at/by` | 修改时间/人 |
| `deleted/deleted_at/by` | 软删除标识 |

共 **60+ 张表**（含分区子表），涵盖权限、审批、通知、流水线、大模型、对外 API 审计等。建表脚本见 [schema.sql](schema.sql)。详见 [数据模型](docs/data-model.md)。

## 7. 文档导航

文档按功能域归类组织，新增功能请放入对应域文档，不要单独建文件。

| 文档 | 功能域 |
| --- | --- |
| [架构设计](docs/architecture.md) | 整体架构、模块划分、关键时序、**元数据存储决策（不用 etcd/zk）**、跨切面 |
| [部署文档](docs/deployment.md) | **多规模部署方案**（S/M/L/XL）、依赖组件安装（PG/Redis/Kafka/ES/MinIO/Jenkins）、HA/灾备/网络/安全 |
| [数据模型](docs/data-model.md) | 全部表设计、索引、约束、迁移策略 |
| [schema.sql](schema.sql) | PostgreSQL 建表脚本（扩展、表、索引、分区、seed） |
| [权限体系](docs/permissions.md) | 角色、菜单树、权限码、动作矩阵、判定流程（权限相关全部） |
| [构建与发布](docs/build-release.md) | Git、构建、镜像、发布、配置发布、CI/CD 流水线、环境晋升（构建发布相关全部） |
| [Kubernetes 集成](docs/kubernetes.md) | 多集群、资源映射、配置注入、Pod/日志/事件、灰度 K8s 实现、CI/CD 流水线 K8s 映射、大模型推理工作负载 |
| [大模型部署](docs/model-serving.md) | 模型仓库/版本/权重、推理框架适配、GPU 调度、推理服务生命周期、Token 计量、推理监控 |
| [协作与体验](docs/collaboration.md) | 工作台、搜索、收藏、审批、通知、动态流、评论、公告、回收站、配额、标签、流水线看板、推理监控 |
| [扩展性设计](docs/scalability.md) | 超大规模（10万应用/万副本）适配：分片、分区、Pod不落库、Informer分片、MQ、WS网关 |
| [API 设计](docs/api.md) | REST + WebSocket 全端点 |
| [前端设计](docs/frontend.md) | 动态菜单、工作台、页面结构、状态管理 |
| [使用操作说明](docs/usage-guide.md) | 面向用户的操作手册：从入门到各功能实操 |

## 8. 目录结构（规划）

```
VortexOps/
├── README.md
├── schema.sql              # PostgreSQL 建表脚本
├── docs/
├── backend/
│   ├── cmd/apiserver/
│   ├── internal/
│   │   ├── domain/
│   │   ├── application/
│   │   ├── infrastructure/
│   │   └── interfaces/
│   └── migrations/
├── frontend/
└── deploy/helm/
```

## 9. 里程碑

| 里程碑 | 内容 |
| --- | --- |
| M1 | 用户/空间/应用/分组 + 权限码/动态菜单/角色（分片键、Pod不落库决策落地） |
| M2 | Git + Jenkins 构建 + 构建模板 + 构建中心 |
| M3 | 整组/灰度发布 + 发布中心 + 审批流 |
| M4 | 配置版本 + diff + 跨分组比对 + 配置集 + 回收站 |
| M5 | 工作台 + 通知 + 审计 + Pod 日志/exec/端口转发 + 跨 Pod 日志搜索（就近代理） |
| M6 | 全局搜索（ES）+ 协作评论 + 制品版本管理/回退 + 新手引导 |
| M7 | 中间件部署（Helm/Operator）+ 参数版本 + 备份恢复 + 连接拓扑 |
| M8 | 工作负载类型（StatefulSet/CronJob/Job）+ HPA 弹性伸缩 + 镜像扫描 + CVE 准入 |
| M9 | 告警中心 + 发布预设/批量发布/发布窗口 + 监控集成 + IM 通知 |
| M10 | 大规模演进：Citus 分片 + syncer 分片 + ws-gateway + Kafka 异步 + 边缘代理分片 |
| M11 | CI/CD 流水线编排 + 质量门禁 + 环境晋升 + 制品签名/SBOM + CI/CD 看板（MTTR/DORA） |
| M12 | 大模型服务：模型仓库/版本 + 推理框架适配（vLLM/Triton）+ GPU 多卡调度 + 推理服务生命周期 + Token 计量 + 推理监控 |

## 10. 非功能性目标

- **体验**：无权限菜单不显示；长任务后台进行 + 通知；危险操作二次确认。
- **安全**：凭证 KMS 加密；Secret 默认掩码；操作全审计。
- **可靠**：乐观锁防并发；Jenkins/K8s 状态对账；发布并发锁。
- **可扩展**：Git/构建/发布/通知均为接口抽象，可插拔。

## 11. 文档维护约定

- **按功能域归类**：新功能优先放入已有域文档；仅当确属全新独立域时新建文件，并在本 README 导航登记。
- **跨文档引用**：使用相对链接，如「详见 [权限体系](docs/permissions.md)」。
- **同步更新**：数据模型/权限/API 变更时，同步更新对应域文档与本 README 导航。
