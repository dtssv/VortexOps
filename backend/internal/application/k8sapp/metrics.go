// Package k8sapp - metrics 子模块。
// 通过 Kubelet Summary API（/api/v1/nodes/{n}/proxy/stats/summary）抓取节点与 Pod 的
// 真实使用指标：CPU 使用率、内存 WorkingSet、磁盘 fs/inodes、网络 RX/TX/错误/丢包。
// 内置于 kubelet，无需额外部署（cadvisor 已内嵌）。
package k8sapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/vortexops/vortexops/pkg/apperr"
)

// NodeSummary 一个节点的 Summary API 解析结果（含其上所有 Pod 的指标）。
type NodeSummary struct {
	Node NodeStats
	Pods []PodStats
}

// NodeStats 节点级指标（取自 Summary.pod/.node 顶层字段）。
type NodeStats struct {
	Name                string
	CPUUsageNanoSec     int64 // 累计纳秒（速率由应用层差分计算）
	MemUsageBytes       int64
	MemWorkingSetBytes  int64
	MemAvailableBytes   int64
	MemAllocatableBytes int64
	CPUAllocatableM     int
	// 磁盘 fs（rootfs/ephemeral-storage）
	FsCapacityBytes  int64
	FsUsedBytes      int64
	FsAvailableBytes int64
	FsInodesTotal    int64
	FsInodesUsed     int64
	// 网络（累计值；速率由应用层差分计算）
	NetRxBytes       int64
	NetTxBytes       int64
	NetRxErrors      int
	NetTxErrors      int
	NetRxDropped     int
	NetTxDropped     int
	// 负载（Summary API 不含，留空；后续可从 /proxy/metrics 解析 node_load1 补）
	Load1  float64
	Load5  float64
	Load15 float64
}

// PodStats Pod 级指标。
type PodStats struct {
	Name                string
	Namespace           string
	CPUUsageNanoSec     int64
	MemUsageBytes       int64
	MemWorkingSetBytes  int64
	NetRxBytes          int64
	NetTxBytes          int64
	NetRxBytesPerSec    float64 // Summary 直接提供速率（部分版本）
	NetTxBytesPerSec    float64
	RestartCount        int
	Phase               string
}

// FetchNodeSummaries 拉取集群所有节点的 Summary（并发，限流 8）。
// 返回值按节点名排序后的列表，便于上层稳定处理。
func (s *Service) FetchNodeSummaries(ctx context.Context, clusterID int64) ([]NodeSummary, error) {
	nodes, err := s.ListNodes(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	// 限流并发抓取
	const concurrency = 8
	sem := make(chan struct{}, concurrency)
	results := make([]NodeSummary, len(nodes))
	errs := make([]error, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, nodeName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sum, ferr := s.fetchSingleNodeSummary(ctx, clusterID, nodeName)
			if ferr != nil {
				errs[idx] = ferr
				return
			}
			results[idx] = *sum
		}(i, node.Name)
	}
	wg.Wait()

	// 收集成功的节点，记录失败节点（不整体失败，部分节点抓不到不应阻塞其他节点）。
	out := make([]NodeSummary, 0, len(results))
	var firstErr error
	for i := range results {
		if errs[i] != nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		out = append(out, results[i])
	}
	// 全部失败才报错；部分成功则返回成功部分。
	if len(out) == 0 && firstErr != nil {
		return nil, apperr.Internal("fetch node summaries (all failed)", firstErr)
	}
	return out, nil
}

// fetchSingleNodeSummary 调单个节点的 /proxy/stats/summary 并解析。
func (s *Service) fetchSingleNodeSummary(ctx context.Context, clusterID int64, nodeName string) (*NodeSummary, error) {
	c, err := s.client(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	// 通过 apiserver 代理访问 kubelet：/api/v1/nodes/{name}/proxy/stats/summary
	req := c.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy").
		Suffix("stats/summary")
	resp, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("proxy stats/summary for node %s: %w", nodeName, err)
	}
	defer resp.Close()
	body, err := io.ReadAll(resp)
	if err != nil {
		return nil, fmt.Errorf("read stats/summary body for node %s: %w", nodeName, err)
	}
	var raw summaryRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode stats/summary for node %s: %w", nodeName, err)
	}
	return convertSummary(nodeName, &raw), nil
}

// ============================================================================
// Summary API JSON 结构（仅保留前端展示所需字段）
// ============================================================================

type summaryRaw struct {
	Node nodeStatsRaw `json:"node"`
	Pods []podStatsRaw `json:"pods"`
}

type nodeStatsRaw struct {
	CPU       cpuStatsRaw    `json:"cpu"`
	Memory    memStatsRaw    `json:"memory"`
	FS        fsStatsRaw     `json:"fs"`
	Network   netStatsRaw    `json:"network"`
	StartTime string         `json:"startTime"`
}

