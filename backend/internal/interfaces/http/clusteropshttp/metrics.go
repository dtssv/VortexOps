// Package clusteropshttp - metrics 子模块 handlers。
// 暴露节点/Pod 指标的最新值、时序查询与手动采集接口。
//
// 路由（由 server.go 注册）：
//   GET    /api/v1/clusters/{id}/node-metrics/latest                 — 全集群节点最新采样
//   GET    /api/v1/clusters/{id}/node-metrics/series?nodeName=&range= — 单节点时序
//   GET    /api/v1/clusters/{id}/pod-metrics/latest?nodeName=         — 全集群/按节点 Pod 最新采样
//   GET    /api/v1/clusters/{id}/pod-metrics/series?namespace=&pod=&range= — 单 Pod 时序
//   POST   /api/v1/clusters/{id}/node-metrics/collect                 — 立即触发一次采集
package clusteropshttp

import (
	"net/http"
	"time"

	"github.com/vortexops/vortexops/internal/domain/clusterops"
	"github.com/vortexops/vortexops/internal/interfaces/http/httpx"
	"github.com/vortexops/vortexops/pkg/apperr"
)

// ============================================================================
// 节点指标
// ============================================================================

// ListNodeLatestMetrics GET /clusters/{id}/node-metrics/latest
func (h *Handler) ListNodeLatestMetrics(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	items, err := h.svc.ListLatestNodeMetrics(r.Context(), cid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toNodeMetricDTOs(items))
}

// ListNodeMetricSeries GET /clusters/{id}/node-metrics/series?nodeName=&range=1h
func (h *Handler) ListNodeMetricSeries(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodeName := r.URL.Query().Get("nodeName")
	if nodeName == "" {
		httpx.WriteError(w, apperr.Validation("nodeName is required", nil))
		return
	}
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1h"
	}
	items, err := h.svc.GetNodeMetricSeries(r.Context(), cid, nodeName, rangeStr)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toNodeMetricDTOs(items))
}

// CollectNodeMetrics POST /clusters/{id}/node-metrics/collect
func (h *Handler) CollectNodeMetrics(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := h.svc.CollectNodeMetrics(r.Context(), cid); err != nil {
		httpx.WriteError(w, err)
		return
	}
	// 采集后返回最新采样，前端可立即刷新
	items, err := h.svc.ListLatestNodeMetrics(r.Context(), cid)
	if err != nil {
		httpx.OK(w, map[string]any{"status": "collected"})
		return
	}
	httpx.OK(w, toNodeMetricDTOs(items))
}

// ============================================================================
// Pod 指标
// ============================================================================

// ListPodLatestMetrics GET /clusters/{id}/pod-metrics/latest?nodeName=
func (h *Handler) ListPodLatestMetrics(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	nodeName := r.URL.Query().Get("nodeName")
	items, err := h.svc.ListLatestPodMetrics(r.Context(), cid, nodeName)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toPodMetricDTOs(items))
}

