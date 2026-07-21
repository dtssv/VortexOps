# 权限体系设计

涵盖角色模型、菜单权限、动作权限、数据范围权限、审批权限与运行时判定。所有权限相关内容统一在此文档。

---

## 1. 设计目标

- **三层授权**：平台（Platform）→ 空间（Workspace）→ 应用（Application），每层可独立配置角色。
- **菜单 + 动作 + 数据** 三维权限：控制「能看到什么菜单」「能点什么按钮」「能看到哪些数据」。
- **内置 + 自定义角色**：四类内置角色满足常见场景；企业可创建如「仅构建发布」「仅集群运维」等自定义角色。
- **显式拒绝**：支持在角色上对特定权限码设置 `granted=false`，优先级高于授予。
- **审批联动**：生产环境发布/配置变更可配置审批流，审批权限独立控制。

## 2. 权限模型

```
User
 ├── platform_role_bindings → Platform Role → Permissions (menu/action/data)
 ├── workspace_members      → Workspace Role → Permissions
 └── application_members    → Application Role → Permissions

EffectivePermissions = ∪(所有角色权限) - 显式拒绝
```

### 2.1 权限码分类

| 类别 | 前缀 | 作用 |
| --- | --- | --- |
| 菜单 | `menu:*` | 控制侧边栏/Tab 是否可见 |
| 动作 | `action:*` | 控制 API 与按钮（构建、发布、删除等） |
| 数据 | `data:*` | 控制列表数据范围（全部 vs 仅自己的） |

### 2.2 权限码命名规范

```
{category}:{module}:{resource}:{operation}

category: menu | action | data
module:   功能模块
resource: 资源或页面
operation: view | create | update | delete | execute | ...
```

示例：
- `menu:cluster:view` — 可见「集群管理」菜单
- `action:release:rolling` — 可执行整组发布
- `data:build:own` — 仅看自己触发的构建（与 `data:build:all` 互斥）

## 3. 内置角色

### 3.1 平台内置角色

| 角色 code | 名称 | 典型场景 |
| --- | --- | --- |
| `platform_admin` | 平台管理员 | 全部权限 |
| `platform_ops` | 平台运维 | 集群/Jenkins/仓库/观测，**无发布** |
| `platform_developer` | 平台开发 | 构建发布 + 应用，**无系统管理** |
| `platform_auditor` | 平台审计 | 审计 + 只读浏览 |
| `platform_build_release` | 发布专员 | **仅**构建发布相关菜单 |

### 3.2 空间/应用内置角色

| 角色 code | 名称 | 定位 |
| --- | --- | --- |
| `admin` | 管理员 | 资源全管理 + 成员 + 审批配置 |
| `developer` | 开发者 | 构建 + 整组/灰度发布 + 配置变更 |
| `tester` | 测试人员 | 构建 + 灰度发布（不可整组、不可改配置） |
| `viewer` | 访客 | 只读 + 日志查看 |

> tester 区别于 developer 在于「不可整组发布」，只能做小范围灰度验证，符合测试人员职责。

## 4. 菜单树结构

### 4.1 设计原则

- **菜单可见 ≠ 操作允许**：菜单由 `menu:*` 控制；页面内按钮/API 由 `action:*` 控制；数据范围由 `data:*` 控制。
- **动态菜单**：前端启动时调用 `GET /me/menus`，后端按用户有效权限过滤菜单树，**无权限的菜单不返回**（非仅置灰）。
- **三级作用域**：`platform`（全局管理）、`workspace`（空间上下文）、`application`（应用上下文）。
- **自定义角色**：企业可在各作用域创建角色，从权限码池勾选；内置角色不可删但可复制为模板。
- **显式拒绝优先**：`role_permissions.granted = false` 覆盖授予，用于「给 developer 但禁止发布生产」等场景。

### 4.2 平台级菜单（scope=platform）

