import { useState } from 'react';
import { Input, Button, Select, Space, App, Table, Tag, Card, Row, Col, Statistic } from 'antd';
import ReactECharts from 'echarts-for-react';
import { useMutation } from '@tanstack/react-query';
import dayjs from 'dayjs';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { monitoringApi } from '@/api/monitoring';
import type { MonitoringQueryResult, MonitoringQueryRangeResult } from '@/api/monitoring';

const RANGE_PRESETS = [
  { label: '最近 15 分钟', value: 15 },
  { label: '最近 1 小时', value: 60 },
  { label: '最近 3 小时', value: 180 },
  { label: '最近 6 小时', value: 360 },
  { label: '最近 12 小时', value: 720 },
  { label: '最近 24 小时', value: 1440 },
];

const STEP_PRESETS = [
  { label: '15s', value: '15s' },
  { label: '30s', value: '30s' },
  { label: '1m', value: '1m' },
  { label: '5m', value: '5m' },
  { label: '10m', value: '10m' },
];

const COMMON_QUERIES = [
  'up',
  'node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes',
  'rate(node_cpu_seconds_total{mode="idle"}[5m])',
  'container_memory_working_set_bytes',
  'rate(container_cpu_usage_seconds_total[5m])',
  'kube_pod_status_phase',
];

const RANGE_TAG_COLORS: Record<string, string> = {
  firing: 'red',
  resolved: 'green',
  suppressed: 'default',
};

export default function MonitoringPage() {
  const { message } = App.useApp();
  const [promQL, setPromQL] = useState('up');
  const [rangeMinutes, setRangeMinutes] = useState(60);
  const [step, setStep] = useState('1m');
  const [instantResults, setInstantResults] = useState<MonitoringQueryResult[]>([]);
  const [rangeResults, setRangeResults] = useState<MonitoringQueryRangeResult[]>([]);

  const runInstant = useMutation({
    mutationFn: () => monitoringApi.query(promQL),
    onSuccess: (data) => {
      setInstantResults(data);
      message.success(`查询完成，返回 ${data.length} 个序列`);
    },
    onError: (e: Error) => message.error(`即时查询失败：${e.message}`),
  });

  const runRange = useMutation({
    mutationFn: () => {
      const end = dayjs();
      const start = end.subtract(rangeMinutes, 'minute');
      return monitoringApi.queryRange({
        query: promQL,
        start: start.toISOString(),
        end: end.toISOString(),
        step,
      });
    },
    onSuccess: (data) => {
      setRangeResults(data);
      message.success(`范围查询完成，返回 ${data.length} 个序列`);
    },
    onError: (e: Error) => message.error(`范围查询失败：${e.message}`),
  });

  const evaluateRules = useMutation({
    mutationFn: () => monitoringApi.evaluateRules(),
    onSuccess: (data) => message.success(`规则评估完成，触发 ${data.triggered} 条告警`),
    onError: (e: Error) => message.error(`规则评估失败：${e.message}`),
  });

  const chartOption = buildChartOption(rangeResults);

  return (
    <PageContainer
      title="容器监控"
      subtitle="基于 Prometheus 的指标查询与告警规则评估"
      extra={
        <Button
          loading={evaluateRules.isPending}
          onClick={() => evaluateRules.mutate()}
        >
          手动评估告警规则
        </Button>
      }
    >
      <Card style={{ marginBottom: 16 }}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <Space.Compact style={{ width: '100%' }}>
            <Input
              placeholder="输入 PromQL 表达式，如 up、rate(container_cpu_usage_seconds_total[5m])"
              value={promQL}
              onChange={(e) => setPromQL(e.target.value)}
              onPressEnter={() => runInstant.mutate()}
              style={{ flex: 1 }}
            />
            <Button type="primary" loading={runInstant.isPending} onClick={() => runInstant.mutate()}>
              即时查询
            </Button>
          </Space.Compact>
          <Space wrap>
            <span style={{ color: '#8c8c8c', fontSize: 13 }}>常用：</span>
            {COMMON_QUERIES.map((q) => (
              <Tag
                key={q}
                style={{ cursor: 'pointer' }}
                onClick={() => setPromQL(q)}
              >
                {q}
              </Tag>
            ))}
          </Space>
        </Space>
      </Card>

      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col>
          <Select
            value={rangeMinutes}
            onChange={setRangeMinutes}
            options={RANGE_PRESETS}
            style={{ width: 160 }}
          />
        </Col>
        <Col>
          <Select
            value={step}
            onChange={setStep}
            options={STEP_PRESETS}
            style={{ width: 100 }}
          />
        </Col>
        <Col>
          <Button
            type="primary"
            loading={runRange.isPending}
            onClick={() => runRange.mutate()}
          >
            范围查询
          </Button>
        </Col>
      </Row>

      {rangeResults.length > 0 ? (
        <Card title="指标趋势" style={{ marginBottom: 16 }}>
          <ReactECharts option={chartOption} style={{ height: 360 }} />
        </Card>
      ) : (
        <Card title="指标趋势" style={{ marginBottom: 16 }}>
          <EmptyState title="执行范围查询后在此显示趋势图" />
        </Card>
      )}

      <Card title={`即时查询结果（${instantResults.length}）`}>
        {instantResults.length === 0 ? (
          <EmptyState title="执行即时查询后在此显示结果" />
        ) : (
          <Table<MonitoringQueryResult>
            rowKey={(r, i) => JSON.stringify(r.metric) + i}
            size="small"
            pagination={{ pageSize: 20, showSizeChanger: true }}
            dataSource={instantResults}
            columns={[
              {
                title: '标签',
                dataIndex: 'metric',
                render: (metric: Record<string, string>) => {
                  const entries = Object.entries(metric);
                  if (entries.length === 0) return <Tag>（无标签）</Tag>;
                  return (
                    <Space wrap size={[4, 4]}>
                      {entries.map(([k, v]) => (
                        <Tag key={k}>{k}={v}</Tag>
                      ))}
                    </Space>
                  );
                },
              },
              {
                title: '时间戳',
                dataIndex: 'value',
                width: 200,
                render: (value: [number, string]) =>
                  dayjs.unix(value[0]).format('YYYY-MM-DD HH:mm:ss'),
              },
              {
                title: '值',
                dataIndex: 'value',
                width: 160,
                render: (value: [number, string]) => (
                  <span style={{ fontFamily: 'monospace', fontWeight: 600 }}>{value[1]}</span>
                ),
              },
            ]}
          />
        )}
      </Card>

      <Card title="告警事件状态说明" style={{ marginTop: 16 }}>
        <Row gutter={16}>
          <Col span={8}>
            <Statistic
              title={<Tag color={RANGE_TAG_COLORS.firing}>firing</Tag>}
              value="触发中"
              valueStyle={{ fontSize: 16 }}
            />
          </Col>
          <Col span={8}>
            <Statistic
              title={<Tag color={RANGE_TAG_COLORS.resolved}>resolved</Tag>}
              value="已恢复"
              valueStyle={{ fontSize: 16 }}
            />
          </Col>
          <Col span={8}>
            <Statistic
              title={<Tag color={RANGE_TAG_COLORS.suppressed}>suppressed</Tag>}
              value="已抑制"
              valueStyle={{ fontSize: 16 }}
            />
          </Col>
        </Row>
      </Card>
    </PageContainer>
  );
}

