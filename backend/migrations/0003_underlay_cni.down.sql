-- 0003_underlay_cni.down.sql
-- 回滚 Underlay CNI 支持。

DROP INDEX IF EXISTS uq_ip_allocations_ip_active;

ALTER TABLE vo_cluster_ip_pools DROP CONSTRAINT IF EXISTS ip_pools_provider_chk;
ALTER TABLE vo_cluster_ip_pools
    ADD CONSTRAINT ip_pools_provider_chk
    CHECK (provider IN ('metallb','calico-ipam','whereabouts','kube-ovn'));

ALTER TABLE vo_cluster_ip_pools DROP COLUMN IF EXISTS metadata;

SELECT '0003_underlay_cni rolled back' AS status;
