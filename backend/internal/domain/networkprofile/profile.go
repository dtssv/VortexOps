// Package networkprofile 定义集群网络方案（Network Profile）。
//
// 不同规模的集群采用不同的网络方案，由集群配置（cluster.metadata.network_profile）选定。
// 统一期望：任意 profile 下分组 Pod 都必须保留固定 IP，且对外以 Pod IP 直连（不经 Service/NodePort/LB）。
//
//   - dev-single     开发环境单集群：默认 Overlay 网卡 + Multus Underlay 副网卡固定 IP 直连。
//   - medium-overlay 中型集群：Overlay 集群内通信 + Multus Underlay 副网卡固定 IP 直连。
//   - large-underlay 大型集群：Underlay（Macvlan/IPVLAN），Pod 拿物理局域网 IP，与办公 PC 同网段直连；
//                    多集群共享超网（如 10.0.0.0/8 切 /16），由平台全局 IPAM 保证 IP 唯一。
//   - xlarge-bgp     超大型集群：Calico BGP-only（无封装 L3 路由），Pod 路由自动宣告到核心交换机；
//                    适合大规模、频繁扩缩，跨集群经 BGP Route Reflector 互联。
//
// 平台只做 IPAM 账本 + CNI annotation 注入 + 能力登记；CNI 本身安装在集群侧（manifest 见部署文档）。
package networkprofile

import (
	"errors"
	"fmt"
)

// Profile 网络方案标识。
type Profile string

const (
	// ProfileDevSingle 开发环境单集群：单机 kind/k3s，Overlay + Multus 副网卡固定 IP 直连。
	ProfileDevSingle Profile = "dev-single"
	// ProfileMediumOverlay 中型集群：标准 Overlay（Flannel/Calico VXLAN），多集群独立 CIDR。
	ProfileMediumOverlay Profile = "medium-overlay"
	// ProfileLargeUnderlay 大型集群：Underlay（Macvlan/IPVLAN），Pod 拿物理局域网 IP，全局 IPAM。
	ProfileLargeUnderlay Profile = "large-underlay"
	// ProfileXLargeBGP 超大型集群：Calico BGP-only（无封装 L3 路由），跨集群经 BGP 互联。
	ProfileXLargeBGP Profile = "xlarge-bgp"
)

// DataPlane 集群数据面实现（决定 L4 LB / 端点感知方式）。
type DataPlane string

const (
	DataPlaneLegacyKubeProxy DataPlane = "legacy-kube-proxy"
	DataPlaneCalico          DataPlane = "calico"
	DataPlaneCilium          DataPlane = "cilium"
)
// 一个 profile 可对应多种 CNI 实现（如 large-underlay 可用 macvlan 或 ipvlan）。
type CNIProvider string

const (
	CNINone        CNIProvider = ""              // 未指定（dev-single 默认）
	CNICalico      CNIProvider = "calico"       // Calico（VXLAN 或 BGP），支持 cni.projectcalico.org/ipAddrs 静态 IP
	CNIFlannel     CNIProvider = "flannel"      // Flannel VXLAN，不支持静态 IP 注解
	CNIMacvlan     CNIProvider = "macvlan"      // Macvlan Underlay
	CNIIPVLAN      CNIProvider = "ipvlan"       // IPVLAN L2 Underlay
	CNIKubeOVN     CNIProvider = "kube-ovn"     // Kube-OVN（Underlay 或 Overlay）
	CNIWhereabouts CNIProvider = "whereabouts"  // Whereabouts IPAM（常与 Multus 配合做副网卡）
	CNICilium      CNIProvider = "cilium"       // Cilium eBPF（Phase 3），支持稳定 IP + L4 LB
)

