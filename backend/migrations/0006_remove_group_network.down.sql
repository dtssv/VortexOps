-- 0006_remove_group_network.down.sql
-- 回滚：恢复分组的网络配置列（数据无法恢复，仅恢复结构）。

ALTER TABLE vo_groups DROP COLUMN IF EXISTS mesh_enabled;

ALTER TABLE vo_groups
  ADD COLUMN IF NOT EXISTS network_mode           VARCHAR(16) NOT NULL DEFAULT 'clusterip',
  ADD COLUMN IF NOT EXISTS service_port_info      JSONB NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS keep_pod_ip            BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS allow_egress_internet  BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS egress_allowlist       JSONB,
  ADD COLUMN IF NOT EXISTS network_policy_enabled BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS ingress_enabled        BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS ingress_host           VARCHAR(255),
  ADD COLUMN IF NOT EXISTS ingress_path           VARCHAR(255),
  ADD COLUMN IF NOT EXISTS dns_policy             VARCHAR(32) NOT NULL DEFAULT 'ClusterFirst',
  ADD COLUMN IF NOT EXISTS host_network           BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE vo_groups
  ADD CONSTRAINT groups_netmode_chk CHECK (network_mode IN ('clusterip','nodeport','loadbalancer','hostnetwork'));

INSERT INTO vo_sys_dictionaries (category, code, label, sort_order, enabled) VALUES
 ('network_mode','clusterip','ClusterIP',10,true),
 ('network_mode','nodeport','NodePort',20,true),
 ('network_mode','loadbalancer','LoadBalancer',30,true),
 ('network_mode','hostnetwork','HostNetwork',40,true)
ON CONFLICT DO NOTHING;
