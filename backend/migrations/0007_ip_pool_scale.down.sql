-- 0007_ip_pool_scale.down.sql
-- 回滚 Phase 1 IP 池规模升级。注意：已分配条目数据不迁移回 allocations（仅删表）。

DROP INDEX IF EXISTS idx_alloc_rtype_rid_status;
DROP INDEX IF EXISTS idx_entries_rtype_rid_status;
DROP INDEX IF EXISTS idx_entries_pool_status;

ALTER TABLE vo_cluster_ip_pool_entries DROP CONSTRAINT IF EXISTS entries_status_chk;

DROP TABLE IF EXISTS vo_cluster_ip_pool_entries;

SELECT '0007_ip_pool_scale reverted' AS status;
