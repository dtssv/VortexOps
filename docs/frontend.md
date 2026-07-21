# 前端设计

## 1. 技术栈

| 项 | 选型 |
| --- | --- |
| 框架 | React 18 + TypeScript 5 |
| 构建 | Vite 5 |
| UI 库 | Ant Design 5 |
| 路由 | React Router 6 |
| 服务端状态 | TanStack Query 5 |
| 客户端状态 | Zustand 4 |
| 表单 | Ant Design Form + 自定义校验 |
| 图表 | ECharts（发布/构建趋势） |
| 代码编辑器 | Monaco Editor（Dockerfile / 配置文件编辑） |
| 实时 | 原生 WebSocket + 自定义 Hook |
| 国际化 | i18next（中/英） |
| 测试 | Vitest + React Testing Library + Playwright（E2E） |
| 代码规范 | ESLint + Prettier + Husky + lint-staged |

## 2. 目录结构

```
frontend/
├── src/
│   ├── api/                  # API 客户端（按模块）
│   │   ├── client.ts         # axios 实例 + 拦截器
│   │   ├── auth.ts
│   │   ├── workspaces.ts
│   │   ├── applications.ts
│   │   ├── groups.ts
│   │   ├── builds.ts
│   │   ├── releases.ts
│   │   └── configs.ts
│   ├── components/           # 通用组件
│   │   ├── Layout/
│   │   ├── PageContainer/
│   │   ├── ResourceStatus/   # 状态标签
│   │   ├── JsonEditor/       # Monaco 封装
│   │   ├── DiffViewer/
│   │   ├── PermissionGate/
│   │   ├── DynamicMenu/      # 后端驱动的动态菜单
│   │   ├── CommandPalette/ # Ctrl+K 全局搜索
│   │   └── NotificationBell/
│   ├── features/             # 业务模块
│   │   ├── dashboard/        # 工作台
│   │   ├── auth/
│   │   ├── workspaces/
│   │   ├── applications/
│   │   ├── groups/
│   │   ├── images/
│   │   ├── builds/
│   │   ├── releases/
│   │   ├── configs/
│   │   ├── approvals/
│   │   ├── notifications/
│   │   ├── recycle-bin/
│   │   ├── audit/
│   │   └── admin/            # 用户/角色/集群/基础镜像
│   ├── hooks/
│   │   ├── useWebSocket.ts
│   │   ├── usePermission.ts
│   │   ├── useDynamicRoutes.ts
│   │   └── useCommandPalette.ts
│   ├── routes/               # 路由定义与守卫
│   ├── stores/               # Zustand stores
│   │   ├── authStore.ts
│   │   └── uiStore.ts
│   ├── types/                # TS 类型
│   ├── utils/
│   ├── App.tsx
│   └── main.tsx
└── package.json
```

## 3. 路由与导航

### 3.1 动态菜单机制

- 登录后请求 `GET /me/menus`，后端返回**已过滤**的菜单树。
- `DynamicMenu` 组件渲染侧边栏；`useDynamicRoutes` 将菜单转为 React Router 路由（懒加载）。
- **无权限的菜单不渲染**（非 disabled），避免用户看到不可用的入口。
- 平台角色不同看到不同菜单：运维见「集群管理」，发布专员见「构建与发布」，互不可见。
- 详见 [菜单与权限码设计](menu-permissions.md)。

### 3.2 顶层布局

```
┌─────────────────────────────────────────────────────────────┐
│ Logo │ 空间/应用切换器 │ Ctrl+K 搜索 │ 通知(3) │ 用户菜单    │
├──────────┬──────────────────────────────────────────────────┤
│ 我的收藏  │                                                  │
│ ──────── │                                                  │
│ 动态菜单  │              内容区                              │
│ (后端驱动)│                                                  │
│  · 工作台 │                                                  │
│  · 构建发布│                                                  │
│  · 集群管理│  (仅运维可见)                                    │
│  · 系统管理│  (仅 admin 可见)                                 │
└──────────┴──────────────────────────────────────────────────┘
```

