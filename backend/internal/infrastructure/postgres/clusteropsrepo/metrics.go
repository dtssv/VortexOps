// Package clusteropsrepo - metrics 子模块仓储实现。
// 节点/Pod 指标时间序列采样的批量插入、时序查询（含 date_bin 降采样）、
// 最新值查询（DISTINCT ON）与过期清理。
package clusteropsrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/vortexops/vortexops/internal/domain/clusterops"
)

// nodeMetricSampleColumns 节点采样列（不含 id，写入时由 DB 生成）。
const nodeMetricSampleColumns = `cluster_id, node_name, ts,
	cpu_usage_m, cpu_allocatable_m,
	mem_usage_bytes, mem_working_set_bytes, mem_available_bytes, mem_allocatable_bytes,
	fs_capacity_bytes, fs_used_bytes, fs_available_bytes, fs_inodes_total, fs_inodes_used,
	net_rx_bytes, net_tx_bytes, net_rx_bytes_per_sec, net_tx_bytes_per_sec,
	net_rx_errors, net_tx_errors, net_rx_dropped, net_tx_dropped,
	load1, load5, load15,
	cpu_usage_pct, mem_usage_pct, fs_usage_pct, fs_inodes_pct`

// nodeMetricSampleSelectCols 查询列（含 id），顺序与 scanNodeMetricSample 一致。
const nodeMetricSampleSelectCols = `id, ` + nodeMetricSampleColumns

const podMetricSampleColumns = `cluster_id, node_name, namespace, pod_name, ts,
	cpu_usage_m, mem_usage_bytes, mem_working_set_bytes,
	net_rx_bytes, net_tx_bytes, net_rx_bytes_per_sec, net_tx_bytes_per_sec,
	restart_count, phase`

const podMetricSampleSelectCols = `id, ` + podMetricSampleColumns

func scanNodeMetricSample(row interface {
	Scan(dest ...any) error
}) (*clusterops.NodeMetricSample, error) {
	s := &clusterops.NodeMetricSample{}
	var load1, load5, load15 *float64 // load 可为 NULL
	if err := row.Scan(
		&s.ID, &s.ClusterID, &s.NodeName, &s.Ts,
		&s.CPUUsageM, &s.CPUAllocatableM,
		&s.MemUsageBytes, &s.MemWorkingSetBytes, &s.MemAvailableBytes, &s.MemAllocatableBytes,
		&s.FsCapacityBytes, &s.FsUsedBytes, &s.FsAvailableBytes, &s.FsInodesTotal, &s.FsInodesUsed,
		&s.NetRxBytes, &s.NetTxBytes, &s.NetRxBytesPerSec, &s.NetTxBytesPerSec,
		&s.NetRxErrors, &s.NetTxErrors, &s.NetRxDropped, &s.NetTxDropped,
		&load1, &load5, &load15,
		&s.CPUUsagePct, &s.MemUsagePct, &s.FsUsagePct, &s.FsInodesPct,
	); err != nil {
		return nil, err
	}
	if load1 != nil {
		s.Load1 = *load1
	}
	if load5 != nil {
		s.Load5 = *load5
	}
	if load15 != nil {
		s.Load15 = *load15
	}
	return s, nil
}

func scanPodMetricSample(row interface {
	Scan(dest ...any) error
}) (*clusterops.PodMetricSample, error) {
	s := &clusterops.PodMetricSample{}
	if err := row.Scan(
		&s.ID, &s.ClusterID, &s.NodeName, &s.Namespace, &s.PodName, &s.Ts,
		&s.CPUUsageM, &s.MemUsageBytes, &s.MemWorkingSetBytes,
		&s.NetRxBytes, &s.NetTxBytes, &s.NetRxBytesPerSec, &s.NetTxBytesPerSec,
		&s.RestartCount, &s.Phase,
	); err != nil {
		return nil, err
	}
	return s, nil
}

// ============================================================================
// 批量插入
// ============================================================================

