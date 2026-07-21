// Package clusteropsapp - metrics 子模块。
// 编排节点/Pod 指标采样：调 k8sapp.FetchNodeSummaries 抓取真实使用指标 → 计算速率（累计差分）
// → 写入时序采样表；提供时序查询与最新值查询供 HTTP 层调用。
package clusteropsapp

import (
	"context"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/vortexops/vortexops/internal/application/k8sapp"
	"github.com/vortexops/vortexops/internal/domain/clusterops"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// 指标采样保留期
const metricRetention = 7 * 24 * time.Hour

// CollectNodeMetrics 抓取集群所有节点的 Summary，转换样本并写入采样表。
// 速率通过累计值差分计算：读取上次最新一条采样的累计字节数，与当前差值除以时间差。
// 首次采样（无历史）速率记 0。
func (s *Service) CollectNodeMetrics(ctx context.Context, clusterID int64) error {
	if clusterID == 0 {
		return apperr.Validation("cluster_id is required", nil)
	}
	if s.metricsRepo == nil {
		return apperr.Internal("metrics repo not configured", nil)
	}

	// 1. 拉 K8s Node 列表（拿 allocatable + Pod owner/restart）
	nodes, err := s.k8s.ListNodes(ctx, clusterID)
	if err != nil {
		return apperr.Internal("list k8s nodes", err)
	}
	nodeAlloc := make(map[string]struct{ cpuM int; memBytes int64 }, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		cpuM, memBytes := k8sapp.AllocatableFromNode(n)
		nodeAlloc[n.Name] = struct{ cpuM int; memBytes int64 }{cpuM, memBytes}
	}

	// 2. 拉 Pod 列表（拿 restart_count 与 phase，Summary API 不含 restart）
	allPods, err := s.k8s.ListPods(ctx, clusterID, "", "")
	if err != nil {
		return apperr.Internal("list k8s pods", err)
	}
	type podKey struct{ ns, name string }
	podMeta := make(map[podKey]struct{ restart int; phase string }, len(allPods))
	for i := range allPods {
		p := &allPods[i]
		k := podKey{p.Namespace, p.Name}
		podMeta[k] = struct{ restart int; phase string }{totalRestartsFromPod(p), string(p.Status.Phase)}
	}

	// 3. 拉 Summary
	summaries, err := s.k8s.FetchNodeSummaries(ctx, clusterID)
	if err != nil {
		return apperr.Internal("fetch node summaries", err)
	}

	// 4. 读上次累计值（用于差分速率）
	prevNodeNet := make(map[string]netStat)
	if prev, err := s.metricsRepo.ListLatestNodeMetrics(ctx, clusterID); err == nil {
		for _, p := range prev {
			prevNodeNet[p.NodeName] = netStat{rx: p.NetRxBytes, tx: p.NetTxBytes, ts: p.Ts}
		}
	}
	prevPodNet := make(map[string]netStat) // key = ns/name
	if prev, err := s.metricsRepo.ListLatestPodMetrics(ctx, clusterID, ""); err == nil {
		for _, p := range prev {
			prevPodNet[p.Namespace+"/"+p.PodName] = netStat{rx: p.NetRxBytes, tx: p.NetTxBytes, ts: p.Ts}
		}
	}

	now := time.Now()
	nodeSamples := make([]clusterops.NodeMetricSample, 0, len(summaries))
	podSamples := make([]clusterops.PodMetricSample, 0)

	// 锁住进程内 CPU 累计值缓存（syncer 调用间隔短，简单加锁即可）。
	prevCPUMu.Lock()
	defer prevCPUMu.Unlock()

	for _, sum := range summaries {
		a := nodeAlloc[sum.Node.Name]
		prevN := prevNodeNet[sum.Node.Name]
		rxRate := computeRate(prevN.rx, sum.Node.NetRxBytes, prevN.ts, now)
		txRate := computeRate(prevN.tx, sum.Node.NetTxBytes, prevN.ts, now)
		// CPU 速率（millicores）= (累计 ns 差 / 时间差秒) / 1e6
		cpuUsageMFinal := computeCPURateM(prevCPUNs[sum.Node.Name], sum.Node.CPUUsageNanoSec, prevN.ts, now)

		allocCPUM := a.cpuM
		allocMem := a.memBytes
		cpuPct := pct(float64(cpuUsageMFinal), float64(allocCPUM))
		memPct := pct(float64(sum.Node.MemWorkingSetBytes), float64(allocMem))
		fsPct := pct(float64(sum.Node.FsUsedBytes), float64(sum.Node.FsCapacityBytes))
		inodePct := pct(float64(sum.Node.FsInodesUsed), float64(sum.Node.FsInodesTotal))

		// 更新进程内缓存（供下次差分）
		prevCPUNs[sum.Node.Name] = sum.Node.CPUUsageNanoSec

		nodeSamples = append(nodeSamples, clusterops.NodeMetricSample{
			ClusterID:           clusterID,
			NodeName:            sum.Node.Name,
			Ts:                  now,
			CPUUsageM:           cpuUsageMFinal,
			CPUAllocatableM:     allocCPUM,
			MemUsageBytes:       sum.Node.MemUsageBytes,
			MemWorkingSetBytes:  sum.Node.MemWorkingSetBytes,
			MemAvailableBytes:   sum.Node.MemAvailableBytes,
			MemAllocatableBytes: allocMem,
			FsCapacityBytes:     sum.Node.FsCapacityBytes,
			FsUsedBytes:         sum.Node.FsUsedBytes,
			FsAvailableBytes:    sum.Node.FsAvailableBytes,
			FsInodesTotal:       sum.Node.FsInodesTotal,
			FsInodesUsed:        sum.Node.FsInodesUsed,
			NetRxBytes:          sum.Node.NetRxBytes,
			NetTxBytes:          sum.Node.NetTxBytes,
			NetRxBytesPerSec:    rxRate,
			NetTxBytesPerSec:    txRate,
			NetRxErrors:         sum.Node.NetRxErrors,
			NetTxErrors:         sum.Node.NetTxErrors,
			NetRxDropped:        sum.Node.NetRxDropped,
			NetTxDropped:        sum.Node.NetTxDropped,
			CPUUsagePct:         cpuPct,
			MemUsagePct:         memPct,
			FsUsagePct:          fsPct,
			FsInodesPct:         inodePct,
		})

		// Pod 样本
		for _, p := range sum.Pods {
			key := p.Namespace + "/" + p.Name
			prevP := prevPodNet[key]
			pRxRate := computeRate(prevP.rx, p.NetRxBytes, prevP.ts, now)
			pTxRate := computeRate(prevP.tx, p.NetTxBytes, prevP.ts, now)
			pCPUm := computeCPURateM(prevCPUNsPod[key], p.CPUUsageNanoSec, prevP.ts, now)
			prevCPUNsPod[key] = p.CPUUsageNanoSec
			meta := podMeta[podKey{p.Namespace, p.Name}]
			podSamples = append(podSamples, clusterops.PodMetricSample{
				ClusterID:          clusterID,
				NodeName:           sum.Node.Name,
				Namespace:          p.Namespace,
				PodName:            p.Name,
				Ts:                 now,
				CPUUsageM:          pCPUm,
				MemUsageBytes:      p.MemUsageBytes,
				MemWorkingSetBytes: p.MemWorkingSetBytes,
				NetRxBytes:         p.NetRxBytes,
				NetTxBytes:         p.NetTxBytes,
				NetRxBytesPerSec:   pRxRate,
				NetTxBytesPerSec:   pTxRate,
				RestartCount:       meta.restart,
				Phase:              meta.phase,
			})
		}
	}

	// 5. 批量写入
	if err := s.metricsRepo.InsertNodeMetricSamples(ctx, nodeSamples); err != nil {
		return apperr.Internal("insert node metric samples", err)
	}
	if err := s.metricsRepo.InsertPodMetricSamples(ctx, podSamples); err != nil {
		return apperr.Internal("insert pod metric samples", err)
	}
	return nil
}

// ============================================================================
// 查询
// ============================================================================

// ListLatestNodeMetrics 返回集群下每个节点的最新采样。
func (s *Service) ListLatestNodeMetrics(ctx context.Context, clusterID int64) ([]clusterops.NodeMetricSample, error) {
	if s.metricsRepo == nil {
		return nil, apperr.Internal("metrics repo not configured", nil)
	}
	items, err := s.metricsRepo.ListLatestNodeMetrics(ctx, clusterID)
	if err != nil {
		return nil, apperr.Internal("list latest node metrics", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].NodeName < items[j].NodeName })
	return items, nil
}

