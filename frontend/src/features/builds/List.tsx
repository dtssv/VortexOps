import { useMemo, useState } from 'react';
import { Button, Card, Select, Space, Table, Tooltip, Typography, App } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { buildApi } from '@/api/builds';
import { workspaceApi } from '@/api/workspaces';
import { applicationApi } from '@/api/applications';
import type { Build } from '@/types';
import { formatRelative, formatDuration, shortSha, truncate } from '@/utils/format';

export default function BuildListPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const [workspaceId, setWorkspaceId] = useState<number | undefined>();
  const [applicationId, setApplicationId] = useState<number | undefined>();
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

  const { data: buildsPage, isLoading: buildsLoading } = useQuery({
    queryKey: ['builds', applicationId, page, size],
    queryFn: () => buildApi.list(applicationId!, { page, size }),
    enabled: !!applicationId,
  });

  const columns: ColumnsType<Build> = useMemo(
    () => [
      {
        title: '构建',
        dataIndex: 'commit_message',
        render: (_, r) => (
          <Space direction="vertical" size={0}>
            <Typography.Text strong>#{r.id}</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {truncate(r.commit_message, 50)}
            </Typography.Text>
          </Space>
        ),
      },
      { title: '分支', dataIndex: 'branch', width: 140, ellipsis: true },
      {
        title: 'Commit',
        dataIndex: 'commit_sha',
        width: 110,
        render: (v: string) => <Typography.Text code>{shortSha(v)}</Typography.Text>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (v: string) => <ResourceStatus status={v} />,
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
          <Button type="link" size="small" onClick={() => navigate(`/builds/${r.id}`)}>
            查看
          </Button>
        ),
      },
    ],
    [navigate],
  );

  const onCreate = () => {
    if (!applicationId) {
      message.info('请进入应用详情触发构建');
      return;
    }
    navigate(`/applications/${applicationId}`);
  };

  return (
    <PageContainer
      title="构建中心"
      subtitle="按应用筛选并查看构建历史"
      extra={
        <Space>
          <Tooltip title="刷新">
            <Button icon={<ReloadOutlined />} onClick={() => setApplicationId((v) => v)} />
          </Tooltip>
          <Button type="primary" icon={<PlusOutlined />} onClick={onCreate}>
            新建构建
          </Button>
        </Space>
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
              setPage(1);
            }}
            options={(wsPage?.items ?? []).map((w) => ({
              label: w.display_name || w.name,
              value: w.id,
            }))}
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
              setPage(1);
            }}
            options={(appPage?.items ?? []).map((a) => ({
              label: a.display_name || a.name,
              value: a.id,
            }))}
            disabled={!workspaceId}
            showSearch
            optionFilterProp="label"
            allowClear
          />
        </Space>
      </Card>

      <Card>
        {!applicationId ? (
          <EmptyState
            title="请先选择应用"
            description="构建按应用维度组织，请选择工作空间后再选择应用"
          />
        ) : (
          <Table<Build>
            rowKey="id"
            columns={columns}
            dataSource={buildsPage?.items}
            loading={buildsLoading}
            onRow={(r) => ({ onClick: () => navigate(`/builds/${r.id}`), style: { cursor: 'pointer' } })}
            pagination={{
              current: buildsPage?.page ?? page,
              pageSize: buildsPage?.size ?? size,
              total: buildsPage?.total ?? 0,
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
