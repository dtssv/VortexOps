import { useState } from 'react';
import { Button, Form, Input, Modal, Select, Space, Table, App, Tag } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { inferenceApi } from '@/api/inference';
import type { InferenceRoute, InferenceService } from '@/types';
import { useUIStore } from '@/stores/uiStore';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const STRATEGY_OPTIONS = [
  { label: '加权（Weighted）', value: 'weighted' },
  { label: '按头部（Header）', value: 'header' },
  { label: '故障转移（Failover）', value: 'failover' },
];

export default function InferenceRoutesPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const wsId = useUIStore((s) => s.currentWorkspaceId);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();

  const { data: routes, isLoading } = useQuery({
    queryKey: ['inference-routes', wsId],
    queryFn: () => inferenceApi.listRoutes(wsId!),
    enabled: !!wsId,
  });

  const { data: servicesData } = useQuery({
    queryKey: ['inference-services-all', wsId],
    queryFn: () => inferenceApi.listServices({ workspace_id: wsId ?? undefined, page: 1, size: 200 }),
    enabled: !!wsId && createOpen,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<InferenceRoute>) => inferenceApi.createRoute({ ...body, workspace_id: wsId! }),
    onSuccess: () => {
      message.success('路由已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['inference-routes', wsId] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => inferenceApi.deleteRoute(id),
    onSuccess: () => {
      message.success('路由已删除');
      queryClient.invalidateQueries({ queryKey: ['inference-routes', wsId] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<InferenceRoute> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '描述', dataIndex: 'description', key: 'description', render: (v?: string) => v || '-' },
    {
      title: '策略',
      dataIndex: 'strategy',
      key: 'strategy',
      width: 140,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: '规则',
      dataIndex: 'rules',
      key: 'rules',
      render: (v?: Record<string, any>) => (v ? <code style={{ fontSize: 12 }}>{JSON.stringify(v)}</code> : '-'),
    },
    {
      title: '默认服务',
      dataIndex: 'default_service_id',
      key: 'default_service_id',
      width: 120,
      render: (v?: number) => v || '-',
    },
    { title: '状态', dataIndex: 'status', key: 'status', width: 100 },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
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
              title: '删除路由',
              content: `确定删除路由「${record.name}」吗？`,
              onOk: () => deleteMutation.mutateAsync(record.id),
            })
          }
        >
          删除
        </Button>
      ),
    },
  ];

  return (
    <PageContainer
      title="推理路由"
      subtitle="多服务流量路由：加权 / Header / 故障转移"
      extra={
        <Button type="primary" icon={<PlusOutlined />} disabled={!wsId} onClick={() => setCreateOpen(true)}>
          新建路由
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={routes || []}
        locale={{ emptyText: <EmptyState title="暂无路由" /> }}
        pagination={false}
      />

      <Modal
        title="新建路由"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={560}
      >
        <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate(v)} initialValues={{ strategy: 'weighted' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：prod-llm-route" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="路由用途说明" />
          </Form.Item>
          <Form.Item name="strategy" label="策略" rules={[{ required: true }]}>
            <Select options={STRATEGY_OPTIONS} />
          </Form.Item>
          <Form.Item name="default_service_id" label="默认推理服务">
            <Select
              allowClear
              placeholder="选择默认服务"
              options={(servicesData?.items || []).map((s: InferenceService) => ({ label: s.name, value: s.id }))}
            />
          </Form.Item>
          <Form.Item name="rules" label="规则（JSON）">
            <Input.TextArea
              rows={4}
              placeholder='加权策略示例：{"weights": {"服务ID": 80, "服务ID2": 20}}；故障转移示例：{"failover_chain": [1, 2]}'
            />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
