# 开发计划

VortexOps 后端 Go 实现的分阶段开发计划。所有代码遵循**生产级、高可用、高性能、无 mock、无 TODO** 原则，分层清晰，可直接编译运行。

> 技术栈：Go 1.22+ / Chi / sqlc + pgx v5 / client-go / Helm SDK。架构分层见 [架构设计](architecture.md)，数据模型见 [data-model.md](data-model.md) 与 [schema.sql](../schema.sql)。

---

## 0. 分层架构（所有阶段遵守）

```
backend/
├── cmd/                          # 进程入口
│   ├── vortexops/                # apiserver 主进程
│   └── migrate/                  # 迁移命令
├── internal/
│   ├── config/                   # 配置加载（env + file，热重载）
│   ├── platform/                 # 平台基础设施（DB/Redis/KMS/logger/meter）
│   ├── domain/                   # 领域模型（纯业务规则，无 IO 依赖）
│   │   ├── identity/             # 用户/角色/权限
│   │   ├── workspace/
│   │   ├── application/
│   │   ├── release/
│   │   ├── pipeline/
│   │   ├── inference/
│   │   └── ...
│   ├── application/              # 应用服务（用例编排，事务边界）
│   │   └── <domain>/
│   ├── infrastructure/           # 基础设施实现（PG 仓储/Redis/K8s/Jenkins...）
│   │   ├── postgres/             # sqlc 生成 + 仓储实现
│   │   ├── redis/
│   │   ├── k8s/
│   │   └── ...
│   └── interfaces/               # 对外适配器
│       ├── http/                 # REST handlers + middleware
│       ├── ws/                   # WebSocket
│       └── grpc/                 # 内部 RPC（大规模）
├── migrations/                   # SQL 迁移文件（由 schema.sql 拆分）
├── pkg/                          # 可复用公共库（错误码/分页/ID 生成）
└── go.mod
```

**依赖方向**：`interfaces → application → domain ← infrastructure`。domain 不依赖任何 IO；infrastructure 实现 domain 定义的接口（依赖倒置）。

---

## 1. 分阶段路线图

| 阶段 | 目标 | 交付物 | 验收 |
| --- | --- | --- | --- |
| **Phase 0** | 工程地基 | go.mod、config、logger、DB 池、迁移、server 生命周期、优雅关停 | `vortexops migrate up` 跑通 schema；`vortexops serve` 启动并通过健康检查 |
| **Phase 1** | 认证与用户 | 用户/刷新令牌领域 + 仓储 + 登录/注册/刷新/登出 + JWT + bcrypt + RBAC 框架 | 端到端登录获取 token，刷新 token，受保护接口鉴权 |
| **Phase 2** | 空间与应用 | workspace/application/group CRUD + 成员 + 配额 + 软删除 + 乐观锁 | 空间-应用-分组三层资源全生命周期 + 权限隔离 |
| **Phase 3** | 集群接入与同步 | cluster CRUD + kubeconfig 加密存储 + syncer（Informer + Redis 缓存 + IP 反查）+ 健康检查 | 接入真实 K8s 集群，Pod/分组运行态缓存秒级刷新 |
| **Phase 4** | 构建与镜像 | git_source + build_template + build + Jenkins 集成 + 日志流（分阶段/归档/搜索）+ 基础镜像 | 真实 Git 触发构建，Jenkins 执行，镜像入库，日志实时查看 |
| **Phase 5** | 发布与配置 | release（整组/灰度/回滚）+ 配置版本/diff/配置集 + 发布预设/窗口/批量 + 审批 | 真实 K8s 发布，进度实时推送，回滚可用 |
| **Phase 6** | 权限菜单与协作 | RBAC 完整（menu/action/data）+ 动态菜单 + 审计 + 通知 + 工作台 + 收藏/动态流 | 角色按权限看到不同菜单，全操作审计 |
| **Phase 7** | 中间件 | middleware_catalog + instance + Operator/Helm 集成 + 备份恢复 + 连接拓扑 | 部署真实 MySQL/Redis 中间件实例 |
| **Phase 8** | CI/CD 流水线 | pipeline 编排引擎 + 质量门禁 + 环境晋升 + 制品签名/SBOM + 部署后验证 | 多阶段流水线 dev→staging→prod 自动晋升 |
| **Phase 9** | 大模型服务 | model registry + 推理框架适配 + GPU 调度 + 推理服务生命周期 + Token 计量 + HPA | 部署 vLLM 推理服务，OpenAI 兼容 API + 计量 |
| **Phase 10** | 对外 API 与自助 | ext-api-gw + scope 鉴权 + 限流/审计/回调 + 自助建空间 | 外部 Token 调用 deploy/scale/config |
| **Phase 11** | 可观测与运维 | Prometheus 指标 + 告警中心 + Pod 日志/exec/端口转发 + 跨 Pod 搜索 | Grafana 看板 + 告警触发 + 就近代理日志 |
| **Phase 12** | 大规模演进 | Citus 分片 + syncer 分片 + ws-gateway 集群 + Kafka 异步 + edge proxy | 10 万应用压测达标 |

