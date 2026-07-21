-- 0004 down: 尽力恢复中间件表与种子数据（开发回滚用）。

CREATE TABLE vo_middleware_catalog (
    id             BIGSERIAL PRIMARY KEY,
    uuid           UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name           VARCHAR(64) NOT NULL UNIQUE,
    type           VARCHAR(32) NOT NULL,
    version        VARCHAR(32) NOT NULL,
    description    TEXT,
    chart_repo     VARCHAR(255),
    chart_name     VARCHAR(128),
    chart_version  VARCHAR(32),
    parameters_schema JSONB,
    is_recommended BOOLEAN NOT NULL DEFAULT false,
    is_system      BOOLEAN NOT NULL DEFAULT false,
    version_col    INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by     BIGINT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by     BIGINT,
    deleted        BOOLEAN NOT NULL DEFAULT false,
    deleted_at     TIMESTAMPTZ,
    deleted_by     BIGINT,
    CONSTRAINT mw_type_chk CHECK (type IN ('mysql','postgresql','redis','mongodb','kafka','zookeeper','elasticsearch','minio','rabbitmq','nacos','consul','etcd'))
);

CREATE TABLE vo_middleware_instances (
    id                BIGSERIAL PRIMARY KEY,
    uuid              UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id      BIGINT NOT NULL REFERENCES vo_workspaces(id),
    application_id    BIGINT REFERENCES vo_applications(id),
    group_id          BIGINT REFERENCES vo_groups(id),
    catalog_id        BIGINT NOT NULL REFERENCES vo_middleware_catalog(id),
    name              VARCHAR(64) NOT NULL,
    display_name      VARCHAR(128),
    description       TEXT,
    cluster_id        BIGINT NOT NULL REFERENCES vo_clusters(id),
    namespace         VARCHAR(128) NOT NULL,
    release_name      VARCHAR(128),
    version           VARCHAR(32) NOT NULL,
    replicas          INT NOT NULL DEFAULT 1,
    parameters        JSONB NOT NULL DEFAULT '{}',
    resources         JSONB,
    storage_size      BIGINT,
    storage_class     VARCHAR(128),
    persistence       BOOLEAN NOT NULL DEFAULT true,
    access_info       JSONB,
    status            VARCHAR(16) NOT NULL DEFAULT 'pending',
    current_version   VARCHAR(32),
    operation_status  VARCHAR(16) NOT NULL DEFAULT 'idle',
    last_operation_at TIMESTAMPTZ,
    owner_id          BIGINT NOT NULL REFERENCES vo_users(id),
    version_col       INT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by        BIGINT,
    deleted           BOOLEAN NOT NULL DEFAULT false,
    deleted_at        TIMESTAMPTZ,
    deleted_by        BIGINT,
    CONSTRAINT mw_inst_status_chk CHECK (status IN ('pending','running','updating','stopped','failed')),
    CONSTRAINT mw_inst_op_chk     CHECK (operation_status IN ('idle','installing','upgrading','scaling','deleting'))
);
CREATE UNIQUE INDEX uk_mw_inst_ws_name ON vo_middleware_instances (workspace_id, name) WHERE deleted = false;
CREATE INDEX idx_mw_inst_group ON vo_middleware_instances (group_id) WHERE deleted = false;

CREATE TABLE vo_middleware_operations (
    id            BIGSERIAL PRIMARY KEY,
    uuid          UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    instance_id   BIGINT NOT NULL REFERENCES vo_middleware_instances(id),
    operation     VARCHAR(16) NOT NULL,
    from_version  VARCHAR(32),
    to_version    VARCHAR(32),
    status        VARCHAR(16) NOT NULL DEFAULT 'running',
    message       TEXT,
    triggered_by  BIGINT NOT NULL REFERENCES vo_users(id),
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    duration_ms   BIGINT
);
CREATE INDEX idx_mw_op_instance ON vo_middleware_operations (instance_id, started_at DESC);
ALTER TABLE vo_middleware_operations ADD CONSTRAINT mw_op_type_chk CHECK (operation IN ('install','upgrade','scale','restart','delete'));

INSERT INTO vo_permissions (code, name, category, scope, description, sort_order, is_system) VALUES
    ('menu:middleware:view', '中间件-查看', 'menu', 'platform', '访问中间件目录与实例', 40, true),
    ('middleware:manage', '中间件-管理', 'action', 'workspace', '部署/升级/扩缩/备份中间件', 370, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO vo_menus (id, parent_id, code, name, path, icon, menu_type, scope, sort_order, visible, permission_code) VALUES
    ('grp-delivery', 'middleware', '中间件', '/middleware', 'middleware', 'menu', 'platform', 240, true, 'menu:middleware:view')
ON CONFLICT DO NOTHING;

CREATE OR REPLACE VIEW vo_v_workspace_overview AS
SELECT
    w.id, w.uuid, w.name, w.display_name, w.status, w.owner_id,
    (SELECT count(*) FROM vo_applications a WHERE a.workspace_id = w.id AND a.deleted = false) AS application_count,
    (SELECT count(*) FROM vo_groups g JOIN vo_applications a ON a.id = g.application_id
       WHERE a.workspace_id = w.id AND g.deleted = false) AS group_count,
    (SELECT count(*) FROM vo_middleware_instances m WHERE m.workspace_id = w.id AND m.deleted = false) AS middleware_count,
    (SELECT count(*) FROM vo_inference_services s WHERE s.workspace_id = w.id AND s.deleted = false) AS inference_count,
    (SELECT count(*) FROM vo_workspace_members wm WHERE wm.workspace_id = w.id AND wm.deleted = false) AS member_count
FROM vo_workspaces w
WHERE w.deleted = false;