```
工作台 (dashboard)                    menu:dashboard:view
├── 概览                              menu:dashboard:overview
└── 我的待办                          menu:dashboard:todo          [审批、失败构建]

空间 (workspace)                      menu:workspace:view
├── 空间列表                          menu:workspace:list
├── 创建空间                          menu:workspace:create        [action:workspace:create] ★ 自助建空间
└── 回收站                            menu:workspace:recycle       [action:recycle:view]

构建与发布 (build-release)             menu:build:view              ★ 核心入口
├── 构建中心                          menu:build:center            [跨空间构建列表]
├── 发布中心                          menu:release:center          [跨空间发布列表]
└── 镜像仓库                          menu:image:registry          [应用镜像浏览，无集群权限可见]

应用管理 (application)                 menu:application:view
├── 应用列表                          menu:application:list
└── 应用详情（动态）                   menu:application:detail

中间件 (middleware)                    menu:middleware:view            ★ 与应用平级
├── 中间件实例列表                    menu:middleware:list
├── 创建中间件                        menu:middleware:create          [action:middleware:create]
├── 中间件详情（动态）                 menu:middleware:detail
├── 中间件备份                        menu:middleware:backup          [action:middleware:backup:create]
└── 中间件目录                        menu:middleware:catalog         [action:admin:middleware:catalog:manage]

大模型 (model)                        menu:model:view                 ★ 与应用/中间件平级
├── 模型仓库/模型列表                 menu:model:list
├── 创建模型                         menu:model:create               [action:model:create]
├── 模型详情（版本/适配器）            menu:model:detail
├── 推理服务列表                     menu:inference:list
├── 创建推理服务                     menu:inference:create           [action:inference:create]
├── 推理服务详情（动态）              menu:inference:detail
└── 模型仓库管理                     menu:admin:model-registry       [action:admin:model:registry:manage]

CI/CD (cicd)                         menu:cicd:view
├── 流水线                           menu:cicd:pipeline              [action:pipeline:view]
├── 流水线执行历史                   menu:cicd:runs                  [action:pipeline:view]
├── 环境晋升                         menu:cicd:promotion             [action:promotion:view]
└── CI/CD 看板                       menu:cicd:dashboard             [action:cicd:dashboard:view]

运维观测 (observability)              menu:ops:view
├── Pod 日志                          menu:ops:logs
├── 事件中心                          menu:ops:events
└── 资源监控                          menu:ops:metrics

系统管理 (admin)                       menu:admin:view              ★ 仅平台/空间管理员
├── 用户管理                          menu:admin:user              [platform_admin]
├── 角色权限                          menu:admin:role              [platform_admin / workspace admin]
├── 集群管理                          menu:admin:cluster           ★ 可单独授权给运维
├── 镜像仓库配置                      menu:admin:registry
├── Jenkins 配置                      menu:admin:jenkins
├── 基础镜像                          menu:admin:base-image
├── 中间件目录                        menu:admin:middleware-catalog  [action:admin:middleware:catalog:manage]
├── 凭证管理                          menu:admin:credential
├── 审批流配置                        menu:admin:approval-flow
├── 系统设置                          menu:admin:settings
├── 公告管理                          menu:admin:announcement
└── 审计日志                          menu:admin:audit

个人中心 (profile)                     menu:profile:view
├── 账号设置                          menu:profile:account
├── API Token                         menu:profile:api-token       [action:token:manage] ★ 含对外 Token
├── 通知偏好                          menu:profile:notification
└── 外观设置                          menu:profile:appearance
```

### 4.3 空间上下文菜单（进入某空间后侧边栏）

```
空间概览                              menu:ws:overview
├── 应用列表                          menu:ws:application:list
├── 成员管理                          menu:ws:member               [action:ws:member:manage]
├── 集群绑定                          menu:ws:cluster:bind         [action:ws:cluster:bind]
├── 空间设置                          menu:ws:settings             [action:ws:update]
├── 空间审计                          menu:ws:audit                [menu:admin:audit 或 ws admin]
└── 动态流                            menu:ws:activity
```

### 4.4 应用上下文菜单（进入某应用后 Tab/子菜单）

