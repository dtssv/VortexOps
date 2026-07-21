import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  App,
  Button,
  Card,
  Col,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Table,
  Tabs,
  Tag,
  Typography,
  message as staticMessage,
} from 'antd';
import { ArrowLeftOutlined, CopyOutlined, SendOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { inferenceApi } from '@/api/inference';
import type { InferenceAPIKey, InferenceRelease, InferenceUsage, InferenceUsageSummary } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatDuration } from '@/utils/format';

const FRAMEWORK_OPTIONS = [
  { label: 'vLLM', value: 'vllm' },
  { label: 'TGI', value: 'tgi' },
  { label: 'Triton', value: 'triton' },
];

export default function InferenceServiceDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const serviceId = Number(id);
  const [tab, setTab] = useState('overview');
  const [scaleOpen, setScaleOpen] = useState(false);
  const [scaleForm] = Form.useForm();
  const [switchOpen, setSwitchOpen] = useState(false);
  const [switchForm] = Form.useForm();
  const [keyOpen, setKeyOpen] = useState(false);
  const [keyForm] = Form.useForm();
  const [plaintext, setPlaintext] = useState<string | null>(null);

  const { data: svc, isLoading } = useQuery({
    queryKey: ['inference-service', serviceId],
    queryFn: () => inferenceApi.getService(serviceId),
    enabled: !!serviceId && !Number.isNaN(serviceId),
  });

  const { data: releases } = useQuery({
    queryKey: ['inference-releases', serviceId],
    queryFn: () => inferenceApi.listReleases(serviceId),
    enabled: !!serviceId && tab === 'history',
  });

  const { data: apiKeys } = useQuery({
    queryKey: ['inference-api-keys', serviceId],
    queryFn: () => inferenceApi.listAPIKeys(serviceId),
    enabled: !!serviceId && tab === 'apikey',
  });

  const { data: usageSummary } = useQuery({
    queryKey: ['inference-usage-summary', serviceId],
    queryFn: () => inferenceApi.usageSummary(serviceId),
    enabled: !!serviceId && tab === 'usage',
  });

  const { data: usagePage } = useQuery({
    queryKey: ['inference-usage', serviceId],
    queryFn: () => inferenceApi.listUsage(serviceId, { page: 1, size: 50 }),
    enabled: !!serviceId && tab === 'usage',
  });

  const scaleMutation = useMutation({
    mutationFn: (replicas: number) => inferenceApi.scale(serviceId, { replicas }),
    onSuccess: () => {
      message.success('扩缩容已触发');
      setScaleOpen(false);
      scaleForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['inference-service', serviceId] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const switchMutation = useMutation({
    mutationFn: (v: { target_model_version_id: number; replicas?: number }) =>
      inferenceApi.deploy(serviceId, { target_model_version_id: v.target_model_version_id, replicas: v.replicas }),
    onSuccess: () => {
      message.success('模型切换已触发');
      setSwitchOpen(false);
      switchForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['inference-service', serviceId] });
      queryClient.invalidateQueries({ queryKey: ['inference-releases', serviceId] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const rollbackMutation = useMutation({
    mutationFn: (releaseId: number) => inferenceApi.rollback(serviceId, { release_id: releaseId }),
    onSuccess: () => {
      message.success('回滚已触发');
      queryClient.invalidateQueries({ queryKey: ['inference-service', serviceId] });
      queryClient.invalidateQueries({ queryKey: ['inference-releases', serviceId] });
    },
    onError: (e: any) => message.error(e?.message || '回滚失败'),
  });

  const createKeyMutation = useMutation({
    mutationFn: (v: { name: string; daily_token_quota?: number; rate_limit_per_min?: number; expires_at?: string }) =>
      inferenceApi.createAPIKey(serviceId, v),
    onSuccess: (key) => {
      message.success('Key 已签发');
      setKeyOpen(false);
      keyForm.resetFields();
      if ((key as InferenceAPIKey).plaintext) setPlaintext((key as InferenceAPIKey).plaintext!);
      queryClient.invalidateQueries({ queryKey: ['inference-api-keys', serviceId] });
    },
    onError: (e: any) => message.error(e?.message || '签发失败'),
  });

  const revokeKeyMutation = useMutation({
    mutationFn: (keyId: number) => inferenceApi.revokeAPIKey(serviceId, keyId),
    onSuccess: () => {
      message.success('Key 已吊销');
      queryClient.invalidateQueries({ queryKey: ['inference-api-keys', serviceId] });
    },
    onError: (e: any) => message.error(e?.message || '吊销失败'),
  });

  if (isLoading) {
    return (
      <PageContainer title="推理服务详情">
        <Spin />
      </PageContainer>
    );
  }

  if (!svc) {
    return (
      <PageContainer title="推理服务详情">
        <EmptyState title="服务不存在" />
      </PageContainer>
    );
  }

  const releaseColumns: ColumnsType<InferenceRelease> = [
    { title: '编号', dataIndex: 'release_number', key: 'release_number', width: 80 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '目标模型版本', dataIndex: 'target_model_version_id', key: 'target_model_version_id', width: 140 },
    { title: '副本', dataIndex: 'replicas', key: 'replicas', width: 80 },
    { title: '策略', dataIndex: 'strategy', key: 'strategy', width: 100 },
    { title: '进度', dataIndex: 'progress_percent', key: 'progress_percent', width: 80, render: (v: number) => `${v ?? 0}%` },
    { title: '开始时间', dataIndex: 'started_at', key: 'started_at', width: 180, render: (t: string) => formatTime(t) },
    { title: '耗时', dataIndex: 'duration_ms', key: 'duration_ms', width: 100, render: (v?: number) => formatDuration(v) },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, record) =>
        record.id !== svc.current_release_id && (
          <Button
            type="link"
            size="small"
            onClick={() =>
              confirmDanger({
                title: '回滚',
                content: `确定回滚到发布 #${record.release_number} 吗？`,
                onOk: () => rollbackMutation.mutateAsync(record.id),
              })
            }
          >
            回滚
          </Button>
        ),
    },
  ];

  const apiKeyColumns: ColumnsType<InferenceAPIKey> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'key_prefix', width: 140 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '每日 Token 配额', dataIndex: 'daily_token_quota', key: 'daily_token_quota', width: 140, render: (v?: number) => v ?? '-' },
    { title: '速率限制/min', dataIndex: 'rate_limit_per_min', key: 'rate_limit_per_min', width: 130, render: (v?: number) => v ?? '-' },
    { title: '最近使用', dataIndex: 'last_used_at', key: 'last_used_at', width: 180, render: (t?: string) => (t ? formatTime(t) : '-') },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, record) => (
        <Button
          type="link"
          size="small"
          danger
          onClick={() =>
            confirmDanger({
              title: '吊销 Key',
              content: `确定吊销 Key「${record.name}」吗？此操作不可逆。`,
              onOk: () => revokeKeyMutation.mutateAsync(record.id),
            })
          }
        >
          吊销
        </Button>
      ),
    },
  ];

  const usageColumns: ColumnsType<InferenceUsage> = [
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    { title: 'Prompt Tokens', dataIndex: 'prompt_tokens', key: 'prompt_tokens', width: 140 },
    { title: 'Completion Tokens', dataIndex: 'completion_tokens', key: 'completion_tokens', width: 160 },
    { title: '总 Tokens', dataIndex: 'total_tokens', key: 'total_tokens', width: 120 },
    { title: '耗时', dataIndex: 'duration_ms', key: 'duration_ms', width: 100, render: (v?: number) => formatDuration(v) },
    { title: '状态码', dataIndex: 'status_code', key: 'status_code', width: 90, render: (v?: number) => v ?? '-' },
  ];

  const summary = usageSummary as InferenceUsageSummary | undefined;

  return (
    <PageContainer
      title={svc.name}
      subtitle={svc.uuid}
      breadcrumb={[{ title: '推理服务', path: '/inference/services' }, { title: svc.name }]}
      extra={
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/inference/services')}>
          返回
        </Button>
      }
    >
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'overview',
            label: '概览',
            children: (
              <>
                <Space style={{ marginBottom: 16 }}>
                  <Button onClick={() => { setScaleOpen(true); scaleForm.setFieldsValue({ replicas: svc.replicas }); }}>
                    扩缩容
                  </Button>
                  <Button onClick={() => setSwitchOpen(true)}>切换模型</Button>
                  <Button
                    danger
                    onClick={() =>
                      confirmDanger({
                        title: '回滚到上一版本',
                        content: '将回滚到上一个发布版本，确定吗？',
                        onOk: async () => {
                          if (svc.current_release_id) await rollbackMutation.mutateAsync(svc.current_release_id);
                        },
                      })
                    }
                  >
                    回滚
                  </Button>
                </Space>
                <Descriptions bordered column={2} size="small">
                  <Descriptions.Item label="状态">
                    <ResourceStatus status={svc.current_status} />
                  </Descriptions.Item>
                  <Descriptions.Item label="就绪">
                    <ResourceStatus status={svc.readiness} />
                  </Descriptions.Item>
                  <Descriptions.Item label="框架">{svc.framework}</Descriptions.Item>
                  <Descriptions.Item label="副本">{svc.replicas}</Descriptions.Item>
                  <Descriptions.Item label="GPU 数">{svc.gpu_count}</Descriptions.Item>
                  <Descriptions.Item label="GPU 型号">{svc.gpu_type || '-'}</Descriptions.Item>
                  <Descriptions.Item label="张量并行">{svc.tensor_parallel_size}</Descriptions.Item>
                  <Descriptions.Item label="流水线并行">{svc.pipeline_parallel_size}</Descriptions.Item>
                  <Descriptions.Item label="命名空间">{svc.namespace}</Descriptions.Item>
                  <Descriptions.Item label="访问模式">{svc.access_mode}</Descriptions.Item>
                  <Descriptions.Item label="模型版本 ID">{svc.base_model_version_id}</Descriptions.Item>
                  <Descriptions.Item label="外部端点">{svc.external_endpoint || '-'}</Descriptions.Item>
                </Descriptions>
              </>
            ),
          },
          {
            key: 'history',
            label: '变更历史',
            children: (
              <Table
                rowKey="id"
                size="small"
                columns={releaseColumns}
                dataSource={releases || []}
                locale={{ emptyText: <EmptyState title="暂无变更记录" /> }}
                pagination={false}
              />
            ),
          },
          {
            key: 'apikey',
            label: 'API Key',
            children: (
              <>
                <Space style={{ marginBottom: 16 }}>
                  <Button type="primary" onClick={() => setKeyOpen(true)}>
                    签发 Key
                  </Button>
                </Space>
                <Table
                  rowKey="id"
                  size="small"
                  columns={apiKeyColumns}
                  dataSource={apiKeys || []}
                  locale={{ emptyText: <EmptyState title="暂无 API Key" /> }}
                  pagination={false}
                />
              </>
            ),
          },
          {
            key: 'usage',
            label: '用量',
            children: (
              <>
                <Row gutter={16} style={{ marginBottom: 16 }}>
                  <Col span={6}>
                    <Card>
                      <Statistic title="总请求数" value={summary?.total_requests ?? 0} />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card>
                      <Statistic title="Prompt Tokens" value={summary?.total_prompt_tokens ?? 0} />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card>
                      <Statistic title="Completion Tokens" value={summary?.total_completion_tokens ?? 0} />
                    </Card>
                  </Col>
                  <Col span={6}>
                    <Card>
                      <Statistic title="平均耗时" value={summary?.avg_duration_ms ?? 0} suffix="ms" />
                    </Card>
                  </Col>
                </Row>
                <Table
                  rowKey="id"
                  size="small"
                  columns={usageColumns}
                  dataSource={usagePage?.items || []}
                  locale={{ emptyText: <EmptyState title="暂无用量记录" /> }}
                  pagination={false}
                />
              </>
            ),
          },
          {
            key: 'playground',
            label: 'Playground',
            children: <Playground serviceId={serviceId} />,
          },
        ]}
      />

      <Modal
        title="扩缩容"
        open={scaleOpen}
        onCancel={() => setScaleOpen(false)}
        onOk={() => scaleForm.submit()}
        confirmLoading={scaleMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={scaleForm} onFinish={(v) => scaleMutation.mutate(v.replicas)}>
          <Form.Item name="replicas" label="副本数" rules={[{ required: true, message: '请输入副本数' }]}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="切换模型"
        open={switchOpen}
        onCancel={() => setSwitchOpen(false)}
        onOk={() => switchForm.submit()}
        confirmLoading={switchMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={switchForm} onFinish={(v) => switchMutation.mutate(v)}>
          <Form.Item name="target_model_version_id" label="目标模型版本 ID" rules={[{ required: true, message: '请输入版本 ID' }]}>
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="replicas" label="副本数">
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="签发 API Key"
        open={keyOpen}
        onCancel={() => setKeyOpen(false)}
        onOk={() => keyForm.submit()}
        confirmLoading={createKeyMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={keyForm} onFinish={(v) => createKeyMutation.mutate(v)}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：prod-key" />
          </Form.Item>
          <Form.Item name="daily_token_quota" label="每日 Token 配额">
            <InputNumber min={0} style={{ width: '100%' }} placeholder="留空表示不限" />
          </Form.Item>
          <Form.Item name="rate_limit_per_min" label="速率限制/min">
            <InputNumber min={0} style={{ width: '100%' }} placeholder="留空表示不限" />
          </Form.Item>
          <Form.Item name="expires_at" label="过期时间">
            <Input placeholder="ISO 时间，如 2026-12-31T23:59:59Z" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="API Key 已生成"
        open={!!plaintext}
        onCancel={() => setPlaintext(null)}
        onOk={() => setPlaintext(null)}
        okText="我已保存"
        cancelText="关闭"
        destroyOnHidden
      >
        <Typography.Paragraph type="warning">
          请立即复制并妥善保存，此 Key 仅显示一次。
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

// Playground 组件：通过推理网关代理 OpenAI 兼容接口测试对话。
function Playground({ serviceId }: { serviceId: number }) {
  const [apiKey, setApiKey] = useState('');
  const [prompt, setPrompt] = useState('你好，请介绍一下自己。');
  const [output, setOutput] = useState('');
  const [loading, setLoading] = useState(false);
  const baseURL = import.meta.env.VITE_API_BASE || '/api/v1';

  const send = async () => {
    if (!apiKey) {
      staticMessage.warning('请输入 API Key');
      return;
    }
    setLoading(true);
    setOutput('');
    try {
      const resp = await fetch(`${baseURL}/inference-services/${serviceId}/v1/chat/completions`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          model: 'default',
          messages: [{ role: 'user', content: prompt }],
          stream: false,
        }),
      });
      const data = await resp.json();
      if (!resp.ok) {
        setOutput(`错误 ${resp.status}: ${data?.error?.message || JSON.stringify(data)}`);
      } else {
        setOutput(data?.choices?.[0]?.message?.content || JSON.stringify(data, null, 2));
      }
    } catch (e: any) {
      setOutput(`请求失败: ${e?.message || e}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Row gutter={16}>
      <Col span={12}>
        <Card title="输入" size="small">
          <Space.Compact style={{ width: '100%', marginBottom: 12 }}>
            <Input.Password
              placeholder="API Key（voi_ 开头）"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
            />
            <Button type="primary" icon={<SendOutlined />} loading={loading} onClick={send}>
              发送
            </Button>
          </Space.Compact>
          <Input.TextArea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            rows={10}
            placeholder="输入 Prompt"
          />
        </Card>
      </Col>
      <Col span={12}>
        <Card title="输出" size="small">
          <Input.TextArea value={output} rows={14} readOnly placeholder="模型回复将显示在这里" />
        </Card>
      </Col>
    </Row>
  );
}