// InsertNodeMetricSamples 批量插入节点采样（multi-row VALUES）。
func (r *Repository) InsertNodeMetricSamples(ctx context.Context, items []clusterops.NodeMetricSample) error {
	if len(items) == 0 {
		return nil
	}
	const cols = nodeMetricSampleColumns
	// 29 个字段/行（与 nodeMetricSampleColumns 列数一致）
	rowsPerItem := 29
	args := make([]any, 0, len(items)*rowsPerItem)
	values := ""
	for i, s := range items {
		if i > 0 {
			values += ","
		}
		base := i*rowsPerItem
		values += fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10,
			base+11, base+12, base+13, base+14, base+15, base+16, base+17, base+18, base+19, base+20,
			base+21, base+22, base+23, base+24, base+25, base+26, base+27, base+28, base+29,
		)
		args = append(args,
			s.ClusterID, s.NodeName, s.Ts,
			s.CPUUsageM, s.CPUAllocatableM,
			s.MemUsageBytes, s.MemWorkingSetBytes, s.MemAvailableBytes, s.MemAllocatableBytes,
			s.FsCapacityBytes, s.FsUsedBytes, s.FsAvailableBytes, s.FsInodesTotal, s.FsInodesUsed,
			s.NetRxBytes, s.NetTxBytes, s.NetRxBytesPerSec, s.NetTxBytesPerSec,
			s.NetRxErrors, s.NetTxErrors, s.NetRxDropped, s.NetTxDropped,
			nullableFloat64(s.Load1), nullableFloat64(s.Load5), nullableFloat64(s.Load15),
			s.CPUUsagePct, s.MemUsagePct, s.FsUsagePct, s.FsInodesPct,
		)
	}
	q := `INSERT INTO vo_cluster_node_metric_sample (` + cols + `) VALUES ` + values
	_, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert node metric samples: %w", err)
	}
	return nil
}

// InsertPodMetricSamples 批量插入 Pod 采样。
func (r *Repository) InsertPodMetricSamples(ctx context.Context, items []clusterops.PodMetricSample) error {
	if len(items) == 0 {
		return nil
	}
	const cols = podMetricSampleColumns
	rowsPerItem := 14
	args := make([]any, 0, len(items)*rowsPerItem)
	values := ""
	for i, s := range items {
		if i > 0 {
			values += ","
		}
		base := i*rowsPerItem
		values += fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12, base+13, base+14,
		)
		args = append(args,
			s.ClusterID, s.NodeName, s.Namespace, s.PodName, s.Ts,
			s.CPUUsageM, s.MemUsageBytes, s.MemWorkingSetBytes,
			s.NetRxBytes, s.NetTxBytes, s.NetRxBytesPerSec, s.NetTxBytesPerSec,
			s.RestartCount, s.Phase,
		)
	}
	q := `INSERT INTO vo_cluster_pod_metric_sample (` + cols + `) VALUES ` + values
	_, err := r.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("insert pod metric samples: %w", err)
	}
	return nil
}

// ============================================================================
// 时序查询（含 date_bin 降采样）
// ============================================================================

// ListNodeMetricSeries 查询单节点时间范围内的采样序列。
// step > 0 时用 date_bin 降采样（取每桶最后一条的聚合值）；step == 0 时返回原始点。
func (r *Repository) ListNodeMetricSeries(ctx context.Context, q clusterops.MetricSeriesQuery) ([]clusterops.NodeMetricSample, error) {
	if q.NodeName == "" {
		return nil, fmt.Errorf("node_name is required for node metric series query")
	}
	if q.Step > 0 {
		// 降采样：每桶取最后一条记录的指标值（子查询 DISTINCT ON 桶+节点）。
		// date_bin 在 PG14+ 可用；以 ts 落桶，按桶内 ts DESC 取首行。
		stepSecs := int64(q.Step.Seconds())
		stepInterval := fmt.Sprintf("%d seconds", stepSecs)
		const sqlFixed = `SELECT ` + nodeMetricSampleSelectCols + ` FROM (
				SELECT DISTINCT ON (date_bin($4::interval, ts, '2000-01-01'), node_name)
					` + nodeMetricSampleSelectCols + `
				FROM vo_cluster_node_metric_sample
				WHERE cluster_id=$1 AND node_name=$2 AND ts >= $3 AND ts <= $5
				ORDER BY date_bin($4::interval, ts, '2000-01-01'), node_name, ts DESC
			) t ORDER BY ts ASC`
		rows, err := r.pool.Query(ctx, sqlFixed, q.ClusterID, q.NodeName, q.From, stepInterval, q.To)
		if err != nil {
			return nil, fmt.Errorf("query node metric series (downsampled): %w", err)
		}
		defer rows.Close()
		return collectNodeSamples(rows)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+nodeMetricSampleSelectCols+` FROM vo_cluster_node_metric_sample
		 WHERE cluster_id=$1 AND node_name=$2 AND ts >= $3 AND ts <= $4 ORDER BY ts ASC`,
		q.ClusterID, q.NodeName, q.From, q.To,
	)
	if err != nil {
		return nil, fmt.Errorf("query node metric series: %w", err)
	}
	defer rows.Close()
	return collectNodeSamples(rows)
}