```
应用概览                              menu:app:overview
├── 分组                              menu:app:group
├── 镜像                              menu:app:image
├── 构建                              menu:app:build               ★ tester+ 可见
├── 发布                              menu:app:release             ★ developer+ 可见
├── 配置                              menu:app:config
├── Git 源                            menu:app:git
├── Webhook                           menu:app:webhook
├── 成员                              menu:app:member
├── 构建模板                          menu:app:build-template
├── 审批记录                          menu:app:approval
└── 应用设置                          menu:app:settings
```

### 4.5 分组详情 Tab

```
概览 / Pod / 配置 / 发布历史 / 事件 / 评论
对应：menu:app:group + action:group:view
```

## 5. 内置角色与菜单映射

### 5.1 平台内置角色

| 角色 | 典型用户 | 可见菜单 | 说明 |
| --- | --- | --- | --- |
| **platform_admin** | 平台管理员 | 全部 | 用户/集群/系统全管理 |
| **platform_ops** | 运维工程师 | 集群管理、中间件目录、Jenkins、镜像仓库配置、运维观测、构建中心（只读）、审计 | **看不到**应用配置编辑、发布按钮 |
| **platform_developer** | 跨团队开发 | 工作台、空间、构建与发布、应用管理、运维观测 | **看不到**系统管理 |
| **platform_auditor** | 审计员 | 工作台（只读）、审计日志、构建/发布中心（只读） | 纯只读 |
| **platform_build_release** | 发布专员 | 构建与发布、镜像、应用列表（无设置） | **仅**构建发布相关，无集群管理 |

### 5.2 空间内置角色

| 角色 | 菜单 | 关键 action |
| --- | --- | --- |
| **ws_admin** | 空间内全部 | 成员、集群绑定、应用 CRUD |
| **ws_developer** | 应用/构建/发布/配置 | 不可管理成员与集群绑定 |
| **ws_tester** | 应用/构建/灰度发布 | 不可整组发布、不可改配置 |
| **ws_viewer** | 应用只读 | 无构建/发布菜单 |

### 5.3 应用内置角色

同空间角色，但作用域缩小到单应用；可通过自定义角色覆盖，如「app_release_only」仅 `menu:app:release` + `action:release:canary`。

## 6. 动作权限矩阵

### 6.1 空间级

| 动作 | admin | developer | tester | viewer |
| --- | :---: | :---: | :---: | :---: |
| 查看空间 | ✅ | ✅ | ✅ | ✅ |
| 编辑/归档空间 | ✅ | ❌ | ❌ | ❌ |
| 管理成员/角色 | ✅ | ❌ | ❌ | ❌ |
| 绑定集群 | ✅ | ❌ | ❌ | ❌ |
| 创建应用 | ✅ | ✅ | ❌ | ❌ |
| 删除应用 | ✅ | ❌ | ❌ | ❌ |
| 查看空间审计 | ✅ | ❌ | ❌ | ❌ |
| 管理空间自定义角色 | ✅ | ❌ | ❌ | ❌ |

### 6.2 应用级

| 动作 | admin | developer | tester | viewer |
| --- | :---: | :---: | :---: | :---: |
| 查看应用/分组/镜像 | ✅ | ✅ | ✅ | ✅ |
| 编辑应用/管理成员 | ✅ | ❌ | ❌ | ❌ |
| 管理 Git 源/Webhook | ✅ | ✅ | ❌ | ❌ |
| 创建/编辑分组 | ✅ | ✅ | ❌ | ❌ |
| 删除分组 | ✅ | ❌ | ❌ | ❌ |
| 触发/取消构建 | ✅ | ✅ | ✅ | ❌ |
| 整组发布 | ✅ | ✅ | ❌ | ❌ |
| 灰度发布/提升/放弃 | ✅ | ✅ | ✅ | ❌ |
| 回滚/终止发布 | ✅ | ✅ | ❌ | ❌ |
| 新建/应用/回滚配置 | ✅ | ✅ | ❌ | ❌ |
| 查看 Pod 日志 | ✅ | ✅ | ✅ | ✅ |
| Pod exec | ✅ | ✅ | ❌ | ❌ |
| 审批（若配置） | ✅ | ✅* | ❌ | ❌ |