侧边栏顶部：**收藏夹** + **最近访问**（来自 `vo_user_preferences`），与动态菜单独立。

### 3.3 路由表（静态骨架 + 动态注册）

| 路径 | 页面 | 权限 |
| --- | --- | --- |
| `/` | 工作台 | 登录 |
| `/search` | 全局搜索结果 | 登录 |
| `/builds` | 构建中心（跨空间） | `menu:build:center` |
| `/releases` | 发布中心 | `menu:release:center` |
| `/approvals` | 我的审批 | 有审批权限 |
| `/recycle-bin` | 回收站 | `action:recycle:view` |
| `/login` | 登录 | 公开 |
| `/workspaces` | 空间列表 | 登录 |
| `/workspaces/:wsUuid` | 空间详情（含应用列表/成员/集群/审计 Tab） | 空间可见 |
| `/workspaces/:wsUuid/applications/new` | 新建应用 | developer+ |
| `/applications/:appUuid` | 应用详情（分组/镜像/构建/发布/成员/Git 源 Tab） | 应用可见 |
| `/applications/:appUuid/groups/:groupUuid` | 分组详情（概览/Pod/配置/发布历史 Tab） | 应用可见 |
| `/applications/:appUuid/builds/:buildUuid` | 构建详情（日志流） | 应用可见 |
| `/applications/:appUuid/builds/new` | 新建构建 | tester+ |
| `/applications/:appUuid/releases/new` | 新建发布 | tester+ |
| `/admin/users` | 用户管理 | `menu:admin:user` |
| `/admin/roles` | 角色权限 | `menu:admin:role` |
| `/admin/clusters` | 集群管理 | `menu:admin:cluster` |
| `/admin/base-images` | 基础镜像目录 | platform_admin |
| `/audit` | 全局审计 | platform_admin |
| `/me` | 个人中心 | 登录 |

### 3.4 路由守卫

- `RequireAuth`：未登录跳 `/login`。
- `RequirePermission(code)`：校验 `action:*` 或 `menu:*` 权限码。
- `RequireAnyPermission(codes)`：满足其一即可。
- 直接访问无权限 URL → 403 页（非跳登录）。

## 4. 工作台页面

- 可拖拽卡片网格（react-grid-layout），布局存 `user_preferences.dashboard_layout`。
- 卡片：待审批、进行中构建/发布、最近失败、收藏、最近访问、公告、趋势图。
- 空状态：新用户引导步骤条。
- 详见 [增强能力](features.md)。

## 5. 全局搜索（Command Palette）

- `Ctrl+K` 唤起，模糊搜索空间/应用/构建/镜像/菜单。
- 结果分组 + 键盘导航；Enter 跳转。
- 无权限资源不出现在结果中。

## 6. 关键页面交互

### 6.1 空间详情

- Tab：应用 / 成员 / 集群绑定 / 审计 / 设置。
- 应用列表：卡片或表格，展示名称、分组数、最近构建/发布、状态。
- 成员管理：表格 + 抽屉表单（添加/改角色），角色下拉含说明。
- 集群绑定：列表 + 「绑定集群」选择器（选集群 + 填 Namespace + 角色）。

### 6.2 应用详情

- Tab：分组 / 镜像 / 构建 / 发布 / 配置 / Git 源 / 成员 / 设置。
- 顶部信息条：应用名、Git 仓库、当前空间、角色徽标。
- 「新建构建」按钮：tester+ 可见。

### 6.3 新建构建（核心流程）

分步表单（Steps）：

1. **代码来源**
   - 选择 Git 源；支持「从构建模板创建」一键填充
   - 选择分支/Tag（异步加载，Select 远程搜索）
   - 展示最近 Commit（SHA、作者、时间、message）
2. **构建配置**
   - 构建策略：Java / Python / Go / Node / 自定义
     - 选「自定义」时展示命令输入框（Monaco，shell 语法）
   - 基础镜像：从 `vo_base_images` 选择（按 runtime 过滤）
   - Dockerfile 来源：模板 / 自定义
     - 选「自定义」时展示 Monaco Dockerfile 编辑器（带基础镜像 FROM 提示）