// ListPodMetricSeries 查询单 Pod 时间范围内的采样序列。
func (r *Repository) ListPodMetricSeries(ctx context.Context, q clusterops.MetricSeriesQuery) ([]clusterops.PodMetricSample, error) {
	if q.Namespace == "" || q.PodName == "" {
		return nil, fmt.Errorf("namespace and pod_name are required for pod metric series query")
	}
	if q.Step > 0 {
		stepSecs := int64(q.Step.Seconds())
		stepInterval := fmt.Sprintf("%d seconds", stepSecs)
		const sql = `SELECT ` + podMetricSampleSelectCols + ` FROM (
				SELECT DISTINCT ON (date_bin($5::interval, ts, '2000-01-01'), pod_name)
					` + podMetricSampleSelectCols + `
				FROM vo_cluster_pod_metric_sample
				WHERE cluster_id=$1 AND namespace=$2 AND pod_name=$3 AND ts >= $4 AND ts <= $6
				ORDER BY date_bin($5::interval, ts, '2000-01-01'), pod_name, ts DESC
			) t ORDER BY ts ASC`
		rows, err := r.pool.Query(ctx, sql, q.ClusterID, q.Namespace, q.PodName, q.From, stepInterval, q.To)
		if err != nil {
			return nil, fmt.Errorf("query pod metric series (downsampled): %w", err)
		}
		defer rows.Close()
		return collectPodSamples(rows)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+podMetricSampleSelectCols+` FROM vo_cluster_pod_metric_sample
		 WHERE cluster_id=$1 AND namespace=$2 AND pod_name=$3 AND ts >= $4 AND ts <= $5 ORDER BY ts ASC`,
		q.ClusterID, q.Namespace, q.PodName, q.From, q.To,
	)
	if err != nil {
		return nil, fmt.Errorf("query pod metric series: %w", err)
	}
	defer rows.Close()
	return collectPodSamples(rows)
}

func collectNodeSamples(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]clusterops.NodeMetricSample, error) {
	var out []clusterops.NodeMetricSample
	for rows.Next() {
		s, err := scanNodeMetricSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func collectPodSamples(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]clusterops.PodMetricSample, error) {
	var out []clusterops.PodMetricSample
	for rows.Next() {
		s, err := scanPodMetricSample(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ============================================================================
// 最新值查询（DISTINCT ON）
// ============================================================================

// ListLatestNodeMetrics 返回集群下每个节点的最新一条采样。
func (r *Repository) ListLatestNodeMetrics(ctx context.Context, clusterID int64) ([]clusterops.NodeMetricSample, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (node_name) `+nodeMetricSampleSelectCols+`
		 FROM vo_cluster_node_metric_sample
		 WHERE cluster_id=$1 ORDER BY node_name, ts DESC`,
		clusterID,
	)
	if err != nil {
		return nil, fmt.Errorf("query latest node metrics: %w", err)
	}
	defer rows.Close()
	return collectNodeSamples(rows)
}

// ListLatestPodMetrics 返回集群下每个 Pod 的最新一条采样。
// nodeName 非空时按所在节点过滤（用于「按节点筛选 Pod」）。
func (r *Repository) ListLatestPodMetrics(ctx context.Context, clusterID int64, nodeName string) ([]clusterops.PodMetricSample, error) {
	if nodeName == "" {
		rows, err := r.pool.Query(ctx,
			`SELECT DISTINCT ON (namespace, pod_name) `+podMetricSampleSelectCols+`
			 FROM vo_cluster_pod_metric_sample
			 WHERE cluster_id=$1 ORDER BY namespace, pod_name, ts DESC`,
			clusterID,
		)
		if err != nil {
			return nil, fmt.Errorf("query latest pod metrics: %w", err)
		}
		defer rows.Close()
		return collectPodSamples(rows)
	}
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (namespace, pod_name) `+podMetricSampleSelectCols+`
		 FROM vo_cluster_pod_metric_sample
		 WHERE cluster_id=$1 AND node_name=$2 ORDER BY namespace, pod_name, ts DESC`,
		clusterID, nodeName,
	)
	if err != nil {
		return nil, fmt.Errorf("query latest pod metrics by node: %w", err)
	}
	defer rows.Close()
	return collectPodSamples(rows)
}

// ============================================================================
// 清理
// ============================================================================

// DeleteMetricsBefore 删除所有早于 before 的节点/Pod 采样（全集群）。
func (r *Repository) DeleteMetricsBefore(ctx context.Context, before time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM vo_cluster_node_metric_sample WHERE ts < $1`, before); err != nil {
		return fmt.Errorf("delete stale node metric samples: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM vo_cluster_pod_metric_sample WHERE ts < $1`, before); err != nil {
		return fmt.Errorf("delete stale pod metric samples: %w", err)
	}
	return nil
}

// ============================================================================
// 辅助
// ============================================================================

func nullableFloat64(v float64) *float64 {
	if v == 0 {
		return nil
	}
	return &v
}

// 编译期断言：Repository 实现 MetricsRepository。
var _ clusterops.MetricsRepository = (*Repository)(nil)
