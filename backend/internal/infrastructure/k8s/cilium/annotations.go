// Package cilium 提供 Cilium eBPF 数据面的注解构造与 CRD 渲染。
package cilium

import (
	"encoding/json"
)

// 稳定 Pod IP 注解（Cilium IPAM / 静态 IP 注入）。
const (
	AnnotationIPv4PodIP = "cilium.io/ipv4-pod-ip"
	AnnotationIPPool    = "ipam.cilium.io/ip-pool"
	DefaultIPPoolName   = "vortexops-stable-ip"
)

// BuildStaticIPAnnotations 为单个 Pod 构造 Cilium 静态 IP 注解。
// poolName 为空时使用 DefaultIPPoolName。
func BuildStaticIPAnnotations(ip, poolName string) map[string]string {
	if ip == "" {
		return nil
	}
	if poolName == "" {
		poolName = DefaultIPPoolName
	}
	return map[string]string{
		AnnotationIPv4PodIP: ip,
		AnnotationIPPool:    poolName,
	}
}

// MergeCalicoCompat 在 Cilium+Calico 链式 CNI 场景保留 Calico 注解（开发环境兼容）。
func MergeCalicoCompat(ann map[string]string, ip string) {
	if ann == nil || ip == "" {
		return
	}
	ips, _ := json.Marshal([]string{ip})
	ann["cni.projectcalico.org/ipAddrs"] = string(ips)
}