// ProfileConfig 单个集群的网络方案配置（从 cluster.metadata.network_profile 反序列化）。
type ProfileConfig struct {
	// Profile 网络方案标识（必填）。
	Profile Profile `json:"profile"`

	// CNI 集群实际安装的 CNI 插件类型（必填，用于 annotation 注入）。
	// 各 profile 的推荐值见下方注释，但允许运维按实际安装填写。
	CNI CNIProvider `json:"cni"`

	// DataPlane 数据面实现：cilium（eBPF L4 LB）、calico、legacy-kube-proxy。
	// 为空时由 CNI 推断（cilium→cilium，calico→calico，其它→legacy-kube-proxy）。
	DataPlane DataPlane `json:"data_plane,omitempty"`

	// CIDR 集群 Pod 网段（dev/medium profile 下可空，由 CNI 自管）。
	// large-underlay 下为该集群分到的 /16（如 10.1.0.0/16）。
	// xlarge-bgp 下为该集群宣告的 Pod CIDR 聚合段。
	CIDR string `json:"cidr,omitempty"`

	// 全局 IPAM 相关（large-underlay 必填）
	// SupernetCIDR 超网（如 10.0.0.0/8），所有集群共享。
	// VortexOps 全局 IPAM 在该超网内为每集群切 /16，再逐 Pod 分配。
	SupernetCIDR string `json:"supernet_cidr,omitempty"`

	// Underlay 相关（large-underlay 必填）
	// VLANID 物理 VLAN ID（如 100）。
	VLANID int `json:"vlan_id,omitempty"`
	// ParentInterface 物理父接口名（如 eth0 / eno1）。
	ParentInterface string `json:"parent_interface,omitempty"`
	// Gateway 物理网关 IP。
	Gateway string `json:"gateway,omitempty"`

	// BGP 相关（xlarge-bgp 必填）
	// BGPPeerIP 核心 BGP Route Reflector / 物理网关的 peer IP。
	BGPPeerIP string `json:"bgp_peer_ip,omitempty"`
	// BGPPeerASN 对端 ASN。
	BGPPeerASN int `json:"bgp_peer_asn,omitempty"`
	// LocalASN 本集群 ASN。
	LocalASN int `json:"local_asn,omitempty"`

	// 通用
	// MultusEnabled 是否启用 Multus 双网卡（默认 Overlay 网卡 + Underlay 副网卡）。
	// dev-single / medium-overlay 必须开启：Overlay 网段不可从集群外直连，业务口走副网卡固定 IP。
	// large-underlay 常配合 Multus 保留集群内 Overlay 通信能力。
	MultusEnabled bool `json:"multus_enabled,omitempty"`
}

// 领域错误。
var (
	ErrInvalidProfile      = errors.New("invalid network profile")
	ErrProfileConfigMissing = errors.New("network profile config missing")
)

