import { useState } from 'react';
import { Select, Tabs, Space, Input, App, Tag, Button, Modal, InputNumber, Table } from 'antd';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { clusterApi } from '@/api/clusters';
import { k8sApi } from '@/api/k8s';
import type {
  K8sNode,
  K8sDeployment,
  K8sStatefulSet,
  K8sDaemonSet,
  K8sPod,
  K8sService,
  K8sIngress,
  K8sPersistentVolume,
  K8sPVC,
  K8sStorageClass,
  K8sConfigMap,
  K8sSecret,
  K8sEvent,
} from '@/api/k8s';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatRelative } from '@/utils/format';

export default function K8sConsolePage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [clusterId, setClusterId] = useState<number>();
  const [namespace, setNamespace] = useState<string>('');

  const { data: clustersPage } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
  });
  const clusters = clustersPage?.items ?? [];

  return (
    <PageContainer title="K8s 运维" subtitle="集群节点、工作负载、存储、网络、配置与事件总览">
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          placeholder="选择集群"
          style={{ width: 240 }}
          showSearch
          optionFilterProp="label"
          options={clusters.map((c) => ({ label: c.display_name || c.name, value: c.id }))}
          onChange={(v) => setClusterId(v)}
          value={clusterId}
        />
        <Input
          placeholder="命名空间（留空查全集群）"
          style={{ width: 200 }}
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          allowClear
        />
      </Space>

      {!clusterId ? (
        <EmptyState title="请先选择一个集群" />
      ) : (
        <Tabs
          defaultActiveKey="nodes"
          items={[
            { key: 'nodes', label: '节点', children: <NodesTab clusterId={clusterId} /> },
            { key: 'workloads', label: '工作负载', children: <WorkloadsTab clusterId={clusterId} namespace={namespace} /> },
            { key: 'pods', label: 'Pod', children: <PodsTab clusterId={clusterId} namespace={namespace} /> },
            { key: 'storage', label: '存储', children: <StorageTab clusterId={clusterId} namespace={namespace} /> },
            { key: 'network', label: '网络', children: <NetworkTab clusterId={clusterId} namespace={namespace} /> },
            { key: 'config', label: '配置', children: <ConfigTab clusterId={clusterId} namespace={namespace} /> },
            { key: 'events', label: '事件', children: <EventsTab clusterId={clusterId} namespace={namespace} /> },
          ]}
        />
      )}
    </PageContainer>
  );
}

