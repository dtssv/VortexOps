import { useMemo, useState } from 'react';
import { Button, Card, Form, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography, App } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, ReloadOutlined, EyeOutlined, InboxOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { configApi } from '@/api/configs';
import { workspaceApi } from '@/api/workspaces';
import type { ConfigItem } from '@/types';
import { formatRelative } from '@/utils/format';
import { confirmDanger } from '@/utils/action';

const CONFIG_TYPE_LABEL: Record<string, string> = {
  env: '环境变量',
  file: '配置文件',
  command: '启动命令',
  composite: '组合配置',
};

interface CreateForm {
  name: string;
  description?: string;
  config_type: string;
}

export default function ConfigListPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [workspaceId, setWorkspaceId] = useState<number | undefined>();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [form] = Form.useForm<CreateForm>();

  const { data: wsPage } = useQuery({
    queryKey: ['workspaces', 'list-all'],
    queryFn: () => workspaceApi.list({ page: 1, size: 200 }),
  });

  const { data: configPage, isLoading } = useQuery({
    queryKey: ['configs', workspaceId, page, size],
    queryFn: () => configApi.list({ workspace_id: workspaceId, page, size }),
  });

  const createMutation = useMutation({
    mutationFn: (vals: CreateForm) =>
      configApi.create({ ...vals, workspace_id: workspaceId }),
    onSuccess: (data) => {
      message.success('配置已创建');
      setCreateOpen(false);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['configs'] });
      navigate(`/configs/${data.id}`);
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const archiveMutation = useMutation({
    mutationFn: (id: number) => configApi.archive(id),
    onSuccess: () => {
      message.success('已归档');
      void queryClient.invalidateQueries({ queryKey: ['configs'] });
    },
    onError: (e: any) => message.error(e?.message || '归档失败'),
  });

  const columns: ColumnsType<ConfigItem> = useMemo(
    () => [
      {
        title: '名称',
        dataIndex: 'name',
        render: (v: string, r) => (
          <Space direction="vertical" size={0}>
            <Typography.Text strong>{v}</Typography.Text>
            {r.description && (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {r.description}
              </Typography.Text>
            )}
          </Space>
        ),
      },
      {
        title: '类型',
        dataIndex: 'config_type',
        width: 110,
        render: (v: string) => CONFIG_TYPE_LABEL[v] ?? v,
      },
      {
        title: '当前版本',
        dataIndex: 'current_version',
        width: 100,
        render: (v: number) => <Tag color="blue">v{v}</Tag>,
      },
      {
        title: '状态',
        dataIndex: 'archived',
        width: 100,
        render: (v: boolean) =>
          v ? <Tag>已归档</Tag> : <ResourceStatus status="active" />,
      },
      {
        title: '更新时间',
        dataIndex: 'updated_at',
        width: 140,
        render: (v: string) => formatRelative(v),
      },
      {
        title: '操作',
        width: 160,
        render: (_, r) => (
          <Space size={0}>
            <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => navigate(`/configs/${r.id}`)}>
              查看
            </Button>
            {!r.archived && (
              <Button
                type="link"
                size="small"
                danger
                icon={<InboxOutlined />}
                onClick={() =>
                  confirmDanger({
                    title: '归档配置',
                    content: `归档后配置将不可用且不可恢复，确定归档「${r.name}」？`,
                    okText: '归档',
                    onOk: () => archiveMutation.mutateAsync(r.id),
                  })
                }
              >
                归档
              </Button>
            )}
          </Space>
        ),
      },
    ],
    [navigate, archiveMutation],
  );

  return (
    <PageContainer
      title="配置管理"
      subtitle="管理应用运行时配置及其版本"
      extra={
        <Space>
          <Tooltip title="刷新">
            <Button icon={<ReloadOutlined />} onClick={() => setWorkspaceId((v) => v)} />
          </Tooltip>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            disabled={!workspaceId}
            onClick={() => setCreateOpen(true)}
          >
            新建配置
          </Button>
        </Space>
      }
    >
      <Card style={{ marginBottom: 16 }}>
        <Select
          placeholder="按工作空间筛选"
          style={{ width: 280 }}
          value={workspaceId}
          onChange={(v) => {
            setWorkspaceId(v);
            setPage(1);
          }}
          options={(wsPage?.items ?? []).map((w) => ({ label: w.display_name || w.name, value: w.id }))}
          showSearch
          optionFilterProp="label"
          allowClear
        />
      </Card>

      <Card>
        <Table<ConfigItem>
          rowKey="id"
          columns={columns}
          dataSource={configPage?.items}
          loading={isLoading}
          onRow={(r) => ({ onClick: () => navigate(`/configs/${r.id}`), style: { cursor: 'pointer' } })}
          pagination={{
            current: configPage?.page ?? page,
            pageSize: configPage?.size ?? size,
            total: configPage?.total ?? 0,
            showSizeChanger: true,
            onChange: (p, s) => {
              setPage(p);
              setSize(s);
            },
          }}
        />
      </Card>

      <Modal
        title="新建配置"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnClose
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(vals) => createMutation.mutate(vals)}
          initialValues={{ config_type: 'env' }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如 payment-svc-prod-config" />
          </Form.Item>
          <Form.Item name="config_type" label="类型" rules={[{ required: true }]}>
            <Select
              options={Object.entries(CONFIG_TYPE_LABEL).map(([v, l]) => ({ value: v, label: l }))}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
