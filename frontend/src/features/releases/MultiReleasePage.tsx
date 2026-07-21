import { useState } from 'react';
import {
  App,
  Button,
  Card,
  Drawer,
  Form,
  Input,
  InputNumber,
  Progress,
  Select,
  Space,
  Steps,
  Table,
  Tag,
  Tooltip,
  Modal,
} from 'antd';
import { PlusOutlined, DeploymentUnitOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { releaseApi } from '@/api/releases';
import { groupApi } from '@/api/applications';
import { buildApi } from '@/api/builds';
import type { ReleaseOrchestration, OrchestrationTarget, Group, Image } from '@/types';
import { formatTime, formatDuration } from '@/utils/format';

const STRATEGY_OPTIONS = [
  { label: '顺序发布', value: 'sequential' },
  { label: '并行发布', value: 'parallel' },
  { label: '金丝雀批次', value: 'canary' },
];

const STATUS_COLOR: Record<string, string> = {
  pending: 'default',
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
  aborted: 'warning',
  paused: 'warning',
};

export function MultiReleasePage() {
  const { appId } = useParams<{ appId: string }>();
  const applicationId = Number(appId);
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [targets, setTargets] = useState<TargetRow[]>([]);
  const [detailDrawer, setDetailDrawer] = useState<{ open: boolean; id?: number }>({ open: false });

  const { data, isLoading } = useQuery({
    queryKey: ['orchestrations', applicationId],
    queryFn: () => releaseApi.listOrchestrations(applicationId, { page: 1, size: 50 }),
    enabled: !!applicationId,
  });

  const groupsQuery = useQuery({
    queryKey: ['groups', applicationId],
    queryFn: () => groupApi.list(applicationId, { page: 1, size: 200 }),
    enabled: !!applicationId && createOpen,
  });

  const imagesQuery = useQuery({
    queryKey: ['images', applicationId],
    queryFn: () => buildApi.listImages(applicationId, { page: 1, size: 100 }),
    enabled: !!applicationId && createOpen,
  });

  const createMutation = useMutation({
    mutationFn: (body: Record<string, any>) => releaseApi.triggerOrchestration(applicationId, body),
    onSuccess: () => {
      message.success('多集群发布已触发');
      setCreateOpen(false);
      createForm.resetFields();
      setTargets([]);
      queryClient.invalidateQueries({ queryKey: ['orchestrations', applicationId] });
    },
    onError: (e: any) => message.error(e?.message || '触发失败'),
  });

  const abortMutation = useMutation({
    mutationFn: (id: number) => releaseApi.abortOrchestration(id),
    onSuccess: () => {
      message.success('已中止');
      queryClient.invalidateQueries({ queryKey: ['orchestrations', applicationId] });
      queryClient.invalidateQueries({ queryKey: ['orchestration-detail'] });
    },
    onError: (e: any) => message.error(e?.message || '中止失败'),
  });

  const columns: ColumnsType<ReleaseOrchestration> = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '策略',
      dataIndex: 'strategy',
      key: 'strategy',
      width: 110,
      render: (v: string) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <Tag color={STATUS_COLOR[v] || 'default'}>{v}</Tag>,
    },
    {
      title: '进度',
      key: 'progress',
      width: 160,
      render: (_: any, r: ReleaseOrchestration) => <Progress percent={r.progress_percent} size="small" status={r.status === 'failed' ? 'exception' : r.status === 'succeeded' ? 'success' : 'active'} />,
    },
    { title: '镜像ID', dataIndex: 'image_id', key: 'image_id', width: 90 },
    { title: '耗时', key: 'duration', width: 100, render: (_: any, r: ReleaseOrchestration) => formatDuration(r.duration_ms) },
    { title: '开始时间', key: 'started', width: 170, render: (_: any, r: ReleaseOrchestration) => (r.started_at ? formatTime(r.started_at) : '-') },
    {
      title: '操作',
      key: 'actions',
      width: 140,
      render: (_: any, r: ReleaseOrchestration) => (
        <Space>
          <a onClick={() => setDetailDrawer({ open: true, id: r.id })}>详情</a>
          {(r.status === 'running' || r.status === 'pending') && (
            <a style={{ color: '#ff4d4f' }} onClick={() => abortMutation.mutate(r.id)}>中止</a>
          )}
        </Space>
      ),
    },
  ];

  const submit = () => {
    createForm.validateFields().then((v) => {
      if (targets.length === 0) {
        message.warning('请至少添加一个目标');
        return;
      }
      createMutation.mutate({
        workspace_id: v.workspace_id,
        name: v.name,
        strategy: v.strategy,
        image_id: v.image_id,
        replicas: v.replicas,
        max_surge: v.max_surge,
        max_unavailable: v.max_unavailable,
        batch_size: v.batch_size,
        batch_interval_sec: v.batch_interval_sec,
        targets: targets.map((t, i) => ({
          group_id: t.group_id,
          cluster_id: t.cluster_id,
          image_id: t.image_id,
          replicas: t.replicas,
          seq: v.strategy === 'sequential' ? i + 1 : 0,
          batch_size: t.batch_size,
          batch_interval_sec: t.batch_interval_sec,
        })),
      });
    });
  };

  return (
    <PageContainer
      title="多集群发布"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          发起多集群发布
        </Button>
      }
    >
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无发布编排" /> }}
        pagination={false}
      />

      <Modal
        title="多集群发布向导"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={submit}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={820}
      >
        <Form layout="vertical" form={createForm} initialValues={{ strategy: 'sequential', max_surge: '25%', max_unavailable: '25%' }}>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="name" label="编排名称" rules={[{ required: true, message: '请输入名称' }]} style={{ flex: 1 }}>
              <Input placeholder="如：v2.3.0 多集群发布" />
            </Form.Item>
            <Form.Item name="strategy" label="策略" rules={[{ required: true }]}>
              <Select options={STRATEGY_OPTIONS} style={{ width: 140 }} />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="image_id" label="统一镜像（可被目标覆盖）" style={{ flex: 1 }}>
              <Select
                allowClear
                placeholder="选择镜像"
                options={(imagesQuery.data?.items || []).map((img: Image) => ({ label: `${img.repository}:${img.tag}`, value: img.id }))}
              />
            </Form.Item>
            <Form.Item name="replicas" label="副本数">
              <InputNumber min={0} placeholder="留空用 group 默认" style={{ width: 160 }} />
            </Form.Item>
          </Space>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="batch_size" label="批次大小（金丝雀）">
              <InputNumber min={0} placeholder="如：2" style={{ width: 140 }} />
            </Form.Item>
            <Form.Item name="batch_interval_sec" label="批次间隔（秒）">
              <InputNumber min={0} placeholder="如：30" style={{ width: 140 }} />
            </Form.Item>
            <Form.Item name="max_surge" label="MaxSurge">
              <Input placeholder="25%" style={{ width: 120 }} />
            </Form.Item>
            <Form.Item name="max_unavailable" label="MaxUnavailable">
              <Input placeholder="25%" style={{ width: 120 }} />
            </Form.Item>
          </Space>

          <TargetEditor
            targets={targets}
            onChange={setTargets}
            groups={groupsQuery.data?.items || []}
            images={imagesQuery.data?.items || []}
          />
        </Form>
      </Modal>

      <OrchestrationDetailDrawer
        state={detailDrawer}
        onClose={() => setDetailDrawer({ open: false })}
        onAbort={(id) => abortMutation.mutate(id)}
      />
    </PageContainer>
  );
}