// --- 节点 Tab ---
function NodesTab({ clusterId }: { clusterId: number }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ['k8s', clusterId, 'nodes'],
    queryFn: () => k8sApi.listNodes(clusterId),
    enabled: !!clusterId,
  });

  const cordonMutation = useMutation({
    mutationFn: ({ node, unschedulable }: { node: string; unschedulable: boolean }) =>
      unschedulable ? k8sApi.cordonNode(clusterId, node) : k8sApi.uncordonNode(clusterId, node),
    onSuccess: () => {
      message.success('节点调度状态已更新');
      queryClient.invalidateQueries({ queryKey: ['k8s', clusterId, 'nodes'] });
    },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const drainMutation = useMutation({
    mutationFn: (node: string) => k8sApi.drainNode(clusterId, node),
    onSuccess: () => {
      message.success('节点驱逐已发起');
      queryClient.invalidateQueries({ queryKey: ['k8s', clusterId, 'nodes'] });
    },
    onError: (e: any) => message.error(e?.message || '驱逐失败'),
  });

  const columns: ColumnsType<K8sNode> = [
    { title: '节点名', dataIndex: ['metadata', 'name'], key: 'name' },
    {
      title: '状态',
      key: 'status',
      width: 120,
      render: (_, n) => {
        const ready = n.status.conditions?.find((c) => c.type === 'Ready');
        const schedulable = !n.spec.unschedulable;
        return (
          <Space>
            <Tag color={ready?.status === 'True' ? 'green' : 'red'}>
              {ready?.status === 'True' ? 'Ready' : 'NotReady'}
            </Tag>
            {!schedulable && <Tag color="orange">不可调度</Tag>}
          </Space>
        );
      },
    },
    {
      title: '版本',
      key: 'version',
      width: 140,
      render: (_, n) => n.status.nodeInfo?.kubeletVersion || '-',
    },
    {
      title: 'OS',
      key: 'os',
      width: 160,
      render: (_, n) => n.status.nodeInfo?.osImage || '-',
    },
    {
      title: 'CPU 容量',
      key: 'cpu',
      width: 110,
      render: (_, n) => n.status.capacity?.cpu || '-',
    },
    {
      title: '内存容量',
      key: 'memory',
      width: 140,
      render: (_, n) => n.status.capacity?.memory || '-',
    },
    {
      title: '创建时间',
      key: 'created',
      width: 180,
      render: (_, n) => formatTime(n.metadata.creationTimestamp),
    },
    {
      title: '操作',
      key: 'actions',
      width: 240,
      render: (_, n) => (
        <Space size="small">
          {n.spec.unschedulable ? (
            <Button size="small" type="link" loading={cordonMutation.isPending} onClick={() => cordonMutation.mutate({ node: n.metadata.name, unschedulable: false })}>
              恢复调度
            </Button>
          ) : (
            <Button size="small" type="link" loading={cordonMutation.isPending} onClick={() => cordonMutation.mutate({ node: n.metadata.name, unschedulable: true })}>
              设置不可调度
            </Button>
          )}
          <Button
            size="small"
            type="link"
            danger
            loading={drainMutation.isPending}
            onClick={() =>
              confirmDanger({
                title: `驱逐节点 ${n.metadata.name}`,
                content: '将驱逐该节点上所有可迁移的 Pod（DaemonSet 除外）。确定继续？',
                okText: '驱逐',
                onOk: () => drainMutation.mutateAsync(n.metadata.name),
              })
            }
          >
            驱逐
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Table<K8sNode>
      rowKey={(r) => r.metadata.name}
      loading={isLoading}
      dataSource={data}
      columns={columns}
      pagination={false}
      locale={{ emptyText: <EmptyState title="暂无节点" /> }}
    />
  );
}

// --- 工作负载 Tab ---
function WorkloadsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const [tab, setTab] = useState('deployments');
  return (
    <Tabs
      activeKey={tab}
      onChange={setTab}
      items={[
        { key: 'deployments', label: 'Deployments', children: <DeploymentsTab clusterId={clusterId} namespace={namespace} /> },
        { key: 'statefulsets', label: 'StatefulSets', children: <StatefulSetsTab clusterId={clusterId} namespace={namespace} /> },
        { key: 'daemonsets', label: 'DaemonSets', children: <DaemonSetsTab clusterId={clusterId} namespace={namespace} /> },
      ]}
    />
  );
}

function DeploymentsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [scaleTarget, setScaleTarget] = useState<K8sDeployment | null>(null);
  const [scaleValue, setScaleValue] = useState(1);
  const { data, isLoading } = useQuery({
    queryKey: ['k8s', clusterId, 'deployments', namespace],
    queryFn: () => k8sApi.listDeployments(clusterId, namespace || undefined),
    enabled: !!clusterId,
  });
  const scaleMutation = useMutation({
    mutationFn: ({ ns, name, replicas }: { ns: string; name: string; replicas: number }) =>
      k8sApi.scaleDeployment(clusterId, ns, name, replicas),
    onSuccess: () => {
      message.success('副本数已更新');
      setScaleTarget(null);
      queryClient.invalidateQueries({ queryKey: ['k8s', clusterId, 'deployments', namespace] });
    },
    onError: (e: any) => message.error(e?.message || '扩缩容失败'),
  });
  const columns: ColumnsType<K8sDeployment> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '期望副本', dataIndex: ['spec', 'replicas'], width: 100 },
    {
      title: '就绪副本',
      key: 'ready',
      width: 120,
      render: (_, d) => `${d.status.readyReplicas || 0} / ${d.spec.replicas || 0}`,
    },
    { title: '已更新', dataIndex: ['status', 'updatedReplicas'], width: 100 },
    { title: '创建时间', key: 'created', width: 180, render: (_, d) => formatTime(d.metadata.creationTimestamp) },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, d) => (
        <Button size="small" type="link" onClick={() => { setScaleTarget(d); setScaleValue(d.spec.replicas || 1); }}>
          扩缩容
        </Button>
      ),
    },
  ];
  return (
    <>
      <Table<K8sDeployment> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 Deployment" /> }} />
      <Modal
        title={`扩缩容 ${scaleTarget?.metadata.name}`}
        open={!!scaleTarget}
        onCancel={() => setScaleTarget(null)}
        onOk={() => scaleTarget && scaleMutation.mutate({ ns: scaleTarget.metadata.namespace, name: scaleTarget.metadata.name, replicas: scaleValue })}
        confirmLoading={scaleMutation.isPending}
      >
        <Space>
          <span>副本数：</span>
          <InputNumber min={0} max={1000} value={scaleValue} onChange={(v) => setScaleValue(v || 0)} />
        </Space>
      </Modal>
    </>
  );
}

function StatefulSetsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [scaleTarget, setScaleTarget] = useState<K8sStatefulSet | null>(null);
  const [scaleValue, setScaleValue] = useState(1);
  const { data, isLoading } = useQuery({
    queryKey: ['k8s', clusterId, 'statefulsets', namespace],
    queryFn: () => k8sApi.listStatefulSets(clusterId, namespace || undefined),
    enabled: !!clusterId,
  });
  const scaleMutation = useMutation({
    mutationFn: ({ ns, name, replicas }: { ns: string; name: string; replicas: number }) =>
      k8sApi.scaleStatefulSet(clusterId, ns, name, replicas),
    onSuccess: () => {
      message.success('副本数已更新');
      setScaleTarget(null);
      queryClient.invalidateQueries({ queryKey: ['k8s', clusterId, 'statefulsets', namespace] });
    },
    onError: (e: any) => message.error(e?.message || '扩缩容失败'),
  });
  const columns: ColumnsType<K8sStatefulSet> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '期望副本', dataIndex: ['spec', 'replicas'], width: 100 },
    { title: '就绪副本', key: 'ready', width: 120, render: (_, d) => `${d.status.readyReplicas || 0} / ${d.spec.replicas || 0}` },
    { title: '创建时间', key: 'created', width: 180, render: (_, d) => formatTime(d.metadata.creationTimestamp) },
    { title: '操作', key: 'actions', width: 120, render: (_, d) => <Button size="small" type="link" onClick={() => { setScaleTarget(d); setScaleValue(d.spec.replicas || 1); }}>扩缩容</Button> },
  ];
  return (
    <>
      <Table<K8sStatefulSet> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 StatefulSet" /> }} />
      <Modal title={`扩缩容 ${scaleTarget?.metadata.name}`} open={!!scaleTarget} onCancel={() => setScaleTarget(null)} onOk={() => scaleTarget && scaleMutation.mutate({ ns: scaleTarget.metadata.namespace, name: scaleTarget.metadata.name, replicas: scaleValue })} confirmLoading={scaleMutation.isPending}>
        <Space><span>副本数：</span><InputNumber min={0} max={1000} value={scaleValue} onChange={(v) => setScaleValue(v || 0)} /></Space>
      </Modal>
    </>
  );
}

function DaemonSetsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ['k8s', clusterId, 'daemonsets', namespace],
    queryFn: () => k8sApi.listDaemonSets(clusterId, namespace || undefined),
    enabled: !!clusterId,
  });
  const columns: ColumnsType<K8sDaemonSet> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '期望', dataIndex: ['status', 'desiredNumberScheduled'], width: 100 },
    { title: '就绪', key: 'ready', width: 120, render: (_, d) => `${d.status.numberReady || 0} / ${d.status.desiredNumberScheduled || 0}` },
    { title: '创建时间', key: 'created', width: 180, render: (_, d) => formatTime(d.metadata.creationTimestamp) },
  ];
  return <Table<K8sDaemonSet> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 DaemonSet" /> }} />;
}

