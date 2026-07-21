package cilium

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/vortexops/vortexops/internal/domain/application"
)

const (
	ciliumGroup = "cilium.io"
	// CiliumLoadBalancerIPPool v2alpha1：为分组虚拟 IP 提供 eBPF L4 后端池（替代 K8s Service）。
	lbPoolAPIVersion = "cilium.io/v2alpha1"
	lbPoolKind       = "CiliumLoadBalancerIPPool"
)

// L4LBInput 分组 L4 负载均衡渲染输入。
type L4LBInput struct {
	Group       *application.Group
	BackendIPs  []string // 健康 Pod 稳定 IP 列表
	ServicePort int      // 默认 80；0 表示不渲染端口段
}

// RenderL4LoadBalancer 渲染 CiliumLoadBalancerIPPool，将流量导向分组 Pod 稳定 IP。
// 使用分组首个稳定 IP 作为虚拟服务锚点（外部/内部 L4 入口），后端为全部 Pod IP。
// 软降级：无后端 IP 时返回 nil。
func RenderL4LoadBalancer(in L4LBInput) *unstructured.Unstructured {
	if in.Group == nil || len(in.BackendIPs) == 0 {
		return nil
	}
	g := in.Group
	port := in.ServicePort
	if port <= 0 {
		port = 80
	}
	vip := in.BackendIPs[0]
	name := fmt.Sprintf("vortexops-lb-g%d", g.ID)

	backends := make([]any, 0, len(in.BackendIPs))
	for _, ip := range in.BackendIPs {
		backends = append(backends, map[string]any{
			"ip":   ip,
			"port": port,
		})
	}

	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": lbPoolAPIVersion,
			"kind":       lbPoolKind,
			"metadata": map[string]any{
				"name": name,
				"labels": map[string]any{
					"app.vortexops.io/managed":   "true",
					"app.vortexops.io/group-id": strconv.FormatInt(g.ID, 10),
				},
				"annotations": map[string]any{
					"app.vortexops.io/vip":       vip,
					"app.vortexops.io/backends":  fmt.Sprintf("%v", in.BackendIPs),
					"app.vortexops.io/l4-port":   strconv.Itoa(port),
				},
			},
			"spec": map[string]any{
				"cidrs": []any{
					map[string]any{"cidr": fmt.Sprintf("%s/32", vip)},
				},
				"serviceSelector": map[string]any{
					"matchLabels": map[string]any{
						"app.vortexops.io/group-id": strconv.FormatInt(g.ID, 10),
					},
				},
				"backends": backends,
			},
		},
	}
}

// GroupResourceName 返回分组 L4 LB CRD 资源名。
func GroupResourceName(groupID int64) string {
	return fmt.Sprintf("vortexops-lb-g%d", groupID)
}