3. **镜像目标**
   - Registry：下拉（应用默认 + 可改）
   - Repository：默认 `{workspace}/{app}`，可改
   - Tag：留空则自动生成规则预览（如 `main-abc1234-20260620-1`）
4. **确认**
   - 汇总展示，二次确认
   - 提交 → 跳转构建详情，WebSocket 推日志

### 6.4 构建详情

- 顶部状态卡片：状态、耗时、Commit、产出镜像（成功则可点「发布」跳转）。
- 步骤时间线（checkout/package/build_image/push/callback）。
- 日志区：终端风格，自动滚动到底部；可暂停滚动；支持搜索、下载。
- 日志通过 WebSocket 实时接收，断线自动重连。

- 失败时展示「相同参数重试」按钮。

### 6.5 新建/编辑分组（资源与网络选择）

分步或单页表单（关键交互）：

1. **基础信息**
   - 分组名、显示名、环境、目标集群、Namespace、副本数。
   - 工作负载类型：Deployment（默认）/ StatefulSet / CronJob（填 cron 表达式，带可视化预览）/ Job（填 completions/parallelism）。
2. **资源规格**（核心）
   - 「套用模板」：从 `resource-templates` 选预设规格（如 `标准-4C8G`、`GPU-8C32G+1*A100`），一键填充后可微调。
   - CPU：滑块 + 输入（核数，转毫核），可设请求/上限。
   - 内存：输入（GB/MB），可设请求/上限。
   - 磁盘：临时盘大小 + StorageClass 下拉（来自集群节点池画像）。
   - 临时存储（ephemeral-storage）：请求/上限。
   - GPU：卡数 + 型号下拉（按集群 `vo_cluster_node_pools` 的 `gpu_type` 过滤，仅展示该集群有的型号）。
     - 选 GPU 后自动提示「将自动添加 nodeSelector 与 tolerations」，并展示将生成的调度约束。
   - 实时预览「将生成的 K8s resources YAML」片段。
3. **网络配置**
   - 网络模式：ClusterIP / NodePort / LoadBalancer / HostNetwork。
   - 端口映射表格：名称、端口、目标端口、协议。
   - 稳定 IP（`keepPodIp`）：开关。开启后提示「重新部署/滚动更新后容器 IP 保持不变（自动保留）」，并显示集群稳定 IP 池剩余可用数；池不足时禁用并提示。
   - 公网访问（`allowEgressInternet`）：开关。
     - 默认关闭（更安全，禁止出公网）。
     - 关闭时可配置公网白名单（`egressAllowlist`）：CIDR 或域名+端口表格（如镜像仓库、npm registry）。
     - 开启时提示安全风险，生产环境需审批。
   - Ingress：开关 + host/path。
   - NetworkPolicy（入流量）：开关（默认仅放行同 Namespace）。
   - HostNetwork / 稳定 IP / 禁止公网等高级选项：需 `action:group:advanced-network`，二次确认。
4. **调度**（高级，可折叠）
   - nodeSelector 键值对、节点亲和性、容忍污点、PriorityClass。
   - GPU 型号选定时此处自动联动填充。
5. **策略与弹性**：滚动/重建、maxSurge/maxUnavailable、健康检查、发布需审批；弹性伸缩（HPA）开关 + min/max + 指标（CPU/内存/自定义）。
6. **确认**：汇总资源/网络/调度/弹性摘要 + diff 预览（编辑时），提交生成配置版本并（可选）触发发布。

> 资源/网络变更不即时生效，提交后进入配置版本 + 发布流程，便于审计回滚。前端在选择 GPU 型号、稳定 IP 时强校验集群能力，避免提交后被 K8s 拒绝调度。

### 6.6 分组详情

