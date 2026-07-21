import { useState } from 'react';
import {
  Button,
  Descriptions,
  Form,
  Input,
  Modal,
  Space,
  Table,
  Tabs,
  Tag,
  App,
} from 'antd';
import { BellOutlined, SyncOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { clusterOpsApi } from '@/api/clusterOps';
import { EmptyState } from '@/components/EmptyState';
import { ResourceStatus } from '@/components/ResourceStatus';
import type { AbnormalNode, AbnormalPod, AffectedApp, Cluster, NotifyAffectedInput } from '@/types';
import { formatRelative, formatTime } from '@/utils/format';
import { ClusterSelector } from './ClusterSelector';

interface AbnormalNotifyTabProps {
  clusters: Cluster[];
  clusterId?: number;
  onClusterChange: (id: number | undefined) => void;
}

export function AbnormalNotifyTab({ clusters, clusterId, onClusterChange }: AbnormalNotifyTabProps) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewApps, setPreviewApps] = useState<AffectedApp[]>([]);
  const [pendingNotify, setPendingNotify] = useState<NotifyAffectedInput | null>(null);
  const [notifyForm] = Form.useForm<{ subject: string; body: string }>();

  const { data: abnormalPods = [], isLoading: podsLoading, refetch: refetchPods } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'abnormal-pods'],
    queryFn: () => clusterOpsApi.listAbnormalPods(clusterId!),
    enabled: !!clusterId,
  });

  const { data: abnormalNodes = [], isLoading: nodesLoading, refetch: refetchNodes } = useQuery({
    queryKey: ['cluster-ops', clusterId, 'abnormal-nodes'],
    queryFn: () => clusterOpsApi.listAbnormalNodes(clusterId!),
    enabled: !!clusterId,
  });

  const previewMutation = useMutation({
    mutationFn: (body: NotifyAffectedInput) => clusterOpsApi.previewAffected(clusterId!, body),
    onSuccess: (apps, body) => {
      setPreviewApps(apps);
      setPendingNotify(body);
      notifyForm.setFieldsValue({
        subject: body.subject || defaultSubject(body),
        body: body.body || defaultBody(body),
      });
      setPreviewOpen(true);
    },
    onError: (e: any) => message.error(e?.message || '预览失败'),
  });

  const notifyMutation = useMutation({
    mutationFn: (body: NotifyAffectedInput) => clusterOpsApi.notifyAffected(clusterId!, body),
    onSuccess: (res) => {
      message.success(`已通知 ${res.total_notified} 位成员`);
      setPreviewOpen(false);
      setPendingNotify(null);
      queryClient.invalidateQueries({ queryKey: ['cluster-ops', clusterId] });
    },
    onError: (e: any) => message.error(e?.message || '通知失败'),
  });

  const openPreview = (body: NotifyAffectedInput) => previewMutation.mutate(body);

  const podColumns: ColumnsType<AbnormalPod> = [
    { title: 'Pod', dataIndex: 'name', key: 'name', width: 160 },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 120 },
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 130 },
    {
      title: '应用',
      key: 'app',
      width: 140,
      render: (_, r) => r.application_name || r.group_name || '-',
    },
    {
      title: '状态',
      key: 'status',
      width: 120,
      render: (_, r) => (
        <Space>
          <Tag color={r.ready ? 'green' : 'red'}>{r.ready ? 'Ready' : 'NotReady'}</Tag>
          {r.restart_count > 0 && <Tag color="orange">重启 {r.restart_count}</Tag>}
        </Space>
      ),
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
      width: 100,
      render: (_, pod) => (
        <Button
          type="link"
          size="small"
          icon={<BellOutlined />}
          loading={previewMutation.isPending}
          onClick={() =>
            openPreview({
              scope: 'pod',
              pod_namespace: pod.namespace,
              pod_name: pod.name,
              node_name: pod.node_name,
            })
          }
        >
          通知
        </Button>
      ),
    },
  ];

  const nodeColumns: ColumnsType<AbnormalNode> = [
    { title: '节点', dataIndex: 'node_name', key: 'node_name', width: 160 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    {
      title: '不可调度',
      dataIndex: 'unschedulable',
      key: 'unschedulable',
      width: 90,
      render: (v: boolean) => (v ? <Tag color="orange">是</Tag> : '否'),
    },
    { title: 'Pod 数', dataIndex: 'pod_count', key: 'pod_count', width: 80 },
    {
      title: '异常 Pod',
      dataIndex: 'abnormal_pod_count',
      key: 'abnormal_pod_count',
      width: 90,
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : v),
    },
    {
      title: '最后同步',
      dataIndex: 'last_synced_at',
      key: 'last_synced_at',
      width: 140,
      render: (t?: string) => (t ? formatRelative(t) : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, node) => (
        <Button
          type="link"
          size="small"
          icon={<BellOutlined />}
          loading={previewMutation.isPending}
          onClick={() =>
            openPreview({
              scope: 'node',
              node_name: node.node_name,
            })
          }
        >
          通知
        </Button>
      ),
    },
  ];

  const memberCount = previewApps.reduce((s, a) => s + a.members.length, 0);

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      <Space wrap>
        <ClusterSelector clusters={clusters} value={clusterId} onChange={onClusterChange} />
        <Button
          icon={<SyncOutlined />}
          disabled={!clusterId}
          onClick={() => {
            refetchPods();
            refetchNodes();
          }}
        >
          刷新
        </Button>
        <Button
          type="primary"
          icon={<BellOutlined />}
          disabled={!clusterId}
          loading={previewMutation.isPending}
          onClick={() => openPreview({ scope: 'cluster' })}
        >
          通知全集群受影响成员
        </Button>
      </Space>

      {!clusterId ? (
        <EmptyState title="请先选择集群" />
      ) : (
        <Tabs
          items={[
            {
              key: 'pods',
              label: `异常 Pod (${abnormalPods.length})`,
              children: (
                <Table<AbnormalPod>
                  rowKey={(r) => `${r.namespace}/${r.name}`}
                  loading={podsLoading}
                  columns={podColumns}
                  dataSource={abnormalPods}
                  pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
                  locale={{ emptyText: <EmptyState title="暂无异常 Pod" /> }}
                />
              ),
            },
            {
              key: 'nodes',
              label: `异常节点 (${abnormalNodes.length})`,
              children: (
                <Table<AbnormalNode>
                  rowKey="node_name"
                  loading={nodesLoading}
                  columns={nodeColumns}
                  dataSource={abnormalNodes}
                  pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
                  locale={{ emptyText: <EmptyState title="暂无异常节点" /> }}
                />
              ),
            },
          ]}
        />
      )}

      <Modal
        title="通知预览"
        open={previewOpen}
        width={720}
        onCancel={() => {
          setPreviewOpen(false);
          setPendingNotify(null);
        }}
        onOk={() => notifyForm.submit()}
        confirmLoading={notifyMutation.isPending}
        okText="确认发送"
        destroyOnHidden
      >
        <Descriptions size="small" column={2} style={{ marginBottom: 16 }}>
          <Descriptions.Item label="受影响应用">{previewApps.length}</Descriptions.Item>
          <Descriptions.Item label="待通知成员">{memberCount}</Descriptions.Item>
        </Descriptions>

        {previewApps.length > 0 && (
          <Table
            size="small"
            rowKey="application_id"
            pagination={false}
            style={{ marginBottom: 16 }}
            dataSource={previewApps}
            columns={[
              { title: '应用', dataIndex: 'application_name', key: 'application_name' },
              {
                title: '分组',
                dataIndex: 'group_names',
                key: 'group_names',
                render: (names: string[]) => names?.join(', ') || '-',
              },
              {
                title: '成员数',
                key: 'members',
                width: 80,
                render: (_, r) => r.members.length,
              },
            ]}
            expandable={{
              expandedRowRender: (record) => (
                <Table
                  size="small"
                  rowKey="user_id"
                  pagination={false}
                  dataSource={record.members}
                  columns={[
                    { title: '用户', dataIndex: 'display_name', key: 'display_name' },
                    { title: '邮箱', dataIndex: 'email', key: 'email' },
                    { title: '角色', dataIndex: 'role_name', key: 'role_name' },
                  ]}
                />
              ),
            }}
          />
        )}

        <Form
          layout="vertical"
          form={notifyForm}
          onFinish={(v) => {
            if (!pendingNotify) return;
            notifyMutation.mutate({
              ...pendingNotify,
              subject: v.subject,
              body: v.body,
            });
          }}
        >
          <Form.Item name="subject" label="通知标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input />
          </Form.Item>
          <Form.Item name="body" label="通知内容" rules={[{ required: true, message: '请输入内容' }]}>
            <Input.TextArea rows={4} />
          </Form.Item>
        </Form>
      </Modal>
    </Space>
  );
}

function defaultSubject(body: NotifyAffectedInput): string {
  if (body.scope === 'pod') return `Pod 异常告警：${body.pod_namespace}/${body.pod_name}`;
  if (body.scope === 'node') return `节点异常告警：${body.node_name}`;
  return '集群异常告警';
}

function defaultBody(body: NotifyAffectedInput): string {
  const time = formatTime(new Date().toISOString());
  if (body.scope === 'pod') {
    return `检测到 Pod ${body.pod_namespace}/${body.pod_name}（节点 ${body.node_name || '-'}）运行异常，请及时关注。时间：${time}`;
  }
  if (body.scope === 'node') {
    return `检测到节点 ${body.node_name} 状态异常，其上工作负载可能受影响，请及时关注。时间：${time}`;
  }
  return `集群存在异常 Pod 或节点，您负责的应用可能受影响，请及时关注。时间：${time}`;
}