interface TargetRow {
  group_id: number;
  cluster_id: number;
  image_id?: number;
  replicas?: number;
  batch_size?: number;
  batch_interval_sec?: number;
}

function TargetEditor({
  targets,
  onChange,
  groups,
  images,
}: {
  targets: TargetRow[];
  onChange: (t: TargetRow[]) => void;
  groups: Group[];
  images: Image[];
}) {
  const add = () => {
    onChange([...targets, { group_id: 0, cluster_id: 0 }]);
  };
  const remove = (i: number) => onChange(targets.filter((_, idx) => idx !== i));
  const update = (i: number, patch: Partial<TargetRow>) => {
    const next = [...targets];
    next[i] = { ...next[i], ...patch };
    if (patch.group_id) {
      const g = groups.find((x) => x.id === patch.group_id);
      if (g) next[i].cluster_id = g.cluster_id;
    }
    onChange(next);
  };

  const columns: ColumnsType<TargetRow> = [
    {
      title: 'Group',
      key: 'group',
      width: 200,
      render: (_: any, _r: TargetRow, i: number) => (
        <Select
          showSearch
          placeholder="选择 Group"
          style={{ width: '100%' }}
          value={targets[i].group_id || undefined}
          optionFilterProp="label"
          options={groups.map((g) => ({ label: `${g.display_name} (${g.cluster_name || g.cluster_id})`, value: g.id }))}
          onChange={(v) => update(i, { group_id: v })}
        />
      ),
    },
    { title: '集群ID', key: 'cluster_id', width: 90, render: (_: any, r: TargetRow) => r.cluster_id || '-' },
    {
      title: '镜像（覆盖）',
      key: 'image',
      width: 200,
      render: (_: any, _r: TargetRow, i: number) => (
        <Select
          allowClear
          placeholder="留空用统一镜像"
          style={{ width: '100%' }}
          value={targets[i].image_id || undefined}
          options={images.map((img) => ({ label: `${img.repository}:${img.tag}`, value: img.id }))}
          onChange={(v) => update(i, { image_id: v })}
        />
      ),
    },
    {
      title: '副本',
      key: 'replicas',
      width: 90,
      render: (_: any, _r: TargetRow, i: number) => (
        <InputNumber min={0} placeholder="默认" value={targets[i].replicas} onChange={(v) => update(i, { replicas: v ?? undefined })} />
      ),
    },
    {
      title: '批次',
      key: 'batch',
      width: 90,
      render: (_: any, _r: TargetRow, i: number) => (
        <InputNumber min={0} placeholder="默认" value={targets[i].batch_size} onChange={(v) => update(i, { batch_size: v ?? undefined })} />
      ),
    },
    {
      title: '',
      key: 'actions',
      width: 60,
      render: (_: any, _r: TargetRow, i: number) => (
        <Button type="link" danger size="small" onClick={() => remove(i)}>删除</Button>
      ),
    },
  ];

  return (
    <Card
      size="small"
      type="inner"
      title="发布目标"
      extra={<Button size="small" icon={<PlusOutlined />} onClick={add}>添加目标</Button>}
      style={{ marginTop: 8 }}
    >
      <Table
        rowKey={(_, i) => String(i)}
        size="small"
        columns={columns}
        dataSource={targets}
        pagination={false}
        locale={{ emptyText: '请添加发布目标' }}
      />
    </Card>
  );
}

