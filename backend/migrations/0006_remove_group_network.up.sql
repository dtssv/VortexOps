-- 0006_remove_group_network.up.sql
-- 移除分组的网络配置列：不再支持 network_mode / service_port_info / ingress / egress / network_policy / dns_policy / host_network。
-- 架构变更：所有端口默认暴露，外部通过稳定 Pod IP 直连；不再创建 K8s Service/Ingress/NetworkPolicy。
-- 新增 mesh_enabled 列（分组维度可选 Mesh，Phase 5 生效，默认 false）。

ALTER TABLE vo_groups
  DROP COLUMN IF EXISTS network_mode,
  DROP COLUMN IF EXISTS service_port_info,
  DROP COLUMN IF EXISTS allow_egress_internet,
  DROP COLUMN IF EXISTS egress_allowlist,
  DROP COLUMN IF EXISTS network_policy_enabled,
  DROP COLUMN IF EXISTS ingress_enabled,
  DROP COLUMN IF EXISTS ingress_host,
  DROP COLUMN IF EXISTS ingress_path,
  DROP COLUMN IF EXISTS dns_policy,
  DROP COLUMN IF EXISTS host_network;

ALTER TABLE vo_groups DROP CONSTRAINT IF EXISTS groups_netmode_chk;

ALTER TABLE vo_groups ADD COLUMN IF NOT EXISTS mesh_enabled BOOLEAN NOT NULL DEFAULT false;

-- 清理 network_mode 字典（不再使用）。
DELETE FROM vo_sys_dictionaries WHERE category = 'network_mode';
