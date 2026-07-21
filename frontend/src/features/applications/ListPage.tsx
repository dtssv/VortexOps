import { useMemo, useState } from 'react';
import { Button, Card, Input, Space, Table, Tabs, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined, PlusOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { applicationApi } from '@/api/applications';
import { isExternalManaged, type Application } from '@/types';
import { formatRelative } from '@/utils/format';

type AppTab = 'web' | 'inference';

const TAB_LABELS: Record<AppTab, string> = {
  web: '应用',
  inference: '模型推理',
};

const TAB_COLOR: Record<AppTab, string> = {
  web: 'blue',
  inference: 'purple',
};

function isAppTab(v: string | null): v is AppTab {
  return v === 'web' || v === 'inference';
}

function appTypeTag(appType?: string) {
  const t = (appType || 'web') as AppTab;
  const label = TAB_LABELS[t] || appType || '应用';
  const color = TAB_COLOR[t] || 'default';
  return <Tag color={color}>{label}</Tag>;
}

function lifecycleTag(lc?: string) {
  if (!lc) return <Tag>未知</Tag>;
  const colorMap: Record<string, string> = {
    active: 'green',
    draft: 'default',
    archived: 'red',
    deprecated: 'volcano',
  };
  return <Tag color={colorMap[lc] || 'default'}>{lc}</Tag>;
}

export default function ApplicationListPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const initialTab = isAppTab(searchParams.get('app_type')) ? (searchParams.get('app_type') as AppTab) : 'web';
  const [tab, setTab] = useState<AppTab>(initialTab);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['applications', 'all', tab, search, page, size],
    queryFn: () =>
      applicationApi.listAll({ page, size, search, app_type: tab }),
    placeholderData: (prev) => prev,
  });

  const columns: ColumnsType<Application> = useMemo(
    () => [
      {
        title: '编号',
        dataIndex: 'code',
        width: 140,
        render: (v?: string) => (
          <Typography.Text type="secondary" code style={{ fontSize: 12 }}>
            {v || '-'}
          </Typography.Text>
        ),
      },
      {
        title: '名称',
        dataIndex: 'name',
        render: (v: string, r) => (
          <Space>
            <Typography.Link onClick={() => navigate(`/applications/${r.id}`)}>{v}</Typography.Link>
            {r.display_name && r.display_name !== v && (
              <Typography.Text type="secondary">{r.display_name}</Typography.Text>
            )}
            {isExternalManaged(r) && <Tag color="gold">外部托管</Tag>}
          </Space>
        ),
      },
      {
        title: '类型',
        dataIndex: 'app_type',
        width: 110,
        render: (v?: string) => appTypeTag(v),
      },
      {
        title: '所属空间',
        dataIndex: 'workspace_id',
        width: 120,
        render: (v: number) => <Typography.Text type="secondary">#{v}</Typography.Text>,
      },
      {
        title: '分组数',
        dataIndex: 'group_count',
        width: 90,
        align: 'center',
        render: (v?: number) => v ?? 0,
      },
      {
        title: '生命周期',
        dataIndex: 'lifecycle',
        width: 110,
        render: (v?: string) => lifecycleTag(v),
      },
      {
        title: '创建时间',
        dataIndex: 'created_at',
        width: 160,
        render: (v: string) => formatRelative(v),
      },
      {
        title: '操作',
        width: 120,
        render: (_, r) => (
          <Button type="link" size="small" onClick={() => navigate(`/applications/${r.id}`)}>
            查看
          </Button>
        ),
      },
    ],
    [navigate],
  );

  const items = [
    { key: 'web', label: TAB_LABELS.web },
    { key: 'inference', label: TAB_LABELS.inference },
  ];

  return (
    <PageContainer
      title="应用管理"
      subtitle="统一管理应用与模型推理服务"
      extra={
        <Space>
          <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isFetching}>
            刷新
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              if (tab === 'inference') {
                navigate('/inference/services');
              } else {
                navigate('/workspaces');
              }
            }}
          >
            新建{TAB_LABELS[tab]}
          </Button>
        </Space>
      }
    >
      <Card>
        <Tabs
          activeKey={tab}
          items={items}
          onChange={(key) => {
            const next = key as AppTab;
            setTab(next);
            setPage(1);
            setSearchParams({ app_type: next });
          }}
        />
        <Space style={{ marginBottom: 16, width: '100%', justifyContent: 'space-between' }}>
          <Input.Search
            placeholder="搜索应用名称"
            allowClear
            style={{ width: 280 }}
            onSearch={(v) => {
              setSearch(v);
              setPage(1);
            }}
          />
        </Space>
        <Table<Application>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items}
          pagination={{
            current: page,
            pageSize: size,
            total: data?.total,
            showSizeChanger: true,
            onChange: (p, ps) => {
              setPage(p);
              setSize(ps);
            },
          }}
          locale={{
            emptyText: <EmptyState title="暂无应用" actionText="选择空间" onAction={() => navigate('/workspaces')} />,
          }}
        />
      </Card>
    </PageContainer>
  );
}
