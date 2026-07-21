import { useState } from 'react';
import { App, Button, Card, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tag, Tooltip } from 'antd';
import { PlusOutlined, ReloadOutlined, LinkOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { bastionApi, type BastionAsset, type CreateBastionAssetInput } from '@/api/bastion';
import { useUIStore } from '@/stores/uiStore';
import { formatTime } from '@/utils/format';

const PROTOCOL_COLOR: Record<string, string> = {
  ssh: 'blue',
  rdp: 'purple',
};

export function BastionAssetsPage() {
  const { message, modal } = App.useApp();
  const queryClient = useQueryClient();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [search, setSearch] = useState('');
  const [protocol, setProtocol] = useState<string>('');
  const [form] = Form.useForm();
  const [editModal, setEditModal] = useState<{ open: boolean; editing?: BastionAsset }>({ open: false });
  const [loginUrl, setLoginUrl] = useState<{ open: boolean; url?: string }>({ open: false });

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['bastion-assets', wsId, search, protocol],
    queryFn: () =>
      bastionApi.listAssets({
        workspace_id: wsId || undefined,
        search: search || undefined,
        protocol: (protocol as 'ssh' | 'rdp') || undefined,
        page: 1,
        size: 200,
      }),
    enabled: !!wsId,
    refetchInterval: 30000,
  });

  const createMutation = useMutation({
    mutationFn: (body: CreateBastionAssetInput) => bastionApi.createAsset(body),
    onSuccess: () => {
      message.success('资产已创建');
      setEditModal({ open: false });
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['bastion-assets'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, body }: { id: number; body: any }) => bastionApi.updateAsset(id, body),
    onSuccess: () => {
      message.success('资产已更新');
      setEditModal({ open: false });
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['bastion-assets'] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => bastionApi.deleteAsset(id),
    onSuccess: () => {
      message.success('资产已删除');
      queryClient.invalidateQueries({ queryKey: ['bastion-assets'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const connectMutation = useMutation({
    mutationFn: (id: number) => bastionApi.connect(id),
    onSuccess: (res) => {
      setLoginUrl({ open: true, url: res.login_url });
    },
    onError: (e: any) => message.error(e?.message || '获取连接失败，请检查 JumpServer 配置'),
  });

  const syncMutation = useMutation({
    mutationFn: () => bastionApi.syncAssets(wsId || undefined),
    onSuccess: (res) => {
      message.success(`已同步 ${res.synced} 项资产`);
      queryClient.invalidateQueries({ queryKey: ['bastion-assets'] });
    },
    onError: (e: any) => message.error(e?.message || '同步失败'),
  });

  const handleSubmit = async () => {
    const values = await form.validateFields();
    const payload: CreateBastionAssetInput = {
      workspace_id: wsId!,
      name: values.name,
      host: values.host,
      port: values.port,
      protocol: values.protocol,
      platform: values.platform || 'linux',
      username: values.username,
      credential_id: values.credential_id,
      tags: values.tags,
      comment: values.comment,
    };
    if (editModal.editing) {
      updateMutation.mutate({ id: editModal.editing.id, body: { ...payload, is_active: values.is_active ?? true } });
    } else {
      createMutation.mutate(payload);
    }
  };

  const columns: ColumnsType<BastionAsset> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '名称', dataIndex: 'name', key: 'name', width: 160 },
    {
      title: '协议',
      dataIndex: 'protocol',
      key: 'protocol',
      width: 80,
      render: (v: string) => <Tag color={PROTOCOL_COLOR[v] || 'default'}>{v.toUpperCase()}</Tag>,
    },
    { title: '主机', key: 'host', width: 220, render: (_: any, r: BastionAsset) => `${r.host}:${r.port}` },
    { title: '平台', dataIndex: 'platform', key: 'platform', width: 100 },
    { title: '用户名', dataIndex: 'username', key: 'username', width: 120 },
    {
      title: '标签',
      dataIndex: 'tags',
      key: 'tags',
      width: 160,
      render: (tags?: string[]) => (tags && tags.length ? tags.map((t) => <Tag key={t}>{t}</Tag>) : '-'),
    },
    {
      title: '状态',
      dataIndex: 'is_active',
      key: 'is_active',
      width: 80,
      render: (v: boolean) => <Tag color={v ? 'success' : 'default'}>{v ? '启用' : '停用'}</Tag>,
    },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 170, render: formatTime },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      render: (_: any, r: BastionAsset) => (
        <Space>
          <Tooltip title="通过 JumpServer SSO 连接">
            <Button size="small" icon={<LinkOutlined />} loading={connectMutation.isPending} onClick={() => connectMutation.mutate(r.id)}>
              连接
            </Button>
          </Tooltip>
          <a onClick={() => { form.setFieldsValue({ ...r, is_active: r.is_active }); setEditModal({ open: true, editing: r }); }}>编辑</a>
          <a
            style={{ color: '#ff4d4f' }}
            onClick={() =>
              modal.confirm({
                title: '删除资产',
                content: `确定删除资产「${r.name}」？`,
                okType: 'danger',
                onOk: () => deleteMutation.mutate(r.id),
              })
            }
          >
            删除
          </a>
        </Space>
      ),
    },
  ];

  return (
    <PageContainer
      title="堡垒机资产"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="协议"
            style={{ width: 120 }}
            value={protocol || undefined}
            onChange={(v) => setProtocol(v || '')}
            options={[
              { label: 'SSH', value: 'ssh' },
              { label: 'RDP', value: 'rdp' },
            ]}
          />
          <Input.Search allowClear placeholder="搜索名称/主机" style={{ width: 220 }} value={search} onChange={(e) => setSearch(e.target.value)} />
          <Button icon={<ReloadOutlined />} loading={syncMutation.isPending} onClick={() => syncMutation.mutate()}>
            同步 JumpServer
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => { form.resetFields(); form.setFieldsValue({ protocol: 'ssh', port: 22, platform: 'linux', is_active: true }); setEditModal({ open: true }); }}
          >
            新建资产
          </Button>
        </Space>
      }
    >
      <Card>
        <Table
          rowKey="id"
          loading={isLoading || isFetching}
          columns={columns}
          dataSource={data?.items || []}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无堡垒机资产" description="点击「同步 JumpServer」拉取资产，或新建手动资产" /> }}
        />
      </Card>

      <Modal
        title={editModal.editing ? '编辑资产' : '新建资产'}
        open={editModal.open}
        onCancel={() => setEditModal({ open: false })}
        onOk={handleSubmit}
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        destroyOnHidden
        width={560}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="name" label="资产名称" rules={[{ required: true, message: '请输入资产名称' }]}>
            <Input placeholder="如 web-prod-01" />
          </Form.Item>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="host" label="主机/IP" rules={[{ required: true, message: '请输入主机' }]} style={{ flex: 1 }}>
              <Input placeholder="10.0.0.1" />
            </Form.Item>
            <Form.Item name="port" label="端口" rules={[{ required: true }]} style={{ width: 110 }}>
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="protocol" label="协议" rules={[{ required: true }]} style={{ width: 120 }}>
              <Select options={[{ label: 'SSH', value: 'ssh' }, { label: 'RDP', value: 'rdp' }]} />
            </Form.Item>
            <Form.Item name="platform" label="平台" style={{ flex: 1 }}>
              <Input placeholder="linux / windows / macos" />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="username" label="登录用户" style={{ flex: 1 }}>
              <Input placeholder="root" />
            </Form.Item>
            <Form.Item name="credential_id" label="凭据ID" style={{ width: 160 }}>
              <InputNumber style={{ width: '100%' }} placeholder="可选" />
            </Form.Item>
          </Space>
          <Form.Item name="tags" label="标签">
            <Select mode="tags" placeholder="回车添加" tokenSeparators={[',']} />
          </Form.Item>
          <Form.Item name="comment" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
          {editModal.editing && (
            <Form.Item name="is_active" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title="连接资产"
        open={loginUrl.open}
        onCancel={() => setLoginUrl({ open: false })}
        footer={[
          <Button key="open" type="primary" href={loginUrl.url} target="_blank" rel="noreferrer">
            打开连接
          </Button>,
          <Button key="copy" onClick={() => { navigator.clipboard?.writeText(loginUrl.url || ''); message.success('已复制到剪贴板'); }}>
            复制链接
          </Button>,
          <Button key="close" onClick={() => setLoginUrl({ open: false })}>
            关闭
          </Button>,
        ]}
      >
        <p style={{ marginBottom: 12 }}>已通过 JumpServer 签发 SSO 连接 URL，点击下方按钮在新窗口打开会话：</p>
        <Input.TextArea rows={3} value={loginUrl.url} readOnly />
      </Modal>
    </PageContainer>
  );
}

export default BastionAssetsPage;
