// Package admission 提供 Mutating Webhook 的注解构造与 JSONPatch 生成。
//
// 设计要点：
//   - BuildCNIAnnotations 复用 workload renderer 的注解逻辑（Calico/Whereabouts/Cilium/Kube-OVN/Macvlan），
//     保证 webhook 注入的注解与发布时渲染的注解一致。
//   - BuildPatch 把目标注解集合转为 JSONPatch op 数组（add/replace），由 admission handler 写入 AdmissionResponse.patch。
//   - 路径转义遵循 RFC 6901：~ → ~0，/ → ~1，避免注解 key 含 "/" 时 patch 路径错乱。
package admission

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/vortexops/vortexops/internal/domain/networkprofile"
	ciliuminfra "github.com/vortexops/vortexops/internal/infrastructure/k8s/cilium"
)

// 稳定 IP 相关注解 key（与 workload/renderer.go 保持一致）。
const (
	AnnotationKeepPodIP    = "app.vortexops.io/keep-pod-ip"
	AnnotationStableIPFmt  = "app.vortexops.io/stable-ip-%d"
	AnnotationReplicaIndex = "app.vortexops.io/replica-index"
	AnnotationAssignedBy   = "app.vortexops.io/ip-assigned-by" // "webhook" | "release"
)

// JSONPatchOp 单个 JSON Patch 操作（RFC 6902）。
type JSONPatchOp struct {
	Op    string `json:"op"`              // "add" | "replace" | "remove"
	Path  string `json:"path"`            // 转义后的 JSON Pointer 路径
	Value string `json:"value,omitempty"` // 仅 add/replace 需要
}

// BuildCNIAnnotations 按 CNI provider 构造固定 IP 的 CNI 注解。
// 与 workload/renderer.go 的 injectCNIAnnotations 逻辑一致，避免 webhook 与 renderer 产生分歧。
//
// 参数：
//   - ip：单个稳定 Pod IP（webhook 按 replica 分配单个 IP，非批量）。
//   - profile：集群网络方案，决定 CNI provider；nil 时默认 whereabouts。
func BuildCNIAnnotations(ip string, profile *networkprofile.ProfileConfig) map[string]string {
	if ip == "" {
		return nil
	}
	ann := map[string]string{}
	stableIPs := []string{ip}
	cni := networkprofile.CNIWhereabouts
	if profile != nil && profile.CNI != "" {
		cni = profile.CNI
	}
	switch cni {
	case networkprofile.CNIWhereabouts:
		ann["k8s.v1.cni.cncf.io/ipAddrs"] = jsonArray(stableIPs)
	case networkprofile.CNICalico:
		ann["cni.projectcalico.org/ipAddrs"] = jsonArray(stableIPs)
	case networkprofile.CNICilium:
		for k, v := range ciliuminfra.BuildStaticIPAnnotations(ip, ciliuminfra.DefaultIPPoolName) {
			ann[k] = v
		}
		ciliuminfra.MergeCalicoCompat(ann, ip)
	case networkprofile.CNIKubeOVN:
		ann["ovn.kubernetes.io/ip_address"] = ip
	case networkprofile.CNIMacvlan, networkprofile.CNIIPVLAN:
		nadName := networkprofile.MultusNADName(cni, 0)
		if profile != nil {
			nadName = profile.NADName()
		}
		ann["k8s.v1.cni.cncf.io/networks"] = networksJSON(nadName, stableIPs)
	}
	return ann
}

// BuildStableIPAnnotations 构造稳定 IP 相关的完整注解集合（平台层 + CNI 层）。
// webhook 调用此函数生成需要 add 到 Pod 的全部注解。
//
// 参数：
//   - ip：分配的稳定 IP。
//   - replicaIndex：Pod 在 group 内的副本序号（0-based）。
//   - profile：集群网络方案（决定 CNI 注解 key）。
func BuildStableIPAnnotations(ip string, replicaIndex int, profile *networkprofile.ProfileConfig) map[string]string {
	ann := map[string]string{
		AnnotationKeepPodIP:    "true",
		AnnotationReplicaIndex: strconv.Itoa(replicaIndex),
		AnnotationAssignedBy:   "webhook",
	}
	if ip != "" {
		ann[fmt.Sprintf(AnnotationStableIPFmt, 0)] = ip
	}
	for k, v := range BuildCNIAnnotations(ip, profile) {
		ann[k] = v
	}
	return ann
}

// BuildPatch 把目标注解集合转为 JSONPatch op 数组。
// 对每个注解：
//   - 若 existing 已存在同名 key → op=replace（避免 add 失败）。
//   - 若 existing 不存在 → op=add。
//
// 调用方需把返回的 []JSONPatchOp 序列化为 JSON 后写入 AdmissionResponse.patch。
func BuildPatch(existing map[string]string, desired map[string]string) []JSONPatchOp {
	if len(desired) == 0 {
		return nil
	}
	patch := make([]JSONPatchOp, 0, len(desired))
	for k, v := range desired {
		op := "add"
		if _, ok := existing[k]; ok {
			op = "replace"
		}
		patch = append(patch, JSONPatchOp{
			Op:    op,
			Path:  fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(k)),
			Value: v,
		})
	}
	return patch
}

// escapeJSONPointer 按 RFC 6901 转义 JSON Pointer 引用 token。
// ~ → ~0，/ → ~1（顺序敏感：先转 ~ 再转 /）。
func escapeJSONPointer(s string) string {
	out := make([]byte, 0, len(s)+4)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '~':
			out = append(out, '~', '0')
		case '/':
			out = append(out, '~', '1')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

// jsonArray 把字符串切片序列化为 JSON 数组字符串（如 ["10.1.1.5"]）。
// 与 workload/renderer.go 的 jsonArray 行为一致。
func jsonArray(items []string) string {
	b, _ := json.Marshal(items)
	return string(b)
}

// networksJSON 生成 Multus networks annotation（指定 NAD + 固定 IP）。
// 与 workload/renderer.go 的 networksJSON 行为一致。
func networksJSON(nadName string, ips []string) string {
	type netEntry struct {
		Name string   `json:"name"`
		IPs  []string `json:"ips,omitempty"`
	}
	entries := []netEntry{{Name: nadName, IPs: ips}}
	b, _ := json.Marshal(entries)
	return string(b)
}