// Validate 校验配置自洽（不校验 CNI 是否真装，那是集群探测职责）。
func (c *ProfileConfig) Validate() error {
	switch c.Profile {
	case ProfileDevSingle:
		// dev-single 最宽松，CNI 可空（默认 Flannel/kindnet）
		return nil
	case ProfileMediumOverlay:
		if c.CNI != CNICalico && c.CNI != CNIFlannel && c.CNI != CNICilium && c.CNI != CNINone {
			return fmt.Errorf("%w: medium-overlay 需要 calico/flannel/cilium CNI，got %s", ErrInvalidProfile, c.CNI)
		}
		return nil
	case ProfileLargeUnderlay:
		if c.CNI != CNIMacvlan && c.CNI != CNIIPVLAN && c.CNI != CNIKubeOVN {
			return fmt.Errorf("%w: large-underlay 需要 macvlan/ipvlan/kube-ovn CNI，got %s", ErrInvalidProfile, c.CNI)
		}
		if c.CIDR == "" {
			return fmt.Errorf("%w: large-underlay 必须配置集群 CIDR（如 10.1.0.0/16）", ErrInvalidProfile)
		}
		if c.SupernetCIDR == "" {
			return fmt.Errorf("%w: large-underlay 必须配置 supernet_cidr（如 10.0.0.0/8）", ErrInvalidProfile)
		}
		if c.ParentInterface == "" {
			return fmt.Errorf("%w: large-underlay 必须配置 parent_interface", ErrInvalidProfile)
		}
		return nil
	case ProfileXLargeBGP:
		if c.CNI != CNICalico && c.CNI != CNICilium {
			return fmt.Errorf("%w: xlarge-bgp 需要 calico/cilium CNI，got %s", ErrInvalidProfile, c.CNI)
		}
		if c.BGPPeerIP == "" || c.BGPPeerASN == 0 || c.LocalASN == 0 {
			return fmt.Errorf("%w: xlarge-bgp 必须配置 bgp_peer_ip/bgp_peer_asn/local_asn", ErrInvalidProfile)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown profile %s", ErrInvalidProfile, c.Profile)
	}
}

// SupportsUnderlay 该 profile 是否以 Underlay 作为主数据面（Pod 主网卡即物理 IP）。
func (c *ProfileConfig) SupportsUnderlay() bool {
	return c != nil && c.Profile == ProfileLargeUnderlay
}

// RequiresUnderlaySecondary Overlay/开发集群的 Overlay 网段不可从集群外直连，
// 必须走 Multus 副网卡 + Underlay IP 池，才能满足「固定 IP + Pod 直连」。
func (c *ProfileConfig) RequiresUnderlaySecondary() bool {
	if c == nil {
		return true
	}
	return c.Profile == ProfileDevSingle || c.Profile == ProfileMediumOverlay
}

// SupportsStaticIP 主 CNI 是否支持静态 Pod IP 注解（不含 Flannel/kindnet）。
func (c *ProfileConfig) SupportsStaticIP() bool {
	if c == nil {
		return false
	}
	switch c.CNI {
	case CNICalico, CNICilium, CNIWhereabouts, CNIMacvlan, CNIIPVLAN, CNIKubeOVN:
		return true
	default:
		return false
	}
}

// ValidateDirectAccessCapability 校验集群是否具备「固定 IP + 对外直连」能力。
// poolProviders 为该集群 IP 池的 provider 列表（如 macvlan、calico-ipam）。
func ValidateDirectAccessCapability(c *ProfileConfig, poolProviders []string) error {
	if c == nil {
		c = &ProfileConfig{Profile: ProfileDevSingle}
	}
	if c.RequiresUnderlaySecondary() {
		if !c.MultusEnabled {
			return fmt.Errorf("%s 需开启 Multus 副网卡，才能为 Pod 分配可对外直连的固定 IP（Overlay 网段不可从集群外直连）", c.Profile.Label())
		}
		if !hasAnyProvider(poolProviders, string(CNIMacvlan), string(CNIIPVLAN)) {
			return fmt.Errorf("%s 需配置 macvlan/ipvlan Underlay IP 池，才能固定 IP 并对外直连", c.Profile.Label())
		}
		return nil
	}
	switch c.Profile {
	case ProfileLargeUnderlay:
		if !hasAnyProvider(poolProviders, string(CNIMacvlan), string(CNIIPVLAN), string(CNIKubeOVN)) {
			return fmt.Errorf("large-underlay 集群需配置 macvlan/ipvlan/kube-ovn IP 池")
		}
		return nil
	case ProfileXLargeBGP:
		if !c.SupportsStaticIP() {
			return fmt.Errorf("xlarge-bgp 集群 CNI（%s）不支持静态 IP，请使用 Calico 或 Cilium", c.CNI)
		}
		if !hasAnyProvider(poolProviders, "calico-ipam", string(CNIWhereabouts), string(CNIKubeOVN)) {
			return fmt.Errorf("xlarge-bgp 集群需配置 calico-ipam/whereabouts IP 池")
		}
		return nil
	default:
		return fmt.Errorf("集群未配置可用的固定 IP 直连能力")
	}
}

func hasAnyProvider(providers []string, want ...string) bool {
	set := map[string]struct{}{}
	for _, p := range providers {
		if p != "" {
			set[p] = struct{}{}
		}
	}
	for _, w := range want {
		if _, ok := set[w]; ok {
			return true
		}
	}
	return false
}

// IsGlobalIPAM 该 profile 是否需要平台全局 IPAM（跨集群唯一 IP 分配）。
// large-underlay 共享超网必须全局；xlarge-bgp 各集群独立 CIDR 经 BGP 路由，
// 严格说不需要全局 IPAM（CIDR 不重叠即可），但为统一账本也走全局记录。
func (c *ProfileConfig) IsGlobalIPAM() bool {
	return c.Profile == ProfileLargeUnderlay || c.Profile == ProfileXLargeBGP
}

// ProfileLabel profile 的中文说明（前端展示用）。
func (p Profile) Label() string {
	switch p {
	case ProfileDevSingle:
		return "开发环境（单集群）"
	case ProfileMediumOverlay:
		return "中型集群（Overlay）"
	case ProfileLargeUnderlay:
		return "大型集群（Underlay 直连）"
	case ProfileXLargeBGP:
		return "超大型集群（BGP 路由）"
	default:
		return string(p)
	}
}

// Description profile 的详细说明（部署文档/提示用）。
func (p Profile) Description() string {
	switch p {
	case ProfileDevSingle:
		return "单机 kind/k3s。默认 Overlay 仅供集群内通信；对外访问走 Multus Underlay 副网卡固定 IP 直连。Flannel 单独不可用，需开启 Multus 并配置 Underlay IP 池。"
	case ProfileMediumOverlay:
		return "标准 Overlay（Calico/Cilium VXLAN）供集群内通信；对外访问走 Multus Underlay 副网卡固定 IP 直连。需开启 Multus 并配置 macvlan/ipvlan IP 池。Flannel 单独不支持静态 IP。"
	case ProfileLargeUnderlay:
		return "Underlay（Macvlan/IPVLAN），Pod 拿物理局域网固定 IP，与办公 PC 同网段直连。多集群共享超网，平台全局 IPAM 保证 IP 唯一。无隧道纯二层/三层转发。"
	case ProfileXLargeBGP:
		return "Calico/Cilium BGP-only（无封装 L3 路由），Pod 固定 IP 经 BGP 宣告到核心交换机/Route Reflector，办公网三层直连。适合超大规模、频繁扩缩，跨集群经 BGP 互联。"
	default:
		return ""
	}
}

// AllProfiles 返回所有 profile（前端下拉/校验用）。
func AllProfiles() []Profile {
	return []Profile{ProfileDevSingle, ProfileMediumOverlay, ProfileLargeUnderlay, ProfileXLargeBGP}
}

// ParseProfile 从字符串解析 profile（容错）。
func ParseProfile(s string) (Profile, error) {
	p := Profile(s)
	for _, valid := range AllProfiles() {
		if p == valid {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrInvalidProfile, s)
}

// DetectCNI 通过查询集群 CNI 配置自动探测已安装的 CNI 插件类型。
// 探测顺序：检查 kube-system 命名空间下的 CNI 配置 ConfigMap / DaemonSet：
//  1. calico-node DaemonSet → CNICalico
//  2. cilium DaemonSet → CNICilium
//  3. kube-flannel DaemonSet → CNIFlannel
//  4. 默认 → CNINone（由调用方决定兜底策略）
//
// 探测失败返回 CNINone 与 error，调用方可降级为 profile 配置的 CNI 或 dev-single 默认。
// DetectCNI 只读查询，无副作用，可安全周期调用。
func DetectCNI(getDaemonSet func(namespace, name string) (bool, error)) (CNIProvider, error) {
	if getDaemonSet == nil {
		return CNINone, fmt.Errorf("getDaemonSet callback is nil")
	}
	// Calico
	if exists, err := getDaemonSet("kube-system", "calico-node"); err == nil && exists {
		return CNICalico, nil
	}
	// Cilium
	if exists, err := getDaemonSet("kube-system", "cilium"); err == nil && exists {
		return CNICilium, nil
	}
	// Flannel
	if exists, err := getDaemonSet("kube-system", "kube-flannel"); err == nil && exists {
		return CNIFlannel, nil
	}
	return CNINone, nil
}

// MultusNADName 返回 Multus NetworkAttachmentDefinition 名称。
// 约定：<cni> 或 <cni>-<vlan_id>（与集群侧预创建的 NAD 名一致）。
func MultusNADName(cni CNIProvider, vlanID int) string {
	if vlanID != 0 {
		return fmt.Sprintf("%s-%d", cni, vlanID)
	}
	return string(cni)
}

// NADName 返回当前 profile 下应注入到 Pod 的 Multus NAD 名。
func (c *ProfileConfig) NADName() string {
	if c == nil {
		return MultusNADName(CNIMacvlan, 0)
	}
	cni := c.CNI
	if cni != CNIMacvlan && cni != CNIIPVLAN {
		cni = CNIMacvlan
	}
	return MultusNADName(cni, c.VLANID)
}

// EffectiveDataPlane 返回有效数据面（显式配置优先，否则由 CNI 推断）。
func (c *ProfileConfig) EffectiveDataPlane() DataPlane {
	if c == nil {
		return DataPlaneLegacyKubeProxy
	}
	if c.DataPlane != "" {
		return c.DataPlane
	}
	switch c.CNI {
	case CNICilium:
		return DataPlaneCilium
	case CNICalico:
		return DataPlaneCalico
	default:
		return DataPlaneLegacyKubeProxy
	}
}