> *developer 可被设为审批人（通过审批流配置，非角色默认权限）。

### 6.3 中间件级

中间件实例复用空间/应用层角色（按空间成员角色判定），动作权限矩阵：

| 动作 | admin | developer | tester | viewer |
| --- | :---: | :---: | :---: | :---: |
| 查看中间件实例/参数/备份 | ✅ | ✅ | ✅ | ✅ |
| 创建中间件实例 | ✅ | ✅ | ❌ | ❌ |
| 编辑/删除中间件 | ✅ | ❌ | ❌ | ❌ |
| 安装/升级/扩缩容 | ✅ | ✅ | ❌ | ❌ |
| 回滚中间件 | ✅ | ✅ | ❌ | ❌ |
| 应用参数变更 | ✅ | ✅ | ❌ | ❌ |
| 创建备份/恢复 | ✅ | ✅ | ✅ | ❌ |
| 管理备份策略 | ✅ | ✅ | ❌ | ❌ |
| 管理连接关系 | ✅ | ✅ | ❌ | ❌ |
| 查看连接信息/密码 | ✅ | ✅ | ❌ | ❌ |

> 生产环境中间件（environment=prod）的升级/扩缩容/参数变更默认走审批流（`release_requires_approval=true`）。

## 7. 完整权限码清单

### 7.1 空间

| 权限码 | 说明 |
| --- | --- |
| `action:workspace:create` | 创建空间（自助建空间，普通用户默认授予，受 `vo_workspace_creation_policies` 约束） |
| `action:workspace:update` | 编辑空间 |
| `action:workspace:archive` | 归档空间 |
| `action:workspace:transfer` | 转让空间 Owner |
| `action:ws:member:manage` | 管理空间成员 |
| `action:ws:cluster:bind` | 绑定/解绑集群 |
| `action:ws:role:manage` | 管理空间自定义角色 |
| `action:token:manage` | 管理个人/对外 API Token |
| `action:admin:ws-policy:manage` | 管理自助建空间策略（平台级） |

### 7.2 应用

| 权限码 | 说明 |
| --- | --- |
| `action:application:create` | 创建应用 |
| `action:application:update` | 编辑应用 |
| `action:application:delete` | 删除应用 |
| `action:app:member:manage` | 管理应用成员 |
| `action:git:manage` | 管理 Git 源 |
| `action:group:create` | 创建分组 |
| `action:group:update` | 编辑分组 |
| `action:group:delete` | 删除分组 |

### 7.3 构建与镜像

| 权限码 | 说明 |
| --- | --- |
| `action:build:create` | 触发构建 |
| `action:build:cancel` | 取消构建 |
| `action:build:template:manage` | 管理构建模板 |
| `action:image:manual` | 手动登记镜像 |
| `action:image:delete` | 删除镜像 |
| `action:image:tag:manage` | 管理制品版本别名（stable/production...） |
| `action:image:rollback` | 回退到指定制品版本 |
| `action:image:cleanup` | 清理旧制品版本 |

### 7.4 中间件

| 权限码 | 说明 |
| --- | --- |
| `action:middleware:create` | 创建中间件实例 |
| `action:middleware:update` | 编辑中间件实例 |
| `action:middleware:delete` | 删除中间件实例 |
| `action:middleware:install` | 安装/部署 |
| `action:middleware:upgrade` | 升级版本 |
| `action:middleware:scale` | 扩缩容 |
| `action:middleware:rollback` | 回滚 |
| `action:middleware:params:apply` | 应用参数变更 |
| `action:middleware:backup:create` | 创建备份 |
| `action:middleware:backup:restore` | 恢复备份 |
| `action:middleware:backup:delete` | 删除备份 |
| `action:middleware:backup:policy` | 管理备份策略 |
| `action:middleware:connection:manage` | 管理应用与中间件连接 |
| `action:admin:middleware:catalog:manage` | 管理中间件目录 |
| `data:middleware:all` | 看空间内全部中间件 |
| `data:middleware:own` | 仅看自己创建的中间件 |

