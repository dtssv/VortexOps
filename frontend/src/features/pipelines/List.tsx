import { useState } from 'react';
import { Button, Drawer, Form, Input, Modal, Select, Space, Table, Tag, App } from 'antd';
import { PlusOutlined, UnorderedListOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { pipelineApi } from '@/api/pipelines';
import type { Pipeline, PipelineRun } from '@/types';
import { StageEditor, type PipelineStage } from './StageEditor';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatDuration } from '@/utils/format';

const TRIGGER_MODE_OPTIONS = [
  { label: '手动', value: 'manual' },
  { label: 'Webhook', value: 'webhook' },
  { label: '定时', value: 'cron' },
];

export default function PipelineListPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [createStages, setCreateStages] = useState<PipelineStage[]>([]);
  const [runsDrawer, setRunsDrawer] = useState<{ open: boolean; pipeline?: Pipeline }>({ open: false });

  const { data, isLoading } = useQuery({
    queryKey: ['pipelines', { page, size }],
    queryFn: () => pipelineApi.list({ page, size }),
  });

  const { data: runsData, isLoading: runsLoading } = useQuery({
    queryKey: ['pipeline-runs', { pipeline_id: runsDrawer.pipeline?.id }],
    queryFn: () => pipelineApi.listRuns({ pipeline_id: runsDrawer.pipeline!.id }),
    enabled: !!runsDrawer.pipeline?.id,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Pipeline>) => pipelineApi.create(body),
    onSuccess: () => {
      message.success('流水线已创建');
      setCreateOpen(false);
      createForm.resetFields();
      setCreateStages([]);
      queryClient.invalidateQueries({ queryKey: ['pipelines'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.delete(id),
    onSuccess: () => {
      message.success('流水线已删除');
      queryClient.invalidateQueries({ queryKey: ['pipelines'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const columns: ColumnsType<Pipeline> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '触发模式', dataIndex: 'trigger_mode', key: 'trigger_mode', width: 120 },
    {
      title: '启用',
      dataIndex: 'enabled',
      key: 'enabled',
      width: 80,
      render: (v: boolean) => (v ? <Tag color="success">启用</Tag> : <Tag>停用</Tag>),
    },
    { title: '版本', dataIndex: 'version', key: 'version', width: 80 },
    { title: '更新时间', dataIndex: 'updated_at', key: 'updated_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '操作',
      key: 'actions',
      width: 220,
      render: (_, record) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<UnorderedListOutlined />}
            onClick={() => setRunsDrawer({ open: true, pipeline: record })}
          >
            查看运行
          </Button>
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除流水线',
                content: `确定删除流水线「${record.name}」吗？`,
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

  const runColumns: ColumnsType<PipelineRun> = [
    { title: '运行号', dataIndex: 'run_number', key: 'run_number', width: 80 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '触发源', dataIndex: 'trigger_source', key: 'trigger_source', width: 120, render: (v?: string) => v || '-' },
    { title: '触发者', dataIndex: 'triggered_by_name', key: 'triggered_by_name', width: 120, render: (v?: string) => v || '-' },
    { title: '开始时间', dataIndex: 'started_at', key: 'started_at', width: 180, render: (t: string) => formatTime(t) },
    { title: '耗时', dataIndex: 'duration_ms', key: 'duration_ms', width: 100, render: (v?: number) => formatDuration(v) },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_, record) => (
        <Button type="link" size="small" onClick={() => navigate(`/pipeline-runs/${record.id}`)}>
          详情
        </Button>
      ),
    },
  ];

  return (
    <PageContainer
      title="流水线"
      subtitle="管理 CI/CD 流水线"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建流水线
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无流水线" /> }}
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

      <Modal
        title="新建流水线"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={createForm} onFinish={(v) => createMutation.mutate({ ...v, stages: createStages })} initialValues={{ trigger_mode: 'manual', enabled: true }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：build-and-deploy" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="trigger_mode" label="触发模式" rules={[{ required: true }]}>
            <Select options={TRIGGER_MODE_OPTIONS} />
          </Form.Item>
          <StageEditor value={createStages} onChange={setCreateStages} />
        </Form>
      </Modal>

      <Drawer
        title={runsDrawer.pipeline ? `运行记录 - ${runsDrawer.pipeline.name}` : '运行记录'}
        open={runsDrawer.open}
        onClose={() => setRunsDrawer({ open: false })}
        width={820}
        destroyOnHidden
      >
        <Table
          rowKey="id"
          size="small"
          loading={runsLoading}
          columns={runColumns}
          dataSource={runsData?.items || []}
          locale={{ emptyText: <EmptyState title="暂无运行记录" /> }}
          pagination={false}
        />
      </Drawer>
    </PageContainer>
  );
}
