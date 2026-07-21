import { useMemo, useState } from 'react';
import {
  Button,
  Card,
  Col,
  Progress,
  Row,
  Segmented,
  Select,
  Space,
  Statistic,
  Table,
  Tabs,
  Tag,
  App,
} from 'antd';
import { SyncOutlined } from '@ant-design/icons';
import ReactECharts from 'echarts-for-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { clusterOpsApi } from '@/api/clusterOps';
import { EmptyState } from '@/components/EmptyState';
import type { Cluster, MetricRange, NodeMetricSample, PodMetricSample } from '@/types';
import {
  formatBytes,
  formatCpuM,
  formatLoad,
  formatPct,
  formatRate,
  formatRelative,
} from '@/utils/format';
import { ClusterSelector } from './ClusterSelector';
import { buildTrendOption, toPoints } from './metricCharts';

interface NodeMonitorTabProps {
  clusters: Cluster[];
  clusterId?: number;
  onClusterChange: (id: number | undefined) => void;
}

const RANGE_OPTIONS: Array<{ label: string; value: MetricRange }> = [
  { label: '1 小时', value: '1h' },
  { label: '6 小时', value: '6h' },
  { label: '24 小时', value: '24h' },
  { label: '7 天', value: '7d' },
];

// 进度条状态：>=90 红、>=75 黄、<75 绿
function progressStatus(pct: number): 'success' | 'normal' | 'exception' {
  if (pct >= 90) return 'exception';
  if (pct >= 75) return 'normal';
  return 'success';
}

