// 集群网络方案（Network Profile）前端常量。
// 与后端 backend/internal/domain/networkprofile/profile.go 保持一致。
// 不同规模的集群采用不同的网络方案，由集群配置（cluster.metadata.network_profile）选定。

export type NetworkProfile = 'dev-single' | 'medium-overlay' | 'large-underlay' | 'xlarge-bgp';

export interface NetworkProfileOption {
  value: NetworkProfile;
  label: string;
  description: string;
  supportsUnderlay: boolean;
  // 该 profile 下可选的 CNI 列表（前端展示/校验用）。
  cniOptions: Array<{ value: string; label: string }>;
}

export const NETWORK_PROFILE_OPTIONS: NetworkProfileOption[] = [
  {
    value: 'dev-single',
    label: '开发环境（单集群）',
    description:
      '单机 kind/k3s。Overlay 仅供集群内通信；对外访问走 Multus Underlay 副网卡固定 IP 直连。必须开启 Multus 并配置 macvlan/ipvlan IP 池。Flannel 单独不可用。',
    supportsUnderlay: false,
    cniOptions: [
      { value: 'calico', label: 'Calico（集群内 Overlay；直连仍需 Multus 副网卡）' },
      { value: 'flannel', label: 'Flannel / kindnet（无静态 IP，必须配 Multus+Underlay 池）' },
    ],
  },
  {
    value: 'medium-overlay',
    label: '中型集群（Overlay）',
    description:
      '标准 Overlay 供集群内通信；对外访问走 Multus Underlay 副网卡固定 IP 直连。必须开启 Multus 并配置 macvlan/ipvlan IP 池。Flannel 单独不支持静态 IP。',
    supportsUnderlay: false,
    cniOptions: [
      { value: 'flannel', label: 'Flannel VXLAN（不可单独用于直连，需 Multus）' },
      { value: 'calico', label: 'Calico VXLAN（集群内 Overlay；直连仍需 Multus 副网卡）' },
    ],
  },
  {
    value: 'large-underlay',
    label: '大型集群（Underlay 直连）',
    description:
      'Underlay（Macvlan/IPVLAN），Pod 拿物理局域网固定 IP，与办公 PC 同网段直连。多集群共享超网，平台全局 IPAM 保证 IP 唯一。无隧道纯二层/三层转发。',
    supportsUnderlay: true,
    cniOptions: [
      { value: 'macvlan', label: 'Macvlan（Pod 独立 MAC，PC 同网段直连）' },
      { value: 'ipvlan', label: 'IPVLAN L2（共享父 MAC，高密度）' },
      { value: 'kube-ovn', label: 'Kube-OVN Underlay' },
    ],
  },
  {
    value: 'xlarge-bgp',
    label: '超大型集群（BGP 路由）',
    description:
      'Calico/Cilium BGP-only（无封装 L3 路由），Pod 固定 IP 经 BGP 宣告到核心交换机，办公网三层直连。适合超大规模、频繁扩缩，跨集群经 BGP 互联。',
    supportsUnderlay: false,
    cniOptions: [
      { value: 'calico', label: 'Calico BGP-only' },
    ],
  },
];

export function getNetworkProfileOption(profile: NetworkProfile): NetworkProfileOption | undefined {
  return NETWORK_PROFILE_OPTIONS.find((o) => o.value === profile);
}

/** 从集群 metadata 解析有效的 network_profile 与 CNI（空字符串视为未配置）。 */
export function requiresUnderlaySecondary(profile?: NetworkProfile | string) {
  return !profile || profile === 'dev-single' || profile === 'medium-overlay';
}

export function cniSupportsStaticIP(cni?: string) {
  return ['calico', 'cilium', 'whereabouts', 'macvlan', 'ipvlan', 'kube-ovn', 'calico-ipam'].includes(cni || '');
}

export function resolveClusterNetworkMeta(metadata?: Record<string, any>) {
  const np = metadata?.network_profile;
  const profile = (typeof np === 'string' ? np : np?.profile) as NetworkProfile | undefined;
  const cniRaw = typeof np === 'object' ? np?.cni : undefined;
  const cni = typeof cniRaw === 'string' && cniRaw.trim() ? cniRaw.trim() : undefined;
  const npObj = typeof np === 'object' ? np : undefined;
  return { profile, cni, npObj };
}

export function buildNetworkProfileConfig(
  profile: NetworkProfile,
  fields: Record<string, any>,
  existing?: Record<string, any>,
): Record<string, any> {
  const {
    cni,
    cidr,
    supernet_cidr,
    vlan_id,
    parent_interface,
    gateway,
    bgp_peer_ip,
    bgp_peer_asn,
    local_asn,
    multus_enabled,
  } = fields;
  const profileConfig: Record<string, any> = {
    ...(existing || {}),
    profile,
    cni: cni || '',
  };
  if (cidr) profileConfig.cidr = cidr;
  if (supernet_cidr) profileConfig.supernet_cidr = supernet_cidr;
  if (vlan_id) profileConfig.vlan_id = Number(vlan_id);
  if (parent_interface) profileConfig.parent_interface = parent_interface;
  if (gateway) profileConfig.gateway = gateway;
  if (bgp_peer_ip) profileConfig.bgp_peer_ip = bgp_peer_ip;
  if (bgp_peer_asn) profileConfig.bgp_peer_asn = Number(bgp_peer_asn);
  if (local_asn) profileConfig.local_asn = Number(local_asn);
  profileConfig.multus_enabled = !!multus_enabled && multus_enabled !== 0;
  return profileConfig;
}
