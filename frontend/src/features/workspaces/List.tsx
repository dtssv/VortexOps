import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, Input, Modal, Form, InputNumber, Select, Space, Table, App } from 'antd';
import { PlusOutlined, EyeOutlined, EditOutlined, DeleteOutlined, SearchOutlined } from '@ant-design/icons';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { workspaceApi } from '@/api/workspaces';
import type { CreateWorkspaceInput } from '@/api/workspaces';
import { confirmDanger } from '@/utils/action';
import { formatRelative } from '@/utils/format';
import type { Workspace } from '@/types';

export default function WorkspaceListPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const [page, setPage] = useState(1);
  const [size, setSize] = useState(10);
  const [search, setSearch] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm<CreateWorkspaceInput>();
  const [editTarget, setEditTarget] = useState<Workspace | null>(null);
  const [editForm] = Form.useForm<Partial<CreateWorkspaceInput>>();

  const queryKey = ['workspaces', 'list', { page, size, search }];

  const { data, isLoading } = useQuery({
    queryKey,
    queryFn: () => workspaceApi.list({ page, size, search: search || undefined }),
  });

  const createMutation = useMutation({
    mutationFn: (body: CreateWorkspaceInput) => workspaceApi.create(body),
    onSuccess: () => {
      message.success('空间创建成功');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['workspaces'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Partial<CreateWorkspaceInput> & { version?: number } }) =>
      workspaceApi.update(id, body),
    onSuccess: () => {
      message.success('空间已更新');
      setEditTarget(null);
      queryClient.invalidateQueries({ queryKey: ['workspaces'] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => workspaceApi.delete(id),
    onSuccess: () => {
      message.success('空间已删除');
      queryClient.invalidateQueries({ queryKey: ['workspaces'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const handleDelete = (ws: Workspace) => {
    confirmDanger({
      title: '删除工作空间',
      content: `确定要删除空间「${ws.display_name || ws.name}」吗？若空间下仍存在应用或集群绑定，将无法删除，请先清理相关资源。`,
      okText: '删除',
      onOk: () => deleteMutation.mutateAsync(ws.id),
    });
  };

  const columns: ColumnsType<Workspace> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_, r) => (
        <a onClick={() => navigate(`/workspaces/${r.id}`)}>{r.display_name || r.name}</a>
      ),
    },
    {
      title: '标识',
      dataIndex: 'name',
      width: 160,
      render: (v: string) => <span style={{ color: '#8c8c8c' }}>{v}</span>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (s: string) => <ResourceStatus status={s} />,
    },
    { title: '应用', dataIndex: 'application_count', width: 80, align: 'center' },
    { title: '分组', dataIndex: 'group_count', width: 80, align: 'center' },
    { title: '成员', dataIndex: 'member_count', width: 80, align: 'center' },
    { title: '更新时间', dataIndex: 'updated_at', width: 160, render: formatRelative },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      render: (_, r) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => navigate(`/workspaces/${r.id}`)}
          >
            查看
          </Button>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => {
              setEditTarget(r);
              editForm.resetFields();
            }}
          >
            编辑
          </Button>
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() => handleDelete(r)}
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <PageContainer
      title="工作空间"
      subtitle="管理你的工作空间及其下的应用、成员和集群绑定"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建空间
        </Button>
      }
    >
      <div style={{ marginBottom: 16 }}>
        <Input
          allowClear
          prefix={<SearchOutlined />}
          placeholder="按名称搜索"
          style={{ width: 280 }}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
        />
      </div>

      <Table<Workspace>
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total ?? 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
        locale={{
          emptyText: (
            <EmptyState
              title="暂无工作空间"
              description="点击右上角「新建空间」开始创建"
              actionText="新建空间"
              onAction={() => setCreateOpen(true)}
            />
          ),
        }}
      />

      <Modal
        title="新建工作空间"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnClose
      >
        <Form
          layout="vertical"
          form={createForm}
          onFinish={(v) => createMutation.mutate(v)}
          initialValues={{ max_applications: 20, max_groups: 50, max_members: 20 }}
        >
          <Form.Item
            name="name"
            label="标识名"
            rules={[
              { required: true, message: '请输入标识名' },
              { pattern: /^[a-z0-9-]+$/, message: '仅支持小写字母、数字和短横线' },
            ]}
          >
            <Input placeholder="例如 my-team" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="例如 我的团队空间" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Space style={{ display: 'flex' }}>
            <Form.Item name="max_applications" label="最大应用数">
              <InputNumber min={0} />
            </Form.Item>
            <Form.Item name="max_groups" label="最大分组数">
              <InputNumber min={0} />
            </Form.Item>
            <Form.Item name="max_members" label="最大成员数">
              <InputNumber min={0} />
            </Form.Item>
          </Space>
        </Form>
      </Modal>

      <Modal
        title={`编辑工作空间 - ${editTarget?.name || ''}`}
        open={!!editTarget}
        onCancel={() => setEditTarget(null)}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnClose
        width={620}
      >
        <Form
          layout="vertical"
          form={editForm}
          initialValues={
            editTarget
              ? {
                  display_name: editTarget.display_name,
                  description: editTarget.description,
                  logo_url: editTarget.logo_url,
                  status: editTarget.status,
                  version: editTarget.version,
                }
              : {}
          }
          onFinish={(v) =>
            updateMutation.mutate({
              id: editTarget!.id,
              body: {
                display_name: v.display_name,
                description: v.description,
                logo_url: v.logo_url,
                status: v.status,
                version: v.version,
              },
            })
          }
        >
          <Form.Item label="标识名" extra="创建后不可修改">
            <Input value={editTarget?.name} disabled />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="例如 我的团队空间" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Form.Item name="logo_url" label="Logo URL">
            <Input placeholder="可选，例如 https://example.com/logo.png" />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select
              options={[
                { label: '活跃', value: 'active' },
                { label: '归档', value: 'archived' },
              ]}
            />
          </Form.Item>
          <Form.Item name="version" hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