function OrchestrationDetailDrawer({
  state,
  onClose,
  onAbort,
}: {
  state: { open: boolean; id?: number };
  onClose: () => void;
  onAbort: (id: number) => void;
}) {
  const { data, isLoading } = useQuery({
    queryKey: ['orchestration-detail', state.id],
    queryFn: () => releaseApi.getOrchestration(state.id!),
    enabled: !!state.id && state.open,
    refetchInterval: state.open ? 5000 : false,
  });

  const o = data?.orchestration;
  const targets = data?.targets || [];

  const targetColumns: ColumnsType<OrchestrationTarget> = [
    { title: '序', dataIndex: 'seq', key: 'seq', width: 50 },
    { title: 'Group ID', dataIndex: 'group_id', key: 'group_id', width: 90 },
    { title: '集群ID', dataIndex: 'cluster_id', key: 'cluster_id', width: 90 },
    { title: '镜像ID', dataIndex: 'image_id', key: 'image_id', width: 90 },
    { title: '副本', dataIndex: 'replicas', key: 'replicas', width: 70 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <Tag color={STATUS_COLOR[v] || 'default'}>{v}</Tag>,
    },
    { title: '关联发布', dataIndex: 'release_id', key: 'release_id', width: 100, render: (v: number) => v || '-' },
    { title: '失败原因', dataIndex: 'failure_reason', key: 'failure_reason', ellipsis: true },
  ];

  return (
    <Drawer
      title={o ? `发布编排 - ${o.name}` : '发布编排'}
      open={state.open}
      onClose={onClose}
      width={860}
      destroyOnHidden
    >
      {isLoading || !o ? (
        <EmptyState title="加载中..." />
      ) : (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Card size="small">
            <Space size="large" wrap>
              <span>策略：<Tag color="blue">{o.strategy}</Tag></span>
              <span>状态：<Tag color={STATUS_COLOR[o.status] || 'default'}>{o.status}</Tag></span>
              <span>耗时：{formatDuration(o.duration_ms)}</span>
            </Space>
            <Progress
              percent={o.progress_percent}
              status={o.status === 'failed' ? 'exception' : o.status === 'succeeded' ? 'success' : 'active'}
              style={{ marginTop: 12 }}
            />
            {o.failure_reason && <div style={{ color: '#ff4d4f', marginTop: 8 }}>{o.failure_reason}</div>}
            {(o.status === 'running' || o.status === 'pending') && (
              <Button danger style={{ marginTop: 12 }} onClick={() => onAbort(o.id)}>中止编排</Button>
            )}
          </Card>
          <Card size="small" title="目标进度">
            <Table
              rowKey="id"
              size="small"
              columns={targetColumns}
              dataSource={targets}
              pagination={false}
            />
          </Card>
        </Space>
      )}
    </Drawer>
  );
}

export default MultiReleasePage;
