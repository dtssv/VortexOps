// Package mesh 按分组维度渲染 Cilium Mesh CRD（L7 治理、mTLS、流量切分）。
package mesh

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/vortexops/vortexops/internal/domain/application"
)

const (
	ciliumGroup = "cilium.io"
)

// RenderInput Mesh CRD 渲染输入。
type RenderInput struct {
	Group      *application.Group
	StableIPs  []string
	Namespace  string
}

// RenderCiliumPolicy 渲染 CiliumNetworkPolicy（L7 HTTP 入站规则 + mTLS 感知）。
func RenderCiliumPolicy(in RenderInput) *unstructured.Unstructured {
	if in.Group == nil || !in.Group.MeshEnabled {
		return nil
	}
	g := in.Group
	ns := in.Namespace
	if ns == "" {
		ns = g.Namespace
	}
	name := fmt.Sprintf("vortexops-mesh-g%d", g.ID)
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "cilium.io/v2",
			"kind":       "CiliumNetworkPolicy",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
				"labels": map[string]any{
					"app.vortexops.io/managed":   "true",
					"app.vortexops.io/group-id": strconv.FormatInt(g.ID, 10),
					"app.vortexops.io/mesh":      "true",
				},
			},
			"spec": map[string]any{
				"endpointSelector": map[string]any{
					"matchLabels": map[string]any{
						"app.vortexops.io/group-id": strconv.FormatInt(g.ID, 10),
					},
				},
				"ingress": []any{
					map[string]any{
						"fromEndpoints": []any{
							map[string]any{
								"matchLabels": map[string]any{
									"app.vortexops.io/managed": "true",
								},
							},
						},
						"toPorts": []any{
							map[string]any{
								"ports": []any{
									map[string]any{"port": "80", "protocol": "TCP"},
									map[string]any{"port": "443", "protocol": "TCP"},
								},
								"rules": map[string]any{
									"http": []any{
										map[string]any{"method": "GET"},
										map[string]any{"method": "POST"},
										map[string]any{"method": "PUT"},
										map[string]any{"method": "DELETE"},
									},
								},
							},
						},
					},
				},
				"egress": []any{
					map[string]any{
						"toEndpoints": []any{
							map[string]any{
								"matchLabels": map[string]any{
									"app.vortexops.io/managed": "true",
								},
							},
						},
					},
				},
			},
		},
	}
}

// RenderCiliumEnvoyConfig 渲染 CiliumEnvoyConfig（L7 路由 + 流量权重切分占位）。
// 候选/主版本分流由 releaseapp 在晋升时更新 backends。
func RenderCiliumEnvoyConfig(in RenderInput) *unstructured.Unstructured {
	if in.Group == nil || !in.Group.MeshEnabled {
		return nil
	}
	g := in.Group
	ns := in.Namespace
	if ns == "" {
		ns = g.Namespace
	}
	name := fmt.Sprintf("vortexops-envoy-g%d", g.ID)
	backends := make([]any, 0, len(in.StableIPs))
	for i, ip := range in.StableIPs {
		backends = append(backends, map[string]any{
			"address": ip,
			"port":    80,
			"weight":  100 / max(len(in.StableIPs), 1),
			"index":   i,
		})
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "cilium.io/v2",
			"kind":       "CiliumEnvoyConfig",
			"metadata": map[string]any{
				"name":      name,
				"namespace": ns,
				"labels": map[string]any{
					"app.vortexops.io/managed":   "true",
					"app.vortexops.io/group-id": strconv.FormatInt(g.ID, 10),
					"app.vortexops.io/mesh":      "true",
				},
			},
			"spec": map[string]any{
				"services": []any{
					map[string]any{
						"name":      g.DeploymentName,
						"namespace": ns,
						"ports":     []any{80, 443},
					},
				},
				"backends": backends,
				"resources": []any{},
			},
		},
	}
}

// RenderAll 渲染分组全部 Mesh CRD（Policy + EnvoyConfig）。
func RenderAll(in RenderInput) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	if p := RenderCiliumPolicy(in); p != nil {
		out = append(out, p)
	}
	if e := RenderCiliumEnvoyConfig(in); e != nil {
		out = append(out, e)
	}
	return out
}

// PolicyName 返回分组 Mesh Policy 资源名。
func PolicyName(groupID int64) string {
	return fmt.Sprintf("vortexops-mesh-g%d", groupID)
}

// EnvoyConfigName 返回分组 EnvoyConfig 资源名。
func EnvoyConfigName(groupID int64) string {
	return fmt.Sprintf("vortexops-envoy-g%d", groupID)
}
