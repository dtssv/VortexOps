-- 配置内容历史快照（配置集 + 分组本地配置 + 绑定生效快照）
CREATE TABLE IF NOT EXISTS vo_config_content_snapshots (
    id BIGSERIAL PRIMARY KEY,
    target_type VARCHAR(32) NOT NULL,
    target_id BIGINT NOT NULL,
    snapshot_no INT NOT NULL,
    content JSONB NOT NULL,
    change_reason VARCHAR(64) NOT NULL DEFAULT 'update',
    files_hash VARCHAR(64),
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (target_type, target_id, snapshot_no)
);

CREATE INDEX IF NOT EXISTS idx_config_snapshots_target
    ON vo_config_content_snapshots(target_type, target_id, created_at DESC);
