// Package clusterops - metrics 子模块。
// 定义节点/Pod 指标时间序列采样的领域结构体与仓储接口扩展。
// 通过 Kubelet Summary API 每 60s 采样一次写入采样表，供前端按时间范围查询绘制趋势图。
package clusterops

import (
	"context"
	"time"
)

// NodeMetricSample 节点指标采样（一条 = 一个节点在某个时刻的指标快照）。
type NodeMetricSample struct {
	ID                  int64
	ClusterID           int64
	NodeName            string
	Ts                  time.Time
	// CPU
	CPUUsageM           int
	CPUAllocatableM     int
	// 内存
	MemUsageBytes       int64
	MemWorkingSetBytes  int64
	MemAvailableBytes   int64
	MemAllocatableBytes int64
	// 磁盘 fs
	FsCapacityBytes     int64
	FsUsedBytes         int64
	FsAvailableBytes    int64
	FsInodesTotal       int64
	FsInodesUsed        int64
	// 网络
	NetRxBytes          int64
	NetTxBytes          int64
	NetRxBytesPerSec    float64
	NetTxBytesPerSec    float64
	NetRxErrors         int
	NetTxErrors         int
	NetRxDropped        int
	NetTxDropped        int
	// 负载
	Load1               float64
	Load5               float64
	Load15              float64
	// 派生百分比（写时计算）
	CPUUsagePct         float64
	MemUsagePct         float64
	FsUsagePct          float64
	FsInodesPct         float64
}

// PodMetricSample Pod 指标采样（一条 = 一个 Pod 在某个时刻的指标快照）。
type PodMetricSample struct {
	ID                 int64
	ClusterID          int64
	NodeName           string
	Namespace          string
	PodName            string
	Ts                 time.Time
	CPUUsageM          int
	MemUsageBytes      int64
	MemWorkingSetBytes int64
	NetRxBytes         int64
	NetTxBytes         int64
	NetRxBytesPerSec   float64
	NetTxBytesPerSec   float64
	RestartCount       int
	Phase              string
}

// MetricSeriesQuery 时序查询参数。
type MetricSeriesQuery struct {
	ClusterID int64
	NodeName  string // 节点指标用；Pod 指标可为空
	Namespace string // Pod 指标用
	PodName   string // Pod 指标用
	From      time.Time
	To        time.Time
	Step      time.Duration // 降采样步长；0 表示不降采样
}

// LatestMetricQuery 最新值查询参数。
type LatestMetricQuery struct {
	ClusterID    int64
	NodeName     string // 可选过滤；空表示全部节点/Pod
	IncludePods  bool   // true 时同时返回 Pod 最新值（仅 Pod 查询用）
}

// MetricSeriesResult 时序查询结果（点数组，前端直接喂给 ECharts）。
type MetricSeriesResult struct {
	Points []NodeMetricSample // 节点时序；Pod 查询时为空
	Pods   []PodMetricSample  // Pod 时序；节点查询时为空
}

// LatestMetricResult 最新值查询结果。
type LatestMetricResult struct {
	Nodes []NodeMetricSample
	Pods  []PodMetricSample
}

// MetricsRepository 指标采样仓储接口（由 clusteropsrepo 实现）。
// 注意：不嵌入 Repository，而是单独定义；具体仓储类型 *clusteropsrepo.Repository
// 同时实现 Repository 与 MetricsRepository。clusteropsapp.Service 持有二者。
type MetricsRepository interface {
	// 批量写入采样
	InsertNodeMetricSamples(ctx context.Context, items []NodeMetricSample) error
	InsertPodMetricSamples(ctx context.Context, items []PodMetricSample) error

	// 时序查询（含降采样）
	ListNodeMetricSeries(ctx context.Context, q MetricSeriesQuery) ([]NodeMetricSample, error)
	ListPodMetricSeries(ctx context.Context, q MetricSeriesQuery) ([]PodMetricSample, error)

	// 最新值查询（DISTINCT ON node_name / pod_name）
	ListLatestNodeMetrics(ctx context.Context, clusterID int64) ([]NodeMetricSample, error)
	ListLatestPodMetrics(ctx context.Context, clusterID int64, nodeName string) ([]PodMetricSample, error)

	// 清理过期采样
	DeleteMetricsBefore(ctx context.Context, before time.Time) error
}
