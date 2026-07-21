import { useState } from 'react';
import {
  App,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
  message as staticMessage,
} from 'antd';
import { PlusOutlined, CopyOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { extApi } from '@/api/extapi';
import type { ExternalCallLog, ExternalToken } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatDuration } from '@/utils/format';

const SCOPE_OPTIONS = [
  { label: 'workspace:read', value: 'ext:workspace:read' },
  { label: 'workspace:write', value: 'ext:workspace:write' },
  { label: 'application:read', value: 'ext:application:read' },
  { label: 'application:write', value: 'ext:application:write' },
  { label: 'build:trigger', value: 'ext:build:trigger' },
  { label: 'release:trigger', value: 'ext:release:trigger' },
  { label: 'cluster:read', value: 'ext:cluster:read' },
  { label: 'inference:call', value: 'ext:inference:call' },
];

const TOKEN_TYPE_OPTIONS = [
  { label: '服务 Token', value: 'service' },
  { label: '个人 Token', value: 'personal' },
];

const METHOD_COLOR: Record<string, string> = {
  GET: 'blue',
  POST: 'green',
  PUT: 'orange',
  DELETE: 'red',
  PATCH: 'purple',
};

export default function ExtTokensPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState('tokens');

  const [tokPage, setTokPage] = useState(1);
  const [tokSize, setTokSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const [logPage, setLogPage] = useState(1);
  const [logSize, setLogSize] = useState(20);

  const { data: tokData, isLoading: tokLoading } = useQuery({
    queryKey: ['ext-tokens', { page: tokPage, size: tokSize }],
    queryFn: () => extApi.listTokens({ page: tokPage, size: tokSize }),
    enabled: tab === 'tokens',
  });

  const { data: logData, isLoading: logLoading } = useQuery({
    queryKey: ['ext-call-logs', { page: logPage, size: logSize }],
    queryFn: () => extApi.listCallLogs({ page: logPage, size: logSize }),
    enabled: tab === 'logs',
  });

  const createMutation = useMutation({
    mutationFn: (body: Parameters<typeof extApi.createToken>[0]) => extApi.createToken(body),
    onSuccess: (tok) => {
      message.success('Token 已创建');
      setCreateOpen(false);
      createForm.resetFields();
      if ((tok as ExternalToken).plaintext) setPlaintext((tok as ExternalToken).plaintext!);
      queryClient.invalidateQueries({ queryKey: ['ext-tokens'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const revokeMutation = useMutation({
    mutationFn: (id: number) => extApi.revokeToken(id),
    onSuccess: () => {
      message.success('Token 已吊销');
      queryClient.invalidateQueries({ queryKey: ['ext-tokens'] });
    },
    onError: (e: any) => message.error(e?.message || '吊销失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => extApi.deleteToken(id),
    onSuccess: () => {
      message.success('Token 已删除');
      queryClient.invalidateQueries({ queryKey: ['ext-tokens'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const tokenColumns: ColumnsType<ExternalToken> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '前缀', dataIndex: 'token_prefix', key: 'token_prefix', width: 140 },
    { title: '类型', dataIndex: 'token_type', key: 'token_type', width: 100, render: (v: string) => <Tag>{v}</Tag> },
    {
      title: 'Scopes',
      dataIndex: 'scopes',
      key: 'scopes',
      render: (scopes: string[]) => (
        <Space size={[4, 4]} wrap>
          {(scopes || []).map((s) => (
            <Tag key={s} color="blue">
              {s}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '最近使用', dataIndex: 'last_used_at', key: 'last_used_at', width: 180, render: (t?: string) => (t ? formatTime(t) : '-') },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      render: (_, record) => (
        <Space>
          {record.status !== 'revoked' && (
            <Button
              type="link"
              size="small"
              danger
              onClick={() =>
                confirmDanger({
                  title: '吊销 Token',
                  content: `确定吊销 Token「${record.name}」吗？此操作不可逆。`,
                  onOk: () => revokeMutation.mutateAsync(record.id),
                })
              }
            >
              吊销
            </Button>
          )}
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除 Token',
                content: `确定删除 Token「${record.name}」吗？`,
                onOk: () => deleteMutation.mutateAsync(record.id),
              })
            }
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  const logColumns: ColumnsType<ExternalCallLog> = [
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '方法',
      dataIndex: 'method',
      key: 'method',
      width: 80,
      render: (v: string) => <Tag color={METHOD_COLOR[v] || 'default'}>{v}</Tag>,
    },
    { title: '路径', dataIndex: 'path', key: 'path', ellipsis: true },
    { title: '操作', dataIndex: 'operation', key: 'operation', width: 160, render: (v?: string) => v || '-' },
    { title: '状态码', dataIndex: 'status_code', key: 'status_code', width: 90, render: (v?: number) => v ?? '-' },
    { title: '耗时', dataIndex: 'duration_ms', key: 'duration_ms', width: 100, render: (v?: number) => formatDuration(v) },
    { title: 'Token 前缀', dataIndex: 'token_prefix', key: 'token_prefix', width: 120, render: (v?: string) => v || '-' },
    { title: '客户端 IP', dataIndex: 'client_ip', key: 'client_ip', width: 140, render: (v?: string) => v || '-' },
  ];

  return (
    <PageContainer title="外部 API Token" subtitle="管理对外 API 访问凭证与调用记录">
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'tokens',
            label: 'Token',
            children: (
              <>
                <Space style={{ marginBottom: 16 }}>
                  <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                    创建 Token
                  </Button>
                </Space>
                <Table
                  rowKey="id"
                  loading={tokLoading}
                  columns={tokenColumns}
                  dataSource={tokData?.items || []}
                  locale={{ emptyText: <EmptyState title="暂无 Token" /> }}
                  pagination={{
                    current: tokPage,
                    pageSize: tokSize,
                    total: tokData?.total || 0,
                    showSizeChanger: true,
                    showTotal: (t) => `共 ${t} 条`,
                    onChange: (p, s) => { setTokPage(p); setTokSize(s); },
                  }}
                />
              </>
            ),
          },
          {
            key: 'logs',
            label: '调用记录',
            children: (
              <Table
                rowKey="id"
                loading={logLoading}
                columns={logColumns}
                dataSource={logData?.items || []}
                locale={{ emptyText: <EmptyState title="暂无调用记录" /> }}
                pagination={{
                  current: logPage,
                  pageSize: logSize,
                  total: logData?.total || 0,
                  showSizeChanger: true,
                  showTotal: (t) => `共 ${t} 条`,
                  onChange: (p, s) => { setLogPage(p); setLogSize(s); },
                }}
              />
            ),
          },
        ]}
      />

      <Modal
        title="创建 Token"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={520}
      >
        <Form
          layout="vertical"
          form={createForm}
          onFinish={(v) => createMutation.mutate(v)}
          initialValues={{ token_type: 'service' }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：ci-deploy-token" />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes" rules={[{ required: true, message: '请至少选择一个 scope' }]}>
            <Checkbox.Group options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="token_type" label="类型" rules={[{ required: true }]}>
            <Select options={TOKEN_TYPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="rate_limit_per_min" label="速率限制/min">
            <InputNumber min={0} style={{ width: '100%' }} placeholder="留空表示不限" />
          </Form.Item>
          <Form.Item name="expires_at" label="过期时间">
            <Input placeholder="ISO 时间，如 2026-12-31T23:59:59Z" />
          </Form.Item>
          <Form.Item name="webhook_url" label="Webhook URL">
            <Input placeholder="https://..." />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="Token 已生成"
        open={!!plaintext}
        onCancel={() => setPlaintext(null)}
        onOk={() => setPlaintext(null)}
        okText="我已保存"
        cancelText="关闭"
        destroyOnHidden
      >
        <Typography.Paragraph type="warning">
          请立即复制并妥善保存，此 Token 仅显示一次。
        </Typography.Paragraph>
        <Space.Compact style={{ width: '100%' }}>
          <Input value={plaintext || ''} readOnly style={{ width: 'calc(100% - 90px)' }} />
          <Button
            type="primary"
            icon={<CopyOutlined />}
            onClick={() => {
              navigator.clipboard.writeText(plaintext || '');
              staticMessage.success('已复制');
            }}
          >
            复制
          </Button>
        </Space.Compact>
      </Modal>
    </PageContainer>
  );
}
