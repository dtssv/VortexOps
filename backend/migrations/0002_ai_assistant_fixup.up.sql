-- ============================================================================
-- 0002_ai_assistant_fixup.up.sql
-- ----------------------------------------------------------------------------
-- 补丁：在已经处于「0001 部分应用」状态的数据库上补齐 AI 助手相关表与种子数据。
-- 背景：早期 0001_init_schema.up.sql 在缺少 pgvector 的环境上执行到 vo_kb_chunks
--       时因 `type "vector" does not exist` 中断，仅创建了 vo_kb_categories /
--       vo_kb_documents。本补丁幂等补齐剩余结构。
-- 适用：PostgreSQL 16 + pgvector 扩展已安装。
-- ============================================================================

-- 1. 确保 pgvector 扩展存在（由 DBA 或迁移前已创建，此处仅幂等确认）。
CREATE EXTENSION IF NOT EXISTS "vector";

-- 2. vo_kb_categories / vo_kb_documents 若已存在则跳过；若不存在则创建。
CREATE TABLE IF NOT EXISTS vo_kb_categories (
    id          BIGSERIAL PRIMARY KEY,
    uuid        UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name        VARCHAR(128) NOT NULL,
    code        VARCHAR(64) NOT NULL UNIQUE,
    description TEXT,
    sort_order  INT NOT NULL DEFAULT 0,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by  BIGINT,
    deleted     BOOLEAN NOT NULL DEFAULT false,
    deleted_at  TIMESTAMPTZ,
    deleted_by  BIGINT
);
COMMENT ON TABLE vo_kb_categories IS 'AI 助手知识库分类（构建/发布/K8s/系统等）';

CREATE TABLE IF NOT EXISTS vo_kb_documents (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    category_id    BIGINT REFERENCES vo_kb_categories(id),
    title          VARCHAR(255) NOT NULL,
    source_type    VARCHAR(32) NOT NULL DEFAULT 'manual',
    source_url     VARCHAR(1024),
    content        TEXT NOT NULL,
    tags           JSONB NOT NULL DEFAULT '[]'::jsonb,
    chunk_count    INT NOT NULL DEFAULT 0,
    status         VARCHAR(16) NOT NULL DEFAULT 'active',
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT
);
COMMENT ON TABLE vo_kb_documents IS 'AI 助手知识库文档（全文存储，分块后向量化）';
CREATE INDEX IF NOT EXISTS idx_kb_documents_category ON vo_kb_documents (category_id) WHERE deleted = false;
CREATE INDEX IF NOT EXISTS idx_kb_documents_status ON vo_kb_documents (status) WHERE deleted = false;

-- 3. vo_kb_chunks：向量分块表。embedding 列类型取决于 pgvector 是否可用。
CREATE TABLE IF NOT EXISTS vo_kb_chunks (
    id           BIGSERIAL PRIMARY KEY,
    uuid         UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    document_id  BIGINT NOT NULL REFERENCES vo_kb_documents(id) ON DELETE CASCADE,
    chunk_index  INT NOT NULL,
    content      TEXT NOT NULL,
    embedding    BYTEA,
    token_count  INT NOT NULL DEFAULT 0,
    version      INT NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by   BIGINT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   BIGINT,
    deleted      BOOLEAN NOT NULL DEFAULT false,
    deleted_at   TIMESTAMPTZ,
    deleted_by   BIGINT
);
COMMENT ON TABLE vo_kb_chunks IS '知识库文档分块，含向量用于 RAG 检索';
CREATE INDEX IF NOT EXISTS idx_kb_chunks_document ON vo_kb_chunks (document_id) WHERE deleted = false;

-- 若 pgvector 可用：把 embedding 列类型升级为 vector(1536)，并创建 ivfflat 余弦相似度索引。
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN
        -- 仅当列类型当前不是 vector 时才转换，避免重复执行报错。
        IF NOT EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name='vo_kb_chunks' AND column_name='embedding'
              AND udt_name='vector'
        ) THEN
            EXECUTE 'ALTER TABLE vo_kb_chunks ALTER COLUMN embedding TYPE vector(1536) USING NULL';
        END IF;
        IF NOT EXISTS (
            SELECT 1 FROM pg_indexes
            WHERE indexname='idx_kb_chunks_embedding'
        ) THEN
            EXECUTE 'CREATE INDEX idx_kb_chunks_embedding ON vo_kb_chunks USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100) WHERE deleted = false';
        END IF;
        RAISE NOTICE 'pgvector 已启用：vo_kb_chunks.embedding = vector(1536)，ivfflat 索引就绪';
    ELSE
        RAISE NOTICE 'pgvector 未启用：vo_kb_chunks.embedding 降级为 BYTEA，向量检索功能不可用';
    END IF;