export function NodeMonitorTab({ clusters, clusterId, onClusterChange }: NodeMonitorTabProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [range, setRange] = useState<MetricRange>('1h');
  const [innerTab, setInnerTab] = useState('overview');

  // 节点最新采样
  const { data: nodeMetrics = [], isLoading: nodesLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'node-metrics', 'latest'],
    queryFn: () => clusterOpsApi.listLatestNodeMetrics(clusterId!),
    enabled: !!clusterId,
  });

  // Pod 最新采样
  const { data: podMetrics = [], isLoading: podsLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'pod-metrics', 'latest'],
    queryFn: () => clusterOpsApi.listLatestPodMetrics(clusterId!),
    enabled: !!clusterId,
  });

  const collectMutation = useMutation({
    mutationFn: () => clusterOpsApi.collectNodeMetrics(clusterId!),
    onSuccess: () => {
      message.success('指标采集已触发');
      queryClient.invalidateQueries({ queryKey: ['cluster-ops', clusterId, 'node-metrics'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-ops', clusterId, 'pod-metrics'] });
    },
    onError: (e: any) => message.error(e?.message || '采集失败'),
  });

  // 概览统计
  const stats = useMemo(() => {
    const nodeCount = nodeMetrics.length;
    const avgCpu = nodeCount ? nodeMetrics.reduce((s, n) => s + n.cpu_usage_pct, 0) / nodeCount : 0;
    const avgMem = nodeCount ? nodeMetrics.reduce((s, n) => s + n.mem_usage_pct, 0) / nodeCount : 0;
    const abnormalPods = podMetrics.filter(
      (p) => p.restart_count >= 5 || (p.phase && p.phase !== 'Running' && p.phase !== 'Succeeded'),
    ).length;
    return { nodeCount, avgCpu, avgMem, abnormalPods };
  }, [nodeMetrics, podMetrics]);

  const toolbar = (
    <Space wrap style={{ marginBottom: 12 }}>
      <ClusterSelector clusters={clusters} value={clusterId} onChange={onClusterChange} />
      <Segmented
        options={RANGE_OPTIONS}
        value={range}
        onChange={(v) => setRange(v as MetricRange)}
      />
      <Button
        icon={<SyncOutlined />}
        loading={collectMutation.isPending}
        disabled={!clusterId}
        onClick={() => collectMutation.mutate()}
      >
        立即采集
      </Button>
      {nodeMetrics[0]?.ts && (
        <span style={{ color: '#8c8c8c', fontSize: 13 }}>
          最近采样：{formatRelative(nodeMetrics[0].ts)}
        </span>
      )}
    </Space>
  );

  if (!clusterId) {
    return (
      <Space direction="vertical" style={{ width: '100%' }} size="middle">
        {toolbar}
        <EmptyState title="请先选择集群" />
      </Space>
    );
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      {toolbar}
      <Tabs
        activeKey={innerTab}
        onChange={setInnerTab}
        items={[
          {
            key: 'overview',
            label: '概览',
            children: (
              <OverviewTab clusterId={clusterId} range={range} stats={stats} nodeMetrics={nodeMetrics} />
            ),
          },
          {
            key: 'nodes',
            label: '节点详情',
            children: (
              <NodesTab clusterId={clusterId} range={range} nodeMetrics={nodeMetrics} loading={nodesLoading} />
            ),
          },
          {
            key: 'pods',
            label: 'Pod 监控',
            children: (
              <PodsTab clusterId={clusterId} range={range} podMetrics={podMetrics} loading={podsLoading} />
            ),
          },
          {
            key: 'disk',
            label: '磁盘存储',
            children: <DiskTab clusterId={clusterId} range={range} nodeMetrics={nodeMetrics} loading={nodesLoading} />,
          },
          {
            key: 'network',
            label: '网络流量',
            children: <NetworkTab clusterId={clusterId} range={range} nodeMetrics={nodeMetrics} loading={nodesLoading} />,
          },
        ]}
      />
    </Space>
  );
}

// ============================================================================
// Tab 1: 概览
// ============================================================================

interface OverviewTabProps {
  clusterId: number;
  range: MetricRange;
  stats: { nodeCount: number; avgCpu: number; avgMem: number; abnormalPods: number };
  nodeMetrics: NodeMetricSample[];
}

function OverviewTab({ clusterId, range, stats, nodeMetrics }: OverviewTabProps) {
  // 集群级趋势：每个节点一条 series，叠加为面积图。
  const { data: seriesByNode = [] } = useMultiNodeSeries(
    clusterId,
    nodeMetrics.map((n) => n.node_name),
    range,
  );

  const cpuSeries = seriesByNode.map((s) => ({
    name: s.nodeName,
    data: toPoints(s.samples, (x) => x.cpu_usage_pct),
  }));
  const memSeries = seriesByNode.map((s) => ({
    name: s.nodeName,
    data: toPoints(s.samples, (x) => x.mem_usage_pct),
  }));
  // 集群总吞吐：各节点 RX/TX 速率求和（每时间点跨节点相加）
  const totalRx = sumAcrossNodes(seriesByNode, (x) => x.net_rx_bytes_per_sec);
  const totalTx = sumAcrossNodes(seriesByNode, (x) => x.net_tx_bytes_per_sec);
  const netSeries = [
    { name: '接收 RX', data: totalRx },
    { name: '发送 TX', data: totalTx },
  ];
  const diskSeries = seriesByNode.map((s) => ({
    name: s.nodeName,
    data: toPoints(s.samples, (x) => x.fs_usage_pct),
  }));

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Row gutter={16}>
        <Col xs={12} sm={6}>
          <Card size="small">
            <Statistic title="节点总数" value={stats.nodeCount} />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small">
            <Statistic
              title="平均 CPU 繁忙度"
              value={stats.avgCpu}
              precision={1}
              suffix="%"
              valueStyle={{ color: stats.avgCpu >= 80 ? '#cf1322' : undefined }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small">
            <Statistic
              title="平均内存使用率"
              value={stats.avgMem}
              precision={1}
              suffix="%"
              valueStyle={{ color: stats.avgMem >= 80 ? '#cf1322' : undefined }}
            />
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small">
            <Statistic
              title="异常 Pod"
              value={stats.abnormalPods}
              valueStyle={{ color: stats.abnormalPods ? '#cf1322' : undefined }}
            />
          </Card>
        </Col>
      </Row>

      <Card title="集群 CPU 使用率（按节点叠加）" size="small">
        <ReactECharts option={buildTrendOption(cpuSeries, '%', { stack: true, area: true })} style={{ height: 280 }} notMerge />
      </Card>
      <Card title="集群内存使用率（按节点叠加）" size="small">
        <ReactECharts option={buildTrendOption(memSeries, '%', { stack: true, area: true })} style={{ height: 280 }} notMerge />
      </Card>
      <Card title="集群网络吞吐" size="small">
        <ReactECharts option={buildTrendOption(netSeries, ' B/s')} style={{ height: 280 }} notMerge />
      </Card>
      <Card title="集群磁盘使用率（按节点）" size="small">
        <ReactECharts option={buildTrendOption(diskSeries, '%')} style={{ height: 280 }} notMerge />
      </Card>
    </Space>
  );
}

// ============================================================================
// Tab 2: 节点详情
// ============================================================================

interface NodesTabProps {
  clusterId: number;
  range: MetricRange;
  nodeMetrics: NodeMetricSample[];
  loading: boolean;
}

function NodesTab({ clusterId, range, nodeMetrics, loading }: NodesTabProps) {
  const [expandedNode, setExpandedNode] = useState<string | null>(null);

  const columns: ColumnsType<NodeMetricSample> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 160, ellipsis: true },
    {
      title: 'CPU 使用率',
      key: 'cpu',
      width: 180,
      render: (_, n) => (
        <Progress
          percent={Math.round(n.cpu_usage_pct)}
          size="small"
          status={progressStatus(n.cpu_usage_pct)}
          format={() => `${formatCpuM(n.cpu_usage_m)}/${formatCpuM(n.cpu_allocatable_m)}`}
        />
      ),
    },
    {
      title: '内存使用率',
      key: 'mem',
      width: 180,
      render: (_, n) => (
        <Progress
          percent={Math.round(n.mem_usage_pct)}
          size="small"
          status={progressStatus(n.mem_usage_pct)}
          format={() => `${formatBytes(n.mem_working_set_bytes)}/${formatBytes(n.mem_allocatable_bytes)}`}
        />
      ),
    },
    {
      title: '磁盘使用率',
      key: 'fs',
      width: 160,
      render: (_, n) => (
        <Progress
          percent={Math.round(n.fs_usage_pct)}
          size="small"
          status={progressStatus(n.fs_usage_pct)}
          format={() => `${formatBytes(n.fs_used_bytes)}/${formatBytes(n.fs_capacity_bytes)}`}
        />
      ),
    },
    {
      title: 'inode',
      key: 'inode',
      width: 90,
      render: (_, n) => formatPct(n.fs_inodes_pct, 1),
    },
    {
      title: '网络 RX/TX',
      key: 'net',
      width: 160,
      render: (_, n) => (
        <span style={{ fontSize: 12 }}>
          ↓{formatRate(n.net_rx_bytes_per_sec)} ↑{formatRate(n.net_tx_bytes_per_sec)}
        </span>
      ),
    },
    {
      title: 'load1',
      key: 'load1',
      width: 70,
      render: (_, n) => formatLoad(n.load1),
    },
    {
      title: '最近采样',
      dataIndex: 'ts',
      key: 'ts',
      width: 140,
      render: (t: string) => formatRelative(t),
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Table<NodeMetricSample>
        rowKey="node_name"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={nodeMetrics}
        pagination={false}
        scroll={{ x: 1100 }}
        locale={{ emptyText: <EmptyState title="暂无节点指标" description="等待 syncer 采样或点击「立即采集」" /> }}
        expandable={{
          expandedRowKeys: expandedNode ? [expandedNode] : [],
          onExpand: (open, record) => setExpandedNode(open ? record.node_name : null),
          expandedRowRender: (record) => (
            <NodeDetailCharts clusterId={clusterId} nodeName={record.node_name} range={range} />
          ),
        }}
      />
    </Space>
  );
}

function NodeDetailCharts({
  clusterId,
  nodeName,
  range,
}: {
  clusterId: number;
  nodeName: string;
  range: MetricRange;
}) {
  const { data: series = [], isLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'node-metrics', 'series', nodeName, range],
    queryFn: () => clusterOpsApi.getNodeMetricSeries(clusterId, nodeName, range),
    enabled: !!nodeName,
  });

  if (isLoading) return <div style={{ padding: 12, color: '#8c8c8c' }}>加载趋势数据...</div>;
  if (!series.length) return <EmptyState title="该时间范围内无采样数据" />;

  const cpuSeries = [
    { name: 'CPU 使用率', data: toPoints(series, (x) => x.cpu_usage_pct) },
    { name: 'load1', data: toPoints(series, (x) => x.load1) },
    { name: 'load5', data: toPoints(series, (x) => x.load5) },
    { name: 'load15', data: toPoints(series, (x) => x.load15) },
  ];
  const memSeries = [
    { name: 'WorkingSet', data: toPoints(series, (x) => x.mem_working_set_bytes) },
    { name: 'Usage', data: toPoints(series, (x) => x.mem_usage_bytes) },
    { name: 'Available', data: toPoints(series, (x) => x.mem_available_bytes) },
  ];
  const diskSeries = [
    { name: '磁盘使用率', data: toPoints(series, (x) => x.fs_usage_pct) },
    { name: 'inode 使用率', data: toPoints(series, (x) => x.fs_inodes_pct) },
    { name: '可用字节(GB)', data: toPoints(series, (x) => x.fs_available_bytes / 1e9) },
  ];
  const netSeries = [
    { name: 'RX 速率', data: toPoints(series, (x) => x.net_rx_bytes_per_sec) },
    { name: 'TX 速率', data: toPoints(series, (x) => x.net_tx_bytes_per_sec) },
    { name: 'RX 错误', data: toPoints(series, (x) => x.net_rx_errors) },
    { name: 'TX 错误', data: toPoints(series, (x) => x.net_tx_errors) },
    { name: 'RX 丢包', data: toPoints(series, (x) => x.net_rx_dropped) },
    { name: 'TX 丢包', data: toPoints(series, (x) => x.net_tx_dropped) },
  ];

  return (
    <Row gutter={[12, 12]}>
      <Col span={12}>
        <Card size="small" title="CPU 与负载">
          <ReactECharts option={buildTrendOption(cpuSeries, '')} style={{ height: 240 }} notMerge />
        </Card>
      </Col>
      <Col span={12}>
        <Card size="small" title="内存（字节）">
          <ReactECharts option={buildTrendOption(memSeries, ' B')} style={{ height: 240 }} notMerge />
        </Card>
      </Col>
      <Col span={12}>
        <Card size="small" title="磁盘与 inode">
          <ReactECharts option={buildTrendOption(diskSeries, '')} style={{ height: 240 }} notMerge />
        </Card>
      </Col>
      <Col span={12}>
        <Card size="small" title="网络（速率/错误/丢包）">
          <ReactECharts option={buildTrendOption(netSeries, '')} style={{ height: 240 }} notMerge />
        </Card>
      </Col>
    </Row>
  );
}

