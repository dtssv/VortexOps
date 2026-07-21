-- ============================================================================
-- 0003_underlay_cni.up.sql
-- ----------------------------------------------------------------------------
-- Underlay CNI 支持：Pod 直接绑定物理局域网 IP（Macvlan/IPVLAN），无隧道封装。
-- 多集群共享同一物理网段（如 10.0.0.0/8），由 VortexOps 全局 IPAM 统一分配，
-- 保证跨集群 IP 唯一。
--
-- 改动：
--   1. vo_cluster_ip_pools：扩展 provider 约束（macvlan/ipvlan）+ 加 metadata 列存
--      Underlay 配置（vlan_id/parent_interface/exclude_ranges 等）。
--   2. vo_cluster_ip_allocations：加跨集群全局唯一 IP 索引（status='allocated' 时
--      ip_address 全局唯一），保证多集群共享网段时不冲突。
-- ============================================================================

-- 1. vo_cluster_ip_pools.metadata：存 Underlay 配置（vlan_id/parent_interface 等）
ALTER TABLE vo_cluster_ip_pools
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 2. 扩展 provider 约束：支持 Macvlan / IPVLAN Underlay
ALTER TABLE vo_cluster_ip_pools DROP CONSTRAINT IF EXISTS ip_pools_provider_chk;
ALTER TABLE vo_cluster_ip_pools
    ADD CONSTRAINT ip_pools_provider_chk
    CHECK (provider IN ('metallb','calico-ipam','whereabouts','kube-ovn','macvlan','ipvlan'));

COMMENT ON COLUMN vo_cluster_ip_pools.metadata IS
    'IP 池扩展配置。Underlay 场景存 vlan_id/parent_interface/exclude_ranges 等；Overlay 场景可空。';

-- 3. 跨集群全局唯一 IP 索引：
--    多集群共享同一物理网段（如 10.0.0.0/8 切 /16 给各集群）时，
--    同一 IP 在 status=allocated 状态下必须全局唯一，避免两个集群的 Pod 拿到同 IP。
--    释放（released）后允许该 IP 被重新分配，故仅对 allocated 状态建唯一索引。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ip_allocations_ip_active
    ON vo_cluster_ip_allocations (ip_address)
    WHERE status = 'allocated';

COMMENT ON INDEX uq_ip_allocations_ip_active IS
    '跨集群全局唯一：allocated 状态下 ip_address 不可重复，保证多集群共享网段时不冲突';

-- 4. ip_allocations 增加注释说明（无结构变更）
COMMENT ON TABLE vo_cluster_ip_allocations IS
    'IP 分配记录。跨集群共享网段时由 uq_ip_allocations_ip_active 保证全局唯一。';

-- 完成
SELECT '0003_underlay_cni applied' AS status;
