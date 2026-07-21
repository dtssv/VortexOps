import { useState } from 'react';
import { App, Button, Card, Input, Modal, Select, Space, Table, Tag } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { approvalApi, type Approval } from '@/api/approvals';
import { useUIStore } from '@/stores/uiStore';
import { formatTime } from '@/utils/format';

const STATUS_COLOR: Record<string, string> = {
  pending: 'processing',
  approved: 'success',
  rejected: 'error',
  expired: 'default',
  canceled: 'default',
};

export function ApprovalsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [statusFilter, setStatusFilter] = useState<string>('pending');
  const [commentModal, setCommentModal] = useState<{ open: boolean; id?: number; action?: 'approve' | 'reject' }>({ open: false });
  const [comment, setComment] = useState('');

  const { data, isLoading } = useQuery({
    queryKey: ['approvals', wsId, statusFilter],
    queryFn: () =>
      approvalApi.list({
        workspace_id: wsId || undefined,
        status: statusFilter || undefined,
        page: 1,
        size: 100,
      }),
    enabled: !!wsId,
    refetchInterval: 10000,
  });

  const actionMutation = useMutation({
    mutationFn: (vars: { id: number; action: 'approve' | 'reject'; comment: string }) =>
      vars.action === 'approve'
        ? approvalApi.approve(vars.id, { comment: vars.comment })
        : approvalApi.reject(vars.id, { comment: vars.comment }),
    onSuccess: () => {
      message.success('操作成功');
      setCommentModal({ open: false });
      setComment('');
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const columns: ColumnsType<Approval> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '资源类型', dataIndex: 'resource_type', key: 'resource_type', width: 110, render: (v: string) => <Tag>{v}</Tag> },
    { title: '资源ID', dataIndex: 'resource_id', key: 'resource_id', width: 90 },
    { title: '操作', dataIndex: 'operation', key: 'operation', width: 100 },
    { title: '申请人', dataIndex: 'requested_by', key: 'requested_by', width: 90 },
    { title: '申请时间', dataIndex: 'requested_at', key: 'requested_at', width: 170, render: formatTime },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <Tag color={STATUS_COLOR[v] || 'default'}>{v}</Tag>,
    },
    { title: '审批人', dataIndex: 'approver_id', key: 'approver_id', width: 90, render: (v: number) => v || '-' },
    { title: '审批时间', dataIndex: 'approved_at', key: 'approved_at', width: 170, render: (v?: string) => (v ? formatTime(v) : '-') },
    { title: '备注', dataIndex: 'comment', key: 'comment', ellipsis: true },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      render: (_: any, r: Approval) =>
        r.status === 'pending' ? (
          <Space>
            <a style={{ color: '#52c41a' }} onClick={() => { setComment(r.id + ''); setCommentModal({ open: true, id: r.id, action: 'approve' }); setComment(''); }}>批准</a>
            <a style={{ color: '#ff4d4f' }} onClick={() => setCommentModal({ open: true, id: r.id, action: 'reject' })}>拒绝</a>
          </Space>
        ) : (
          '-'
        ),
    },
  ];

  return (
    <PageContainer
      title="发布审批"
      extra={
        <Select
          value={statusFilter}
          onChange={setStatusFilter}
          style={{ width: 140 }}
          options={[
            { label: '待审批', value: 'pending' },
            { label: '已批准', value: 'approved' },
            { label: '已拒绝', value: 'rejected' },
            { label: '全部', value: '' },
          ]}
        />
      }
    >
      <Card>
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items || []}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无审批记录" /> }}
        />
      </Card>

      <Modal
        title={commentModal.action === 'approve' ? '批准审批' : '拒绝审批'}
        open={commentModal.open}
        onCancel={() => setCommentModal({ open: false })}
        onOk={() =>
          commentModal.id &&
          commentModal.action &&
          actionMutation.mutate({ id: commentModal.id, action: commentModal.action, comment })
        }
        confirmLoading={actionMutation.isPending}
        destroyOnHidden
      >
        <Input.TextArea rows={3} placeholder="备注（可选）" value={comment} onChange={(e) => setComment(e.target.value)} />
      </Modal>
    </PageContainer>
  );
}

export default ApprovalsPage;
