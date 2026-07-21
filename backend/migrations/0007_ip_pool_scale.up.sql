-- 0007_ip_pool_scale.up.sql
-- ----------------------------------------------------------------------------
-- Phase 1: IP 池升级到 10万+规模
--
-- 现状瓶颈（枚举式 IPAM）：
--   ListAvailableIPs 调 enumerateCIDR 在内存中枚举整个 CIDR（/16 = 65534 IP），
--   再与 vo_cluster_ip_allocations 全表做差集。10万+ 规模下：
--     - 内存峰值高（单池 65534 字符串 × N 池）
--     - 计数热点（allocated_count 单行 UPDATE 串行化）
--     - 无 SKIP LOCKED，并发分配冲突重试
--     - AllocateIP 单 IP 单事务，批量分配 N 个 = N 次事务
--
-- 方案：预生成 IP 条目表 + SKIP LOCKED 批量预占。
--   vo_cluster_ip_pool_entries：建池时一次性预生成所有可用 IP 条目（分批 INSERT）。
--   分配时 SELECT ... FOR UPDATE SKIP LOCKED LIMIT n 直接批量预占，单事务完成。
--   vo_cluster_ip_allocations 保留为历史/审计表（记录资源绑定关系），entries 表为
--   分配状态的权威来源。两表由 AllocateIP/ReleaseIP/AllocateIPsBatch 同步。
--
-- 兼容：保留 ListAvailableIPs 接口（Phase 2 webhook 单 IP 分配降级用），
--       改为查 entries 表而非内存枚举。
-- ============================================================================

-- 1. 预生成 IP 条目表（替代内存枚举）
CREATE TABLE IF NOT EXISTS vo_cluster_ip_pool_entries (
    id            BIGSERIAL PRIMARY KEY,
    ip_pool_id    BIGINT NOT NULL REFERENCES vo_cluster_ip_pools(id) ON DELETE CASCADE,
    ip_address    VARCHAR(64) NOT NULL,
    status        VARCHAR(16) NOT NULL DEFAULT 'free',  -- free / allocated / reserved
    resource_type VARCHAR(32),                          -- group / service（allocated 时填充）
    resource_id   BIGINT,
    replica_index INT,
    allocated_at  TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (ip_pool_id, ip_address)
);

-- 2. 复合索引：按池+状态查可用 IP（SKIP LOCKED 批量分配核心索引）
CREATE INDEX IF NOT EXISTS idx_entries_pool_status
    ON vo_cluster_ip_pool_entries (ip_pool_id, status);

-- 3. 按资源查分配（ListAllocationsByResource 用，替代扫 allocations 表）
CREATE INDEX IF NOT EXISTS idx_entries_rtype_rid_status
    ON vo_cluster_ip_pool_entries (resource_type, resource_id, status);

-- 4. vo_cluster_ip_allocations 增加复合索引（ListAllocationsByResource 已有查询用）
CREATE INDEX IF NOT EXISTS idx_alloc_rtype_rid_status
    ON vo_cluster_ip_allocations (resource_type, resource_id, status);

-- 5. 状态约束
ALTER TABLE vo_cluster_ip_pool_entries DROP CONSTRAINT IF EXISTS entries_status_chk;
ALTER TABLE vo_cluster_ip_pool_entries
    ADD CONSTRAINT entries_status_chk
    CHECK (status IN ('free', 'allocated', 'reserved'));

COMMENT ON TABLE vo_cluster_ip_pool_entries IS
    '预生成 IP 条目表。建池时批量 INSERT，分配时 SELECT FOR UPDATE SKIP LOCKED 批量预占。10万+规模核心表。';
COMMENT ON COLUMN vo_cluster_ip_pool_entries.status IS
    'free=未分配, allocated=已分配给资源, reserved=保留（网络/广播/网关等不可用）';

-- 完成
SELECT '0007_ip_pool_scale applied' AS status;