// ListPodMetricSeries GET /clusters/{id}/pod-metrics/series?namespace=&pod=&range=1h
func (h *Handler) ListPodMetricSeries(w http.ResponseWriter, r *http.Request) {
	cid, err := clusterIDFromURL(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	ns := r.URL.Query().Get("namespace")
	pod := r.URL.Query().Get("pod")
	if ns == "" || pod == "" {
		httpx.WriteError(w, apperr.Validation("namespace and pod are required", nil))
		return
	}
	rangeStr := r.URL.Query().Get("range")
	if rangeStr == "" {
		rangeStr = "1h"
	}
	items, err := h.svc.GetPodMetricSeries(r.Context(), cid, ns, pod, rangeStr)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.OK(w, toPodMetricDTOs(items))
}

// ============================================================================
// DTO
// ============================================================================

type nodeMetricDTO struct {
	ID                  int64   `json:"id"`
	ClusterID           int64   `json:"cluster_id"`
	NodeName            string  `json:"node_name"`
	Ts                  string  `json:"ts"` // RFC3339
	CPUUsageM           int     `json:"cpu_usage_m"`
	CPUAllocatableM     int     `json:"cpu_allocatable_m"`
	MemUsageBytes       int64   `json:"mem_usage_bytes"`
	MemWorkingSetBytes  int64   `json:"mem_working_set_bytes"`
	MemAvailableBytes   int64   `json:"mem_available_bytes"`
	MemAllocatableBytes int64   `json:"mem_allocatable_bytes"`
	FsCapacityBytes     int64   `json:"fs_capacity_bytes"`
	FsUsedBytes         int64   `json:"fs_used_bytes"`
	FsAvailableBytes    int64   `json:"fs_available_bytes"`
	FsInodesTotal       int64   `json:"fs_inodes_total"`
	FsInodesUsed        int64   `json:"fs_inodes_used"`
	NetRxBytes          int64   `json:"net_rx_bytes"`
	NetTxBytes          int64   `json:"net_tx_bytes"`
	NetRxBytesPerSec    float64 `json:"net_rx_bytes_per_sec"`
	NetTxBytesPerSec    float64 `json:"net_tx_bytes_per_sec"`
	NetRxErrors         int     `json:"net_rx_errors"`
	NetTxErrors         int     `json:"net_tx_errors"`
	NetRxDropped        int     `json:"net_rx_dropped"`
	NetTxDropped        int     `json:"net_tx_dropped"`
	Load1               float64 `json:"load1"`
	Load5               float64 `json:"load5"`
	Load15              float64 `json:"load15"`
	CPUUsagePct         float64 `json:"cpu_usage_pct"`
	MemUsagePct         float64 `json:"mem_usage_pct"`
	FsUsagePct          float64 `json:"fs_usage_pct"`
	FsInodesPct         float64 `json:"fs_inodes_pct"`
}

func toNodeMetricDTOs(items []clusterops.NodeMetricSample) []nodeMetricDTO {
	out := make([]nodeMetricDTO, 0, len(items))
	for _, s := range items {
		out = append(out, nodeMetricDTO{
			ID: s.ID, ClusterID: s.ClusterID, NodeName: s.NodeName, Ts: s.Ts.Format(time.RFC3339),
			CPUUsageM: s.CPUUsageM, CPUAllocatableM: s.CPUAllocatableM,
			MemUsageBytes: s.MemUsageBytes, MemWorkingSetBytes: s.MemWorkingSetBytes,
			MemAvailableBytes: s.MemAvailableBytes, MemAllocatableBytes: s.MemAllocatableBytes,
			FsCapacityBytes: s.FsCapacityBytes, FsUsedBytes: s.FsUsedBytes, FsAvailableBytes: s.FsAvailableBytes,
			FsInodesTotal: s.FsInodesTotal, FsInodesUsed: s.FsInodesUsed,
			NetRxBytes: s.NetRxBytes, NetTxBytes: s.NetTxBytes,
			NetRxBytesPerSec: s.NetRxBytesPerSec, NetTxBytesPerSec: s.NetTxBytesPerSec,
			NetRxErrors: s.NetRxErrors, NetTxErrors: s.NetTxErrors,
			NetRxDropped: s.NetRxDropped, NetTxDropped: s.NetTxDropped,
			Load1: s.Load1, Load5: s.Load5, Load15: s.Load15,
			CPUUsagePct: s.CPUUsagePct, MemUsagePct: s.MemUsagePct,
			FsUsagePct: s.FsUsagePct, FsInodesPct: s.FsInodesPct,
		})
	}
	return out
}

type podMetricDTO struct {
	ID                 int64   `json:"id"`
	ClusterID          int64   `json:"cluster_id"`
	NodeName           string  `json:"node_name"`
	Namespace          string  `json:"namespace"`
	PodName            string  `json:"pod_name"`
	Ts                 string  `json:"ts"`
	CPUUsageM          int     `json:"cpu_usage_m"`
	MemUsageBytes      int64   `json:"mem_usage_bytes"`
	MemWorkingSetBytes int64   `json:"mem_working_set_bytes"`
	NetRxBytes         int64   `json:"net_rx_bytes"`
	NetTxBytes         int64   `json:"net_tx_bytes"`
	NetRxBytesPerSec   float64 `json:"net_rx_bytes_per_sec"`
	NetTxBytesPerSec   float64 `json:"net_tx_bytes_per_sec"`
	RestartCount       int     `json:"restart_count"`
	Phase              string  `json:"phase"`
}

func toPodMetricDTOs(items []clusterops.PodMetricSample) []podMetricDTO {
	out := make([]podMetricDTO, 0, len(items))
	for _, s := range items {
		out = append(out, podMetricDTO{
			ID: s.ID, ClusterID: s.ClusterID, NodeName: s.NodeName, Namespace: s.Namespace,
			PodName: s.PodName, Ts: s.Ts.Format(time.RFC3339),
			CPUUsageM: s.CPUUsageM, MemUsageBytes: s.MemUsageBytes, MemWorkingSetBytes: s.MemWorkingSetBytes,
			NetRxBytes: s.NetRxBytes, NetTxBytes: s.NetTxBytes,
			NetRxBytesPerSec: s.NetRxBytesPerSec, NetTxBytesPerSec: s.NetTxBytesPerSec,
			RestartCount: s.RestartCount, Phase: s.Phase,
		})
	}
	return out
}