// --- Pod Tab ---
function PodsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ['k8s', clusterId, 'pods', namespace],
    queryFn: () => k8sApi.listPods(clusterId, namespace || undefined),
    enabled: !!clusterId,
  });
  const deleteMutation = useMutation({
    mutationFn: ({ ns, name }: { ns: string; name: string }) => k8sApi.deletePod(clusterId, ns, name),
    onSuccess: () => {
      message.success('Pod 已删除（将由控制器重建）');
      queryClient.invalidateQueries({ queryKey: ['k8s', clusterId, 'pods', namespace] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });
  const columns: ColumnsType<K8sPod> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '节点', dataIndex: ['spec', 'nodeName'], width: 160 },
    {
      title: '状态',
      key: 'phase',
      width: 110,
      render: (_, p) => {
        const color = p.status.phase === 'Running' ? 'green' : p.status.phase === 'Pending' ? 'orange' : p.status.phase === 'Failed' ? 'red' : 'default';
        return <Tag color={color}>{p.status.phase}</Tag>;
      },
    },
    { title: 'Pod IP', dataIndex: ['status', 'podIP'], width: 140 },
    { title: '创建时间', key: 'created', width: 180, render: (_, p) => formatTime(p.metadata.creationTimestamp) },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, p) => (
        <Button size="small" type="link" danger onClick={() => confirmDanger({ title: `删除 Pod ${p.metadata.name}`, content: 'Pod 将由控制器重建。确定删除？', okText: '删除', onOk: () => deleteMutation.mutateAsync({ ns: p.metadata.namespace, name: p.metadata.name }) })}>
          删除
        </Button>
      ),
    },
  ];
  return <Table<K8sPod> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 Pod" /> }} />;
}

// --- 存储 Tab ---
function StorageTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const [tab, setTab] = useState('pv');
  return (
    <Tabs
      activeKey={tab}
      onChange={setTab}
      items={[
        { key: 'pv', label: 'PersistentVolume', children: <PVTab clusterId={clusterId} /> },
        { key: 'pvc', label: 'PVC', children: <PVCTab clusterId={clusterId} namespace={namespace} /> },
        { key: 'sc', label: 'StorageClass', children: <SCTab clusterId={clusterId} /> },
      ]}
    />
  );
}

function PVTab({ clusterId }: { clusterId: number }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'pv'], queryFn: () => k8sApi.listPersistentVolumes(clusterId), enabled: !!clusterId });
  const columns: ColumnsType<K8sPersistentVolume> = [
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '容量', key: 'capacity', width: 120, render: (_, p) => p.spec.capacity?.storage || '-' },
    { title: '访问模式', key: 'access', width: 160, render: (_, p) => (p.spec.accessModes || []).join(', ') },
    { title: 'StorageClass', dataIndex: ['spec', 'storageClassName'], width: 160 },
    { title: '状态', key: 'phase', width: 120, render: (_, p) => { const c = p.status.phase === 'Bound' ? 'green' : p.status.phase === 'Available' ? 'blue' : 'default'; return <Tag color={c}>{p.status.phase}</Tag>; } },
    { title: '创建时间', key: 'created', width: 180, render: (_, p) => formatTime(p.metadata.creationTimestamp) },
  ];
  return <Table<K8sPersistentVolume> rowKey={(r) => r.metadata.name} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 PV" /> }} />;
}

function PVCTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'pvc', namespace], queryFn: () => k8sApi.listPersistentVolumeClaims(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sPVC> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '请求容量', key: 'req', width: 120, render: (_, p) => p.spec.resources?.requests?.storage || '-' },
    { title: '访问模式', key: 'access', width: 160, render: (_, p) => (p.spec.accessModes || []).join(', ') },
    { title: 'StorageClass', dataIndex: ['spec', 'storageClassName'], width: 160 },
    { title: '状态', key: 'phase', width: 120, render: (_, p) => <Tag color={p.status.phase === 'Bound' ? 'green' : 'orange'}>{p.status.phase}</Tag> },
    { title: '创建时间', key: 'created', width: 180, render: (_, p) => formatTime(p.metadata.creationTimestamp) },
  ];
  return <Table<K8sPVC> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 PVC" /> }} />;
}

function SCTab({ clusterId }: { clusterId: number }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'sc'], queryFn: () => k8sApi.listStorageClasses(clusterId), enabled: !!clusterId });
  const columns: ColumnsType<K8sStorageClass> = [
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: 'Provisioner', dataIndex: 'provisioner', width: 240 },
    { title: '创建时间', key: 'created', width: 180, render: (_, s) => formatTime(s.metadata.creationTimestamp) },
  ];
  return <Table<K8sStorageClass> rowKey={(r) => r.metadata.name} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 StorageClass" /> }} />;
}

// --- 网络 Tab ---
function NetworkTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const [tab, setTab] = useState('svc');
  return (
    <Tabs activeKey={tab} onChange={setTab} items={[
      { key: 'svc', label: 'Service', children: <SvcTab clusterId={clusterId} namespace={namespace} /> },
      { key: 'ingress', label: 'Ingress', children: <IngressTab clusterId={clusterId} namespace={namespace} /> },
    ]} />
  );
}

function SvcTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'services', namespace], queryFn: () => k8sApi.listServices(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sService> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '类型', dataIndex: ['spec', 'type'], width: 120, render: (t: string) => <Tag>{t}</Tag> },
    { title: 'ClusterIP', dataIndex: ['spec', 'clusterIP'], width: 150 },
    { title: '端口', key: 'ports', width: 200, render: (_, s) => (s.spec.ports || []).map((p) => `${p.port}/${p.protocol}`).join(', ') },
    { title: '创建时间', key: 'created', width: 180, render: (_, s) => formatTime(s.metadata.creationTimestamp) },
  ];
  return <Table<K8sService> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 Service" /> }} />;
}

function IngressTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'ingresses', namespace], queryFn: () => k8sApi.listIngresses(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sIngress> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: 'Hosts', key: 'hosts', render: (_, ing) => (ing.spec.rules || []).map((r) => r.host).join(', ') || '-' },
    { title: '创建时间', key: 'created', width: 180, render: (_, ing) => formatTime(ing.metadata.creationTimestamp) },
  ];
  return <Table<K8sIngress> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 Ingress" /> }} />;
}

// --- 配置 Tab ---
function ConfigTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const [tab, setTab] = useState('cm');
  return (
    <Tabs activeKey={tab} onChange={setTab} items={[
      { key: 'cm', label: 'ConfigMap', children: <CMTab clusterId={clusterId} namespace={namespace} /> },
      { key: 'secret', label: 'Secret', children: <SecretTab clusterId={clusterId} namespace={namespace} /> },
    ]} />
  );
}

function CMTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'configmaps', namespace], queryFn: () => k8sApi.listConfigMaps(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sConfigMap> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '数据键数', key: 'keys', width: 100, render: (_, c) => Object.keys(c.data || {}).length },
    { title: '创建时间', key: 'created', width: 180, render: (_, c) => formatTime(c.metadata.creationTimestamp) },
  ];
  return <Table<K8sConfigMap> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 ConfigMap" /> }} />;
}

function SecretTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'secrets', namespace], queryFn: () => k8sApi.listSecrets(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sSecret> = [
    { title: '命名空间', dataIndex: ['metadata', 'namespace'], width: 140 },
    { title: '名称', dataIndex: ['metadata', 'name'] },
    { title: '类型', dataIndex: 'type', width: 200, render: (t: string) => <Tag>{t}</Tag> },
    { title: '创建时间', key: 'created', width: 180, render: (_, s) => formatTime(s.metadata.creationTimestamp) },
  ];
  return <Table<K8sSecret> rowKey={(r) => `${r.metadata.namespace}/${r.metadata.name}`} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无 Secret" /> }} />;
}

// --- 事件 Tab ---
function EventsTab({ clusterId, namespace }: { clusterId: number; namespace: string }) {
  const { data, isLoading } = useQuery({ queryKey: ['k8s', clusterId, 'events', namespace], queryFn: () => k8sApi.listEvents(clusterId, namespace || undefined), enabled: !!clusterId });
  const columns: ColumnsType<K8sEvent> = [
    { title: '最后发生', key: 'last', width: 180, render: (_, e) => formatRelative(e.lastTimestamp) },
    { title: '类型', dataIndex: 'type', width: 80, render: (t: string) => <Tag color={t === 'Warning' ? 'orange' : t === 'Normal' ? 'blue' : 'default'}>{t}</Tag> },
    { title: '原因', dataIndex: 'reason', width: 140 },
    { title: '对象', key: 'obj', width: 200, render: (_, e) => `${e.involvedObject.kind}/${e.involvedObject.name}` },
    { title: '命名空间', dataIndex: ['involvedObject', 'namespace'], width: 140 },
    { title: '消息', dataIndex: 'message' },
    { title: '次数', dataIndex: 'count', width: 80 },
  ];
  return <Table<K8sEvent> rowKey={(r) => r.metadata.name} loading={isLoading} dataSource={data} columns={columns} pagination={{ pageSize: 50 }} locale={{ emptyText: <EmptyState title="暂无事件" /> }} />;
}
