import { useState } from 'react';
import {
  Button,
  Checkbox,
  DatePicker,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  App,
} from 'antd';
import {
  SyncOutlined,
  ThunderboltOutlined,
  PlusOutlined,
  CloudServerOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import dayjs from 'dayjs';
import { clusterApi } from '@/api/clusters';
import { clusterOpsApi } from '@/api/clusterOps';
import { k8sApi } from '@/api/k8s';
import { EmptyState } from '@/components/EmptyState';
import { ResourceStatus } from '@/components/ResourceStatus';
import type {
  AbnormalPod,
  Cluster,
  ClusterNodeStatus,
  ClusterOperation,
  ClusterOperationType,
  CreateClusterOperationInput,
} from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';
import { ClusterSelector } from './ClusterSelector';

const OP_TYPE_LABEL: Record<ClusterOperationType, string> = {
  restart: '计划重启',
  drain: '计划驱逐',
  cordon: '计划不可调度',
  uncordon: '计划恢复调度',
  sync_status: '计划同步状态',
};

interface ClusterOpsTabProps {
  clusters: Cluster[];
  clusterId?: number;
  onClusterChange: (id: number | undefined) => void;
}

export function ClusterOpsTab({ clusters, clusterId, onClusterChange }: ClusterOpsTabProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [planOpen, setPlanOpen] = useState(false);
  const [scaleOpen, setScaleOpen] = useState(false);
  const [planForm] = Form.useForm<CreateClusterOperationInput & { scheduled_at_picker: dayjs.Dayjs }>();
  const [scaleForm] = Form.useForm<{ node_pool_id: string; desired_size: number }>();

  const selectedCluster = clusters.find((c) => c.id === clusterId);
  const hasProvider = !!(selectedCluster?.provider || selectedCluster?.labels?.provider);

  const { data: nodes = [] } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'node-statuses'],
    queryFn: () => clusterOpsApi.listNodeStatuses(clusterId!),
    enabled: !!clusterId,
  });

  const { data: opsData, isLoading: opsLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'operations'],
    queryFn: () => clusterOpsApi.listOperations(clusterId!, { page: 1, size: 50 }),
    enabled: !!clusterId,
  });

  const { data: abnormalPods = [], isLoading: podsLoading } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'abnormal-pods'],
    queryFn: () => clusterOpsApi.listAbnormalPods(clusterId!),
    enabled: !!clusterId,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['cluster-ops', clusterId] });
    queryClient.invalidateQueries({ queryKey: ['clusters'] });
  };

  const probeMutation = useMutation({
    mutationFn: () => clusterApi.probe(clusterId!),
    onSuccess: (res: any) => {
      message.success(`探测成功${res?.node_count != null ? `，节点数：${res.node_count}` : ''}`);
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '探测失败'),
  });

  const syncMutation = useMutation({
    mutationFn: () => clusterOpsApi.syncNodeStatuses(clusterId!),
    onSuccess: (items) => {
      message.success(`已同步 ${items.length} 个节点`);
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '同步失败'),
  });

  const cordonMutation = useMutation({
    mutationFn: ({ node, cordon }: { node: string; cordon: boolean }) =>
      cordon ? k8sApi.cordonNode(clusterId!, node) : k8sApi.uncordonNode(clusterId!, node),
    onSuccess: () => {
      message.success('节点调度状态已更新');
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const drainMutation = useMutation({
    mutationFn: (node: string) => k8sApi.drainNode(clusterId!, node),
    onSuccess: () => {
      message.success('节点驱逐已发起');
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '驱逐失败'),
  });

  const rebuildMutation = useMutation({
    mutationFn: (pod: AbnormalPod) => k8sApi.deletePod(clusterId!, pod.namespace, pod.name),
    onSuccess: () => {
      message.success('Pod 已删除，控制器将自动重建');
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '重建失败'),
  });

  const createOpMutation = useMutation({
    mutationFn: (body: CreateClusterOperationInput) =>
      clusterOpsApi.createOperation(clusterId!, body),
    onSuccess: () => {
      message.success('计划运维任务已创建');
      setPlanOpen(false);
      planForm.resetFields();
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const cancelOpMutation = useMutation({
    mutationFn: (opId: number) => clusterOpsApi.cancelOperation(clusterId!, opId),
    onSuccess: () => {
      message.success('任务已取消');
      invalidate();
    },
    onError: (e: any) => message.error(e?.message || '取消失败'),
  });

  const scaleMutation = useMutation({
    mutationFn: ({ nodePoolId, desiredSize }: { nodePoolId: string; desiredSize: number }) =>
      k8sApi.scaleNodePool(clusterId!, nodePoolId, desiredSize),
    onSuccess: (res) => {
      message.success(`节点池扩缩容已提交${res?.operation_id ? `（${res.operation_id}）` : ''}`);
      setScaleOpen(false);
      scaleForm.resetFields();
    },
    onError: (e: any) => message.error(e?.message || '扩缩容失败'),
  });

  const nodeColumns: ColumnsType<ClusterNodeStatus> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 160 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '调度',
      key: 'sched',
      width: 90,
      render: (_, n) => (n.unschedulable ? <Tag color="orange">不可调度</Tag> : <Tag>可调度</Tag>),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, n) => (
        <Space size="small" wrap>
          {n.unschedulable ? (
            <Button
              type="link"
              size="small"
              loading={cordonMutation.isPending}
              onClick={() => cordonMutation.mutate({ node: n.node_name, cordon: false })}
            >
              恢复调度
            </Button>
          ) : (
            <Button
              type="link"
              size="small"
              loading={cordonMutation.isPending}
              onClick={() => cordonMutation.mutate({ node: n.node_name, cordon: true })}
            >
              不可调度
            </Button>
          )}
          <Button
            type="link"
            size="small"
            danger
            loading={drainMutation.isPending}
            onClick={() =>
              confirmDanger({
                title: `驱逐节点 ${n.node_name}`,
                content: '将驱逐该节点上所有可迁移的 Pod。确定继续？',
                okText: '驱逐',
                onOk: () => drainMutation.mutateAsync(n.node_name),
              })
            }
          >
            驱逐
          </Button>
          <Button
            type="link"
            size="small"
            onClick={() => {
              planForm.setFieldsValue({
                node_name: n.node_name,
                operation_type: 'restart',
                notify_affected: true,
                scheduled_at_picker: dayjs().add(1, 'hour'),
              });
              setPlanOpen(true);
            }}
          >
            计划重启
          </Button>
        </Space>
      ),
    },
  ];

  const opColumns: ColumnsType<ClusterOperation> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 140, render: (v?: string) => v || '-' },
    {
      title: '类型',
      dataIndex: 'operation_type',
      key: 'operation_type',
      width: 130,
      render: (v: ClusterOperationType) => OP_TYPE_LABEL[v] || v,
    },
    {
      title: '计划时间',
      dataIndex: 'scheduled_at',
      key: 'scheduled_at',
      width: 170,
      render: (t: string) => formatTime(t),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '通知',
      dataIndex: 'notify_affected',
      key: 'notify_affected',
      width: 70,
      render: (v: boolean) => (v ? '是' : '否'),
    },
    {
      title: '错误',
      dataIndex: 'error_message',
      key: 'error_message',
      ellipsis: true,
      render: (v?: string) => v || '-',
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_, op) =>
        op.status === 'pending' ? (
          <Button
            type="link"
            size="small"
            danger
            loading={cancelOpMutation.isPending}
            onClick={() => cancelOpMutation.mutate(op.id)}
          >
            取消
          </Button>
        ) : null,
    },
  ];

  const podColumns: ColumnsType<AbnormalPod> = [
    { title: 'Pod', dataIndex: 'name', key: 'name', width: 180 },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 120 },
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 140 },
    { title: '阶段', dataIndex: 'phase', key: 'phase', width: 90 },
    {
      title: '重启次数',
      dataIndex: 'restart_count',
      key: 'restart_count',
      width: 90,
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : v),
    },
    {
      title: '原因',
      dataIndex: 'reason',
      key: 'reason',
      ellipsis: true,
      render: (v: string | undefined, r: AbnormalPod) => v || r.message || '-',
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_, pod) => (
        <Button
          type="link"
          size="small"
          danger
          loading={rebuildMutation.isPending}
          onClick={() =>
            confirmDanger({
              title: `重建 Pod ${pod.name}`,
              content: '将删除该 Pod，由控制器自动重建。确定继续？',
              okText: '重建',
              onOk: () => rebuildMutation.mutateAsync(pod),
            })
          }
        >
          重建
        </Button>
      ),
    },
  ];

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Space wrap>
        <ClusterSelector clusters={clusters} value={clusterId} onChange={onClusterChange} />
        <Button
          icon={<ThunderboltOutlined />}
          disabled={!clusterId}
          loading={probeMutation.isPending}
          onClick={() => probeMutation.mutate()}
        >
          探测集群
        </Button>
        <Button
          icon={<SyncOutlined />}
          disabled={!clusterId}
          loading={syncMutation.isPending}
          onClick={() => syncMutation.mutate()}
        >
          同步节点状态
        </Button>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={!clusterId}
          onClick={() => {
            planForm.resetFields();
            planForm.setFieldsValue({
              operation_type: 'restart',
              notify_affected: true,
              scheduled_at_picker: dayjs().add(1, 'hour'),
            });
            setPlanOpen(true);
          }}
        >
          创建计划任务
        </Button>
        {hasProvider && (
          <Button
            icon={<CloudServerOutlined />}
            disabled={!clusterId}
            onClick={() => setScaleOpen(true)}
          >
            节点池扩缩容
          </Button>
        )}
      </Space>

      {!clusterId ? (
        <EmptyState title="请先选择集群" />
      ) : (
        <>
          <Table<ClusterNodeStatus>
            rowKey="id"
            size="small"
            title={() => '节点运维'}
            columns={nodeColumns}
            dataSource={nodes}
            pagination={false}
            locale={{ emptyText: '暂无节点，请先同步节点状态' }}
          />

          <Table<ClusterOperation>
            rowKey="id"
            size="small"
            title={() => '计划运维任务'}
            loading={opsLoading}
            columns={opColumns}
            dataSource={opsData?.items || []}
            pagination={false}
            locale={{ emptyText: '暂无计划任务' }}
          />

          <Table<AbnormalPod>
            rowKey={(r) => `${r.namespace}/${r.name}`}
            size="small"
            title={() => '异常 Pod（重建）'}
            loading={podsLoading}
            columns={podColumns}
            dataSource={abnormalPods}
            pagination={false}
            locale={{ emptyText: '暂无异常 Pod' }}
          />
        </>
      )}

      <Modal
        title="创建计划运维任务"
        open={planOpen}
        onCancel={() => setPlanOpen(false)}
        onOk={() => planForm.submit()}
        confirmLoading={createOpMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={planForm}
          onFinish={(v) => {
            const { scheduled_at_picker, ...rest } = v;
            createOpMutation.mutate({
              ...rest,
              scheduled_at: scheduled_at_picker?.toISOString(),
            });
          }}
        >
          <Form.Item name="node_name" label="目标节点" extra="sync_status 类型可留空">
            <Select
              allowClear
              placeholder="选择节点"
              options={nodes.map((n) => ({ label: n.node_name, value: n.node_name }))}
            />
          </Form.Item>
          <Form.Item
            name="operation_type"
            label="操作类型"
            rules={[{ required: true, message: '请选择操作类型' }]}
          >
            <Select
              options={(
                Object.entries(OP_TYPE_LABEL) as [ClusterOperationType, string][]
              ).map(([value, label]) => ({ value, label }))}
            />
          </Form.Item>
          <Form.Item
            name="scheduled_at_picker"
            label="计划执行时间"
            rules={[{ required: true, message: '请选择时间' }]}
          >
            <DatePicker showTime style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="notify_affected" valuePropName="checked">
            <Checkbox>执行前通知受影响应用成员</Checkbox>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="节点池扩缩容"
        open={scaleOpen}
        onCancel={() => setScaleOpen(false)}
        onOk={() => scaleForm.submit()}
        confirmLoading={scaleMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={scaleForm}
          onFinish={(v) => scaleMutation.mutate({ nodePoolId: v.node_pool_id, desiredSize: v.desired_size })}
        >
          <Form.Item
            name="node_pool_id"
            label="节点池 ID"
            rules={[{ required: true, message: '请输入节点池 ID' }]}
          >
            <Input placeholder="云厂商节点池 ID" />
          </Form.Item>
          <Form.Item
            name="desired_size"
            label="期望节点数"
            rules={[{ required: true, message: '请输入期望节点数' }]}
          >
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}