- Tab：概览 / Pod / 配置 / 配置集 / 制品版本 / 弹性伸缩 / 发布历史 / 事件 / YAML。
- 概览：当前镜像（制品版本号+digest）、副本数（期望/可用/HPA 范围）、资源规格（CPU/内存/GPU/磁盘）、网络模式与稳定 IP/公网访问、资源使用率、最近发布、配置版本。
- Pod 列表：状态、节点、IP、重启次数、启动时间；行操作「查看日志」「exec」「端口转发」「重启」（权限控制）。
  - 日志：抽屉内终端风格 + 跨 Pod 搜索栏（关键词高亮、跳转实时流）。
  - exec：xterm.js 终端，支持 resize、复制粘贴；生产 Pod exec 二次确认。
- 弹性伸缩 Tab：HPA 开关、min/max、指标表格（CPU/内存/自定义）、实时副本数曲线、伸缩事件列表。
- YAML Tab：只读展示渲染后的 K8s 工作负载 YAML（Monaco，可复制），便于高级用户核对。
- 配置 Tab 见 6.7。
- 发布历史：时间线，每条含镜像、配置版本、类型、操作人、状态、操作（回滚/查看事件）。
- 顶部操作：「克隆分组」「批量操作」入口。

### 6.7 配置管理

- 顶部：当前版本号 + 「新建版本」按钮 + 「跨分组比对」按钮。
- 版本列表：版本、变更说明、操作人、时间、状态（当前/历史）。
- 版本对比：选两个版本 → DiffViewer 展示差异：
  - 文件级：按 path 展示新增/删除/修改，文件内容用 Monaco diff editor（行级 diff）。
  - 命令参数：command/args/env 键值差异表格。
  - 资源/网络段：CPU/内存/GPU/网络模式等差异。
  - Secret 字段仅显示「是否变更」，不显示明文。
- 跨分组比对：「选择两个分组」→ 比对各自当前生效配置（或指定版本），展示同 path 文件差异、env 差异、资源差异、关联配置集差异；支持「复制某分组配置到另一分组」（生成草稿）。
- 编辑（新建版本抽屉）：
  - 文件配置：表格行（路径、权限、是否 Secret），点编辑弹出 Monaco（按扩展名高亮）；支持上传文件。
  - 命令参数：command/args 输入（数组可视化，每行一项）；env 键值对表格，可标记 Secret。
  - envFrom：引用选择器。
  - 变更说明输入。
- 应用版本：二次确认 → 触发 config_only release → 跳转发布详情观察。
- 回滚：选历史版本 → 「回滚到此版本」二次确认 → 触发 rollback release。

- 编辑时 localStorage 草稿自动保存。

### 6.8 配置集（ConfigSet）

- 配置集列表页（空间/应用维度）：名称、scope、当前版本、关联分组数、最近更新。
- 配置集详情：
  - 基本信息与合并策略（overlay/prepend/append）。
  - 版本历史：版本、变更说明、操作人、时间、是否 current；支持版本间比对（同配置 DiffViewer）。
  - 新建版本抽屉（结构同分组配置编辑）。
  - 关联分组 Tab：展示关联了哪些分组、是否锁版本（pinned）、优先级；「升级影响面」提示（升级 current 后哪些非 pinned 分组下次发布会变）。
- 分组「配置集」Tab：
  - 已关联配置集列表（含优先级、锁版本状态、贡献的配置摘要）。
  - 「关联配置集」操作：选配置集、设优先级、是否锁定版本（锁定可选具体版本，否则跟随 current）。
  - 「解析最终配置」按钮：展示合并后（配置集 + 自身）的最终生效配置，便于确认。

### 6.9 制品版本（镜像版本）

- 应用「制品版本」Tab / 分组「制品版本」Tab：
  - 版本列表：版本号、tag、digest、git commit（短 SHA + message）、来源构建、扫描状态、使用分组、是否别名、是否曾被回退。
  - 版本别名管理：为版本打/移除别名（stable/production/canary），别名可在版本间移动（拖拽或下拉）。
  - 行操作：「回退到该版本」（跳转发布向导，预填该制品版本，以 digest 为准）、「查看构建详情」。
  - 回退发布向导：展示「当前版本 → 目标版本」差异（commit、tag、digest），可选同时改配置，走与正常发布一致的滚动/审批流程。
  - 保留策略：展示保留数与可清理数，「清理旧版本」按钮（被别名引用/近期回退的受保护，不可清）。

