import { useState } from 'react';
import { Button, Descriptions, Drawer, Form, Input, Modal, Select, Space, Table, Tag, App, Typography, InputNumber, Popover } from 'antd';
import { PlusOutlined, CloudServerOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { inferenceApi } from '@/api/inference';
import { workspaceApi } from '@/api/workspaces';
import { clusterApi } from '@/api/clusters';
import type { Model, ModelVersion, ModelRegistry } from '@/types';
import { useUIStore } from '@/stores/uiStore';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatBytes } from '@/utils/format';

const PROVIDER_OPTIONS = [
  { label: 'HuggingFace', value: 'huggingface' },
  { label: 'OSS（阿里云）', value: 'oss' },
  { label: 'S3', value: 's3' },
  { label: 'Local', value: 'local' },
  { label: 'Custom', value: 'custom' },
];

const PRECISION_OPTIONS = [
  { label: 'FP32', value: 'fp32' },
  { label: 'FP16', value: 'fp16' },
  { label: 'BF16', value: 'bf16' },
  { label: 'INT8', value: 'int8' },
  { label: 'INT4', value: 'int4' },
];

const QUANT_OPTIONS = [
  { label: 'None', value: 'none' },
  { label: 'GPTQ', value: 'gptq' },
  { label: 'AWQ', value: 'awq' },
  { label: 'SqueezeLLM', value: 'squeezellm' },
];

const FRAMEWORK_OPTIONS = [
  { label: 'vLLM', value: 'vllm' },
  { label: 'TGI', value: 'tgi' },
  { label: 'Triton', value: 'triton' },
  { label: 'SGLang', value: 'sglang' },
  { label: 'Ollama', value: 'ollama' },
];

