-- ============================================================================
-- 0005_node_pod_metrics.up.sql
-- ----------------------------------------------------------------------------
-- 增强集群节点/Pod 监控：时间序列采样表。
-- Kubelet Summary API 每 60s 采样一次，append-only 写入两张采样表，
-- 供前端按时间范围查询并绘制趋势图（CPU/内存/磁盘/网络/负载）。
--
-- 设计要点：
--   1. append-only：只 INSERT，不 UPDATE，避免锁竞争。
--   2. 时序索引：(cluster_id, node_name, ts DESC) 支撑「单节点时间范围」查询。
--   3. 降采样在后端 SQL 层用 date_bin 完成（Postgres 14+）。
--   4. 保留策略：syncer 启动时跑 DELETE WHERE ts < now() - 7d，之后每天清理一次。
-- ============================================================================

-- ---------- 5.1 vo_cluster_node_metric_sample ----------
-- 节点指标采样表（每节点每采样周期一条）。
CREATE TABLE vo_cluster_node_metric_sample (
    id                       BIGSERIAL PRIMARY KEY,
    cluster_id               BIGINT NOT NULL REFERENCES vo_clusters(id),
    node_name                VARCHAR(256) NOT NULL,
    ts                       TIMESTAMPTZ NOT NULL,
    -- CPU
    cpu_usage_m              INT NOT NULL DEFAULT 0,           -- 真实使用 millicores
    cpu_allocatable_m        INT NOT NULL DEFAULT 0,
    -- 内存
    mem_usage_bytes          BIGINT NOT NULL DEFAULT 0,
    mem_working_set_bytes    BIGINT NOT NULL DEFAULT 0,        -- OOM killer 关注的指标
    mem_available_bytes      BIGINT NOT NULL DEFAULT 0,
    mem_allocatable_bytes    BIGINT NOT NULL DEFAULT 0,
    -- 磁盘 fs (rootfs)
    fs_capacity_bytes        BIGINT NOT NULL DEFAULT 0,
    fs_used_bytes            BIGINT NOT NULL DEFAULT 0,
    fs_available_bytes       BIGINT NOT NULL DEFAULT 0,
    fs_inodes_total          BIGINT NOT NULL DEFAULT 0,
    fs_inodes_used           BIGINT NOT NULL DEFAULT 0,
    -- 网络（累计 + 速率）
    net_rx_bytes             BIGINT NOT NULL DEFAULT 0,        -- 累计接收字节数
    net_tx_bytes             BIGINT NOT NULL DEFAULT 0,        -- 累计发送字节数
    net_rx_bytes_per_sec     DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_tx_bytes_per_sec     DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_rx_errors            INT NOT NULL DEFAULT 0,
    net_tx_errors            INT NOT NULL DEFAULT 0,
    net_rx_dropped           INT NOT NULL DEFAULT 0,
    net_tx_dropped           INT NOT NULL DEFAULT 0,
    -- 负载
    load1                    DOUBLE PRECISION,
    load5                    DOUBLE PRECISION,
    load15                   DOUBLE PRECISION,
    -- 派生百分比（写时计算，避免每次查询都算）
    cpu_usage_pct            DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_usage_pct            DOUBLE PRECISION NOT NULL DEFAULT 0,
    fs_usage_pct             DOUBLE PRECISION NOT NULL DEFAULT 0,
    fs_inodes_pct            DOUBLE PRECISION NOT NULL DEFAULT 0
);

COMMENT ON TABLE vo_cluster_node_metric_sample IS
    '节点指标时间序列采样。每 60s 由 syncer 通过 Kubelet Summary API 抓取后批量写入。';

CREATE INDEX idx_node_metric_sample_query
    ON vo_cluster_node_metric_sample (cluster_id, node_name, ts DESC);
CREATE INDEX idx_node_metric_sample_cluster_ts
    ON vo_cluster_node_metric_sample (cluster_id, ts DESC);

-- ---------- 5.2 vo_cluster_pod_metric_sample ----------
-- Pod 指标采样表（每 Pod 每采样周期一条），node_name 用于「Pod 所在节点」关联展示。
CREATE TABLE vo_cluster_pod_metric_sample (
    id                       BIGSERIAL PRIMARY KEY,
    cluster_id               BIGINT NOT NULL REFERENCES vo_clusters(id),
    node_name                VARCHAR(256) NOT NULL,           -- Pod 所在节点
    namespace                VARCHAR(256) NOT NULL,
    pod_name                 VARCHAR(256) NOT NULL,
    ts                       TIMESTAMPTZ NOT NULL,
    cpu_usage_m              INT NOT NULL DEFAULT 0,
    mem_usage_bytes          BIGINT NOT NULL DEFAULT 0,
    mem_working_set_bytes    BIGINT NOT NULL DEFAULT 0,
    net_rx_bytes             BIGINT NOT NULL DEFAULT 0,
    net_tx_bytes             BIGINT NOT NULL DEFAULT 0,
    net_rx_bytes_per_sec     DOUBLE PRECISION NOT NULL DEFAULT 0,
    net_tx_bytes_per_sec     DOUBLE PRECISION NOT NULL DEFAULT 0,
    restart_count            INT NOT NULL DEFAULT 0,
    phase                    VARCHAR(32) NOT NULL DEFAULT ''
);

COMMENT ON TABLE vo_cluster_pod_metric_sample IS
    'Pod 指标时间序列采样。node_name 便于「按节点筛选 Pod」与「Pod 所在节点」展示。';

CREATE INDEX idx_pod_metric_sample_node
    ON vo_cluster_pod_metric_sample (cluster_id, node_name, ts DESC);
CREATE INDEX idx_pod_metric_sample_pod
    ON vo_cluster_pod_metric_sample (cluster_id, namespace, pod_name, ts DESC);

-- 完成
SELECT '0005_node_pod_metrics applied' AS status;