### 7.5 发布与配置

| 权限码 | 说明 |
| --- | --- |
| `action:release:rolling` | 整组发布 |
| `action:release:canary` | 灰度发布 |
| `action:release:promote` | 灰度提升 |
| `action:release:abort` | 放弃灰度 |
| `action:release:rollback` | 回滚 |
| `action:release:cancel` | 终止发布 |
| `action:config:create` | 新建配置版本 |
| `action:config:apply` | 应用配置 |
| `action:config:rollback` | 回滚配置 |
| `action:config:compare` | 配置版本/跨分组比对 |
| `action:config:set:manage` | 管理配置集（创建/编辑/删除/版本） |
| `action:config:set:bind` | 配置集关联分组/解绑/锁版本 |
| `action:group:advanced-network` | 高级网络选项（hostNetwork/keepPodIp/禁止公网） |
| `action:approval:approve` | 审批通过 |
| `action:approval:reject` | 审批拒绝 |

### 7.6 大模型服务

| 权限码 | 说明 |
| --- | --- |
| `action:model:create` | 创建模型 |
| `action:model:update` | 编辑模型 |
| `action:model:delete` | 删除模型 |
| `action:model:version:manage` | 管理模型版本（新增/下载/设当前） |
| `action:model:adapter:manage` | 管理适配器 |
| `action:inference:create` | 创建推理服务 |
| `action:inference:update` | 更新推理服务 |
| `action:inference:delete` | 删除推理服务 |
| `action:inference:deploy` | 部署/切模型/扩缩容/回滚/停止 |
| `action:inference:apikey:manage` | 签发/撤销 API Key |
| `action:inference:usage:view` | 查看 Token 用量 |
| `action:inference:route:manage` | 管理多模型路由 |
| `action:admin:model:registry:manage` | 管理模型仓库（平台级） |

### 7.7 CI/CD 流水线与晋升

| 权限码 | 说明 |
| --- | --- |
| `action:pipeline:view` | 查看流水线与执行 |
| `action:pipeline:manage` | 创建/编辑/删除流水线与阶段 |
| `action:pipeline:trigger` | 触发流水线 |
| `action:pipeline:gate:approve` | 人工门禁通过/拒绝 |
| `action:promotion:view` | 查看晋升 |
| `action:promotion:create` | 发起晋升 |
| `action:promotion:approve` | 审批晋升（prod） |
| `action:promotion:abort` | 终止/回滚晋升 |
| `action:artifact:sign` | 签名制品 |
| `action:artifact:verify` | 验签 |
| `action:cicd:dashboard:view` | 查看 CI/CD 看板与指标 |

### 7.8 运维

| 权限码 | 说明 |
| --- | --- |
| `action:pod:logs` | 查看 Pod 日志 |
| `action:pod:logs:search` | 跨 Pod 日志搜索 |
| `action:pod:exec` | 进入容器终端 |
| `action:pod:portforward` | 端口转发 |
| `action:pod:restart` | 重启 Pod |
| `action:group:autoscaling:manage` | 管理 HPA 弹性伸缩 |
| `action:alert:rule:manage` | 管理告警规则 |
| `action:alert:silence:manage` | 管理告警静默 |
| `action:alert:event:resolve` | 手动解决告警事件 |
| `action:release:preset:manage` | 管理发布预设 |
| `action:release:batch` | 批量发布 |
| `action:group:clone` | 克隆分组 |
| `action:group:yaml:view` | 查看 K8s YAML |

### 7.9 平台管理

| 权限码 | 说明 |
| --- | --- |
| `action:admin:user:manage` | 用户管理 |
| `action:admin:role:manage` | 角色权限管理 |
| `action:admin:cluster:manage` | 集群 CRUD |
| `action:admin:registry:manage` | 镜像仓库配置 |
| `action:admin:jenkins:manage` | Jenkins 配置 |
| `action:admin:base-image:manage` | 基础镜像 |
| `action:admin:credential:manage` | 凭证管理 |
| `action:admin:settings:manage` | 系统设置 |
| `action:admin:audit:view` | 查看审计 |
| `action:recycle:view` | 查看回收站 |
| `action:recycle:restore` | 恢复资源 |
| `action:recycle:purge` | 彻底删除 |