### 6.10 新建发布

弹窗或抽屉：
- 「套用预设」：从 `release-presets` 选常用参数一键填充。
- 发布类型：整组 / 灰度 / 纯配置变更 / 回退制品版本
- 选择镜像：列表（构建产物 + 手动登记），展示版本号/Tag/Commit/构建时间/大小/扫描状态；CVE 准入不通过则置灰并提示
- 选择配置版本：默认当前，可改；展示关联配置集是否将变更
- 灰度时填：灰度 Pod 数或比例（提示「最大 N」），可滚动预览影响
- 变更说明
- 提交 → 跳转发布详情，实时事件流（patched / pod_ready / pod_failed ...）

- prod 环境红色警示 + 二次确认；若需审批则提交后跳转审批状态页。
- 发布窗口外：提示「当前不在发布窗口」，需 admin 强制（二次确认 + 审计）。

### 6.10.1 批量发布

- 选择多个分组 + 同一镜像/配置 → 批量发布总览页：每组一行（分组、当前版本、目标版本、状态、进度）。
- 串行/并行切换；失败时按 `stopOnFailure` 决定是否停止后续。
- 单组点进可看该组发布详情。

### 6.11 发布详情

- 状态卡片 + 进度条
- 事件时间线（来自 release_events）
- 关联 Pod 状态变化
- 操作按钮（按状态与权限）：
  - 灰度中：「提升为整组」「放弃灰度」
  - 进行中：「终止」
  - 已完成：「回滚到此版本」

- 「评论」Tab：团队协作讨论，支持 @mention。

### 6.12 新手引导与空状态

- **首次登录引导**（Onboarding Tour）：检测 `user_preferences.onboarded`，未引导则启动分步浮层导览：工作台 → 空间 → 应用 → 分组 → 构建 → 发布，每步可跳过/结束，完成后标记已引导。
- **空状态引导**：每个列表为空时展示插画 + 「创建」按钮 + 一句话说明 + 「查看文档」链接：
  - 无空间：「创建你的第一个空间」
  - 无应用：「在空间下创建应用」
  - 无分组：「为应用创建分组并选择资源规格」
  - 无构建/发布：引导从绑定 Git 源开始
- **模板快速开始**：创建应用/分组时提供「从模板创建」入口（预填示例配置），降低上手门槛。
- **上下文帮助**：复杂字段（GPU 型号、固定 IP、HPA 指标、CVE 准入）旁有「?」悬浮说明 + 「了解更多」跳转使用说明文档。
- **快捷键面板**：`?` 展示全局快捷键（搜索、新建、跳转等）。
- **角色引导**：根据用户角色（developer/tester/admin）默认聚焦不同入口（developer 聚焦构建发布，ops 聚焦集群中间件）。

### 6.13 CI/CD 流水线

- **流水线列表**：卡片/列表展示，含触发方式、最近 run 状态、成功率。
- **流水线编辑器（可视化）**：拖拽编排阶段（build/test/scan/image/deploy/verify/promote），阶段卡片可配置门禁、失败策略、参数；阶段间连线显示执行顺序，并行阶段并列。支持 YAML/可视化双视图切换。
- **阶段配置动态表单**：选 deploy 阶段填目标分组/环境；选 scan 阶段填扫描器与阈值；按阶段类型 schema 渲染。
- **执行历史**：列表展示各 run，含触发者、commit、状态、耗时、各阶段缩略状态条（绿/红/灰/黄=暂停）。
- **执行详情（核心）**：阶段泳道图，每阶段卡片显示状态、耗时、门禁结果、关联 build/release 链接；点阶段看日志流；人工门禁阶段显示「通过/拒绝」按钮。
- **实时日志**：WS 推送，阶段日志分块着色，支持搜索/下载。
- **环境晋升视图**：dev→staging→prod 流向图，显示当前各环境制品版本与晋升状态。
- **CI/CD 看板**：成功率、MTTR、DORA 指标卡片 + 趋势图；阶段耗时分布柱状图定位瓶颈；失败原因聚类。

