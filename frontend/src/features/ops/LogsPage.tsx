import { useState } from 'react';
import { Button, DatePicker, Form, Input, Space, Table, Tag } from 'antd';
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { opsApi } from '@/api/ops';
import { formatTime } from '@/utils/format';

const { RangePicker } = DatePicker;

const LEVEL_COLOR: Record<string, string> = {
  debug: 'default',
  info: 'blue',
  warn: 'warning',
  warning: 'warning',
  error: 'error',
  fatal: 'red',
};

interface LogEntry {
  timestamp?: string;
  level?: string;
  message?: string;
  pod?: string;
  namespace?: string;
  [k: string]: any;
}

export default function LogsPage() {
  const [form] = Form.useForm();
  const [params, setParams] = useState<Record<string, any>>({});
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(50);

  const queryParams = { ...params, page, size };
  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['logs', queryParams],
    queryFn: () => opsApi.searchLogs(queryParams),
  });

  const onSearch = () => {
    const v = form.getFieldsValue();
    const next: Record<string, any> = {};
    if (v.workspace_id) next.workspace_id = Number(v.workspace_id);
    if (v.cluster_id) next.cluster_id = Number(v.cluster_id);
    if (v.namespace) next.namespace = v.namespace;
    if (v.keyword) next.keyword = v.keyword;
    if (v.range && v.range.length === 2) {
      next.start_time = v.range[0].toISOString();
      next.end_time = v.range[1].toISOString();
    }
    setParams(next);
    setPage(1);
  };

  const onReset = () => {
    form.resetFields();
    setParams({});
    setPage(1);
  };

  const columns: ColumnsType<LogEntry> = [
    { title: '时间', dataIndex: 'timestamp', key: 'timestamp', width: 200, render: (t?: string) => (t ? formatTime(t) : '-') },
    {
      title: '级别',
      dataIndex: 'level',
      key: 'level',
      width: 90,
      render: (v?: string) => (v ? <Tag color={LEVEL_COLOR[v.toLowerCase()] || 'default'}>{v}</Tag> : '-'),
    },
    { title: 'Pod', dataIndex: 'pod', key: 'pod', width: 200, ellipsis: true, render: (v?: string) => v || '-' },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 140, render: (v?: string) => v || '-' },
    { title: '消息', dataIndex: 'message', key: 'message', ellipsis: true, render: (v?: string) => v || '-' },
  ];

  return (
    <PageContainer title="日志查询" subtitle="搜索集群 Pod 日志（MVP，暂不支持实时流）">
      <Form form={form} layout="inline" style={{ marginBottom: 16 }}>
        <Form.Item name="workspace_id" label="工作空间ID">
          <Input allowClear placeholder="数字" style={{ width: 140 }} />
        </Form.Item>
        <Form.Item name="cluster_id" label="集群ID">
          <Input allowClear placeholder="数字" style={{ width: 140 }} />
        </Form.Item>
        <Form.Item name="namespace" label="命名空间">
          <Input allowClear placeholder="如：default" style={{ width: 160 }} />
        </Form.Item>
        <Form.Item name="keyword" label="关键词">
          <Input allowClear placeholder="如：error" style={{ width: 180 }} />
        </Form.Item>
        <Form.Item name="range" label="时间范围">
          <RangePicker showTime />
        </Form.Item>
        <Form.Item>
          <Space>
            <Button type="primary" icon={<SearchOutlined />} loading={isFetching} onClick={onSearch}>
              查询
            </Button>
            <Button icon={<ReloadOutlined />} onClick={onReset}>
              重置
            </Button>
          </Space>
        </Form.Item>
      </Form>

      <Table
        rowKey={(r, i) => `${r?.timestamp}-${i}`}
        loading={isLoading}
        columns={columns}
        dataSource={(data?.items || []) as LogEntry[]}
        locale={{ emptyText: <EmptyState title="暂无日志" description="输入查询条件后搜索" /> }}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total || 0,
          showSizeChanger: true,
          pageSizeOptions: ['20', '50', '100', '200'],
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
      />
    </PageContainer>
  );
}