### 7.10 数据范围

| 权限码 | 说明 |
| --- | --- |
| `data:build:all` | 看空间/应用内全部构建 |
| `data:build:own` | 仅看自己触发的构建 |
| `data:release:all` | 看全部发布 |
| `data:release:own` | 仅看自己发起的发布 |

### 7.11 对外 API Scope

> 对外 Token 用 scope 授权（见 [对外 API](api-external.md)），与 RBAC 叠加：scope 收紧操作范围，RBAC 控制可见资源，二者均需通过。

| Scope | 对应内部能力 |
| --- | --- |
| `ext:workspace:read` | 查询空间/应用/分组 |
| `ext:deploy` | 部署/灰度/回滚 |
| `ext:scale` | 扩缩容 |
| `ext:config` | 配置版本管理 |
| `ext:configset` | 配置集管理 |
| `ext:build` | 触发/查询构建 |
| `ext:image` | 镜像/制品版本 |
| `ext:middleware` | 中间件管理 |
| `ext:inference` | 推理服务管理 |
| `ext:pipeline` | 流水线触发/查询 |
| `ext:status` | 运行时状态查询 |
| `ext:rollback` | 回滚（发布/配置/制品/推理） |

## 8. EffectiveRole 计算规则

| 场景 | 空间角色 | 应用成员 | 应用 EffectiveRole |
| --- | --- | --- | --- |
| 空间 admin | admin | 无 | admin（隐式） |
| 空间 admin | admin | developer | admin（空间 admin 不可降级） |
| 空间 developer | developer | 无 | **无权**（需显式加入应用） |
| 空间 developer | developer | tester | tester |
| 仅应用成员 | 无 | developer | developer |
| 平台 platform_ops | — | — | 仅平台级菜单权限，不自动进入任何空间 |

**关键规则**：
1. 空间 admin 隐式拥有该空间下所有应用的 admin。
2. 空间 developer/tester/viewer **不自动继承**应用访问，必须显式加入 `vo_application_members`。
3. 平台角色与空间角色**叠加**（并集），显式拒绝优先。
4. 自定义角色完全替代内置角色绑定（通过 `role_id` 指向自定义 role）。

## 9. 菜单与 API 权限联动

| 用户类型 | 可见菜单 | 不可用菜单 |
| --- | --- | --- |
| 发布专员 | 构建中心、发布中心、镜像、应用列表 | 集群管理、用户管理、系统设置 |
| 运维工程师 | 集群管理、运维观测、审计（只读） | 构建、发布、配置编辑 |
| 测试人员 | 应用详情、构建、灰度发布 | 整组发布、配置、系统管理 |
| 空间访客 | 应用/分组只读 | 构建、发布、成员管理 |

前端：**无 menu 权限则不渲染菜单项**；后端：即使绕过前端直接调 API，仍校验 `action:*`。

## 10. 运行时判定流程

```
1. 解析请求 → 确定 scope（platform / workspace / application）与 resource
2. 加载用户在该 scope 的有效角色列表：
   - platform_role_bindings
   - workspace_members.role_id → roles
   - application_members.role_id → roles
   - 空间 admin 隐式继承应用 admin（不可被应用级降级）
3. 合并 role_permissions → 有效权限码集合（显式拒绝优先）
4. 菜单请求：过滤 menus WHERE permission_code IN 有效权限码
5. API 请求：校验 action:* 权限码
6. 列表查询：附加 data:* 过滤（如 data:build:own → WHERE triggered_by = current_user）
```

## 11. 实现要点

### 11.1 登录响应 `/me`

```json
{
  "user": {"uuid":"...","displayName":"Alice","locale":"zh-CN"},
  "platformRoles": ["platform_developer"],
  "permissions": ["menu:build:view","action:build:create", "..."],
  "menus": [ /* 已过滤的菜单树 */ ],
  "preferences": {"theme":"dark","defaultWorkspaceUuid":"..."},
  "unreadNotifications": 3,
  "pendingApprovals": 1
}
```