每个阶段交付可编译、可测试、可运行的代码，不依赖后续阶段。

---

## 2. 跨阶段工程规范

### 2.1 代码规范
- **无 mock**：所有仓储/外部集成用真实实现；测试用 testcontainers 起真实 PG/Redis。
- **无 TODO/FIXME**：提交前自检，未完成的能力不写半成品。
- **错误处理**：领域错误用 sentinel error + 类型断言；HTTP 层统一转错误码。
- **事务边界**：在 application 层用 `pgx.Tx` 显式控制；仓储接受 `Querier` 接口（兼容 tx 与 pool）。
- **并发**：context 贯穿；共享状态用 channel 或 sync；发布/流水线用 Redis 分布式锁。
- **配置**：env 优先，file 兜底；敏感配置走 KMS/Secret，不入日志。

### 2.2 性能与可用性
- DB 连接池（pgx）+ PgBouncer；查询走索引，N+1 用 `IN` 批量或 join。
- Redis 缓存热点（权限集/Pod 运行态），失效用 TTL + 主动 evict。
- HTTP 用 chi 路由 + 中间件链；超时/限流/恢复中间件必备。
- 优雅关停：SIGTERM → 拒绝新请求 → 等待 in-flight → 关连接池 → 退出。
- 可观测：结构化日志（slog）+ Prometheus 指标 + trace ID 透传。

### 2.3 测试
- 单元测试：领域规则、错误映射。
- 集成测试：testcontainers（PG/Redis）跑真实仓储。
- 端到端：每个阶段一个 happy-path e2e。

---

## 3. Phase 0 详细任务（工程地基）

1. `go.mod`（module `github.com/vortexops/vortexops`，go 1.22）
2. `internal/config`：env + yaml 加载，含 DB/Redis/JWT/KMS/Server 配置
3. `internal/platform/logger`：slog 结构化日志，级别可热调
4. `internal/platform/db`：pgxpool 连接池，健康检查，超时
5. `internal/platform/redis`：go-redis v9 客户端
6. `migrations/`：由 schema.sql 拆分为有序迁移文件
7. `cmd/migrate`：golang-migrate 驱动的迁移命令（up/down/version）
8. `cmd/vortexops`：apiserver 入口，HTTP server + 优雅关停 + 健康检查
9. `pkg/apperr`：统一错误码与 HTTP 映射
10. 编译验证 + Dockerfile

---

## 4. Phase 1 详细任务（认证与用户）

1. `internal/domain/identity`：User/RefreshToken 实体 + 领域错误
2. `internal/platform/security`：bcrypt 密码哈希 + JWT 签发/校验（access/refresh 双 token）
3. `internal/infrastructure/postgres/identity`：User/RefreshToken 仓储（sqlc 生成查询）
4. `internal/application/identity`：AuthService（登录/注册/刷新/登出/修改密码）
5. `internal/interfaces/http/auth`：handlers + 中间件（JWT 校验/权限/限流/审计）
6. `pkg/idgen`：UUID 生成 + 业务 ID
7. 端到端：注册→登录→拿 token→访问受保护接口→刷新→登出

---

## 5. 当前进度

- [x] Phase 0 规划
- [x] Phase 0 实现
- [x] Phase 1–8 实现
- [x] Phase 9 大模型服务
- [x] Phase 10 对外 API 与自助
- [x] Phase 11 可观测与运维
- [x] Phase 12 大规模演进
- [x] Integration（docker-compose.dev.yml 全栈）