### 6.14 大模型服务

- **模型列表**：卡片展示模型，含 modality、framework、最新版本、关联推理服务数。
- **模型详情**：
  - **版本 Tab**：版本列表，含量化、精度、参数量、权重大小、下载状态（进度条）、设当前版本、回退。
  - **适配器 Tab**：LoRA 适配器列表，上传/启用/删除。
  - **关联推理服务 Tab**：用此模型的服务列表。
- **创建推理服务（向导）**：
  1. 选模型 + 版本 + 推理框架（按框架动态渲染参数表单）。
  2. 选集群、副本数、GPU 卡数 + 型号（校验 TP*PP=卡数）、张量/流水线并行度。
  3. 框架参数（max_model_len、gpu_memory_utilization 等按 schema）。
  4. 自动伸缩（QPS/队列阈值、min/max）、API Key 开关、审批开关。
  5. 预估 GPU 与显存占用，YAML 预览。
- **推理服务详情**：
  - **概览**：状态、endpoint、当前权重版本、GPU 占用、Token 用量趋势、延迟 P99。
  - **变更历史 Tab**：deploy/switch_model/scale/rollback 记录，可回滚。
  - **Pod Tab**：Pod 列表（GPU 占用），日志/exec。
  - **弹性伸缩 Tab**：HPA 配置与事件。
  - **API Key Tab**：签发/撤销、限流配额、最后使用。
  - **用量 Tab**：按 Key/调用方的 Token 用量统计、时序图。
  - **监控 Tab**：吞吐、延迟（TTFT/P99）、队列、显存、GPU 利用率（Grafana 嵌入或自绘）。
  - **YAML Tab**：K8s 资源只读视图。
- **切模型**：选目标版本 + 策略（rolling/blue_green/canary）+ 蓝绿时显示双组状态、灰度时显示流量比例。
- **模型仓库管理**（admin）：仓库列表、凭证、设默认。

### 6.15 日志查看（构建/发布/推理）

排障核心场景，需高效定位：

- **阶段化分块**：构建按步骤（checkout/package/build_image/push）分卡片，每步独立状态/耗时，失败步自动展开标红；发布按事件时间轴（开始滚动→Pod 创建→就绪→进度 N%→完成）。
- **实时流 + 续传**：WS 增量推送，断线用 `fromOffset` 续传不丢日志；超大日志完成后归档走范围拉取。
- **全文搜索**：顶部搜索框，命中行高亮 + 「下一条/上一条」跳转 + 行号；后端按行号定位或 ES。
- **错误自动定位**：失败时顶部展示 `error_line` 摘要 + 「跳到错误行」按钮。
- **级别高亮与过滤**：自动识别 ERROR/WARN/FAIL 着色，可按级别过滤显示。
- **时间戳**：相对时间（`+12s`）/绝对时间切换。
- **下载/分享**：单步或全量下载（txt/zip）；生成过期分享链接（权限校验）。
- **日志对比**：失败构建 vs 上次成功构建日志 diff，定位变化。
- **深链**：失败提示一键跳到对应日志行。

### 6.16 自助建空间

- 顶部「+ 新建空间」入口对所有有 `action:workspace:create` 的用户开放（普通用户默认有）。
- **向导**：
  1. 基础信息：名称（唯一校验）、显示名、描述、Logo。
  2. 集群绑定：从允许的集群选（或用策略默认），填 Namespace（默认=空间名）。
  3. 预览：显示策略给的默认配额（CPU/内存/Pod/GPU）与上限提示。
- **策略提示**：若达 `max_workspaces_per_user` 上限或策略禁用，入口禁用并提示原因；需审批则提交后显示「待审批」。
- **创建后**：自动成为空间 admin，引导跳转到空间添加应用/成员。
- **空间管理**：admin 可改配置、绑集群、管成员、转 Owner、设配额、归档。

### 6.17 API Token 管理（个人中心）