// ============================================================================
// Tab 3: Pod 监控
// ============================================================================

interface PodsTabProps {
  clusterId: number;
  range: MetricRange;
  podMetrics: PodMetricSample[];
  loading: boolean;
}

function PodsTab({ clusterId, range, podMetrics, loading }: PodsTabProps) {
  const [nodeFilter, setNodeFilter] = useState<string | undefined>(undefined);
  const [expandedPod, setExpandedPod] = useState<string | null>(null);

  const nodeOptions = useMemo(() => {
    const names = Array.from(new Set(podMetrics.map((p) => p.node_name)));
    return names.map((n) => ({ label: n, value: n }));
  }, [podMetrics]);

  const filtered = nodeFilter ? podMetrics.filter((p) => p.node_name === nodeFilter) : podMetrics;

  const columns: ColumnsType<PodMetricSample> = [
    { title: 'Pod', dataIndex: 'pod_name', key: 'pod_name', width: 200, ellipsis: true },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 120 },
    { title: '所在节点', dataIndex: 'node_name', key: 'node_name', width: 140, ellipsis: true },
    {
      title: 'CPU',
      key: 'cpu',
      width: 90,
      render: (_, p) => formatCpuM(p.cpu_usage_m),
    },
    {
      title: '内存(WS)',
      key: 'mem',
      width: 100,
      render: (_, p) => formatBytes(p.mem_working_set_bytes),
    },
    {
      title: '网络 RX/TX',
      key: 'net',
      width: 150,
      render: (_, p) => (
        <span style={{ fontSize: 12 }}>
          ↓{formatRate(p.net_rx_bytes_per_sec)} ↑{formatRate(p.net_tx_bytes_per_sec)}
        </span>
      ),
    },
    {
      title: '重启',
      dataIndex: 'restart_count',
      key: 'restart_count',
      width: 70,
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : v),
    },
    {
      title: '阶段',
      dataIndex: 'phase',
      key: 'phase',
      width: 90,
      render: (v: string) => {
        const color = v === 'Running' ? 'green' : v === 'Succeeded' ? 'blue' : 'orange';
        return <Tag color={color}>{v || '-'}</Tag>;
      },
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Space>
        <span>按节点筛选：</span>
        <Select
          allowClear
          placeholder="全部节点"
          style={{ width: 240 }}
          options={nodeOptions}
          value={nodeFilter}
          onChange={setNodeFilter}
          showSearch
          optionFilterProp="label"
        />
      </Space>
      <Table<PodMetricSample>
        rowKey={(r) => `${r.namespace}/${r.pod_name}`}
        size="small"
        loading={loading}
        columns={columns}
        dataSource={filtered}
        pagination={{ pageSize: 50, showSizeChanger: true, showTotal: (t) => `共 ${t} 条` }}
        scroll={{ x: 1100 }}
        locale={{ emptyText: <EmptyState title="暂无 Pod 指标" /> }}
        expandable={{
          expandedRowKeys: expandedPod ? [expandedPod] : [],
          onExpand: (open, record) =>
            setExpandedPod(open ? `${record.namespace}/${record.pod_name}` : null),
          expandedRowRender: (record) => (
            <PodDetailCharts clusterId={clusterId} namespace={record.namespace} podName={record.pod_name} range={range} />
          ),
        }}
      />
    </Space>
  );
}

