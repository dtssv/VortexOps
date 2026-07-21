import { useMemo, useState } from 'react';
import { Button, Card, Progress, Select, Space, Table, Tag, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { releaseApi } from '@/api/releases';
import { workspaceApi } from '@/api/workspaces';
import { applicationApi, groupApi } from '@/api/applications';
import type { Release } from '@/types';
import { formatRelative, formatDuration } from '@/utils/format';
import { strategyLabel } from './labels';

export default function ReleaseListPage() {
  const navigate = useNavigate();
  const [workspaceId, setWorkspaceId] = useState<number | undefined>();
  const [applicationId, setApplicationId] = useState<number | undefined>();
  const [groupId, setGroupId] = useState<number | undefined>();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);

  const { data: wsPage } = useQuery({
    queryKey: ['workspaces', 'list-all'],
    queryFn: () => workspaceApi.list({ page: 1, size: 200 }),
  });

  const { data: appPage, isLoading: appsLoading } = useQuery({
    queryKey: ['applications', workspaceId],
    queryFn: () => applicationApi.list(workspaceId!, { page: 1, size: 200 }),
    enabled: !!workspaceId,
  });

  const { data: groupPage, isLoading: groupsLoading } = useQuery({
    queryKey: ['groups', applicationId],
    queryFn: () => groupApi.list(applicationId!),
    enabled: !!applicationId,
  });

  const { data: releasesPage, isLoading: releasesLoading } = useQuery({
    queryKey: ['releases', groupId, page, size],
    queryFn: () => releaseApi.list(groupId!, { page, size }),
    enabled: !!groupId,
  });

  const columns: ColumnsType<Release> = useMemo(
    () => [
      {
        title: '发布编号',
        dataIndex: 'release_number',
        width: 100,
        render: (v: number, r) => (
          <Typography.Text strong>#{r.release_number}</Typography.Text>
        ),
      },
      {
        title: '镜像',
        dataIndex: 'image_ref',
        ellipsis: true,
        render: (v: string) => (v ? <Tag color="blue">{v}</Tag> : '-'),
      },
      {
        title: '策略',
        dataIndex: 'strategy',
        width: 100,
        render: (v: string) => strategyLabel(v),
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (v: string) => <ResourceStatus status={v} />,
      },
      {
        title: '进度',
        dataIndex: 'progress_percent',
        width: 160,
        render: (v: number) => <Progress percent={v ?? 0} size="small" status={v === 100 ? 'success' : 'active'} />,
      },
      { title: '触发者', dataIndex: 'triggered_by_name', width: 120 },
      {
        title: '开始时间',
        dataIndex: 'started_at',
        width: 140,
        render: (v: string) => formatRelative(v),
      },
      {
        title: '耗时',
        dataIndex: 'duration_ms',
        width: 100,
        render: (v: number) => formatDuration(v),
      },
      {
        title: '操作',
        width: 90,
        render: (_, r) => (
          <Button type="link" size="small" onClick={() => navigate(`/releases/${r.id}`)}>
            查看
          </Button>
        ),
      },
    ],
    [navigate],
  );

  return (
    <PageContainer
      title="发布中心"
      subtitle="按分组筛选并查看发布历史"
      extra={
        <Tooltip title="刷新">
          <Button icon={<ReloadOutlined />} onClick={() => setGroupId((v) => v)} />
        </Tooltip>
      }
    >
      <Card style={{ marginBottom: 16 }}>
        <Space wrap>
          <Select
            placeholder="选择工作空间"
            style={{ width: 240 }}
            value={workspaceId}
            onChange={(v) => {
              setWorkspaceId(v);
              setApplicationId(undefined);
              setGroupId(undefined);
              setPage(1);
            }}
            options={(wsPage?.items ?? []).map((w) => ({ label: w.display_name || w.name, value: w.id }))}
            showSearch
            optionFilterProp="label"
            allowClear
          />
          <Select
            placeholder="选择应用"
            style={{ width: 280 }}
            value={applicationId}
            loading={appsLoading}
            onChange={(v) => {
              setApplicationId(v);
              setGroupId(undefined);
              setPage(1);
            }}
            options={(appPage?.items ?? []).map((a) => ({ label: a.display_name || a.name, value: a.id }))}
            disabled={!workspaceId}
            showSearch
            optionFilterProp="label"
            allowClear
          />
          <Select
            placeholder="选择分组"
            style={{ width: 280 }}
            value={groupId}
            loading={groupsLoading}
            onChange={(v) => {
              setGroupId(v);
              setPage(1);
            }}
            options={(groupPage?.items ?? []).map((g) => ({
              label: `${g.display_name || g.name} (${g.environment})`,
              value: g.id,
            }))}
            disabled={!applicationId}
            showSearch
            optionFilterProp="label"
            allowClear
          />
        </Space>
      </Card>

      <Card>
        {!groupId ? (
          <EmptyState
            title="请先选择分组"
            description="发布按分组维度组织，请依次选择工作空间、应用、分组"
          />
        ) : (
          <Table<Release>
            rowKey="id"
            columns={columns}
            dataSource={releasesPage?.items}
            loading={releasesLoading}
            onRow={(r) => ({ onClick: () => navigate(`/releases/${r.id}`), style: { cursor: 'pointer' } })}
            pagination={{
              current: releasesPage?.page ?? page,
              pageSize: releasesPage?.size ?? size,
              total: releasesPage?.total ?? 0,
              showSizeChanger: true,
              onChange: (p, s) => {
                setPage(p);
                setSize(s);
              },
            }}
          />
        )}
      </Card>
    </PageContainer>
  );
}