- **Token 列表**：名称、前缀、类型（personal/external）、scope、范围（空间/应用）、过期、最后使用、状态。
- **创建 Token**：
  - 选类型：personal（自用）/ external（对外集成）。
  - 勾选 scope（external 时从 `ext:*` 选）。
  - 选范围：全部可见空间或指定空间/应用。
  - 设限流（每分钟）、IP 白名单、过期时间、回调 URL（external）。
  - 创建后**仅显示一次明文**，提示复制保存，可一键复制。
- **管理**：查看调用记录（跳转审计）、撤销、续期。
- **调用记录**：管理员可在「审计 → 对外 API 调用」按 Token/操作/资源/时间查询。

## 7. 状态管理

### 7.1 authStore（Zustand）

```ts
interface AuthStore {
  user: User | null
  accessToken: string | null
  permissions: string[]           // 有效权限码
  menus: MenuTree[]               // 动态菜单
  unreadNotifications: number
  pendingApprovals: number
  login(credentials): Promise<void>
  logout(): void
  refresh(): Promise<void>
  hasPermission(code: string): boolean
}
```

### 7.2 服务端状态（TanStack Query）

- 每个资源一个 query key 命名空间：`['workspaces']`、`['applications', appUuid]` 等。
- 写操作后 `queryClient.invalidateQueries`。
- 长列表用 `useInfiniteQuery` 游标分页。

### 7.3 权限上下文

- `usePermission('action:release:rolling')` → `{ can: boolean }`。
- `PermissionGate`：无权限时 **不渲染** 子节点（非 disabled）。
- 菜单权限与按钮权限分离：有菜单无 action 时进入页面但操作按钮隐藏。

## 8. WebSocket 抽象

```ts
function useWebSocket<T>(channel: string, params: object) {
  // 自动连接、鉴权、订阅、重连、清理
  // 返回 { data: T | null, status: 'connecting'|'open'|'closed' }
}
```

业务 Hook：
- `useBuildLogs(buildUuid)`：返回日志行数组。
- `useReleaseEvents(releaseUuid)`：返回事件流。
- `usePodLogs(groupUuid, podName, container)`：返回日志行。

## 9. 设计规范

### 9.1 视觉

- 主色：Ant Design 蓝（#1677ff）作为强调色；状态色统一：
  - 成功：green
  - 运行中：blue（带脉冲动画）
  - 失败：red
  - 等待/暂停：gold
  - 已禁用/已归档：default
- 表格密度：默认 middle；详情页可切 compact。

### 9.2 交互

- 危险操作（删除、回滚、终止发布）：`Modal.confirm` 二次确认，需输入资源名或勾选确认。
- 长操作：按钮 loading + 全局 toast 反馈。
- 错误：统一 `notification.error`，含 traceId 便于反馈。
- 空状态：插画 + 引导文案 + 主操作按钮。

### 9.3 响应式

- 最低支持 1280px；侧边栏可折叠适配小屏。
- 不优先移动端（管理后台场景）。

## 10. 性能

- 路由级懒加载（React.lazy + Suspense）。
- 大表格虚拟滚动（AntD Table `virtual` 或 react-window）。
- 日志组件：仅渲染可视区行（虚拟列表），最大缓冲 10000 行，超出丢弃最早。
- 静态资源 CDN + gzip/brotli。

## 11. 安全

- accessToken 存内存（Zustand），不落 localStorage；refreshToken 存 HttpOnly Cookie（后端设置）。
- 所有用户输入经前端校验 + 后端校验（前端校验仅为体验）。
- Dockerfile / 配置内容提交前在前端做基本 sanitization（不依赖前端做安全）。
- CSP 头由后端返回，限制 Monaco/图表资源来源。

## 12. 可访问性

- 所有交互元素键盘可达；表格行操作有 `aria-label`。
- 颜色非唯一信息载体（状态除颜色外有文字/图标）。
- i18n 默认中文，可切英文。

## 13. 构建与部署

- `npm run build` 产出静态资源，由后端 `embed.FS` 嵌入 Go 二进制（单镜像部署），或独立 Nginx 容器。
- 环境变量：`VITE_API_BASE` 指向后端 API。
- Dockerfile：多阶段构建（node 构建 → nginx 运行）。
