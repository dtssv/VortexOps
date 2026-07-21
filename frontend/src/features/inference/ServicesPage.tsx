import { useState } from 'react';
import { Button, Drawer, Form, Input, InputNumber, Select, Space, Table, App, Typography } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { inferenceApi } from '@/api/inference';
import { clusterApi } from '@/api/clusters';
import type { InferenceService } from '@/types';
import { useUIStore } from '@/stores/uiStore';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const FRAMEWORK_OPTIONS = [
  { label: 'vLLM', value: 'vllm' },
  { label: 'TGI', value: 'tgi' },
  { label: 'Triton', value: 'triton' },
  { label: 'SGLang', value: 'sglang' },
  { label: 'Ollama', value: 'ollama' },
];

const ACCESS_MODE_OPTIONS = [
  { label: '内部（ClusterIP）', value: 'internal' },
  { label: '公网（LoadBalancer/Ingress）', value: 'external' },
];

export default function InferenceServicesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [deployOpen, setDeployOpen] = useState(false);
  const [deployForm] = Form.useForm();
  const [selectedModelId, setSelectedModelId] = useState<number | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['inference-services', { workspace_id: wsId, page, size }],
    queryFn: () => inferenceApi.listServices({ workspace_id: wsId ?? undefined, page, size }),
  });

  const { data: clustersData } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
    enabled: deployOpen,
  });

  const { data: modelsData } = useQuery({
    queryKey: ['models', wsId],
    queryFn: () => inferenceApi.listModels(wsId!),
    enabled: !!wsId && deployOpen,
  });

  const { data: modelVersions } = useQuery({
    queryKey: ['model-versions', selectedModelId],
    queryFn: () => inferenceApi.listModelVersions(selectedModelId!),
    enabled: !!selectedModelId,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<InferenceService>) => inferenceApi.createService({ ...body, workspace_id: wsId! }),
    onSuccess: async (svc) => {
      try {
        await inferenceApi.deploy(svc.id, {
          target_model_version_id: deployForm.getFieldValue('base_model_version_id'),
          replicas: deployForm.getFieldValue('replicas'),
        });
        message.success('服务已创建并开始部署');
      } catch (e: any) {
        message.warning(`服务已创建，但部署失败：${e?.message || '未知错误'}`);
      }
      setDeployOpen(false);
      deployForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['inference-services'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => inferenceApi.deleteService(id),
    onSuccess: () => {
      message.success('服务已删除');
      queryClient.invalidateQueries({ queryKey: ['inference-services'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<InferenceService> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '应用/分组',
      key: 'app_group',
      width: 160,
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          {record.application_id ? (
            <Typography.Link onClick={() => navigate(`/applications/${record.application_id}`)}>
              应用 #{record.application_id}
            </Typography.Link>
          ) : (
            <Typography.Text type="secondary">-</Typography.Text>
          )}
          {record.group_id ? (
            <Typography.Link onClick={() => navigate(`/groups/${record.group_id}`)}>
              分组 #{record.group_id}
            </Typography.Link>
          ) : null}
        </Space>
      ),
    },
    { title: '框架', dataIndex: 'framework', key: 'framework', width: 100 },
    { title: '副本', dataIndex: 'replicas', key: 'replicas', width: 80 },
    { title: 'GPU 数', dataIndex: 'gpu_count', key: 'gpu_count', width: 90 },
    { title: 'GPU 型号', dataIndex: 'gpu_type', key: 'gpu_type', width: 120, render: (v?: string) => v || '-' },
    {
      title: '当前状态',
      dataIndex: 'current_status',
      key: 'current_status',
      width: 110,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '就绪',
      dataIndex: 'readiness',
      key: 'readiness',
      width: 110,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '访问模式', dataIndex: 'access_mode', key: 'access_mode', width: 100 },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => navigate(`/inference/services/${record.id}`)}>
            查看
          </Button>
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除推理服务',
                content: `确定删除服务「${record.name}」吗？`,
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
      title="推理服务"
      subtitle="部署并管理大模型推理服务"
      extra={
        <Button type="primary" icon={<PlusOutlined />} disabled={!wsId} onClick={() => setDeployOpen(true)}>
          部署推理服务
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无推理服务" /> }}
        onRow={(record) => ({ onClick: () => navigate(`/inference/services/${record.id}`), style: { cursor: 'pointer' } })}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total || 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => {
            setPage(p);
            setSize(s);
          },
        }}
      />

      <Drawer
        title="部署推理服务"
        open={deployOpen}
        onClose={() => setDeployOpen(false)}
        width={520}
        destroyOnHidden
        extra={
          <Button type="primary" loading={createMutation.isPending} onClick={() => deployForm.submit()}>
            部署
          </Button>
        }
      >
        <Form layout="vertical" form={deployForm} onFinish={(v) => createMutation.mutate(v)} initialValues={{ framework: 'vllm', replicas: 1, gpu_count: 1, tensor_parallel_size: 1, pipeline_parallel_size: 1, access_mode: 'internal' }} onValuesChange={(changed) => {
          if (changed.model_id) setSelectedModelId(changed.model_id);
        }}>
          <Form.Item name="name" label="服务名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：llama3-8b-service" />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="cluster_id" label="集群" rules={[{ required: true, message: '请选择集群' }]} style={{ flex: 1 }}>
              <Select
                placeholder="选择集群"
                options={(clustersData?.items || []).map((c) => ({ label: c.name, value: c.id }))}
              />
            </Form.Item>
            <Form.Item name="namespace" label="命名空间" rules={[{ required: true, message: '请输入命名空间' }]} style={{ flex: 1 }}>
              <Input placeholder="如：default" />
            </Form.Item>
          </Space>
          <Form.Item name="model_id" label="模型" rules={[{ required: true, message: '请选择模型' }]}>
            <Select
              placeholder="选择模型"
              options={(modelsData || []).map((m) => ({ label: m.display_name || m.name, value: m.id }))}
            />
          </Form.Item>
          <Form.Item name="base_model_version_id" label="模型版本" rules={[{ required: true, message: '请选择模型版本' }]}>
            <Select
              placeholder="选择模型版本"
              notFoundContent={selectedModelId ? '暂无版本' : '请先选择模型'}
              options={(modelVersions || []).map((v) => ({
                label: `${v.version} · ${v.precision}${v.quantization && v.quantization !== 'none' ? ` · ${v.quantization}` : ''} · ${v.download_status === 'ready' ? '已下载' : '未下载'}`,
                value: v.id,
                disabled: v.download_status !== 'ready',
              }))}
            />
          </Form.Item>
          <Form.Item name="framework" label="推理框架" rules={[{ required: true }]}>
            <Select options={FRAMEWORK_OPTIONS} />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="replicas" label="副本数" style={{ flex: 1 }} rules={[{ required: true }]}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="gpu_count" label="GPU 数" style={{ flex: 1 }} rules={[{ required: true }]}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="gpu_type" label="GPU 型号" style={{ flex: 1 }}>
              <Input placeholder="如：A100" />
            </Form.Item>
          </Space>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="tensor_parallel_size" label="张量并行" style={{ flex: 1 }}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="pipeline_parallel_size" label="流水线并行" style={{ flex: 1 }}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
          </Space>
          <Form.Item name="access_mode" label="访问模式" rules={[{ required: true }]}>
            <Select options={ACCESS_MODE_OPTIONS} placeholder="选择访问模式" />
          </Form.Item>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            提示：仅「已下载」状态的版本可选。如需下载，请在「模型管理 → 查看版本」中触发。
          </Typography.Text>
        </Form>
      </Drawer>
    </PageContainer>
  );
}