type cpuStatsRaw struct {
	UsageNanoCores    int64 `json:"usageNanoCores"`    // 速率（瞬时核数 * 1e9）
	UsageCoreNanoSecs int64 `json:"usageCoreNanoSeconds"` // 累计纳秒
}

type memStatsRaw struct {
	UsageBytes      int64 `json:"usageBytes"`
	WorkingSetBytes int64 `json:"workingSetBytes"`
	AvailableBytes  int64 `json:"availableBytes"`
}

type fsStatsRaw struct {
	CapacityBytes  int64 `json:"capacityBytes"`
	UsedBytes      int64 `json:"usedBytes"`
	AvailableBytes int64 `json:"availableBytes"`
	InodesTotal    int64 `json:"inodes"`
	InodesUsed     int64 `json:"inodesUsed"`
}

type netStatsRaw struct {
	RxBytes    int64 `json:"rxBytes"`
	TxBytes    int64 `json:"txBytes"`
	RxErrors   int   `json:"rxErrors"`
	TxErrors   int   `json:"txErrors"`
	RxDropped  int   `json:"rxDropped"`
	TxDropped  int   `json:"txDropped"`
}

type podStatsRaw struct {
	PodRef     podRefRaw    `json:"podRef"`
	CPU        cpuStatsRaw  `json:"cpu"`
	Memory     memStatsRaw  `json:"memory"`
	Network    netStatsRaw  `json:"network"`
	Containers []containerStatsRaw `json:"containers"`
}

type podRefRaw struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type containerStatsRaw struct {
	Name   string       `json:"name"`
	CPU    cpuStatsRaw  `json:"cpu"`
	Memory memStatsRaw  `json:"memory"`
	// RestartCount 不在 Summary API；由应用层从 Pod status 获取并合并。
}

// convertSummary 把 raw JSON 转成 NodeSummary（含 Pods）。
func convertSummary(nodeName string, raw *summaryRaw) *NodeSummary {
	ns := &NodeSummary{
		Node: NodeStats{
			Name:               nodeName,
			CPUUsageNanoSec:    raw.Node.CPU.UsageCoreNanoSecs,
			MemUsageBytes:      raw.Node.Memory.UsageBytes,
			MemWorkingSetBytes: raw.Node.Memory.WorkingSetBytes,
			MemAvailableBytes:  raw.Node.Memory.AvailableBytes,
			FsCapacityBytes:    raw.Node.FS.CapacityBytes,
			FsUsedBytes:        raw.Node.FS.UsedBytes,
			FsAvailableBytes:   raw.Node.FS.AvailableBytes,
			FsInodesTotal:      raw.Node.FS.InodesTotal,
			FsInodesUsed:       raw.Node.FS.InodesUsed,
			NetRxBytes:         raw.Node.Network.RxBytes,
			NetTxBytes:         raw.Node.Network.TxBytes,
			NetRxErrors:        raw.Node.Network.RxErrors,
			NetTxErrors:        raw.Node.Network.TxErrors,
			NetRxDropped:       raw.Node.Network.RxDropped,
			NetTxDropped:       raw.Node.Network.TxDropped,
		},
	}
	// 取该节点的 allocatable（需从 K8s Node.Status 拿；此处先用 Summary 的 cpu.usageNanoCores 反推已用，
	// allocatable 由应用层在 CollectNodeMetrics 时从 ListNodes 结果补齐）。
	pods := make([]PodStats, 0, len(raw.Pods))
	for _, p := range raw.Pods {
		ps := PodStats{
			Name:               p.PodRef.Name,
			Namespace:          p.PodRef.Namespace,
			CPUUsageNanoSec:    p.CPU.UsageCoreNanoSecs,
			MemUsageBytes:      p.Memory.UsageBytes,
			MemWorkingSetBytes: p.Memory.WorkingSetBytes,
			NetRxBytes:         p.Network.RxBytes,
			NetTxBytes:         p.Network.TxBytes,
		}
		pods = append(pods, ps)
	}
	ns.Pods = pods
	return ns
}

// cpuQuantityToMilliShared 复用 service.go 的同名逻辑（此处仅声明避免重复定义冲突时改用别名）。
// 实际由 service.go 提供，本文件不重复定义。

// AllocatableFromNode 从 corev1.Node 提取 CPU(millicores) 与内存(Bytes)。
// 已导出供 clusteropsapp.CollectNodeMetrics 复用。
func AllocatableFromNode(node *corev1.Node) (cpuM int, memBytes int64) {
	cpuM = int(node.Status.Allocatable.Cpu().MilliValue())
	memBytes = node.Status.Allocatable.Memory().Value()
	return
}

// 编译期断言：确保 resource 包被使用（allocatableFromNode 依赖）。
var _ = resource.Quantity{}
