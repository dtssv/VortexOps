import { useState } from 'react';
import {
  App,
  Button,
  Checkbox,
  Form,
  Input,
  Modal,
  Space,
  Table,
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
import type { ExternalToken } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

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

export default function MeTokensPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['ext-tokens', 'personal', { page, size }],
    queryFn: () => extApi.listTokens({ page, size }),
  });

  // Filter to personal tokens client-side; backend has no personal filter.
  const personalTokens = (data?.items || []).filter((t) => t.token_type === 'personal');
  const personalTotal = personalTokens.length;

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

  const columns: ColumnsType<ExternalToken> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '前缀', dataIndex: 'token_prefix', key: 'token_prefix', width: 140 },
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
    { title: '过期时间', dataIndex: 'expires_at', key: 'expires_at', width: 180, render: (t?: string) => (t ? formatTime(t) : '永不过期') },
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

  return (
    <PageContainer
      title="我的 Token"
      subtitle="管理个人 API 访问凭证"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          创建 Token
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={personalTokens}
        locale={{ emptyText: <EmptyState title="暂无个人 Token" /> }}
        pagination={{
          current: page,
          pageSize: size,
          total: personalTotal,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
      />

      <Modal
        title="创建个人 Token"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={520}
      >
        <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate({ ...v, token_type: 'personal' })}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：my-cli-token" />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes" rules={[{ required: true, message: '请至少选择一个 scope' }]}>
            <Checkbox.Group options={SCOPE_OPTIONS} />
          </Form.Item>
          <Form.Item name="expires_at" label="过期时间">
            <Input placeholder="ISO 时间，如 2026-12-31T23:59:59Z（留空则永不过期）" />
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