function PodDetailCharts({
  clusterId,
  namespace,
  podName,
  range,
}: {
  clusterId: number;
  namespace: string;
  podName: string;
  range: MetricRange;
}) {
  const { data: series = [], isLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'pod-metrics', 'series', namespace, podName, range],
    queryFn: () => clusterOpsApi.getPodMetricSeries(clusterId, namespace, podName, range),
  });
  if (isLoading) return <div style={{ padding: 12, color: '#8c8c8c' }}>加载趋势数据...</div>;
  if (!series.length) return <EmptyState title="该时间范围内无采样数据" />;

  const cpuSeries = [{ name: 'CPU', data: toPoints(series, (x) => x.cpu_usage_m / 1000) }];
  const memSeries = [
    { name: 'WorkingSet', data: toPoints(series, (x) => x.mem_working_set_bytes) },
    { name: 'Usage', data: toPoints(series, (x) => x.mem_usage_bytes) },
  ];
  const netSeries = [
    { name: 'RX 速率', data: toPoints(series, (x) => x.net_rx_bytes_per_sec) },
    { name: 'TX 速率', data: toPoints(series, (x) => x.net_tx_bytes_per_sec) },
  ];
  return (
    <Row gutter={[12, 12]}>
      <Col span={8}>
        <Card size="small" title="CPU（核）">
          <ReactECharts option={buildTrendOption(cpuSeries, ' 核')} style={{ height: 220 }} notMerge />
        </Card>
      </Col>
      <Col span={8}>
        <Card size="small" title="内存（字节）">
          <ReactECharts option={buildTrendOption(memSeries, ' B')} style={{ height: 220 }} notMerge />
        </Card>
      </Col>
      <Col span={8}>
        <Card size="small" title="网络（速率）">
          <ReactECharts option={buildTrendOption(netSeries, ' B/s')} style={{ height: 220 }} notMerge />
        </Card>
      </Col>
    </Row>
  );
}