// ListLatestPodMetrics 返回集群下每个 Pod 的最新采样。
// nodeName 非空时按所在节点过滤。
func (s *Service) ListLatestPodMetrics(ctx context.Context, clusterID int64, nodeName string) ([]clusterops.PodMetricSample, error) {
	if s.metricsRepo == nil {
		return nil, apperr.Internal("metrics repo not configured", nil)
	}
	items, err := s.metricsRepo.ListLatestPodMetrics(ctx, clusterID, nodeName)
	if err != nil {
		return nil, apperr.Internal("list latest pod metrics", err)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].PodName < items[j].PodName
	})
	return items, nil
}

// GetNodeMetricSeries 查询单节点时间范围内的采样序列。
// rangeStr: "1h"|"6h"|"24h"|"7d"；step 由 rangeStr 映射。
func (s *Service) GetNodeMetricSeries(ctx context.Context, clusterID int64, nodeName, rangeStr string) ([]clusterops.NodeMetricSample, error) {
	if s.metricsRepo == nil {
		return nil, apperr.Internal("metrics repo not configured", nil)
	}
	if nodeName == "" {
		return nil, apperr.Validation("node_name is required", nil)
	}
	from, to, step := resolveRange(rangeStr)
	return s.metricsRepo.ListNodeMetricSeries(ctx, clusterops.MetricSeriesQuery{
		ClusterID: clusterID, NodeName: nodeName, From: from, To: to, Step: step,
	})
}