END $$;

-- 4. 用户画像
CREATE TABLE IF NOT EXISTS vo_user_profiles (
    id                  BIGSERIAL PRIMARY KEY,
    uuid                UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id             BIGINT NOT NULL UNIQUE,
    expertise_level     VARCHAR(32) NOT NULL DEFAULT 'unknown',
    roles               JSONB NOT NULL DEFAULT '[]'::jsonb,
    domains             JSONB NOT NULL DEFAULT '[]'::jsonb,
    communication_style VARCHAR(32) NOT NULL DEFAULT 'balanced',
    preferred_language  VARCHAR(16) NOT NULL DEFAULT 'zh-CN',
    summary             TEXT,
    interaction_count   INT NOT NULL DEFAULT 0,
    last_updated_at     TIMESTAMPTZ,
    version             INT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by          BIGINT,
    deleted             BOOLEAN NOT NULL DEFAULT false,
    deleted_at          TIMESTAMPTZ,
    deleted_by          BIGINT
);
COMMENT ON TABLE vo_user_profiles IS 'AI 助手用户画像，持续学习用户特征以个性化回答';
CREATE INDEX IF NOT EXISTS idx_user_profiles_user ON vo_user_profiles (user_id) WHERE deleted = false;

-- 5. 对话会话
CREATE TABLE IF NOT EXISTS vo_chat_sessions (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    user_id         BIGINT NOT NULL,
    title           VARCHAR(255),
    scene           VARCHAR(32),
    summary         TEXT,
    entities        JSONB NOT NULL DEFAULT '{}'::jsonb,
    message_count   INT NOT NULL DEFAULT 0,
    last_intent     VARCHAR(32),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT
);
COMMENT ON TABLE vo_chat_sessions IS 'AI 助手对话会话，持久化多轮对话与上下文';
CREATE INDEX IF NOT EXISTS idx_chat_sessions_user ON vo_chat_sessions (user_id, last_active_at DESC) WHERE deleted = false;

-- 6. 对话消息
CREATE TABLE IF NOT EXISTS vo_chat_messages (
    id              BIGSERIAL PRIMARY KEY,
    uuid            UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    session_id      BIGINT NOT NULL REFERENCES vo_chat_sessions(id) ON DELETE CASCADE,
    user_id         BIGINT NOT NULL,
    role            VARCHAR(16) NOT NULL,
    content         TEXT NOT NULL,
    intent          JSONB,
    tools           JSONB,
    "references"    JSONB,
    latency_ms      INT,
    version         INT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by      BIGINT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      BIGINT,
    deleted         BOOLEAN NOT NULL DEFAULT false,
    deleted_at      TIMESTAMPTZ,
    deleted_by      BIGINT
);
COMMENT ON TABLE vo_chat_messages IS 'AI 助手对话消息';
CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON vo_chat_messages (session_id, id) WHERE deleted = false;

-- 7. 权限（注意：live schema 用 category 而非 type，enabled 而非 visible）
INSERT INTO vo_permissions (code, name, category, scope, description, sort_order, enabled) VALUES
 ('kb:view',         '知识库-查看',     'menu',   'platform', '查看 AI 助手知识库文档',    160, true),
 ('kb:manage',       '知识库-管理',     'action', 'platform', '创建/编辑/删除知识库文档',   161, true)
ON CONFLICT (code) DO NOTHING;

-- 8. 默认分类（created_by=1：平台管理员，避免 NULL 导致 Go 侧 Scan 失败）
INSERT INTO vo_kb_categories (name, code, description, sort_order, created_by, updated_by) VALUES
 ('通用',     'general',  '通用问答知识库',           10, 1, 1),
 ('镜像构建', 'build',    '构建失败、Dockerfile、Jenkins/Tekton 相关', 20, 1, 1),
 ('应用发布', 'release',  '发布、回滚、滚动策略相关',  30, 1, 1),
 ('K8s 运维', 'k8s',      'Pod/Deployment/Service/事件相关', 40, 1, 1),
 ('系统配置', 'system',   'RBAC、凭证、集群接入、系统设置', 50, 1, 1)
