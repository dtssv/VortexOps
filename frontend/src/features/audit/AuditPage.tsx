import { useState } from 'react';
import { Button, DatePicker, Drawer, Form, Input, Space, Table, Tag, Typography } from 'antd';
import { SearchOutlined, ReloadOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { useQuery } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { auditApi } from '@/api/audit';
import type { AuditLog } from '@/types';
import { formatTime, formatDuration } from '@/utils/format';

const { RangePicker } = DatePicker;

const METHOD_COLOR: Record<string, string> = {
  GET: 'blue',
  POST: 'green',
  PUT: 'orange',
  DELETE: 'red',
  PATCH: 'purple',
};

const ACTION_COLOR: Record<string, string> = {
  login: 'blue',
  logout: 'default',
  create: 'green',
  update: 'orange',
  delete: 'red',
  scale: 'purple',
  restart: 'cyan',
  rollback: 'volcano',
  sync: 'geekblue',
  refresh: 'blue',
  view: 'default',
};

function statusColor(code?: number): string {
  if (code == null) return 'default';
  if (code < 300) return 'success';
  if (code < 400) return 'blue';
  if (code < 500) return 'warning';
  return 'error';
}

export default function AuditPage() {
  const [form] = Form.useForm();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [filters, setFilters] = useState<Record<string, any>>({});
  const [detail, setDetail] = useState<AuditLog | null>(null);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['audit-logs', { ...filters, page, size }],
    queryFn: () => auditApi.list({ ...filters, page, size }),
  });

  const onSearch = () => {
    const v = form.getFieldsValue();
    const next: Record<string, any> = {};
    if (v.resource_type) next.resource_type = v.resource_type;
    if (v.action) next.action = v.action;
    if (v.user_id) next.user_id = Number(v.user_id);
    if (v.range && v.range.length === 2) {
      next.start_time = v.range[0].toISOString();
      next.end_time = v.range[1].toISOString();
    }
    setFilters(next);
    setPage(1);
  };

  const onReset = () => {
    form.resetFields();
    setFilters({});
    setPage(1);
  };

  const columns: ColumnsType<AuditLog> = [
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    { title: '用户', dataIndex: 'user_name', key: 'user_name', width: 120, render: (v?: string) => v || '-' },
    {
      title: '操作',
      key: 'action',
      width: 120,
      render: (_, r) => {
        const color = ACTION_COLOR[r.action] || 'default';
        return <Tag color={color}>{r.action}</Tag>;
      },
    },
    {
      title: '资源类型',
      key: 'resource',
      width: 180,
      render: (_, r) => (
        <Space size={4} wrap>
          <Tag>{r.resource_type}</Tag>
          {r.resource_name && <Typography.Text type="secondary" ellipsis style={{ maxWidth: 120 }}>{r.resource_name}</Typography.Text>}
        </Space>
      ),
    },
    {
      title: 'HTTP 请求',
      key: 'request',
      width: 220,
      ellipsis: true,
      render: (_, r) =>
        r.method || r.path ? (
          <Space size={4}>
            {r.method && <Tag color={METHOD_COLOR[r.method] || 'default'}>{r.method}</Tag>}
            {r.path && <Typography.Text style={{ fontSize: 12 }}>{r.path}</Typography.Text>}
          </Space>
        ) : (
          '-'
        ),
    },
    {
      title: '状态码',
      dataIndex: 'status_code',
      key: 'status_code',
      width: 90,
      render: (v?: number) => (v != null ? <Tag color={statusColor(v)}>{v}</Tag> : '-'),
    },
    { title: '耗时', dataIndex: 'duration_ms', key: 'duration_ms', width: 90, render: (v?: number) => formatDuration(v) },
  ];

  return (
    <PageContainer title="审计日志" subtitle="查看平台操作审计记录">
      <Form form={form} layout="inline" style={{ marginBottom: 16 }}>
        <Form.Item name="resource_type" label="资源类型">
          <Input allowClear placeholder="如：cluster" />
        </Form.Item>
        <Form.Item name="action" label="操作">
          <Input allowClear placeholder="如：create" />
        </Form.Item>
        <Form.Item name="user_id" label="用户ID">
          <Input allowClear placeholder="数字" />
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
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        onRow={(record) => ({ onClick: () => setDetail(record), style: { cursor: 'pointer' } })}
        locale={{ emptyText: <EmptyState title="暂无审计记录" /> }}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total || 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
      />

      <Drawer
        title="审计详情"
        open={!!detail}
        onClose={() => setDetail(null)}
        width={640}
        destroyOnHidden
      >
        {detail && (
          <div>
            <Typography.Paragraph>
              <strong>时间：</strong>
              {formatTime(detail.created_at)}
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>用户：</strong>
              {detail.user_name || '-'}（ID: {detail.user_id ?? '-'}）
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>操作：</strong>
              {detail.action}
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>资源：</strong>
              {detail.resource_type}
              {detail.resource_name ? ` / ${detail.resource_name}` : ''}
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>请求：</strong>
              {detail.method} {detail.path}
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>状态码：</strong>
              {detail.status_code ?? '-'} · <strong>耗时：</strong>
              {formatDuration(detail.duration_ms)}
            </Typography.Paragraph>
            <Typography.Paragraph>
              <strong>客户端：</strong>
              {detail.client_ip || '-'} · {detail.user_agent || '-'}
            </Typography.Paragraph>

            <Typography.Title level={5}>请求体</Typography.Title>
            <pre
              style={{
                background: '#f5f5f5',
                padding: 12,
                borderRadius: 4,
                maxHeight: 240,
                overflow: 'auto',
                fontSize: 12,
              }}
            >
              {detail.request_body ? JSON.stringify(detail.request_body, null, 2) : '-'}
            </pre>

            <Typography.Title level={5}>响应摘要</Typography.Title>
            <pre
              style={{
                background: '#f5f5f5',
                padding: 12,
                borderRadius: 4,
                maxHeight: 240,
                overflow: 'auto',
                fontSize: 12,
              }}
            >
              {detail.response_summary ? JSON.stringify(detail.response_summary, null, 2) : '-'}
            </pre>

            {detail.error_message && (
              <>
                <Typography.Title level={5}>错误信息</Typography.Title>
                <Typography.Paragraph type="danger">{detail.error_message}</Typography.Paragraph>
              </>
            )}
          </div>
        )}
      </Drawer>
    </PageContainer>
  );
}
