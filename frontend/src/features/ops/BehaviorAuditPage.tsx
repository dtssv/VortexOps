import { useState } from 'react';
import { Card, Input, Select, Space, Table, Tag } from 'antd';
import { useQuery } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { opsApi, type BehaviorAuditLog } from '@/api/ops';
import { useUIStore } from '@/stores/uiStore';
import { formatTime } from '@/utils/format';

const RISK_COLOR: Record<string, string> = {
  info: 'blue',
  warn: 'orange',
  danger: 'red',
};

const RISK_LABEL: Record<string, string> = {
  info: '信息',
  warn: '警告',
  danger: '危险',
};

export function BehaviorAuditPage() {
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [risk, setRisk] = useState<string>('');
  const [userId, setUserId] = useState<string>('');

  const { data, isLoading } = useQuery({
    queryKey: ['behavior-audit', wsId, risk, userId],
    queryFn: () =>
      opsApi.listBehavior({
        workspace_id: wsId || undefined,
        user_id: userId ? Number(userId) : undefined,
        page: 1,
        size: 200,
      }),
    // 不限制 wsId：平台管理员未选 workspace 时查全部，避免页面空白误以为"菜单不存在"。
    refetchInterval: 30000,
  });

  const items = (data?.items || []).filter((l) => !risk || l.risk_level === risk);

  const columns: ColumnsType<BehaviorAuditLog> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '用户', dataIndex: 'user_name', key: 'user_name', width: 120 },
    { title: '集群', dataIndex: 'cluster_id', key: 'cluster_id', width: 90 },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 140 },
    { title: 'Pod', dataIndex: 'pod', key: 'pod', width: 200 },
    { title: '会话ID', dataIndex: 'session_id', key: 'session_id', width: 90 },
    {
      title: '命令',
      dataIndex: 'command',
      key: 'command',
      render: (v: string) => <code style={{ wordBreak: 'break-all' }}>{v}</code>,
    },
    {
      title: '风险',
      dataIndex: 'risk_level',
      key: 'risk_level',
      width: 90,
      render: (v: string) => <Tag color={RISK_COLOR[v] || 'default'}>{RISK_LABEL[v] || v}</Tag>,
    },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 170, render: formatTime },
  ];

  return (
    <PageContainer
      title="行为审计"
      extra={
        <Space>
          <Input
            allowClear
            placeholder="用户ID"
            style={{ width: 130 }}
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
          />
          <Select
            allowClear
            placeholder="风险级别"
            style={{ width: 130 }}
            value={risk || undefined}
            onChange={(v) => setRisk(v || '')}
            options={[
              { label: '信息', value: 'info' },
              { label: '警告', value: 'warn' },
              { label: '危险', value: 'danger' },
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
          dataSource={items}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无行为审计记录" description="WebSSH 终端中执行的命令将在此审计（含风险分级）" /> }}
        />
      </Card>
    </PageContainer>
  );
}

export default BehaviorAuditPage;
