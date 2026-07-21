// Package podnet 提供 Pod 网络地址解析（展示 IP / 占用 IP 检测）。
package podnet

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	// AnnotationStableIP0 平台分配的稳定 IP（物理网或 Overlay 固定 IP）。
	AnnotationStableIP0 = "app.vortexops.io/stable-ip-0"
)

// DisplayIP 返回应在 UI / DNS 映射中展示的 Pod IP。
// Multus 双网卡时 status.podIP 为主网卡（常为 Overlay），稳定物理 IP 在注解或副网卡上。
func DisplayIP(p *corev1.Pod) string {
	if p == nil {
		return ""
	}
	if ann := p.Annotations[AnnotationStableIP0]; ann != "" {
		return ann
	}
	if len(p.Status.PodIPs) > 1 {
		for _, pip := range p.Status.PodIPs {
			if pip.IP != "" && pip.IP != p.Status.PodIP {
				return pip.IP
			}
		}
	}
	return p.Status.PodIP
}

// InUseIPs 返回该 Pod 当前占用的稳定 IP 列表（供 webhook IPAM 去重）。
func InUseIPs(p *corev1.Pod) []string {
	if p == nil {
		return nil
	}
	seen := make(map[string]struct{}, 3)
	var out []string
	add := func(ip string) {
		if ip == "" {
			return
		}
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	if p.Status.Phase == corev1.PodRunning {
		add(DisplayIP(p))
		if p.Status.PodIP != "" {
			add(p.Status.PodIP)
		}
		for _, pip := range p.Status.PodIPs {
			add(pip.IP)
		}
	}
	return out
}