export default function InferenceModelsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const storeWsId = useUIStore((s) => s.currentWorkspaceId);
  const [wsId, setWsId] = useState<number | null>(storeWsId);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [versionDrawer, setVersionDrawer] = useState<{ open: boolean; model?: Model }>({ open: false });
  const [registryOpen, setRegistryOpen] = useState(false);
  const [registryForm] = Form.useForm();
  const [versionCreateOpen, setVersionCreateOpen] = useState(false);
  const [versionForm] = Form.useForm();
  const [downloadOpen, setDownloadOpen] = useState<{ open: boolean; version?: ModelVersion }>({ open: false });
  const [downloadForm] = Form.useForm();

  const { data: wsPage } = useQuery({
    queryKey: ['workspaces', { page: 1, size: 200 }],
    queryFn: () => workspaceApi.list({ page: 1, size: 200 }),
  });

  const effectiveWsId = wsId ?? wsPage?.items?.[0]?.id ?? null;

  const { data: models, isLoading } = useQuery({
    queryKey: ['models', effectiveWsId],
    queryFn: () => inferenceApi.listModels(effectiveWsId!),
    enabled: !!effectiveWsId,
  });

  const { data: registries } = useQuery({
    queryKey: ['model-registries', effectiveWsId],
    queryFn: () => inferenceApi.listRegistries(effectiveWsId!),
    enabled: !!effectiveWsId,
  });

  const { data: versions, isLoading: versionsLoading, refetch: refetchVersions } = useQuery({
    queryKey: ['model-versions', versionDrawer.model?.id],
    queryFn: () => inferenceApi.listModelVersions(versionDrawer.model!.id),
    enabled: !!versionDrawer.model?.id,
    refetchInterval: versionDrawer.open ? 5000 : false,
  });

  const { data: clustersData } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
    enabled: downloadOpen.open,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Model>) => inferenceApi.createModel({ ...body, workspace_id: effectiveWsId! }),
    onSuccess: () => {
      message.success('模型已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['models', effectiveWsId] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => inferenceApi.deleteModel(id),
    onSuccess: () => {
      message.success('模型已删除');
      queryClient.invalidateQueries({ queryKey: ['models', effectiveWsId] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const createRegistryMutation = useMutation({
    mutationFn: (body: Partial<ModelRegistry>) =>
      inferenceApi.createRegistry({ ...body, workspace_id: effectiveWsId! }),
    onSuccess: () => {
      message.success('模型仓库已创建');
      setRegistryOpen(false);
      registryForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['model-registries', effectiveWsId] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteRegistryMutation = useMutation({
    mutationFn: (id: number) => inferenceApi.deleteRegistry(id),
    onSuccess: () => {
      message.success('仓库已删除');
      queryClient.invalidateQueries({ queryKey: ['model-registries', effectiveWsId] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const createVersionMutation = useMutation({
    mutationFn: (v: Partial<ModelVersion>) =>
      inferenceApi.createModelVersion(versionDrawer.model!.id, { ...v, is_default: false }),
    onSuccess: () => {
      message.success('版本已创建');
      setVersionCreateOpen(false);
      versionForm.resetFields();
      refetchVersions();
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const downloadMutation = useMutation({
    mutationFn: ({ id, cluster_id, namespace }: { id: number; cluster_id: number; namespace: string }) =>
      inferenceApi.downloadModelVersion(id, { cluster_id, namespace }),
    onSuccess: () => {
      message.success('下载已触发，请稍候');
      setDownloadOpen({ open: false });
      downloadForm.resetFields();
      refetchVersions();
    },
    onError: (e: any) => message.error(e?.message || '触发下载失败'),
  });

  const columns: ColumnsType<Model> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '显示名称', dataIndex: 'display_name', key: 'display_name', render: (v?: string) => v || '-' },
    { title: '基础架构', dataIndex: 'base_architecture', key: 'base_architecture', render: (v?: string) => v || '-' },
    { title: '参数量', dataIndex: 'parameter_count', key: 'parameter_count', render: (v?: string) => v || '-' },
    { title: 'License', dataIndex: 'license', key: 'license', render: (v?: string) => v || '-' },
    {
      title: '操作',
      key: 'actions',
      width: 240,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => setVersionDrawer({ open: true, model: record })}>
            查看版本
          </Button>
          <Button type="link" size="small" onClick={() => navigate('/inference/services')}>
            服务管理
          </Button>
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除模型',
                content: `确定删除模型「${record.name}」吗？`,
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

  const versionColumns: ColumnsType<ModelVersion> = [
    { title: '版本', dataIndex: 'version', key: 'version', width: 120 },
    { title: '精度', dataIndex: 'precision', key: 'precision', width: 100 },
    { title: '量化', dataIndex: 'quantization', key: 'quantization', width: 120, render: (v?: string) => v || '-' },
    { title: '框架', dataIndex: 'framework', key: 'framework', width: 120 },
    {
      title: '下载状态',
      dataIndex: 'download_status',
      key: 'download_status',
      width: 120,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '进度',
      dataIndex: 'download_progress',
      key: 'download_progress',
      width: 120,
      render: (v: number) => `${v ?? 0}%`,
    },
    {
      title: '默认',
      dataIndex: 'is_default',
      key: 'is_default',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">默认</Tag> : '-'),
    },
    { title: '大小', dataIndex: 'weights_size_bytes', key: 'weights_size_bytes', width: 100, render: (v?: number) => formatBytes(v) },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '操作',
      key: 'actions',
      width: 180,
      render: (_, record) => (
        <Space>
          {record.download_status !== 'ready' && (
            <Button
              type="link"
              size="small"
              disabled={record.download_status === 'downloading'}
              onClick={() => setDownloadOpen({ open: true, version: record })}
            >
              {record.download_status === 'downloading' ? '下载中' : '下载'}
            </Button>
          )}
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除版本',
                content: `确定删除版本「${record.version}」吗？`,
                onOk: async () => {
                  await inferenceApi.deleteModelVersion(record.id);
                  message.success('已删除');
                  refetchVersions();
                },
              })
            }
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  const registryList = registries || [];

  return (
    <PageContainer
      title="模型管理"
      subtitle="管理 AI 模型仓库、模型与版本"
      extra={
        <Space>
          <Select
            placeholder="选择工作空间"
            style={{ width: 200 }}
            value={effectiveWsId ?? undefined}
            onChange={(v) => setWsId(v)}
            options={(wsPage?.items || []).map((ws) => ({ label: ws.display_name || ws.name, value: ws.id }))}
          />
          <Popover
            trigger="click"
            placement="bottomRight"
            content={
              <div style={{ width: 420 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 8 }}>
                  <Typography.Text strong>模型仓库</Typography.Text>
                  <Button type="link" size="small" icon={<PlusOutlined />} onClick={() => setRegistryOpen(true)}>
                    新建
                  </Button>
                </div>
                <Table
                  rowKey="id"
                  size="small"
                  pagination={false}
                  dataSource={registryList}
                  locale={{ emptyText: '暂无仓库' }}
                  columns={[
                    { title: '名称', dataIndex: 'name', key: 'name' },
                    { title: '类型', dataIndex: 'provider', key: 'provider', width: 110 },
                    { title: 'PVC', dataIndex: 'cache_pvc_name', key: 'pvc', width: 120, render: (v?: string) => v || '-' },
                    {
                      title: '',
                      key: 'op',
                      width: 60,
                      render: (_, r) => (
                        <Button
                          type="link"
                          size="small"
                          danger
                          onClick={() =>
                            confirmDanger({
                              title: '删除仓库',
                              content: `确定删除仓库「${r.name}」吗？`,
                              onOk: () => deleteRegistryMutation.mutateAsync(r.id),
                            })
                          }
                        >
                          删除
                        </Button>
                      ),
                    },
                  ]}
                />
              </div>
            }
          >
            <Button icon={<CloudServerOutlined />}>模型仓库 ({registryList.length})</Button>
          </Popover>
          <Button type="primary" icon={<PlusOutlined />} disabled={!effectiveWsId} onClick={() => setCreateOpen(true)}>
            新建模型
          </Button>
        </Space>
      }
    >
      {!effectiveWsId ? (
        <EmptyState title="请先选择工作空间" description="选择上方工作空间后查看模型" />
      ) : registryList.length === 0 ? (
        <EmptyState
          title="尚未创建模型仓库"
          description="模型仓库用于托管权重缓存（PVC）与拉取凭据。请先点击「模型仓库」创建一个仓库（如 HuggingFace）。"
        />
      ) : (
        <Table
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={models || []}
          locale={{ emptyText: <EmptyState title="暂无模型" /> }}
          pagination={false}
        />
      )}

      {/* 新建模型仓库 */}
      <Modal
        title="新建模型仓库"
        open={registryOpen}
        onCancel={() => setRegistryOpen(false)}
        onOk={() => registryForm.submit()}
        confirmLoading={createRegistryMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={registryForm} onFinish={(v) => createRegistryMutation.mutate(v)}>
          <Form.Item name="name" label="仓库名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：hf-cache" />
          </Form.Item>
          <Form.Item name="provider" label="类型" rules={[{ required: true }]}>
            <Select options={PROVIDER_OPTIONS} placeholder="选择仓库类型" />
          </Form.Item>
          <Form.Item name="endpoint" label="Endpoint">
            <Input placeholder="如：https://huggingface.co（留空使用默认）" />
          </Form.Item>
          <Form.Item name="cache_pvc_name" label="缓存 PVC 名称" rules={[{ required: true, message: '请输入 PVC 名称' }]}>
            <Input placeholder="如：model-cache-pvc（需预先在该集群创建）" />
          </Form.Item>
          <Form.Item name="cache_path" label="缓存路径">
            <Input placeholder="如：/models（PVC 内子路径）" />
          </Form.Item>
          <Typography.Text type="secondary">
            提示：下载权重时会用此 PVC 挂载到拉取 Job；推理服务部署时也会挂载此 PVC 读取权重。
          </Typography.Text>
        </Form>
      </Modal>

      {/* 新建模型 */}
      <Modal
        title="新建模型"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={createForm}
          onFinish={(v) => createMutation.mutate(v)}
          key={`create-${effectiveWsId}`}
        >
          <Form.Item name="registry_id" label="模型仓库" rules={[{ required: true, message: '请选择仓库' }]}>
            <Select
              placeholder="选择模型仓库"
              options={registryList.map((r) => ({ label: `${r.name} (${r.provider})`, value: r.id }))}
            />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：llama3-8b（HuggingFace 仓库名）" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="如：Llama 3 8B" />
          </Form.Item>
          <Form.Item name="base_architecture" label="基础架构">
            <Input placeholder="如：transformer / llama" />
          </Form.Item>
          <Form.Item name="parameter_count" label="参数量">
            <Input placeholder="如：8B / 70B" />
          </Form.Item>
          <Form.Item name="license" label="License">
            <Input placeholder="如：apache-2.0" />
          </Form.Item>
        </Form>
      </Modal>

      {/* 版本管理 Drawer */}
      <Drawer
        title={versionDrawer.model ? `版本 - ${versionDrawer.model.name}` : '版本'}
        open={versionDrawer.open}
        onClose={() => setVersionDrawer({ open: false })}
        width={1100}
        destroyOnHidden
        extra={
          versionDrawer.model && (
            <Space>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setVersionCreateOpen(true)}>
                新增版本
              </Button>
              <Button onClick={() => refetchVersions()}>刷新</Button>
            </Space>
          )
        }
      >
        <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 12 }}>
          模型版本列出所有可用的权重、精度与下载状态。点击「下载」将触发 K8s Job 从仓库拉取权重到缓存 PVC。
        </Typography.Text>
        <Table
          rowKey="id"
          size="small"
          loading={versionsLoading}
          columns={versionColumns}
          dataSource={versions || []}
          locale={{ emptyText: <EmptyState title="暂无版本" /> }}
          pagination={false}
        />
      </Drawer>

      {/* 新增模型版本 */}
      <Modal
        title="新增模型版本"
        open={versionCreateOpen}
        onCancel={() => setVersionCreateOpen(false)}
        onOk={() => versionForm.submit()}
        confirmLoading={createVersionMutation.isPending}
        destroyOnHidden
        width={560}
      >
        <Form
          layout="vertical"
          form={versionForm}
          onFinish={(v) => createVersionMutation.mutate(v)}
          initialValues={{ precision: 'fp16', quantization: 'none', framework: 'vllm' }}
        >
          <Form.Item name="version" label="版本标识" rules={[{ required: true, message: '请输入版本' }]}>
            <Input placeholder="如：main / v1.0 / quantized-gptq" />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="precision" label="精度" style={{ flex: 1 }} rules={[{ required: true }]}>
              <Select options={PRECISION_OPTIONS} />
            </Form.Item>
            <Form.Item name="quantization" label="量化" style={{ flex: 1 }}>
              <Select options={QUANT_OPTIONS} />
            </Form.Item>
            <Form.Item name="framework" label="推理框架" style={{ flex: 1 }} rules={[{ required: true }]}>
              <Select options={FRAMEWORK_OPTIONS} />
            </Form.Item>
          </Space>
          <Form.Item name="weights_path" label="权重路径">
            <Input placeholder="HuggingFace 仓库 ID（如 meta-llama/Llama-3-8B）或 S3/OSS URI" />
          </Form.Item>
          <Form.Item name="weights_size_bytes" label="权重大小（字节）">
            <InputNumber style={{ width: '100%' }} placeholder="可选" />
          </Form.Item>
          <Form.Item name="recommended_gpu_count" label="推荐 GPU 数">
            <InputNumber min={1} style={{ width: '100%' }} placeholder="如：1" />
          </Form.Item>
          <Typography.Text type="secondary">
            创建后可点击「下载」将权重拉取到模型仓库的缓存 PVC。
          </Typography.Text>
        </Form>
      </Modal>

      {/* 触发下载 */}
      <Modal
        title="下载模型权重"
        open={downloadOpen.open}
        onCancel={() => setDownloadOpen({ open: false })}
        onOk={() => downloadForm.submit()}
        confirmLoading={downloadMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={downloadForm} onFinish={(v) => downloadMutation.mutate({ id: downloadOpen.version!.id, ...v })}>
          <Descriptions size="small" column={1} style={{ marginBottom: 16 }}>
            <Descriptions.Item label="版本">{downloadOpen.version?.version}</Descriptions.Item>
            <Descriptions.Item label="权重路径">{downloadOpen.version?.weights_path || '-'}</Descriptions.Item>
          </Descriptions>
          <Form.Item name="cluster_id" label="目标集群" rules={[{ required: true, message: '请选择集群' }]}>
            <Select
              placeholder="选择集群（需已配置模型仓库的 PVC）"
              options={(clustersData?.items || []).map((c) => ({ label: c.name, value: c.id }))}
            />
          </Form.Item>
          <Form.Item name="namespace" label="命名空间" rules={[{ required: true, message: '请输入命名空间' }]}>
            <Input placeholder="如：default" />
          </Form.Item>
          <Typography.Text type="secondary">
            系统将在该集群命名空间内创建一个 Job，从模型仓库拉取权重到缓存 PVC。
          </Typography.Text>
        </Form>
      </Modal>
    </PageContainer>
  );
}