ON CONFLICT (code) DO NOTHING;

-- 9. 默认 FAQ 文档
INSERT INTO vo_kb_documents (category_id, title, source_type, content, tags, chunk_count, status, created_by, updated_by)
SELECT id, '构建失败常见原因', 'manual',
 '## 构建失败常见原因\n\n### 镜像拉取失败\n- 检查基础镜像仓库地址是否正确\n- 检查凭证是否有效（Harbor/Registry 凭证）\n- 检查网络连通性\n\n### Dockerfile 语法错误\n- 检查指令拼写\n- 检查 COPY/ADD 路径是否存在\n- 检查多阶段构建引用的镜像是否存在\n\n### 依赖拉取超时\n- Maven/Gradle/npm 仓库镜像配置\n- 网络代理配置\n- 依赖版本是否存在',
 '["build","dockerfile","failure"]', 0, 'active', 1, 1
FROM vo_kb_categories WHERE code='build'
AND NOT EXISTS (SELECT 1 FROM vo_kb_documents WHERE title='构建失败常见原因');

INSERT INTO vo_kb_documents (category_id, title, source_type, content, tags, chunk_count, status, created_by, updated_by)
SELECT id, 'Pod 启动失败排查指南', 'manual',
 '## Pod 启动失败排查指南\n\n### CrashLoopBackOff\n- 查看 kubectl logs --previous 获取上次退出日志\n- 常见原因：配置错误、依赖服务不可达、启动命令错误、资源限制过低\n\n### ImagePullBackOff\n- 检查镜像地址是否正确\n- 检查 imagePullSecrets 是否配置\n- 检查镜像是否存在\n\n### Pending\n- kubectl describe pod 查看 Events\n- 资源不足、节点污点未容忍、PVC 未绑定',
 '["k8s","pod","startup","crashloopbackoff"]', 0, 'active', 1, 1
FROM vo_kb_categories WHERE code='k8s'
AND NOT EXISTS (SELECT 1 FROM vo_kb_documents WHERE title='Pod 启动失败排查指南');

INSERT INTO vo_kb_documents (category_id, title, source_type, content, tags, chunk_count, status, created_by, updated_by)
SELECT id, '发布失败排查指南', 'manual',
 '## 发布失败排查指南\n\n### 滚动更新卡住\n- 检查 readiness probe 配置\n- 检查 maxSurge/maxUnavailable\n- 检查 PDB（PodDisruptionBudget）是否阻止\n- 检查新 Pod 是否就绪\n\n### 健康检查失败\n- 检查 liveness/readiness 路径与端口\n- 检查应用启动时间是否超过 initialDelaySeconds\n\n### 回滚操作\n- 在发布详情页点击「回滚」\n- Helm 发布可使用 helm rollback',
 '["release","rollback","rolling","healthcheck"]', 0, 'active', 1, 1
FROM vo_kb_categories WHERE code='release'
AND NOT EXISTS (SELECT 1 FROM vo_kb_documents WHERE title='发布失败排查指南');

-- 10. AI 嵌入系统设置
-- value 列为 JSONB，需用合法 JSON 文本：字符串值需带双引号。
INSERT INTO vo_system_settings (key, value, description, is_public) VALUES
 ('ai.embedding.provider',  '"openai"'::jsonb,                      '向量嵌入 Provider（openai | openai_compatible | ollama）', false),
 ('ai.embedding.url',       '""'::jsonb,                            '向量嵌入 API 基地址（如 https://api.openai.com）', false),
 ('ai.embedding.api_key',   '""'::jsonb,                            '向量嵌入 API Key', false),
 ('ai.embedding.model',     '"text-embedding-3-small"'::jsonb,      '向量嵌入模型名', false),
 ('ai.embedding.dimensions', '1536'::jsonb,                         '向量维度（需与 vo_kb_chunks.embedding 一致）', false)
ON CONFLICT (key) DO NOTHING;

-- 11. 把 0001 中需要追加到 vo_table_names 触发器的 AI 表登记进来，
--     以便 updated_at 自动维护触发器对它们生效。
--     （0001 的 trigger 段已经包含这些表名，本补丁不重复创建触发器函数。）

-- 完成
SELECT '0002_ai_assistant_fixup applied' AS status;