function buildChartOption(results: MonitoringQueryRangeResult[]) {
  if (results.length === 0) return {};
  const xSet = new Set<number>();
  results.forEach((series) => series.values.forEach(([ts]) => xSet.add(ts)));
  const xData = Array.from(xSet).sort((a, b) => a - b);
  const xLabels = xData.map((ts) => dayjs.unix(ts).format('HH:mm:ss'));

  const seriesData = results.map((series) => {
    const label = formatSeriesLabel(series.metric);
    const map = new Map(series.values.map(([ts, val]) => [ts, parseFloat(val)]));
    return {
      name: label,
      type: 'line' as const,
      showSymbol: false,
      smooth: true,
      data: xData.map((ts) => {
        const v = map.get(ts);
        return v === undefined ? null : v;
      }),
    };
  });

  return {
    tooltip: { trigger: 'axis' },
    legend: {
      type: 'scroll' as const,
      bottom: 0,
    },
    grid: { left: 60, right: 20, top: 20, bottom: 60 },
    xAxis: { type: 'category', data: xLabels, boundaryGap: false },
    yAxis: { type: 'value' },
    series: seriesData,
  };
}

function formatSeriesLabel(metric: Record<string, string>): string {
  if (!metric || Object.keys(metric).length === 0) return 'series';
  const priority = ['pod', 'container', 'instance', 'node', 'namespace', 'job'];
  for (const key of priority) {
    if (metric[key]) return metric[key];
  }
  return Object.entries(metric).map(([k, v]) => `${k}=${v}`).join(',');
}
