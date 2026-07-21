# 构建与发布

涵盖 Git 集成、构建流水线、镜像管理、发布流程（整组/灰度/回滚）与配置发布。所有构建发布相关内容统一在此文档。

---

## 1. 总览

```
Git 仓库 ──分支/Tag──> Jenkins 流水线 ──产出──> 镜像仓库 ──选择──> 分组发布
   ▲                        │                       │                │
   │                        │                       │                ▼
   └── Webhook（可选）──────┘                       │         K8s Deployment
                                                    │
                                        整组发布 / 部分 Pod 灰度发布
```

核心动作：
1. **构建（Build）**：选定 Git 来源、分支、构建策略、基础镜像/Dockerfile，触发 Jenkins 流水线，产出镜像。
2. **镜像（Image）**：构建产物入库，可被选择用于发布。
3. **发布（Release）**：选定分组、镜像、配置版本，整组或灰度发布到 K8s。

## 2. Git 集成

### 2.1 Provider 抽象

- 抽象 `GitProvider` 接口，实现：GitHub / GitLab / Gitea / 通用 Git。
- 能力：列举分支、列举 Tag、获取 Commit、Webhook 回调、浅克隆凭证透传。
- 凭证存储：用户名密码 / Deploy Token / SSH Key，KMS 加密落库（见 [数据模型](data-model.md#credentials)）。

### 2.2 Webhook 自动构建

- Git Push / MR Merge 触发（`git_sources.webhook_enabled`）。
- 可配置分支过滤规则（如仅 `main`、`release/*`）。

## 3. 构建策略

### 3.1 内置策略

| 策略 | 检测/默认命令 | 产出物 |
| --- | --- | --- |
| `java` | Maven：`mvn -B clean package -DskipTests`；Gradle：`./gradlew build -x test` | `target/*.jar` 或 `build/libs/*.jar` |
| `python` | `pip install -r requirements.txt`（仅打包依赖与源码） | 项目目录（镜像内含 venv） |
| `go` | `go mod download && go build -o app ./...` | 单二进制 `app` |
| `node` | 检测 lock 文件：pnpm > yarn > npm；`install && run build` | `dist/` |
| `custom` | 用户提供的 Shell 命令 | 由命令决定 |

### 3.2 Dockerfile 来源

- **基础镜像模式（`template`）**：选择 `vo_base_images` 一条记录，平台用内置模板渲染 Dockerfile：
  - Java 模板示例：
    ```dockerfile
    FROM {base_image}
    WORKDIR /app
    COPY {artifact} /app/app.jar
    ENTRYPOINT ["java","-jar","/app/app.jar"]
    ```
  - 各语言对应模板，支持暴露端口、时区、非 root 用户等参数。
- **自定义 Dockerfile（`custom`）**：用户填写完整 Dockerfile，构建上下文为仓库根目录（或指定子目录 `context_path`）。
  - 安全：自定义 Dockerfile 在隔离 Builder 中执行，禁止 `--network=host`，限制资源。

### 3.3 构建模板（`vo_build_templates`）

- 应用/空间级保存常用构建配置（策略、基础镜像、Dockerfile、命令）。
- 
    ·时「从模板创建」，一键填充。
- 模板使用次数统计，推荐常用模板。

### 3.4 构建参数（BuildSpec）

```json
{
  "gitSourceId": "uuid",
  "refType": "branch",
  "refValue": "main",
  "buildStrategy": "java",
  "buildCommand": null,
  "contextPath": ".",
  "baseImageId": "uuid",
  "dockerfileSource": "template",
  "dockerfileContent": null,
  "targetImage": {
    "registry": "harbor.example.com",
    "repository": "team-svc/payment",
    "tag": "main-abc1234-20260620-1"
  },
  "buildArgs": {"PROFILE": "prod"}
}
```

镜像 Tag 命名规则（可配置）：`{branch}-{commitShort}-{yyyymmdd}-{seq}`，便于追溯。

## 4. Jenkins 流水线

### 4.1 接入方式

- 平台通过 Jenkins REST API：
  - `POST /job/{pipeline}/buildWithParameters` 触发。
  - `GET /job/{pipeline}/lastBuild/api/json` 查询状态。
  - `GET /job/{pipeline}/{n}/consoleText` 拉取日志。
  - Webhook 回调（Generic Webhook Trigger 插件）或平台轮询。
- 推荐使用 **Jenkins Job DSL** 由平台动态创建/更新 Pipeline Job，参数化注入：
  - `GIT_URL`、`GIT_CREDENTIAL_ID`、`GIT_REF`
  - `BUILD_STRATEGY`、`BUILD_COMMAND`
  - `DOCKERFILE_CONTENT`（base64）或 `BASE_IMAGE`
  - `IMAGE_REGISTRY`、`IMAGE_REPOSITORY`、`IMAGE_TAG`
  - `REGISTRY_CREDENTIAL_ID`
  - `CALLBACK_URL`、`BUILD_UUID`

### 4.2 Pipeline 脚本骨架（Declarative）

```groovy
pipeline {
  agent { label 'builder' }
  options { timestamps(); timeout(time: 30, unit: 'MINUTES') }
  environment {
    GIT_URL              = "${params.GIT_URL}"
    GIT_REF              = "${params.GIT_REF}"
    IMAGE                = "${params.IMAGE_REGISTRY}/${params.IMAGE_REPOSITORY}:${params.IMAGE_TAG}"
  }
  stages {
    stage('Checkout') {
      steps {
        git branch: env.GIT_REF, credentialsId: params.GIT_CREDENTIAL_ID, url: env.GIT_URL
        script { env.COMMIT_SHA = sh(script:'git rev-parse HEAD', returnStdout:true).trim() }
      }
    }
    stage('Package') {
      steps {
        script {
          switch(params.BUILD_STRATEGY) {
            case 'java':   sh 'mvn -B clean package -DskipTests'; break
            case 'go':     sh 'go mod download && go build -o app ./...'; break
            case 'node':   sh 'npm ci && npm run build'; break
            case 'python': sh 'pip install -r requirements.txt'; break
            case 'custom': sh "${params.BUILD_COMMAND}"; break
          }
        }
      }
    }
    stage('Build Image') {
      steps {
        script {
          def df = params.DOCKERFILE_SOURCE == 'custom'
            ? new String(params.DOCKERFILE_CONTENT.decodeBase64())
            : renderTemplate(params.BASE_IMAGE, params.BUILD_STRATEGY)
          writeFile file: 'Dockerfile.generated', text: df
          docker.withRegistry("https://${params.IMAGE_REGISTRY}", params.REGISTRY_CREDENTIAL_ID) {
            def img = docker.build(env.IMAGE, "-f Dockerfile.generated ${params.CONTEXT_PATH}")
            img.push()
            env.DIGEST = sh(script:"docker inspect --format='{{index .RepoDigests 0}}' ${env.IMAGE}", returnStdout:true).trim()
          }
        }
      }
    }
    stage('Callback') {
      steps {
        script {
          httpRequest httpMode: 'POST',
            url: "${params.CALLBACK_URL}",
            contentType: 'APPLICATION_JSON',
            requestBody: """{"buildUuid":"${params.BUILD_UUID}","status":"success","commitSha":"${env.COMMIT_SHA}","digest":"${env.DIGEST}"}"""
        }
      }
    }
  }
  post {
    failure {
      script { notifyCallback(params.BUILD_UUID, 'failed') }
    }
  }
}
```

### 4.3 状态对账（Reconciler）

- Jenkins 回调可能丢失，平台每 30s 扫描 `builds.status IN ('pending','running')` 的记录，向 Jenkins 查询实际状态并修正。
- 超时（默认 30min）自动标记 `failed`。

### 4.4 日志流

- 前端通过 WebSocket 订阅 `builds/{uuid}/logs`。
- 平台侧：持续拉取 Jenkins console，按 offset 增量推送，落对象存储供历史查询。

### 4.4.1 日志查看体验增强

构建/发布日志是排障核心，需支持大规模日志的高效查看与定位：

- **阶段化日志**：构建按 `vo_build_steps`（checkout/package/build_image/push/callback）分块展示，每步独立状态、耗时、折叠/展开；失败步骤自动展开并标红。流水线同理按阶段（`vo_pipeline_stage_runs`）分块。
- **实时流 + 历史归档**：进行中走 WS 增量流（带 offset，断线可续传）；完成后日志归档到对象存储（`build_steps.log_storage_key`），历史查看走范围拉取（支持 `Range: bytes=`），避免全量加载百万行。
- **全文搜索**：日志量大时支持关键字搜索（高亮所有命中 + 跳转上下一条/上一条），后端基于归档日志建轻量索引（按行号定位）或对接 ES（超长日志场景）。
- **错误自动定位**：构建/步骤失败时自动提取首个错误行摘要存 `build_steps.error_line`，列表与详情直接展示「失败原因」，无需翻日志。
- **级别高亮**：自动识别 `ERROR`/`WARN`/`FAIL` 等关键字着色（红/黄），可按级别过滤显示。
- **时间戳对齐**：每行带相对时间（`+12s`）与绝对时间切换；多步骤时间轴连续，便于看总耗时分布。
- **下载与分享**：单步/全量日志下载（原始 txt / 压缩 zip）；生成带过期时间的分享链接（含权限校验），便于转交同事。
- **对比**：失败构建可与上次成功构建的日志对比，快速定位差异（如依赖版本变化导致）。
- **日志即链接**：构建/发布详情的失败提示可一键跳转到对应日志行（深链）。
- **发布事件流**：发布日志不仅含 K8s 事件，还含平台编排动作（开始滚动→Pod 创建→就绪检查→进度 N%→完成），按时间轴展示，进度条与日志联动。

### 4.5 构建中心（跨空间）

- 统一列表：空间、应用、构建号、状态、触发人、耗时、分支。
- 筛选：状态、时间、触发人、空间、应用。
- 批量取消（admin）。
- 失败构建一键「用相同参数重试」。

## 5. 镜像管理与部署策略

- 构建成功 → 创建 `vo_images` 记录（source=build, build_id, digest, size, version_number 自增）。
- 手动登记外部镜像（source=manual）→ 校验仓库可达性后入库。
- 列表按应用过滤，展示版本号、Tag、digest、来源、Commit、构建时间、大小、扫描状态、使用分组、是否别名。
- 删除：软删除 `status=deleted`，若镜像未被任何 release 引用、未被别名引用、近期未被回退，可调用 Registry API 删除（可选）。
- 镜像扫描：可选集成 Trivy，扫描结果存 `images.scan_result`。

### 5.1 镜像部署策略（CVE 准入）

扫描结果不只展示，还可作为发布准入门槛：

| 策略 | 行为 |
| --- | --- |
| `off` | 不阻断（仅展示扫描结果） |
| `warn` | 有高危 CVE 时发布前弹窗警告，可继续 |
| `block_critical`（推荐 prod） | 有 Critical CVE 时禁止发布，必须换镜像或加白名单 |
| `block_high` | Critical 或 High CVE 都禁止发布 |

- 策略按「空间 + 环境」配置（如 prod=block_critical，dev=off）。
- 白名单：特定 CVE 可加白名单（已修复无影响/误报），白名单有效期与原因必填。
- 未扫描或扫描中的镜像：按策略可设为「禁止」或「允许但标记」。
- 准入失败时发布按钮置灰并展示原因（「镜像含 3 个 Critical CVE：CVE-xxxx...」）。

## 6. 发布流程

### 6.1 整组发布（Rolling）

**目标**：将分组当前运行的 Deployment 全部 Pod 替换为新镜像（与可选的新配置版本）。

**步骤**：
1. 校验权限 `release.rolling`。
2. 加载 Group 当前镜像与配置版本，记录到 `releases.previous_*`。
3. 若分组 `release_requires_approval=true`，进入审批流（见 [协作功能](collaboration.md#审批流)）。
4. 计算目标 PodTemplate：
   - `spec.template.spec.containers[0].image = target_image.full_reference`
   - 应用配置版本（命令参数、env、文件挂载）→ 详见 [K8s 集成](kubernetes.md#配置注入)。
5. `PATCH` Deployment（`strategic-merge`），记录返回的 `revision`。
6. 创建 `vo_releases` 记录，status=running。
7. 启动发布观察器（异步）：
   - `Watch` Deployment 或轮询 `Rollout status`。
   - 推送进度事件到 `vo_release_events` 与 WebSocket。
   - 全部 Ready → status=succeeded；超时或失败 → status=failed，**不自动回滚**（需用户确认）。

### 6.2 部分 Pod 灰度发布（Canary by Pod）

**目标**：在不动主 Deployment 的前提下，运行少量新版本 Pod 验证。

**方案**：创建独立 Canary Deployment（K8s 实现详见 [K8s 集成](kubernetes.md#灰度发布的-k8s-实现)）。

**步骤**：
1. 校验权限 `release.canary`。
2. 校验 `canary_replicas` ≤ group.replicas（建议 ≤ 30%）。
3. 计算 Canary Deployment 名：`{group.deployment_name}-canary`。
4. 复制主 Deployment 的 PodTemplate，替换镜像与配置，加标签 `vortexops.io/canary: "true"`、`vortexops.io/release: {releaseUuid}`。
5. 副本数 = `canary_replicas`。
6. 创建 Canary Deployment（与主 Deployment 共享 Selector，但通过标签区分）。
7. 灰度期可：
   - **路由流量**（可选）：若使用 Service + 标签选择器，灰度 Pod 会被纳入；若需精确流量比例，集成 Ingress/Service Mesh。
   - **观察指标/日志**：前端聚合展示主 vs canary Pod。
8. 决策：
   - **提升为整组（promote）**：触发一次整组发布（target_image = canary 镜像），成功后删除 Canary Deployment。
   - **放弃（abort）**：删除 Canary Deployment，release status=`rolled_back`。

### 6.3 回滚

- 回滚 = 用 `previous_image_id` + `previous_config_version` 触发一次整组发布。
- 回滚后 `vo_releases` 新增一条 type=rolling、`is_rollback=true` 记录。
- 也可回滚到任意历史 release：前端列出历史发布，选择「回滚到此版本」。

### 6.4 发布观察器实现

```go
type ReleaseWatcher struct {
    releaseID int64
    client    kubernetes.Interface
    ...
}

func (w *ReleaseWatcher) Run(ctx context.Context) {
    // Watch Deployment 或轮询
    // 推送事件：patched / progress / pod_ready / pod_failed / timeout
    // 全部 Ready 或超时后更新 release.status
}
```

- 单实例运行通过 leader election；多实例下通过 DB 行锁（`SELECT FOR UPDATE`）抢占 release。
- 超时配置：默认 10min，可按分组配置。

## 7. 发布与配置的关系

发布时必须明确「使用哪个配置版本」：
- 默认使用 Group 的 `current_config_id`（即当前生效配置）。
- 可选择「同时应用新配置版本」：发布即变更镜像+配置。
- 纯配置变更（不改镜像）：发布 type=`config_only`，target_image=当前镜像，target_config_id=新版本。

> 纯配置变更也是一次 release 记录，便于审计与回滚。配置注入实现见 [K8s 集成](kubernetes.md#配置注入)。

### 7.1 资源与网络规格随发布生效

分组的硬件资源（CPU/内存/磁盘/GPU）与网络配置（Service 类型/稳定 IP/公网访问/Ingress/NetworkPolicy）属于分组规格的一部分，变更方式与配置一致：

- 修改资源/网络 → 生成新的配置版本快照（含 `resources` 与 `network` 段）→ 走发布流程应用。
- 默认 type=`config_only`，目标镜像不变，仅重建 Pod 以应用新 `resources.requests/limits`、`nodeSelector`、`tolerations`、Service/Ingress/NetworkPolicy。
- 改 `network_mode` / `keep_pod_ip` / `allow_egress_internet` 会重建 Service/Pod/NetworkPolicy，可能短暂断连，生产环境需审批。
- 资源/网络字段到 K8s 的映射见 [K8s 集成 §4 资源与网络映射](kubernetes.md#4-资源与网络映射)。
- 发布单中展示资源/网络 diff（如 CPU 4C→8C、开启公网访问），便于审批人评估影响。

### 7.2 制品版本管理与回退

每个应用维护一份制品版本历史（`vo_images` 表，每条 = 一个制品版本，含 `version_number`/`digest`/git 来源）：

- **版本列表**：按构建时间倒序，展示版本号、tag、digest、git commit、来源构建、扫描状态、被哪些分组使用、是否曾被回退。
- **版本别名**（`vo_image_version_tags`）：可为版本打别名（`stable`/`production`/`canary`），别名可在版本间移动；回退也可「回退到某别名当前指向的版本」。
- **回退到指定制品版本**：在分组发布时选择历史制品版本（或别名）作为 `target_image_id`，触发一次 `type=rollback` 的 release：
  - 平台以该版本的 `digest` 为准（不可变），确保回退精确。
  - 记录 `previous_image_id`，可再次回退（来回切换）。
  - 回退走与正常发布相同的滚动/审批/观察流程。
  - 标记该版本 `is_rollback_target=true`（统计用）。
- **制品保留策略**：按应用保留最近 N 个制品版本（默认 50），超出的可清理（发布中/被别名引用/近期被回退的除外），保证可随时回退到近期任意版本。

> 回退制品版本不改配置（沿用分组当前配置版本），如需同时改配置可在发布时一并指定 target_config_id。

### 7.3 配置版本回退

- 每个分组的配置版本化（`vo_configs` 表，`version` 自增），任意历史版本可回退。
- 回退配置 = 选择历史 version 作为 `target_config_id`，触发 `type=config_only`（或 `rollback`）release，记录 `previous_config_id`。
- 配置回退也是一次 release，可审计、可再次回退。
- 回退时不改镜像（沿用当前制品版本）。

### 7.4 配置版本比对

- **同分组版本间比对**：选两个配置版本 → DiffViewer 展示差异：
  - 文件级：按 path 展示新增/删除/修改，文件内容行级 diff（Monaco diff editor）。
  - 命令参数：command/args/env 键值对差异表格。
  - Secret 字段：仅展示「是否变更」，不展示明文 diff。
  - 资源/网络段：CPU/内存/GPU/网络模式等差异。
- **跨分组比对**：选两个分组（可跨应用，需同空间权限）→ 比对各自当前生效配置（或指定版本）：
  - 用于排查「同样的应用，A 分组正常 B 分组异常」的配置差异。
  - 展示同 path 文件内容差异、env 差异、资源差异、关联配置集差异。
  - 比对结果可导出，可「将某分组的配置复制到另一分组」（生成新配置版本草稿，需再发布生效）。
- **配置集版本比对**：配置集自身版本间比对（结构同上）；配置集版本与分组配置比对（看配置集贡献了哪些内容）。

### 7.5 配置集（ConfigSet）

配置集是独立于分组的可复用配置，可关联多个分组共享。数据模型见 [数据模型 §8.3-8.6](data-model.md#83-config_sets配置集可关联多个分组共享)，合并实现见 [K8s 集成 §3.4](kubernetes.md#34-配置集configset合并)。

- **用途**：多个分组共用同一份日志配置、数据库连接、证书、限流规则等，改一处、关联分组下次发布自动生效。
- **版本化**：配置集自身版本化（`config_set_versions`），可回退到历史版本。
- **关联与锁定**：
  - `config_set_bindings` 关联配置集与分组，一个分组可关联多个（按 `priority` 合并）。
  - `pinned=true` 锁定特定版本，不随配置集升级而变（生产稳定性）；`pinned=false` 跟随 current。
- **合并**：发布时按 priority 合并各配置集版本，再叠加分组自身配置（自身优先级最高），结果记入 release。
- **影响面**：配置集升级时，平台展示「将影响哪些关联分组」，便于评估。
- **比对**：配置集版本间比对、配置集与分组配置比对（见 §7.4）。

## 8. 发布单（Release Ticket）

- 每次发布生成「发布单」：变更说明、镜像（制品版本号+digest）、配置 diff、关联配置集 diff、资源/网络 diff、审批记录、事件时间线、评论。
- 可分享链接（需登录 + 权限）。
- 支持 @同事 讨论（`comments`，详见 [协作功能](collaboration.md#评论)）。

## 9. 发布保护

- 分组级 `release_requires_approval`（尤其 prod）。
- 发布窗口：`vo_release_windows` 限制生产发布时间段（如工作日 10:00-18:00），窗口外发布需特批（admin 强制 + 审计）。
- 并发锁：同分组同时仅 1 个发布。
- 镜像部署策略：按 CVE 准入阻断不安全镜像发布（见 §5.1）。
- 变更冻结：可设置「变更冻结期」（如大促期间），冻结期内生产发布需平台 admin 审批。
- 健康检查门控：发布中若新 Pod 健康检查失败，自动暂停并告警（不继续滚动）。

### 9.1 发布预设（Release Preset）

- `vo_release_presets` 存常用发布参数（灰度比例、滚动策略、超时、是否审批、通知通道、灰度自动提升时间）。
- 发布向导中「套用预设」一键填充，再微调。
- 预设按 platform/workspace/application 三级作用域，应用级预设优先。
- 典型预设：「标准灰度 10%」「全量滚动（无审批，dev）」「生产滚动（审批+通知 oncall）」。

### 9.2 按工作负载类型的发布差异

| workload_type | 发布行为 |
| --- | --- |
| deployment | 滚动/灰度/重建，支持灰度提升与放弃 |
| statefulset | 有序滚动（可分区 partition 灰度），无独立灰度 Deployment |
| cronjob | 更新 jobTemplate，下次 schedule 触发生效；可「立即触发一次」 |
| job | 「重新运行」生成新 Job 实例（旧 Job 按 ttlSecondsAfterFinished 清理） |

## 10. 发布中心（跨空间）

- 统一发布列表、状态筛选、发布日历视图。
- 生产发布成功/失败推送通知。

## 11. 失败与重试

| 场景 | 处理 |
| --- | --- |
| 构建失败 | images 不创建；release 不可选；用户可重新触发构建（新 build 记录） |
| 镜像 push 失败 | build=failed，重试需重新构建 |
| 发布 PATCH 失败 | release=failed，不修改 Deployment 实际状态；可重试 |
| 发布超时 | release=failed，Deployment 处于半滚动状态；用户决定回滚或继续 |
| 回滚失败 | release=failed（回滚记录），告警，需人工介入 K8s |
| Jenkins 不可达 | build 卡在 pending，Reconciler 标记 failed；可配置降级 |

## 12. 关键参数默认值

| 参数 | 默认 |
| --- | --- |
| 构建超时 | 30 min |
| 发布超时 | 10 min |
| 灰度最大副本比例 | 30% |
| 滚动 maxSurge | 25% |
| 滚动 maxUnavailable | 25% |
| 镜像保留数（每应用） | 50（超出可清理，发布中除外） |
| Reconciler 扫描间隔 | 30s |

---

## 13. 中间件部署

平台不仅部署应用，也部署中间件（MySQL/Redis/Kafka/ES/RabbitMQ/MongoDB/PostgreSQL/MinIO 等）。中间件作为有状态工作负载，挂在空间下，与应用平级，独立成域，复用权限、审批、通知、审计、回收站。数据模型见 [数据模型 §9 中间件](data-model.md#9-中间件)，K8s 资源映射见 [Kubernetes 集成 §中间件资源映射](kubernetes.md#中间件资源映射)。

### 13.1 与应用部署的差异

| 维度 | 应用（无状态） | 中间件（有状态） |
| --- | --- | --- |
| 工作负载 | Deployment | StatefulSet / Operator CR / Helm Release |
| 镜像来源 | 构建产物 | 中间件目录 + 版本（Chart/Operator） |
| 持久化 | 通常无 | PVC 必备，按类型选择 StorageClass |
| 变更类型 | 整组/灰度发布 | 安装/升级/扩缩容/参数变更/回滚 |
| 灰度 | Canary Deployment | 主从切换/分片滚动（按中间件特性） |
| 备份 | 不涉及 | 必备（定时 + 手动，支持恢复） |
| 配置 | 文件 + 命令参数 | Helm values / Operator spec（参数化） |
| 网络 | Service | Headless Service + 普通 Service |

### 13.2 中间件目录（`vo_middleware_catalog`）

平台预置常见中间件类型，每类含：
- 安装方式：`helm`（推荐）/ `operator` / `manifest`
- Chart 仓库与默认版本
- 参数 schema（JSON Schema，用于前端动态表单渲染）
- 端口与暴露信息

系统管理员可扩展目录（自定义 Chart/Operator），需 `action:admin:middleware:catalog:manage`。

### 13.3 部署流程

```
用户选择中间件类型 → 填写实例名/环境/集群/Namespace
   → 参数表单（按 schema_config 渲染）→ 选择版本
   → 校验资源（StorageClass/配额）
   → [需审批则进入审批流]
   → 调用 Helm install / Operator CR create
   → middleware_release(install) 记录
   → 观察器轮询实例就绪（Pod ready + 健康检查）
   → 成功 → 状态 running，记录 access_info
   → 失败 → 状态 failed，保留 Helm release 供排查/回滚
```

### 13.4 变更类型

| 变更 | 类型 | 说明 |
| --- | --- | --- |
| 安装 | `install` | 首次部署 |
| 升级版本 | `upgrade` | Chart/Operator 版本升级，`helm upgrade` |
| 扩缩容 | `scale` | 改副本数（如 Kafka broker 3→5） |
| 参数变更 | `config_only` | 改 values/spec（资源、密码、特性开关） |
| 回滚 | `rollback` | `helm rollback` 到历史 revision |
| 卸载 | `uninstall` | `helm uninstall`（可选保留 PVC） |

每次变更生成 `middleware_releases` 记录，含 before/after 版本与参数，可审计可回滚。

### 13.5 参数管理（`middleware_params`）

- 版本化：每次参数变更生成新版本，支持 diff 与回滚。
- Secret 参数（密码、证书）加密存储，回显掩码。
- 表单按 `catalog.schema_config` 动态渲染（字段类型、枚举、默认值、是否 secret）。
- 应用参数变更 = 一次 `config_only` 中间件 release。

### 13.6 备份与恢复

- **备份方式**（按中间件类型选择）：
  - 逻辑备份：`pg_dump` / `mysqldump` / `mongoexport`
  - 物理备份：`xtrabackup` / `rdb`（Redis）
  - 卷快照：`VolumeSnapshot`（CSI 快照，最快）
  - 集群级：`Velero`（含 PV）
- **策略**（`middleware_backup_policies`）：Cron 定时、保留份数。
- **恢复**（`middleware_backups.restore_info`）：恢复到新实例或覆盖现有（生产覆盖需审批）。
- 备份存储：对象存储（S3/MinIO），保留期到期自动清理。

### 13.7 应用与中间件连接（`middleware_connections`）

- 记录「哪个分组用了哪个中间件实例」及连接凭证。
- 用途：
  - 拓扑视图：应用↔中间件依赖关系。
  - 影响面分析：中间件升级/故障时，反查受影响应用。
  - 凭证注入：分组配置可引用连接的凭证（`envFrom`/`secretRef`）。

### 13.8 中间件发布保护

- 生产中间件（prod）升级/扩缩容默认需审批。
- 升级前自动触发备份（可配置）。
- 并发锁：同实例同时仅 1 个变更。
- 版本兼容性校验：升级跨度大的版本（如 MySQL 5.7→8.0）强制提示风险。

### 13.9 失败与重试

| 场景 | 处理 |
| --- | --- |
| 安装失败 | 状态 failed；保留 Helm release 供排查；可一键卸载重试 |
| 升级失败 | 自动 `helm rollback` 到上一 revision；状态 failed 记录原因 |
| 扩缩容超时 | 状态 failed；PVC 绑定/数据同步慢导致，提示人工检查 |
| 备份失败 | 记录失败原因；不影响实例运行；下次定时重试 |
| 恢复失败 | 状态 failed；原实例不动；提示人工介入 |
| Operator 不可用 | 安装该类型失败；提示运维检查 Operator 部署 |

---

## 14. CI/CD 流水线

单次构建只解决「代码→镜像」，完整 CI/CD 还需把「构建→测试→扫描→部署→验证→晋升」编排成流水线，并辅以质量门禁、环境晋升、部署后验证。数据模型见 [数据模型 §7.6-7.11](data-model.md#76-pipelinescicd-流水线定义)。

### 14.1 流水线编排

流水线（`vo_pipelines`）由阶段（`vo_pipeline_stages`）组成，阶段间按门禁推进，阶段内任务可并行：

```
build → test → scan → image → deploy → verify → promote
```

- **阶段类型**：`build`（构建）/ `test`（单元/集成测试）/ `scan`（代码扫描+镜像扫描）/ `image`（构建推送镜像）/ `deploy`（发布到分组）/ `verify`（部署后验证）/ `promote`（触发环境晋升）。
- **触发**：手动 / Webhook（push/tag）/ 定时 / 上游流水线完成（晋升链）。
- **阶段间门禁**（`pipeline_stages.gate`）：测试通过率、CVE 数、代码覆盖率、自定义检查（SonarQube 等）、人工审批。
- **失败策略**（`on_failure`）：`abort`（终止）/ `manual_retry`（暂停等人重试）/ `continue`（继续）。
- **产物传递**：build 阶段产出制品版本 ID，后续 deploy 阶段引用，无需重新指定镜像。

### 14.2 质量门禁

| 门禁 | 说明 |
| --- | --- |
| 测试门禁 | 单元/集成测试通过率 ≥ 阈值（如 100%），失败用例数 = 0 |
| 覆盖率门禁 | 代码覆盖率 ≥ 阈值（如 80%） |
| 代码扫描门禁 | SonarQube/CodeQL 无新增 Critical/Blocker |
| 镜像扫描门禁 | CVE 准入策略（见 §5.1），Critical CVE 阻断 |
| 签名门禁 | 制品必须有效签名（cosign/notation），验签失败阻断 |
| 人工门禁 | 关键阶段（如 prod deploy）需人工审批通过 |

门禁评估结果存 `pipeline_stage_runs.gate_result`，未通过则阶段 `paused`（人工门禁）或 `failed`（自动门禁）。

### 14.3 制品签名与 SBOM

- **签名**：构建后用 cosign/notation 对镜像签名，存 `vo_artifacts_signatures`。
- **验签**：deploy 阶段校验签名，未签名/验签失败按门禁策略阻断（生产强制）。
- **SBOM**：生成 CycloneDX/SPDX 物料清单，存对象存储，便于供应链合规与漏洞追踪。
- **来源证明**（SLSA provenance）：记录构建来源（commit、构建参数），满足供应链安全要求。

### 14.4 环境晋升（dev → staging → prod）

`vo_promotions` 把某制品版本从低环境晋升到高环境：

- **晋升链**：dev → staging → prod，每级可配独立流水线（`trigger=promotion` + `trigger_on_pipeline`）。
- **配置差异**：不同环境可用不同配置版本（`artifact_config_version`）。
- **策略**：`auto`（自动滚动）/ `canary`（灰度晋升）/ `manual`（人工发布）。
- **部署后验证**（`auto_promote_on_verify`）：部署后跑冒烟/集成测试，通过才完成晋升；失败则回滚并标记。
- **审批**：prod 晋升默认需审批（`approval_instance_id`）。
- **可追溯**：每次晋升关联流水线 run 与发布记录，可审计。

### 14.5 部署后验证

deploy 阶段后可选 verify 阶段，自动验证部署正确性：

- **健康探针**：等 Pod 就绪 + 健康检查通过。
- **冒烟测试**：调用服务关键接口（如 `/health`、核心业务 API）。
- **金丝雀分析**（可选高级）：灰度期间采集指标（错误率、延迟），与基线对比，异常自动回滚。
- **人工确认**：可选「需人工确认验证通过」才推进。
- 验证失败 → 按策略回滚或暂停告警。

### 14.6 GitOps / 配置即代码（可选）

- 应用/分组/配置可导出为声明式配置（YAML）存 Git 仓库。
- 平台监听仓库变更，自动同步为平台资源（drift detection + 采纳/拉回）。
- 适用于「以 Git 为单一事实来源」的团队，所有变更经 PR 审核后生效。
- 与平台 UI 操作双向同步：UI 改动可回写 Git，Git 改动同步到平台。

### 14.7 多集群滚动发布编排

跨多集群发布同一应用（如异地多活）：

- 编排器按集群顺序/并行发布（如先发备集群验证再发主集群）。
- 单集群失败可暂停后续集群，避免全局故障。
- 复用 `vo_release_presets` 与批量发布能力，汇总为发布总览。

### 14.8 CI/CD 可观测

- **流水线看板**：跨空间流水线 run 列表，按状态/应用/时间筛选，看板视图。
- **MTTR/MTBF**：统计从失败到恢复的平均时间、平均间隔，衡量交付稳定性。
- **DORA 指标**：部署频率、变更前置时间、变更失败率、恢复时长（可选）。
- **瓶颈分析**：各阶段耗时分布，定位慢阶段（如测试 30min 占总时长 60%）。
- **失败聚类**：按失败原因聚类，常见问题一目了然。