// ============================================================================
// Tab 4: 磁盘存储
// ============================================================================

function DiskTab({
  clusterId,
  range,
  nodeMetrics,
  loading,
}: {
  clusterId: number;
  range: MetricRange;
  nodeMetrics: NodeMetricSample[];
  loading: boolean;
}) {
  const { data: seriesByNode = [] } = useMultiNodeSeries(
    clusterId,
    nodeMetrics.map((n) => n.node_name),
    range,
  );

  const columns: ColumnsType<NodeMetricSample> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 160, ellipsis: true },
    {
      title: '容量',
      key: 'cap',
      width: 110,
      render: (_, n) => formatBytes(n.fs_capacity_bytes),
    },
    {
      title: '已用',
      key: 'used',
      width: 110,
      render: (_, n) => formatBytes(n.fs_used_bytes),
    },
    {
      title: '可用',
      key: 'avail',
      width: 110,
      render: (_, n) => formatBytes(n.fs_available_bytes),
    },
    {
      title: '使用率',
      key: 'pct',
      width: 160,
      render: (_, n) => (
        <Progress
          percent={Math.round(n.fs_usage_pct)}
          size="small"
          status={progressStatus(n.fs_usage_pct)}
          format={(p) => `${p}%`}
        />
      ),
    },
    {
      title: 'inode 使用率',
      key: 'inode',
      width: 140,
      render: (_, n) => (
        <Progress
          percent={Math.round(n.fs_inodes_pct)}
          size="small"
          status={progressStatus(n.fs_inodes_pct)}
          format={(p) => `${p}%`}
        />
      ),
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Card title="磁盘使用率趋势（按节点）" size="small">
        <ReactECharts
          option={buildTrendOption(
            seriesByNode.map((s) => ({
              name: s.nodeName,
              data: toPoints(s.samples, (x) => x.fs_usage_pct),
            })),
            '%',
          )}
          style={{ height: 300 }}
          notMerge
        />
      </Card>
      <Card title="inode 使用率趋势（按节点）" size="small">
        <ReactECharts
          option={buildTrendOption(
            seriesByNode.map((s) => ({
              name: s.nodeName,
              data: toPoints(s.samples, (x) => x.fs_inodes_pct),
            })),
            '%',
          )}
          style={{ height: 300 }}
          notMerge
        />
      </Card>
      <Table<NodeMetricSample>
        rowKey="node_name"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={nodeMetrics}
        pagination={false}
        scroll={{ x: 900 }}
        locale={{ emptyText: <EmptyState title="暂无磁盘指标" /> }}
      />
    </Space>
  );
}

