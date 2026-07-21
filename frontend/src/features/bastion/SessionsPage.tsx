import { useState } from 'react';
import { App, Button, Card, Input, Modal, Select, Space, Table, Tag } from 'antd';
import { PlayCircleOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { bastionApi, type BastionSession } from '@/api/bastion';
import { useUIStore } from '@/stores/uiStore';
import { formatTime } from '@/utils/format';

const STATUS_COLOR: Record<string, string> = {
  active: 'processing',
  closed: 'default',
};

function formatDuration(ms?: number) {
  if (!ms) return '-';
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m${sec % 60}s`;
  const hr = Math.floor(min / 60);
  return `${hr}h${min % 60}m`;
}

export function BastionSessionsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [status, setStatus] = useState<string>('');
  const [assetId, setAssetId] = useState<string>('');
  const [replayModal, setReplayModal] = useState<{ open: boolean; url?: string }>({ open: false });

  const { data, isLoading } = useQuery({
    queryKey: ['bastion-sessions', wsId, status, assetId],
    queryFn: () =>
      bastionApi.listSessions({
        workspace_id: wsId || undefined,
        status: (status as 'active' | 'closed') || undefined,
        asset_id: assetId ? Number(assetId) : undefined,
        page: 1,
        size: 200,
      }),
    enabled: !!wsId,
    refetchInterval: 15000,
  });

  const replayMutation = useMutation({
    mutationFn: (id: number) => bastionApi.getReplay(id),
    onSuccess: (res) => setReplayModal({ open: true, url: res.replay_url }),
    onError: (e: any) => message.error(e?.message || '获取录像失败'),
  });

  const columns: ColumnsType<BastionSession> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '资产', dataIndex: 'asset_name', key: 'asset_name', width: 180 },
    { title: '用户', dataIndex: 'username', key: 'username', width: 140 },
    {
      title: '协议',
      dataIndex: 'protocol',
      key: 'protocol',
      width: 80,
      render: (v: string) => <Tag color={v === 'rdp' ? 'purple' : 'blue'}>{v.toUpperCase()}</Tag>,
    },
    { title: '来源IP', dataIndex: 'remote_addr', key: 'remote_addr', width: 150 },
    { title: '登录方式', dataIndex: 'login_from', key: 'login_from', width: 120 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 90,
      render: (v: string) => <Tag color={STATUS_COLOR[v] || 'default'}>{v === 'active' ? '进行中' : '已结束'}</Tag>,
    },
    { title: '开始时间', dataIndex: 'started_at', key: 'started_at', width: 170, render: (v?: string) => (v ? formatTime(v) : '-') },
    { title: '结束时间', dataIndex: 'ended_at', key: 'ended_at', width: 170, render: (v?: string) => (v ? formatTime(v) : '-') },
    { title: '时长', dataIndex: 'duration_ms', key: 'duration_ms', width: 90, render: formatDuration },
    { title: '命令数', dataIndex: 'command_count', key: 'command_count', width: 80 },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_: any, r: BastionSession) =>
        r.status === 'closed' ? (
          <Button size="small" icon={<PlayCircleOutlined />} loading={replayMutation.isPending} onClick={() => replayMutation.mutate(r.id)}>
            回放
          </Button>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <PageContainer
      title="会话录像"
      extra={
        <Space>
          <Input
            allowClear
            placeholder="资产ID"
            style={{ width: 140 }}
            value={assetId}
            onChange={(e) => setAssetId(e.target.value)}
          />
          <Select
            allowClear
            placeholder="状态"
            style={{ width: 120 }}
            value={status || undefined}
            onChange={(v) => setStatus(v || '')}
            options={[
              { label: '进行中', value: 'active' },
              { label: '已结束', value: 'closed' },
            ]}
          />
        </Space>
      }
    >
      <Card>
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items || []}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无会话记录" description="会话由 JumpServer 同步，可在系统设置中配置同步" /> }}
        />
      </Card>

      <Modal
        title="会话回放"
        open={replayModal.open}
        onCancel={() => setReplayModal({ open: false })}
        footer={[
          <Button key="open" type="primary" href={replayModal.url} target="_blank" rel="noreferrer">
            打开回放
          </Button>,
          <Button key="close" onClick={() => setReplayModal({ open: false })}>
            关闭
          </Button>,
        ]}
      >
        <p style={{ marginBottom: 12 }}>点击下方按钮在 JumpServer 中查看完整会话回放（含命令审计）：</p>
        <Input.TextArea rows={3} value={replayModal.url} readOnly />
      </Modal>
    </PageContainer>
  );
}

export default BastionSessionsPage;