### 11.2 中间件

```go
func RequirePermission(codes ...string) func(http.Handler) http.Handler
func RequireAnyPermission(codes ...string) func(http.Handler) http.Handler
func RequireWorkspaceMember() func(http.Handler) http.Handler
func RequireAppMember() func(http.Handler) http.Handler
```

### 11.3 数据范围过滤

```go
func ApplyDataScope(q Query, user User, perm string) Query {
    if user.Has("data:build:all") { return q }
    if user.Has("data:build:own") { return q.Where("triggered_by = ?", user.ID) }
    return q.Where("1=0") // 无权限
}
```

### 11.4 缓存

- 用户权限集缓存 Redis，TTL 5min；角色/成员变更时按 user_id 失效。
- 菜单树按「权限码集合 hash」缓存，权限变更时 bust。

### 11.5 前端动态菜单

- 登录后并行请求：`GET /me`、`GET /me/menus`、`GET /me/permissions`。
- `vo_menus` 返回树形结构，含 `path`、`icon`、`children`。
- 路由注册：仅注册后端返回的菜单路由；直接访问无权限路由 → 403 页。
- 按钮级：`usePermission('action:release:rolling')` 控制显隐。
- **体验优化**：
  - 空间/应用切换器只展示有权限的项。
  - 收藏夹（`user_favorites`）独立于菜单，置顶展示。
  - 最近访问（`user_preferences.recent_resources`）快速跳转。
  - 无权限时菜单项**不渲染**（非 disabled），避免用户困惑。

## 12. 审批权限

- 审批动作 `action:approval:approve` / `action:approval:reject` 由审批流步骤指定审批人（角色/用户/应用 Owner），**不**完全依赖应用角色。
- 生产分组可设 `release_requires_approval=true`，发布进入 `pending_approval`，审批通过后自动执行。
- 审批流详细设计见 [协作功能](collaboration.md#审批流)。

## 13. API Token 权限

- 个人 API Token 可勾选权限码子集（不超过用户自身 EffectivePermissions）。
- 服务账号（未来）：独立 user，绑定 platform 角色。

## 14. 边界场景

| 场景 | 处理 |
| --- | --- |
| 用户被移出空间/应用 | 权限缓存失效；JWT 自然过期（≤15min） |
| 角色被禁用 | 绑定该角色的用户立即失去对应权限 |
| 自定义角色删除 | 需无成员绑定；内置角色不可删 |
| 权限变更 | 前端轮询或 WS 推送 `permissions.changed`，提示刷新 |
| 回收站 | 需 `action:recycle:view`；恢复需 `action:recycle:restore` |

## 15. 安全原则

- 默认最小权限：新用户无任何空间访问；创建空间后成为该空间 admin。
- 高危操作二次确认 + 审计：删除、回滚、purge、生产发布。
- Secret/凭证：即使 admin 查看也记审计；tester/viewer 不可见。

## 16. 自定义角色示例

### 「仅构建发布专员」

```yaml
name: 构建发布专员
scope: platform
permissions:
  menus:
    - menu:dashboard:view
    - menu:build:view
    - menu:build:center
    - menu:release:center
    - menu:image:registry
    - menu:application:view
    - menu:application:list
    - menu:app:build
    - menu:app:release
    - menu:app:image
  actions:
    - action:build:create
    - action:build:cancel
    - action:release:rolling
    - action:release:canary
    - action:release:rollback
  deny:
    - menu:admin:view          # 显式拒绝系统管理
    - action:admin:cluster:manage
    - action:config:apply      # 不允许改配置
```

### 「集群运维只读」

```yaml
name: 集群运维
scope: platform
permissions:
  menus:
    - menu:admin:cluster
    - menu:ops:view
    - menu:ops:logs
    - menu:ops:events
    - menu:ops:metrics
  actions:
    - action:pod:logs
  deny:
    - action:release:rolling
    - action:build:create
```
