import { useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  App as AntdApp,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
} from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { BreadcrumbSwitcher } from '@/components/BreadcrumbSwitcher';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { workspaceApi } from '@/api/workspaces';
import type { CreateWorkspaceInput } from '@/api/workspaces';
import { applicationApi } from '@/api/applications';
import type { CreateApplicationInput } from '@/api/applications';
import { LANGUAGE_OPTIONS } from '@/api/builds';
import { clusterApi } from '@/api/clusters';
import { rbacApi } from '@/api/rbac';
import { auditApi } from '@/api/audit';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatRelative } from '@/utils/format';
import type { Application, WorkspaceMember, ClusterBinding, AuditLog } from '@/types';

const CLUSTER_ROLES = ['primary', 'secondary'];

export default function WorkspaceDetailPage() {
  const params = useParams();
  const id = Number(params.id);
  const navigate = useNavigate();
  const { message } = AntdApp.useApp();
  const queryClient = useQueryClient();

  const { data: ws, isLoading } = useQuery({
    queryKey: ['workspace', id],
    queryFn: () => workspaceApi.get(id),
    enabled: !!id,
  });

  return (
    <PageContainer
      title={ws?.display_name || ws?.name || '工作空间'}
      subtitle={ws?.description}
      breadcrumb={[
        { title: '空间', path: '/workspaces' },
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={ws?.display_name || ws?.name}
              currentValue={ws?.id}
              currentPath={ws ? `/workspaces/${ws.id}` : undefined}
              queryKeyPrefix={['workspaces']}
              loadOptions={(search) =>
                workspaceApi
                  .list({ search: search || undefined, page: 1, size: 50 })
                  .then((p) =>
                    p.items.map((w) => ({
                      label: w.display_name || w.name,
                      value: w.id,
                      path: `/workspaces/${w.id}`,
                    })),
                  )
              }
            />
          ),
        },
      ]}
    >
      <Card loading={isLoading}>
        <Tabs
          defaultActiveKey="apps"
          items={[
            { key: 'apps', label: '应用', children: <ApplicationsTab workspaceId={id} /> },
            { key: 'members', label: '成员', children: <MembersTab workspaceId={id} /> },
            { key: 'clusters', label: '集群绑定', children: <ClustersTab workspaceId={id} /> },
            { key: 'audit', label: '审计', children: <AuditTab workspaceId={id} /> },
            {
              key: 'settings',
              label: '设置',
              children: <SettingsTab workspaceId={id} name={ws?.name} />,
            },
          ]}
        />
      </Card>
    </PageContainer>
  );

  function ApplicationsTab({ workspaceId }: { workspaceId: number }) {
    const [page, setPage] = useState(1);
    const [size, setSize] = useState(10);
    const [createOpen, setCreateOpen] = useState(false);
    const [form] = Form.useForm<CreateApplicationInput>();

    const { data, isLoading } = useQuery({
      queryKey: ['workspace', workspaceId, 'apps', { page, size }],
      queryFn: () => applicationApi.list(workspaceId, { page, size }),
    });

    const createMutation = useMutation({
      mutationFn: (v: CreateApplicationInput) => applicationApi.create(workspaceId, v),
      onSuccess: () => {
        message.success('应用已创建');
        setCreateOpen(false);
        form.resetFields();
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'apps'] });
      },
      onError: (e: any) => message.error(e?.message || '创建失败'),
    });

    const deleteMutation = useMutation({
      mutationFn: (id: number) => applicationApi.delete(id),
      onSuccess: () => {
        message.success('应用已删除');
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'apps'] });
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    const columns: ColumnsType<Application> = [
      {
        title: '名称',
        dataIndex: 'name',
        render: (_, r) => (
          <a onClick={() => navigate(`/applications/${r.id}`)}>{r.display_name || r.name}</a>
        ),
      },
      { title: '类型', dataIndex: 'app_type', width: 120 },
      { title: '语言', dataIndex: 'language', width: 100, render: (v?: string) => (v ? <Tag color="blue">{v}</Tag> : '-') },
      { title: '工作负载', dataIndex: 'workload_type', width: 120 },
      { title: '分组', dataIndex: 'group_count', width: 80, align: 'center' },
      { title: '更新时间', dataIndex: 'updated_at', width: 160, render: formatRelative },
      {
        title: '操作',
        key: 'actions',
        width: 100,
        render: (_, r) => (
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '删除应用',
                content: `确定删除应用「${r.display_name || r.name}」吗？若应用下存在分组，将无法删除。`,
                okText: '删除',
                onOk: () => deleteMutation.mutateAsync(r.id),
              })
            }
          >
            删除
          </Button>
        ),
      },
    ];

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            新建应用
          </Button>
        </div>
        <Table<Application>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items}
          pagination={{
            current: page,
            pageSize: size,
            total: data?.total ?? 0,
            onChange: (p, s) => {
              setPage(p);
              setSize(s);
            },
          }}
          locale={{ emptyText: <EmptyState title="暂无应用" description="点击右上角「新建应用」开始创建" actionText="新建应用" onAction={() => setCreateOpen(true)} /> }}
        />
        <Modal
          title="新建应用"
          open={createOpen}
          onCancel={() => setCreateOpen(false)}
          onOk={() => form.submit()}
          confirmLoading={createMutation.isPending}
          destroyOnClose
        >
          <Form
            layout="vertical"
            form={form}
            onFinish={(v) => createMutation.mutate(v)}
          >
            <Form.Item
              name="name"
              label="标识名"
              rules={[
                { required: true, message: '请输入标识名' },
                { pattern: /^[a-z0-9-]+$/, message: '仅支持小写字母、数字和短横线' },
              ]}
            >
              <Input placeholder="例如 my-service" />
            </Form.Item>
            <Form.Item name="display_name" label="显示名称">
              <Input placeholder="例如 我的服务" />
            </Form.Item>
            <Form.Item name="description" label="描述">
              <Input.TextArea rows={2} placeholder="可选" />
            </Form.Item>
            <Form.Item name="app_type" label="应用类型">
              <Select
                allowClear
                placeholder="例如 web / worker / job"
                options={[
                  { label: 'Web 服务', value: 'web' },
                  { label: 'Worker', value: 'worker' },
                  { label: '定时任务', value: 'job' },
                  { label: 'AI 推理', value: 'inference' },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="language"
              label="开发语言"
              rules={[{ required: true, message: '请选择开发语言' }]}
              extra="创建后不可修改，决定构建时可用的基础镜像与构建工具"
            >
              <Select
                placeholder="选择主要开发语言"
                options={LANGUAGE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
              />
            </Form.Item>
            <Form.Item name="workload_type" label="工作负载类型">
              <Select
                allowClear
                placeholder="例如 deployment / statefulset"
                options={[
                  { label: 'Deployment', value: 'deployment' },
                  { label: 'StatefulSet', value: 'statefulset' },
                  { label: 'CronJob', value: 'cronjob' },
                ]}
              />
            </Form.Item>
            <Form.Item name="git_url" label="Git 仓库地址">
              <Input placeholder="例如 https://github.com/org/my-service.git" />
            </Form.Item>
            <Form.Item name="default_branch" label="默认分支">
              <Input placeholder="例如 main" />
            </Form.Item>
          </Form>
        </Modal>
      </>
    );
  }

  function MembersTab({ workspaceId }: { workspaceId: number }) {
    const [addOpen, setAddOpen] = useState(false);
    const [form] = Form.useForm<{ user_id: number; role_id: number }>();

    const { data, isLoading } = useQuery({
      queryKey: ['workspace', workspaceId, 'members'],
      queryFn: () => workspaceApi.listMembers(workspaceId),
    });

    const { data: wsRoles } = useQuery({
      queryKey: ['roles', { scope: 'workspace', page: 1, size: 200 }],
      queryFn: () => rbacApi.listRoles({ scope: 'workspace', page: 1, size: 200 }),
    });
    const roleMap = new Map((wsRoles?.items ?? []).map((r) => [r.id, r]));
    const roleOptions = (wsRoles?.items ?? []).map((r) => ({ label: r.name, value: r.id }));

    const addMutation = useMutation({
      mutationFn: (v: { user_id: number; role_id: number }) =>
        workspaceApi.addMember(workspaceId, v),
      onSuccess: () => {
        message.success('成员已添加');
        setAddOpen(false);
        form.resetFields();
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'members'] });
      },
      onError: (e: any) => message.error(e?.message || '添加失败'),
    });

    const removeMutation = useMutation({
      mutationFn: (userId: number) => workspaceApi.removeMember(workspaceId, userId),
      onSuccess: () => {
        message.success('成员已移除');
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'members'] });
      },
      onError: (e: any) => message.error(e?.message || '移除失败'),
    });

    const columns: ColumnsType<WorkspaceMember> = [
      {
        title: '用户',
        dataIndex: 'user_id',
        render: (_, r) => r.display_name || r.username || `用户 ${r.user_id}`,
      },
      {
        title: '角色',
        dataIndex: 'role_id',
        width: 160,
        render: (rid: number, r) => r.role_name || roleMap.get(rid)?.name || `#${rid}`,
      },
      { title: '加入时间', dataIndex: 'joined_at', width: 180, render: formatTime },
      {
        title: '操作',
        key: 'actions',
        width: 100,
        render: (_, r) => (
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '移除成员',
                content: `确定将「${r.display_name || r.username || `用户 ${r.user_id}`}」移出本空间？`,
                okText: '移除',
                onOk: () => removeMutation.mutateAsync(r.user_id),
              })
            }
          >
            移除
          </Button>
        ),
      },
    ];

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
            添加成员
          </Button>
        </div>
        <Table<WorkspaceMember>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无成员" /> }}
        />
        <Modal
          title="添加成员"
          open={addOpen}
          onCancel={() => setAddOpen(false)}
          onOk={() => form.submit()}
          confirmLoading={addMutation.isPending}
          destroyOnClose
        >
          <Form layout="vertical" form={form} onFinish={(v) => addMutation.mutate(v)}>
            <Form.Item
              name="user_id"
              label="用户 ID"
              rules={[{ required: true, message: '请输入用户 ID' }]}
            >
              <Input type="number" placeholder="用户 ID" />
            </Form.Item>
            <Form.Item
              name="role_id"
              label="角色"
              rules={[{ required: true, message: '请选择角色' }]}
            >
              <Select options={roleOptions} placeholder="选择空间角色" />
            </Form.Item>
          </Form>
        </Modal>
      </>
    );
  }

  function ClustersTab({ workspaceId }: { workspaceId: number }) {
    const [addOpen, setAddOpen] = useState(false);
    const [form] = Form.useForm<{ cluster_id: number; namespace: string; role: string }>();

    const { data: bindings, isLoading } = useQuery({
      queryKey: ['workspace', workspaceId, 'clusters'],
      queryFn: () => workspaceApi.listClusterBindings(workspaceId),
    });

    const { data: clusterPage } = useQuery({
      queryKey: ['clusters', 'list', { size: 200 }],
      queryFn: () => clusterApi.list({ size: 200 }),
    });

    const addMutation = useMutation({
      mutationFn: (v: { cluster_id: number; namespace: string; role: string }) =>
        workspaceApi.addClusterBinding(workspaceId, v),
      onSuccess: () => {
        message.success('集群绑定已添加');
        setAddOpen(false);
        form.resetFields();
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'clusters'] });
      },
      onError: (e: any) => message.error(e?.message || '添加失败'),
    });

    const removeMutation = useMutation({
      mutationFn: (clusterId: number) => workspaceApi.removeClusterBinding(workspaceId, clusterId),
      onSuccess: () => {
        message.success('已解绑');
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId, 'clusters'] });
      },
      onError: (e: any) => message.error(e?.message || '解绑失败'),
    });

    const columns: ColumnsType<ClusterBinding> = [
      { title: '集群', dataIndex: 'cluster_name' },
      { title: '命名空间', dataIndex: 'namespace', width: 160 },
      { title: '角色', dataIndex: 'role', width: 120, render: (r: string) => <ResourceStatus status={r} text={r} /> },
      { title: '创建时间', dataIndex: 'created_at', width: 180, render: formatTime },
      {
        title: '操作',
        key: 'actions',
        width: 100,
        render: (_, r) => (
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '解绑集群',
                content: `确定解绑「${r.cluster_name} / ${r.namespace}」？`,
                okText: '解绑',
                onOk: () => removeMutation.mutateAsync(r.cluster_id),
              })
            }
          >
            解绑
          </Button>
        ),
      },
    ];

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddOpen(true)}>
            绑定集群
          </Button>
        </div>
        <Table<ClusterBinding>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={bindings}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂未绑定集群" /> }}
        />
        <Modal
          title="绑定集群"
          open={addOpen}
          onCancel={() => setAddOpen(false)}
          onOk={() => form.submit()}
          confirmLoading={addMutation.isPending}
          destroyOnClose
        >
          <Form layout="vertical" form={form} onFinish={(v) => addMutation.mutate(v)}>
            <Form.Item
              name="cluster_id"
              label="集群"
              rules={[{ required: true, message: '请选择集群' }]}
            >
              <Select
                placeholder="选择集群"
                options={(clusterPage?.items ?? []).map((c) => ({
                  label: c.display_name || c.name,
                  value: c.id,
                }))}
              />
            </Form.Item>
            <Form.Item
              name="namespace"
              label="命名空间"
              rules={[{ required: true, message: '请输入命名空间' }]}
            >
              <Input placeholder="例如 default" />
            </Form.Item>
            <Form.Item
              name="role"
              label="角色"
              rules={[{ required: true, message: '请选择角色' }]}
            >
              <Select options={CLUSTER_ROLES.map((r) => ({ label: r, value: r }))} placeholder="选择角色" />
            </Form.Item>
          </Form>
        </Modal>
      </>
    );
  }

  function AuditTab({ workspaceId }: { workspaceId: number }) {
    const [page, setPage] = useState(1);
    const [size, setSize] = useState(10);
    const { data, isLoading } = useQuery({
      queryKey: ['workspace', workspaceId, 'audit', { page, size }],
      queryFn: () => auditApi.list({ workspace_id: workspaceId, page, size }),
    });

    const columns: ColumnsType<AuditLog> = [
      { title: '时间', dataIndex: 'created_at', width: 180, render: formatTime },
      { title: '用户', dataIndex: 'user_name', width: 140 },
      { title: '资源', dataIndex: 'resource_type', width: 140 },
      { title: '操作', dataIndex: 'action', width: 120 },
      { title: '名称', dataIndex: 'resource_name' },
      { title: '状态', dataIndex: 'status_code', width: 80, align: 'center' },
    ];

    return (
      <Table<AuditLog>
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total ?? 0,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
        locale={{ emptyText: <EmptyState title="暂无审计记录" /> }}
      />
    );
  }

  function SettingsTab({ workspaceId, name }: { workspaceId: number; name?: string }) {
    const [form] = Form.useForm<Partial<CreateWorkspaceInput>>();

    const updateMutation = useMutation({
      mutationFn: (v: Partial<CreateWorkspaceInput> & { version?: number }) =>
        workspaceApi.update(workspaceId, v),
      onSuccess: () => {
        message.success('已保存');
        queryClient.invalidateQueries({ queryKey: ['workspace', workspaceId] });
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    const deleteMutation = useMutation({
      mutationFn: () => workspaceApi.delete(workspaceId),
      onSuccess: () => {
        message.success('空间已删除');
        queryClient.invalidateQueries({ queryKey: ['workspaces'] });
        navigate('/workspaces');
      },
      onError: (e: any) => message.error(e?.message || '删除失败'),
    });

    return (
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Form
          layout="vertical"
          form={form}
          initialValues={{
            display_name: ws?.display_name,
            description: ws?.description,
            logo_url: ws?.logo_url,
            status: ws?.status,
            version: ws?.version,
          }}
          onFinish={(v) => updateMutation.mutate(v)}
          style={{ maxWidth: 600 }}
        >
          <Form.Item label="标识名" extra="创建后不可修改">
            <Input value={ws?.name} disabled />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="显示名称" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="空间描述" />
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
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={updateMutation.isPending}>
              保存
            </Button>
          </Form.Item>
        </Form>

        <Card title="危险操作" size="small" style={{ borderColor: '#ffccc7' }}>
          <Button
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '删除工作空间',
                content: `确定删除空间「${name || workspaceId}」？若空间下仍存在应用或集群绑定，将无法删除，请先清理相关资源。`,
                okText: '删除',
                onOk: () => deleteMutation.mutateAsync(),
              })
            }
          >
            删除此空间
          </Button>
        </Card>
      </Space>
    );
  }
}