// ============================================================================
// Tab 5: 网络流量
// ============================================================================

function NetworkTab({
  clusterId,
  range,
  nodeMetrics,
  loading,
}: {
  clusterId: number;
  range: MetricRange;
  nodeMetrics: NodeMetricSample[];
  loading: boolean;
}) {
  const { data: seriesByNode = [] } = useMultiNodeSeries(
    clusterId,
    nodeMetrics.map((n) => n.node_name),
    range,
  );

  const rxSeries = seriesByNode.map((s) => ({
    name: s.nodeName,
    data: toPoints(s.samples, (x) => x.net_rx_bytes_per_sec),
  }));
  const txSeries = seriesByNode.map((s) => ({
    name: s.nodeName,
    data: toPoints(s.samples, (x) => x.net_tx_bytes_per_sec),
  }));
  // 错误/丢包：各节点累计求和
  const errSeries = [
    { name: 'RX 错误', data: sumAcrossNodes(seriesByNode, (x) => x.net_rx_errors) },
    { name: 'TX 错误', data: sumAcrossNodes(seriesByNode, (x) => x.net_tx_errors) },
    { name: 'RX 丢包', data: sumAcrossNodes(seriesByNode, (x) => x.net_rx_dropped) },
    { name: 'TX 丢包', data: sumAcrossNodes(seriesByNode, (x) => x.net_tx_dropped) },
  ];

  const columns: ColumnsType<NodeMetricSample> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 160, ellipsis: true },
    {
      title: 'RX 速率',
      key: 'rx',
      width: 110,
      render: (_, n) => formatRate(n.net_rx_bytes_per_sec),
    },
    {
      title: 'TX 速率',
      key: 'tx',
      width: 110,
      render: (_, n) => formatRate(n.net_tx_bytes_per_sec),
    },
    {
      title: '累计 RX',
      key: 'rx_total',
      width: 110,
      render: (_, n) => formatBytes(n.net_rx_bytes),
    },
    {
      title: '累计 TX',
      key: 'tx_total',
      width: 110,
      render: (_, n) => formatBytes(n.net_tx_bytes),
    },
    {
      title: 'RX 错误',
      dataIndex: 'net_rx_errors',
      key: 'rx_err',
      width: 90,
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : v),
    },
    {
      title: 'TX 错误',
      dataIndex: 'net_tx_errors',
      key: 'tx_err',
      width: 90,
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : v),
    },
    {
      title: '丢包(RX/TX)',
      key: 'drop',
      width: 110,
      render: (_, n) => (
        <span style={{ fontSize: 12 }}>
          {n.net_rx_dropped}/{n.net_tx_dropped}
        </span>
      ),
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Card title="节点接收速率 RX（按节点）" size="small">
        <ReactECharts option={buildTrendOption(rxSeries, ' B/s')} style={{ height: 300 }} notMerge />
      </Card>
      <Card title="节点发送速率 TX（按节点）" size="small">
        <ReactECharts option={buildTrendOption(txSeries, ' B/s')} style={{ height: 300 }} notMerge />
      </Card>
      <Card title="错误包与丢包（集群总和）" size="small">
        <ReactECharts option={buildTrendOption(errSeries, '')} style={{ height: 280 }} notMerge />
      </Card>
      <Table<NodeMetricSample>
        rowKey="node_name"
        size="small"
        loading={loading}
        columns={columns}
        dataSource={nodeMetrics}
        pagination={false}
        scroll={{ x: 1100 }}
        locale={{ emptyText: <EmptyState title="暂无网络指标" /> }}
      />
    </Space>
  );
}