// GetPodMetricSeries 查询单 Pod 时间范围内的采样序列。
func (s *Service) GetPodMetricSeries(ctx context.Context, clusterID int64, namespace, podName, rangeStr string) ([]clusterops.PodMetricSample, error) {
	if s.metricsRepo == nil {
		return nil, apperr.Internal("metrics repo not configured", nil)
	}
	if namespace == "" || podName == "" {
		return nil, apperr.Validation("namespace and pod_name are required", nil)
	}
	from, to, step := resolveRange(rangeStr)
	return s.metricsRepo.ListPodMetricSeries(ctx, clusterops.MetricSeriesQuery{
		ClusterID: clusterID, Namespace: namespace, PodName: podName, From: from, To: to, Step: step,
	})
}

// CleanupOldMetrics 删除超过保留期的采样（由 syncer 定时调用）。
func (s *Service) CleanupOldMetrics(ctx context.Context) error {
	if s.metricsRepo == nil {
		return nil
	}
	return s.metricsRepo.DeleteMetricsBefore(ctx, time.Now().Add(-metricRetention))
}

// ============================================================================
// 辅助
// ============================================================================

type netStat struct {
	rx, tx int64
	ts     time.Time
}

// prevCPUNs / prevCPUNsPod 进程内缓存上次累计 CPU ns（避免每次 Collect 读 DB 两次）。
// syncer 进程内长期存活，map 在多次 Collect 间累积。
var (
	prevCPUNs     = make(map[string]int64)     // key = node_name
	prevCPUNsPod  = make(map[string]int64)     // key = ns/name
	prevCPUMu     sync.Mutex
)

// computeRate 计算字节速率（bytes/sec）：(curr - prev) / dt。
// 首次（prev=0 或 ts 为零）或 curr < prev（计数器重置）时返回 0。
func computeRate(prevBytes, currBytes int64, prevTs, currTs time.Time) float64 {
	if prevBytes <= 0 || currBytes < prevBytes || prevTs.IsZero() {
		return 0
	}
	dt := currTs.Sub(prevTs).Seconds()
	if dt <= 0 {
		return 0
	}
	return float64(currBytes-prevBytes) / dt
}

// computeCPURateM 计算 CPU 使用 millicores：(累计 ns 差 / dt 秒) / 1e6。
func computeCPURateM(prevNs, currNs int64, prevTs, currTs time.Time) int {
	if prevNs <= 0 || currNs < prevNs || prevTs.IsZero() {
		return 0
	}
	dt := currTs.Sub(prevTs).Seconds()
	if dt <= 0 {
		return 0
	}
	// ns/s = cores * 1e9 → millicores = ns/s / 1e6
	return int(float64(currNs-prevNs) / dt / 1e6)
}

func pct(used, total float64) float64 {
	if total <= 0 {
		return 0
	}
	p := used / total * 100
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	return p
}

// resolveRange 把 "1h"/"6h"/"24h"/"7d" 映射为 (from, to, step)。
// step 映射保证点数 ≤ ~200。
func resolveRange(rangeStr string) (time.Time, time.Time, time.Duration) {
	to := time.Now()
	var d, step time.Duration
	switch rangeStr {
	case "6h":
		d, step = 6*time.Hour, 5*time.Minute
	case "24h":
		d, step = 24*time.Hour, 15*time.Minute
	case "7d":
		d, step = 7*24*time.Hour, time.Hour
	case "1h":
		fallthrough
	default:
		d, step = time.Hour, time.Minute
	}
	return to.Add(-d), to, step
}

// totalRestartsFromPod 从 corev1.Pod 累加所有容器的 restart count。
func totalRestartsFromPod(p *corev1.Pod) int {
	n := 0
	for _, cs := range p.Status.ContainerStatuses {
		n += int(cs.RestartCount)
	}
	return n
}
