import { useState } from 'react';
import { Button, Segmented, Space, Table, Tag, App } from 'antd';
import { CheckOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { notificationApi } from '@/api/audit';
import type { Notification } from '@/types';
import { formatTime, formatRelative, truncate } from '@/utils/format';

const LEVEL_COLOR: Record<string, string> = {
  info: 'blue',
  success: 'success',
  warning: 'warning',
  error: 'error',
  critical: 'red',
};

export default function NotificationsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [readFilter, setReadFilter] = useState<'all' | 'unread' | 'read'>('all');

  const params: { page: number; size: number; read?: boolean } = { page, size };
  if (readFilter === 'unread') params.read = false;
  if (readFilter === 'read') params.read = true;

  const { data, isLoading } = useQuery({
    queryKey: ['notifications', params],
    queryFn: () => notificationApi.list(params),
  });

  const markAllMutation = useMutation({
    mutationFn: () => notificationApi.markAllRead(),
    onSuccess: () => {
      message.success('已全部标记为已读');
      queryClient.invalidateQueries({ queryKey: ['notifications'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const columns: ColumnsType<Notification> = [
    {
      title: '标题',
      dataIndex: 'title',
      key: 'title',
      render: (v: string, record) => (
        <Space>
          <span style={{ fontWeight: record.read ? 'normal' : 600 }}>{v}</span>
          {!record.read && <Tag color="processing">未读</Tag>}
        </Space>
      ),
    },
    {
      title: '级别',
      dataIndex: 'level',
      key: 'level',
      width: 90,
      render: (v: string) => <Tag color={LEVEL_COLOR[v] || 'default'}>{v}</Tag>,
    },
    {
      title: '内容',
      dataIndex: 'content',
      key: 'content',
      ellipsis: true,
      render: (v?: string) => truncate(v, 80),
    },
    {
      title: '状态',
      dataIndex: 'read',
      key: 'read',
      width: 80,
      render: (v: boolean) => (v ? <Tag>已读</Tag> : <Tag color="processing">未读</Tag>),
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 200, render: (t: string) => <span title={formatTime(t)}>{formatRelative(t)}</span> },
  ];

  return (
    <PageContainer
      title="通知中心"
      subtitle="查看你的站内通知"
      extra={
        <Button
          type="primary"
          icon={<CheckOutlined />}
          loading={markAllMutation.isPending}
          onClick={() => markAllMutation.mutate()}
        >
          全部已读
        </Button>
      }
    >
      <Space style={{ marginBottom: 16 }}>
        <Segmented
          value={readFilter}
          onChange={(v) => {
            setReadFilter(v as typeof readFilter);
            setPage(1);
          }}
          options={[
            { label: '全部', value: 'all' },
            { label: '未读', value: 'unread' },
            { label: '已读', value: 'read' },
          ]}
        />
      </Space>
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无通知" /> }}
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
    </PageContainer>
  );
}