// ============================================================================
// Hook: 批量拉取多节点时序（概览/磁盘/网络共用）
// ============================================================================

interface NodeSeries {
  nodeName: string;
  samples: NodeMetricSample[];
}

// useMultiNodeSeries 串行拉取每个节点的时序（节点数通常 ≤ 几十，串行可接受；
// 若需并发可用 useQueries，此处保持简单）。
function useMultiNodeSeries(clusterId: number, nodeNames: string[], range: MetricRange) {
  // 用一个聚合 queryKey 缓存所有节点的结果。
  const { data, isLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'node-metrics', 'series-multi', nodeNames.join(','), range],
    queryFn: async () => {
      const results: NodeSeries[] = [];
      for (const name of nodeNames) {
        const samples = await clusterOpsApi.getNodeMetricSeries(clusterId, name, range);
        results.push({ nodeName: name, samples });
      }
      return results;
    },
    enabled: nodeNames.length > 0,
  });
  return { data: data ?? [], isLoading };
}

// sumAcrossNodes 把多个节点时序按时间戳对齐求和，返回单一 series。
// 用于「集群总吞吐」「集群总错误」等聚合视图。
function sumAcrossNodes(
  seriesByNode: NodeSeries[],
  pick: (s: NodeMetricSample) => number,
): [number, number][] {
  const map = new Map<number, number>();
  for (const ns of seriesByNode) {
    for (const s of ns.samples) {
      const t = new Date(s.ts).getTime();
      map.set(t, (map.get(t) ?? 0) + pick(s));
    }
  }
  return Array.from(map.entries()).sort((a, b) => a[0] - b[0]);
}
