import { useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
  App,
} from 'antd';
import {
  PlusOutlined,
  ThunderboltOutlined,
  KeyOutlined,
  EditOutlined,
  DeleteOutlined,
  DashboardOutlined,
  ToolOutlined,
} from '@ant-design/icons';
import { NodeMonitorTab } from './NodeMonitorTab';
import { ClusterOpsTab } from './ClusterOpsTab';
import { AbnormalNotifyTab } from './AbnormalNotifyTab';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { clusterApi, type UpdateClusterInput } from '@/api/clusters';
import type { Cluster, Credential } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatRelative } from '@/utils/format';
import { NETWORK_PROFILE_OPTIONS, getNetworkProfileOption, resolveClusterNetworkMeta, buildNetworkProfileConfig, type NetworkProfile } from './networkProfiles';

const PROVIDER_OPTIONS = [
  { label: '阿里云 ACK', value: 'aliyun' },
  { label: '腾讯云 TKE', value: 'tencent' },
  { label: '华为云 CCE', value: 'huawei' },
  { label: 'AWS EKS', value: 'aws' },
  { label: 'Azure AKS', value: 'azure' },
  { label: 'Google GKE', value: 'gcp' },
  { label: '自建', value: 'self' },
];

export default function ClustersPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [tab, setTab] = useState('clusters');
  const [selectedClusterId, setSelectedClusterId] = useState<number>();

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [editTarget, setEditTarget] = useState<Cluster | null>(null);
  const [editForm] = Form.useForm<UpdateClusterInput & Record<string, any>>();
  const [credOpen, setCredOpen] = useState(false);
  const [credForm] = Form.useForm();
  const [rotateTarget, setRotateTarget] = useState<Credential | null>(null);
  const [rotateForm] = Form.useForm();
  const [ipTargetCluster, setIpTargetCluster] = useState<Cluster | null>(null);
  const [ipCreateOpen, setIpCreateOpen] = useState(false);
  const [ipForm] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['clusters', { page, size }],
    queryFn: () => clusterApi.list({ page, size }),
  });

  const { data: credentials } = useQuery({
    queryKey: ['credentials'],
    queryFn: () => clusterApi.listCredentials({ page: 1, size: 100 }),
  });

  const { data: ipPools } = useQuery({
    queryKey: ['ip-pools', ipTargetCluster?.id],
    queryFn: () => clusterApi.listIPPools(ipTargetCluster!.id),
    enabled: !!ipTargetCluster?.id,
  });

  const createMutation = useMutation({
    mutationFn: (body: Partial<Cluster>) => clusterApi.create(body),
    onSuccess: () => {
      message.success('集群已接入');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
    onError: (e: any) => message.error(e?.message || '接入失败'),
  });

  const updateMutation = useMutation({
    mutationFn: async ({ id, body }: { id: number; body: UpdateClusterInput }) => {
      // 后台探测会递增 version，提交前拉取最新版本避免乐观锁冲突。
      const latest = await clusterApi.get(id);
      return clusterApi.update(id, { ...body, version: latest.version });
    },
    onSuccess: () => {
      message.success('集群已更新');
      setEditTarget(null);
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
    onError: (e: any) => {
      const msg = e?.message || '更新失败';
      if (/concurrently|refresh/i.test(msg)) {
        message.error('集群状态已变更（可能刚完成探测），请关闭后重新打开编辑再保存');
        queryClient.invalidateQueries({ queryKey: ['clusters'] });
      } else {
        message.error(msg);
      }
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => clusterApi.delete(id),
    onSuccess: () => {
      message.success('集群已删除');
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const probeMutation = useMutation({
    mutationFn: (id: number) => clusterApi.probe(id),
    onSuccess: (res) => {
      const nodeCount = (res as any)?.node_count;
      message.success(`探测成功${nodeCount != null ? `，节点数：${nodeCount}` : ''}`);
      queryClient.invalidateQueries({ queryKey: ['clusters'] });
    },
    onError: (e: any) => message.error(e?.message || '探测失败'),
  });

  const createCredMutation = useMutation({
    mutationFn: (body: { name: string; type: string; description?: string; kubeconfig: string }) =>
      clusterApi.createCredential(body),
    onSuccess: () => {
      message.success('凭证已上传');
      setCredOpen(false);
      credForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['credentials'] });
    },
    onError: (e: any) => message.error(e?.message || '上传失败'),
  });

  const rotateCredMutation = useMutation({
    mutationFn: ({ id, kubeconfig }: { id: number; kubeconfig: string }) =>
      clusterApi.rotateCredential(id, { kubeconfig }),
    onSuccess: () => {
      message.success('凭证已轮换');
      setRotateTarget(null);
      rotateForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['credentials'] });
    },
    onError: (e: any) => message.error(e?.message || '轮换失败'),
  });

  const deleteCredMutation = useMutation({
    mutationFn: (id: number) => clusterApi.deleteCredential(id),
    onSuccess: () => {
      message.success('凭证已删除');
      queryClient.invalidateQueries({ queryKey: ['credentials'] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const createIPPoolMutation = useMutation({
    mutationFn: ({ clusterId, body }: { clusterId: number; body: Record<string, any> }) =>
      clusterApi.createIPPool(clusterId, body),
    onSuccess: () => {
      message.success('IP 池已创建');
      setIpCreateOpen(false);
      ipForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['ip-pools', ipTargetCluster?.id] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const deleteIPPoolMutation = useMutation({
    mutationFn: (id: number) => clusterApi.deleteIPPool(id),
    onSuccess: () => {
      message.success('IP 池已删除');
      queryClient.invalidateQueries({ queryKey: ['ip-pools', ipTargetCluster?.id] });
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  // IP 池表单：监听 provider 字段，用于动态展示 underlay 扩展配置。
  const providerWatching = Form.useWatch('provider', ipForm);
  const isUnderlayProvider = providerWatching === 'macvlan' || providerWatching === 'ipvlan';

  // 打开「新建 IP 池」时，按当前集群 CNI 自动预选 Provider。
  useEffect(() => {
    if (!ipCreateOpen || !ipTargetCluster) return;
    const { cni } = resolveClusterNetworkMeta(ipTargetCluster.metadata);
    const map: Record<string, string> = {
      'whereabouts': 'whereabouts',
      'calico': 'calico-ipam',
      'kube-ovn': 'kube-ovn',
      'macvlan': 'macvlan',
      'ipvlan': 'ipvlan',
    };
    ipForm.setFieldsValue({ provider: (cni && map[cni]) || 'whereabouts' });
  }, [ipCreateOpen, ipTargetCluster, ipForm]);

  const clusterColumns: ColumnsType<Cluster> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'API Server', dataIndex: 'api_server', key: 'api_server', ellipsis: true, render: (v?: string) => v || '-' },
    { title: '版本', dataIndex: 'k8s_version', key: 'k8s_version', width: 100, render: (v?: string) => v || '-' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 100,
      render: (v: string) => <ResourceStatus status={v} />,
    },
    { title: '节点数', dataIndex: 'node_count', key: 'node_count', width: 80, render: (v?: number) => v ?? '-' },
    { title: '区域', dataIndex: 'region', key: 'region', width: 120, render: (v?: string) => v || '-' },
    {
      title: '网络方案',
      key: 'network_profile',
      width: 140,
      render: (_, record) => {
        const np = record.metadata?.network_profile;
        const profile = typeof np === 'string' ? np : np?.profile;
        if (!profile) return <Tag>默认</Tag>;
        const opt = getNetworkProfileOption(profile as NetworkProfile);
        return opt ? <Tag color={opt.supportsUnderlay ? 'green' : 'blue'}>{opt.label}</Tag> : <Tag>{profile}</Tag>;
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 420,
      render: (_, record) => (
        <Space wrap>
          <Button
            type="link"
            size="small"
            icon={<DashboardOutlined />}
            onClick={() => {
              setSelectedClusterId(record.id);
              setTab('monitor');
            }}
          >
            监控
          </Button>
          <Button
            type="link"
            size="small"
            icon={<ToolOutlined />}
            onClick={() => {
              setSelectedClusterId(record.id);
              setTab('ops');
            }}
          >
            运维
          </Button>
          <Button
            type="link"
            size="small"
            icon={<ThunderboltOutlined />}
            loading={probeMutation.isPending}
            onClick={() => probeMutation.mutate(record.id)}
          >
            探测
          </Button>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => {
              const { profile, cni, npObj } = resolveClusterNetworkMeta(record.metadata);
              setEditTarget(record);
              editForm.setFieldsValue({
                display_name: record.display_name,
                description: record.description,
                region: record.region,
                environment: record.environment,
                version: record.version,
                network_profile: profile,
                cni,
                cidr: npObj?.cidr,
                supernet_cidr: npObj?.supernet_cidr,
                vlan_id: npObj?.vlan_id,
                parent_interface: npObj?.parent_interface,
                gateway: npObj?.gateway,
                bgp_peer_ip: npObj?.bgp_peer_ip,
                bgp_peer_asn: npObj?.bgp_peer_asn,
                local_asn: npObj?.local_asn,
                multus_enabled: npObj?.multus_enabled ? 1 : 0,
                insecure_skip_tls: !!record.insecure_skip_tls,
              });
            }}
          >
            编辑
          </Button>
          <Button type="link" size="small" onClick={() => setIpTargetCluster(record)}>
            IP 池
          </Button>
          <Button
            type="link"
            size="small"
            danger
            icon={<DeleteOutlined />}
            onClick={() =>
              confirmDanger({
                title: '删除集群',
                content: `确定删除集群「${record.name}」吗？若集群仍被工作空间绑定或存在分组，将无法删除。`,
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

  const credColumns: ColumnsType<Credential> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '类型', dataIndex: 'type', key: 'type', width: 140 },
    { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true, render: (v?: string) => v || '-' },
    { title: '最近轮换', dataIndex: 'last_rotated_at', key: 'last_rotated_at', width: 180, render: (t?: string) => (t ? formatTime(t) : '-') },
    { title: '创建时间', dataIndex: 'created_at', key: 'created_at', width: 180, render: (t: string) => formatTime(t) },
    {
      title: '操作',
      key: 'actions',
      width: 180,
      render: (_, record) => (
        <Space>
          <Button type="link" size="small" onClick={() => { setRotateTarget(record); rotateForm.resetFields(); }}>
            轮换
          </Button>
          <Button
            type="link"
            size="small"
            danger
            onClick={() =>
              confirmDanger({
                title: '删除凭证',
                content: `确定删除凭证「${record.name}」吗？`,
                onOk: () => deleteCredMutation.mutateAsync(record.id),
              })
            }
          >
            删除
          </Button>
        </Space>
      ),
    },
  ];

  const ipPoolColumns = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'CIDR', dataIndex: 'cidr', key: 'cidr' },
    {
      title: 'Provider',
      dataIndex: 'provider',
      key: 'provider',
      width: 140,
      render: (v: string) => {
        if (!v) return <Tag>未设置</Tag>;
        const colorMap: Record<string, string> = {
          macvlan: 'green',
          ipvlan: 'green',
          'calico-ipam': 'blue',
          whereabouts: 'blue',
          'kube-ovn': 'blue',
          metallb: 'purple',
        };
        return <Tag color={colorMap[v] || 'default'}>{v}</Tag>;
      },
    },
    { title: '网关', dataIndex: 'gateway', key: 'gateway', width: 130, render: (v?: string) => v || '-' },
    {
      title: '容量',
      key: 'capacity',
      width: 110,
      render: (_: any, r: any) => `${r.allocated_count ?? 0} / ${r.total_count ?? 0}`,
    },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_: any, record: any) => (
        <Button
          type="link"
          size="small"
          danger
          onClick={() =>
            confirmDanger({
              title: '删除 IP 池',
              content: `确定删除 IP 池「${record.name}」吗？`,
              onOk: () => deleteIPPoolMutation.mutateAsync(record.id),
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
      title="集群管理"
      subtitle="接入并管理 Kubernetes 集群"
      extra={
        <Space>
          <Button icon={<KeyOutlined />} onClick={() => setCredOpen(true)}>
            上传凭证
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
            接入集群
          </Button>
        </Space>
      }
    >
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'clusters',
            label: '集群',
            children: (
              <Table
                rowKey="id"
                loading={isLoading}
                columns={clusterColumns}
                dataSource={data?.items || []}
                locale={{ emptyText: <EmptyState title="暂无集群" /> }}
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
            ),
          },
          {
            key: 'credentials',
            label: '凭证管理',
            children: (
              <Table
                rowKey="id"
                columns={credColumns}
                dataSource={credentials?.items || []}
                locale={{ emptyText: <EmptyState title="暂无凭证" actionText="上传凭证" onAction={() => setCredOpen(true)} /> }}
                pagination={false}
              />
            ),
          },
          {
            key: 'monitor',
            label: '节点监控',
            children: (
              <NodeMonitorTab
                clusters={data?.items || []}
                clusterId={selectedClusterId}
                onClusterChange={setSelectedClusterId}
              />
            ),
          },
          {
            key: 'ops',
            label: '集群运维',
            children: (
              <ClusterOpsTab
                clusters={data?.items || []}
                clusterId={selectedClusterId}
                onClusterChange={setSelectedClusterId}
              />
            ),
          },
          {
            key: 'notify',
            label: '异常通知',
            children: (
              <AbnormalNotifyTab
                clusters={data?.items || []}
                clusterId={selectedClusterId}
                onClusterChange={setSelectedClusterId}
              />
            ),
          },
        ]}
      />

      <Modal
        title="接入集群"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        width={620}
      >
        <Form layout="vertical" form={createForm} initialValues={{ network_profile: 'dev-single', cni: 'calico' }} onFinish={(v) => {
          // provider 不属于后端 Cluster 字段，放入 labels 保留。
          // network_profile / cni / underlay 配置放入 metadata.network_profile（对象形式）。
          const { provider, kubeconfig, network_profile, cni, cidr, supernet_cidr, vlan_id, parent_interface, gateway, bgp_peer_ip, bgp_peer_asn, local_asn, multus_enabled, ...rest } = v;
          const np = network_profile as NetworkProfile | undefined;
          const metadata: Record<string, any> = {};
          if (np) {
            metadata.network_profile = buildNetworkProfileConfig(np, {
              cni, cidr, supernet_cidr, vlan_id, parent_interface, gateway,
              bgp_peer_ip, bgp_peer_asn, local_asn, multus_enabled,
            });
          }
          createMutation.mutate({
            ...rest,
            kubeconfig,
            insecure_skip_tls: !!v.insecure_skip_tls,
            labels: provider ? { provider } : {},
            metadata,
          });
        }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：prod-ack-01" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="如：生产集群 01" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="provider" label="提供商" style={{ flex: 1 }}>
              <Select options={PROVIDER_OPTIONS} placeholder="选择提供商" allowClear />
            </Form.Item>
            <Form.Item name="region" label="区域" style={{ flex: 1 }}>
              <Input placeholder="如：cn-hangzhou" />
            </Form.Item>
          </Space>
          <Card size="small" title="网络方案" style={{ marginBottom: 16 }}>
            <Form.Item shouldUpdate noStyle>
              {({ getFieldValue }) => {
                const profile = getFieldValue('network_profile') as NetworkProfile | undefined;
                const opt = profile ? getNetworkProfileOption(profile) : undefined;
                return (
                  <>
                    <Form.Item name="network_profile" label="方案" extra={opt?.description} tooltip="不同规模集群采用不同网络方案，由集群 CNI 实现；平台只做 IPAM 账本与 annotation 注入">
                      <Select
                        placeholder="选择网络方案（默认 dev-single）"
                        allowClear
                        options={NETWORK_PROFILE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
                      />
                    </Form.Item>
                    {opt && opt.cniOptions.length > 0 && (
                      <Form.Item name="cni" label="CNI 插件" rules={[{ required: true, message: '请选择 CNI' }]}>
                        <Select options={opt.cniOptions} placeholder="选择集群已安装的 CNI" />
                      </Form.Item>
                    )}
                    {(profile === 'dev-single' || profile === 'medium-overlay') && (
                      <>
                        <Form.Item
                          name="multus_enabled"
                          label="Multus 副网卡"
                          extra="必须开启：Overlay 网段不可从集群外直连，业务口走 Underlay 固定 IP"
                        >
                          <Select options={[{ label: '关闭', value: 0 }, { label: '开启', value: 1 }]} />
                        </Form.Item>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="vlan_id" label="VLAN ID" style={{ flex: 1 }}>
                            <Input placeholder="如 100，对应 NAD 名 macvlan-100" />
                          </Form.Item>
                          <Form.Item name="parent_interface" label="父接口" style={{ flex: 1 }}>
                            <Input placeholder="eth0 / eno1" />
                          </Form.Item>
                        </Space>
                      </>
                    )}
                    {profile === 'large-underlay' && (
                      <>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="cidr" label="集群 CIDR（/16）" style={{ flex: 1 }} rules={[{ required: true, message: '如 10.1.0.0/16' }]}>
                            <Input placeholder="10.1.0.0/16" />
                          </Form.Item>
                          <Form.Item name="supernet_cidr" label="超网（/8）" style={{ flex: 1 }} rules={[{ required: true, message: '如 10.0.0.0/8' }]}>
                            <Input placeholder="10.0.0.0/8" />
                          </Form.Item>
                        </Space>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="vlan_id" label="VLAN ID" style={{ flex: 1 }}>
                            <Input placeholder="如 100" />
                          </Form.Item>
                          <Form.Item name="parent_interface" label="父接口" style={{ flex: 1 }} rules={[{ required: true, message: '如 eth0' }]}>
                            <Input placeholder="eth0 / eno1" />
                          </Form.Item>
                          <Form.Item name="gateway" label="网关" style={{ flex: 1 }}>
                            <Input placeholder="如 10.1.0.1" />
                          </Form.Item>
                        </Space>
                        <Form.Item name="multus_enabled" valuePropName="checked" label="Multus 双网卡" extra="开启后默认网卡走 Overlay（集群内通信），副网卡走 Underlay 固定 IP 直连">
                          <Select options={[{ label: '关闭', value: 0 }, { label: '开启', value: 1 }]} />
                        </Form.Item>
                      </>
                    )}
                    {profile === 'xlarge-bgp' && (
                      <>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="bgp_peer_ip" label="BGP Peer IP" style={{ flex: 1 }} rules={[{ required: true }]}>
                            <Input placeholder="核心交换机/RR IP" />
                          </Form.Item>
                          <Form.Item name="bgp_peer_asn" label="对端 ASN" style={{ flex: 1 }} rules={[{ required: true }]}>
                            <Input placeholder="如 64512" />
                          </Form.Item>
                          <Form.Item name="local_asn" label="本集群 ASN" style={{ flex: 1 }} rules={[{ required: true }]}>
                            <Input placeholder="如 64513" />
                          </Form.Item>
                        </Space>
                      </>
                    )}
                  </>
                );
              }}
            </Form.Item>
          </Card>
          <Form.Item
            name="api_server"
            label="API Server"
            extra="可选，留空时将从 kubeconfig 自动解析"
          >
            <Input placeholder="如：https://10.0.0.1:6443" />
          </Form.Item>
          <Form.Item
            name="kubeconfig"
            label="kubeconfig 内容"
            rules={[{ required: true, message: '请输入 kubeconfig' }]}
            extra="host-net 开发模式请使用 deploy/export/kubeconfig-vortexops.yaml（server 通常为 https://192.168.65.3:6443）"
          >
            <Input.TextArea rows={8} placeholder="粘贴 kubeconfig 内容（YAML）" />
          </Form.Item>
          <Form.Item name="insecure_skip_tls" valuePropName="checked" extra="仅开发环境：server 地址不在证书 SAN 内时勾选（如使用 compose 网关 172.22.0.1）">
            <Checkbox>跳过 TLS 证书校验</Checkbox>
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`编辑集群 - ${editTarget?.name || ''}`}
        open={!!editTarget}
        onCancel={() => setEditTarget(null)}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnHidden
        width={620}
      >
        <Form
          layout="vertical"
          form={editForm}
          onFinish={(v) => {
            const {
              network_profile,
              cni,
              cidr,
              supernet_cidr,
              vlan_id,
              parent_interface,
              gateway,
              bgp_peer_ip,
              bgp_peer_asn,
              local_asn,
              multus_enabled,
              kubeconfig,
              ...rest
            } = v as UpdateClusterInput & Record<string, any>;
            const existingMeta = { ...(editTarget?.metadata || {}) };
            let metadata = existingMeta;
            if (network_profile) {
              const existingNp =
                typeof existingMeta.network_profile === 'object' ? existingMeta.network_profile : undefined;
              metadata = {
                ...existingMeta,
                network_profile: buildNetworkProfileConfig(
                  network_profile as NetworkProfile,
                  { cni, cidr, supernet_cidr, vlan_id, parent_interface, gateway, bgp_peer_ip, bgp_peer_asn, local_asn, multus_enabled },
                  existingNp,
                ),
              };
            }
            const body: UpdateClusterInput = {
              display_name: rest.display_name,
              description: rest.description,
              region: rest.region,
              environment: rest.environment,
              metadata,
              insecure_skip_tls: !!(v as any).insecure_skip_tls,
            };
            if (kubeconfig?.trim()) {
              body.kubeconfig = kubeconfig.trim();
            }
            updateMutation.mutate({ id: editTarget!.id, body });
          }}
        >
          <Form.Item label="标识名" extra="创建后不可修改">
            <Input value={editTarget?.name} disabled />
          </Form.Item>
          <Form.Item label="API Server" extra="注册时解析的地址；更换 kubeconfig 后探测会按新配置连接">
            <Input value={(editTarget as any)?.api_server} disabled />
          </Form.Item>
          <Form.Item
            name="kubeconfig"
            label="更新 kubeconfig"
            extra="可选。host-net 请粘贴 deploy/export/kubeconfig-vortexops.yaml（server 为 https://192.168.65.3:6443）"
          >
            <Input.TextArea rows={6} placeholder="留空则不修改；粘贴新 kubeconfig 可修复连接问题" />
          </Form.Item>
          <Form.Item name="insecure_skip_tls" valuePropName="checked" extra="server 不在证书 SAN 内时勾选（如 172.22.0.1）；正常应使用 192.168.65.3 且无需勾选">
            <Checkbox>跳过 TLS 证书校验</Checkbox>
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="如：生产集群 01" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item name="region" label="区域" style={{ flex: 1 }}>
              <Input placeholder="如：cn-hangzhou" />
            </Form.Item>
            <Form.Item name="environment" label="环境" style={{ flex: 1 }}>
              <Input placeholder="如：production" />
            </Form.Item>
          </Space>
          <Card size="small" title="网络方案" style={{ marginBottom: 8 }}>
            <Form.Item shouldUpdate noStyle>
              {({ getFieldValue }) => {
                const profile = getFieldValue('network_profile') as NetworkProfile | undefined;
                const opt = profile ? getNetworkProfileOption(profile) : undefined;
                return (
                  <>
                    <Form.Item name="network_profile" label="方案" extra={opt?.description}>
                      <Select
                        placeholder="选择网络方案"
                        allowClear
                        options={NETWORK_PROFILE_OPTIONS.map((o) => ({ label: o.label, value: o.value }))}
                      />
                    </Form.Item>
                    {opt && opt.cniOptions.length > 0 && (
                      <Form.Item name="cni" label="CNI 插件" rules={[{ required: true, message: '请选择 CNI' }]}>
                        <Select options={opt.cniOptions} placeholder="选择集群已安装的 CNI" />
                      </Form.Item>
                    )}
                    {(profile === 'dev-single' || profile === 'medium-overlay') && (
                      <>
                        <Form.Item
                          name="multus_enabled"
                          label="Multus 副网卡"
                          extra="必须开启：Overlay 网段不可从集群外直连，业务口走 Underlay 固定 IP"
                        >
                          <Select options={[{ label: '关闭', value: 0 }, { label: '开启', value: 1 }]} />
                        </Form.Item>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="vlan_id" label="VLAN ID" style={{ flex: 1 }}>
                            <Input placeholder="如 100，对应 NAD 名 macvlan-100" />
                          </Form.Item>
                          <Form.Item name="parent_interface" label="父接口" style={{ flex: 1 }}>
                            <Input placeholder="eth0 / eno1" />
                          </Form.Item>
                        </Space>
                      </>
                    )}
                    {profile === 'large-underlay' && (
                      <>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="cidr" label="集群 CIDR（/16）" style={{ flex: 1 }}>
                            <Input placeholder="10.1.0.0/16" />
                          </Form.Item>
                          <Form.Item name="supernet_cidr" label="超网（/8）" style={{ flex: 1 }}>
                            <Input placeholder="10.0.0.0/8" />
                          </Form.Item>
                        </Space>
                        <Space style={{ width: '100%' }} size="middle">
                          <Form.Item name="vlan_id" label="VLAN ID" style={{ flex: 1 }}>
                            <Input placeholder="如 100" />
                          </Form.Item>
                          <Form.Item name="parent_interface" label="父接口" style={{ flex: 1 }}>
                            <Input placeholder="eth0 / eno1" />
                          </Form.Item>
                          <Form.Item name="gateway" label="网关" style={{ flex: 1 }}>
                            <Input placeholder="如 10.1.0.1" />
                          </Form.Item>
                        </Space>
                        <Form.Item name="multus_enabled" label="Multus 双网卡">
                          <Select options={[{ label: '关闭', value: 0 }, { label: '开启', value: 1 }]} />
                        </Form.Item>
                      </>
                    )}
                    {profile === 'xlarge-bgp' && (
                      <Space style={{ width: '100%' }} size="middle">
                        <Form.Item name="bgp_peer_ip" label="BGP Peer IP" style={{ flex: 1 }}>
                          <Input placeholder="核心交换机/RR IP" />
                        </Form.Item>
                        <Form.Item name="bgp_peer_asn" label="对端 ASN" style={{ flex: 1 }}>
                          <Input placeholder="如 64512" />
                        </Form.Item>
                        <Form.Item name="local_asn" label="本集群 ASN" style={{ flex: 1 }}>
                          <Input placeholder="如 64513" />
                        </Form.Item>
                      </Space>
                    )}
                  </>
                );
              }}
            </Form.Item>
          </Card>
          <Form.Item name="version" hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="上传凭证"
        open={credOpen}
        onCancel={() => setCredOpen(false)}
        onOk={() => credForm.submit()}
        confirmLoading={createCredMutation.isPending}
        destroyOnHidden
      >
        <Form layout="vertical" form={credForm} onFinish={(v) => createCredMutation.mutate({ ...v, type: v.type || 'kubeconfig' })} initialValues={{ type: 'kubeconfig' }}>
          <Form.Item name="name" label="凭证名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：prod-kubeconfig" />
          </Form.Item>
          <Form.Item name="type" label="类型" rules={[{ required: true }]}>
            <Select
              options={[
                { label: 'kubeconfig', value: 'kubeconfig' },
                { label: 'service_account', value: 'service_account' },
              ]}
            />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input />
          </Form.Item>
          <Form.Item name="kubeconfig" label="kubeconfig 内容" rules={[{ required: true, message: '请输入 kubeconfig' }]}>
            <Input.TextArea rows={8} placeholder="粘贴 kubeconfig 内容" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`轮换凭证 - ${rotateTarget?.name || ''}`}
        open={!!rotateTarget}
        onCancel={() => setRotateTarget(null)}
        onOk={() => rotateForm.submit()}
        confirmLoading={rotateCredMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={rotateForm}
          onFinish={(v) => rotateTarget && rotateCredMutation.mutate({ id: rotateTarget.id, kubeconfig: v.kubeconfig })}
        >
          <Form.Item name="kubeconfig" label="新 kubeconfig 内容" rules={[{ required: true, message: '请输入 kubeconfig' }]}>
            <Input.TextArea rows={8} placeholder="粘贴新 kubeconfig 内容" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`IP 池 - ${ipTargetCluster?.name || ''}`}
        open={!!ipTargetCluster}
        onCancel={() => setIpTargetCluster(null)}
        footer={null}
        width={720}
        destroyOnHidden
      >
        {(() => {
          const { profile, cni } = resolveClusterNetworkMeta(ipTargetCluster?.metadata);
          const profileOpt = profile ? getNetworkProfileOption(profile) : undefined;
          const overlayDirect = !profile || profile === 'dev-single' || profile === 'medium-overlay';
          return (
            <Alert
              type={overlayDirect || !cni ? 'warning' : 'success'}
              showIcon
              style={{ marginBottom: 12 }}
              message={
                <span>
                  集群网络方案：<b>{profileOpt?.label || profile || '未配置（默认 dev-single）'}</b>
                  {cni ? <span>，CNI=<b>{cni}</b></span> : <span>，<b>未配置 CNI</b></span>}
                </span>
              }
              description={
                overlayDirect
                  ? 'Overlay/开发集群的 Pod Overlay IP 不可从集群外直连。请开启 Multus，并新建 macvlan/ipvlan Underlay IP 池作为业务口固定 IP。Flannel 单独不支持静态 IP。'
                  : profile === 'large-underlay'
                  ? 'Underlay 集群：新建 IP 池 Provider 应与 CNI 匹配（macvlan/ipvlan/kube-ovn），Pod 拿物理固定 IP 直连。'
                  : 'BGP 集群：新建 calico-ipam/whereabouts IP 池，Pod 固定 IP 经 BGP 宣告后三层直连。'
              }
            />
          );
        })()}
        <Space style={{ marginBottom: 12 }}>
          <Button type="primary" size="small" icon={<PlusOutlined />} onClick={() => setIpCreateOpen(true)}>
            新建 IP 池
          </Button>
        </Space>
        <Table
          rowKey="id"
          size="small"
          columns={ipPoolColumns}
          dataSource={ipPools || []}
          pagination={false}
          locale={{ emptyText: '暂无 IP 池' }}
        />
      </Modal>

      <Modal
        title="新建 IP 池"
        open={ipCreateOpen}
        onCancel={() => setIpCreateOpen(false)}
        onOk={() => ipForm.submit()}
        confirmLoading={createIPPoolMutation.isPending}
        destroyOnHidden
        width={560}
      >
        <Form
          layout="vertical"
          form={ipForm}
          onFinish={(v) => {
            if (!ipTargetCluster) return;
            const reservedIPs = (v.reserved_ips as string | undefined)
              ? (v.reserved_ips as string).split(/[,\n\r\s]+/).filter(Boolean)
              : [];
            const metadata: Record<string, any> = {};
            if (v.vlan_id != null && v.vlan_id !== '') metadata.vlan_id = Number(v.vlan_id);
            if (v.parent_interface) metadata.parent_interface = v.parent_interface;
            if (v.exclude_ranges) metadata.exclude_ranges = v.exclude_ranges.split(/[,\n\r\s]+/).filter(Boolean);
            createIPPoolMutation.mutate({
              clusterId: ipTargetCluster.id,
              body: {
                name: v.name,
                cidr: v.cidr,
                gateway: v.gateway || undefined,
                provider: v.provider,
                reserved_ips: reservedIPs.length > 0 ? reservedIPs : undefined,
                metadata: Object.keys(metadata).length > 0 ? metadata : undefined,
              },
            });
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="如：underlay-pool" />
          </Form.Item>
          <Form.Item
            name="provider"
            label="Provider"
            rules={[{ required: true, message: '请选择 Provider' }]}
            extra={(() => {
              const { cni } = resolveClusterNetworkMeta(ipTargetCluster?.metadata);
              if (!cni) return '集群未配置 CNI，请先在集群编辑里设置网络方案与 CNI';
              const map: Record<string, string> = {
                'whereabouts': 'whereabouts',
                'calico': 'calico-ipam',
                'kube-ovn': 'kube-ovn',
                'macvlan': 'macvlan',
                'ipvlan': 'ipvlan',
              };
              return `集群 CNI=${cni}，推荐 Provider=${map[cni] || cni}`;
            })()}
          >
            <Select
              options={[
                { label: 'Macvlan — 物理局域网 IP，外部可直连 Pod', value: 'macvlan' },
                { label: 'IPVLAN — 共享父 MAC，高密度 underlay', value: 'ipvlan' },
                { label: 'Kube-OVN — Overlay/Underlay 均可', value: 'kube-ovn' },
                { label: 'Calico IPAM — Overlay 固定 IP', value: 'calico-ipam' },
                { label: 'Whereabouts — Overlay 固定 IP（配合 Multus）', value: 'whereabouts' },
                { label: 'MetalLB — LoadBalancer VIP（非 Pod IP）', value: 'metallb' },
              ]}
            />
          </Form.Item>
          <Form.Item name="cidr" label="CIDR" rules={[{ required: true, message: '请输入 CIDR' }]} extra="IP 范围，须与 CNI 实际分配网段一致">
            <Input placeholder={providerWatching === 'macvlan' || providerWatching === 'ipvlan' ? '如 10.1.1.0/24（物理局域网网段）' : '如 10.42.0.0/24（集群 Pod 网段）'} />
          </Form.Item>
          <Form.Item name="gateway" label="网关" extra={isUnderlayProvider ? '物理网关，必填' : '可选'}>
            <Input placeholder={isUnderlayProvider ? '如 10.1.1.1' : '通常留空'} />
          </Form.Item>
          <Form.Item name="reserved_ips" label="保留 IP" extra="逗号或换行分隔，这些 IP 不分配给 Pod">
            <Input.TextArea rows={2} placeholder="如 10.1.1.1, 10.1.1.2" />
          </Form.Item>
          {isUnderlayProvider && (
            <>
              <Form.Item name="vlan_id" label="VLAN ID" extra="物理 VLAN，须与集群侧 NAD 一致">
                <Input placeholder="如 100" />
              </Form.Item>
              <Form.Item name="parent_interface" label="父接口" extra="节点物理网卡名">
                <Input placeholder="如 eth0" />
              </Form.Item>
            </>
          )}
          <Form.Item name="exclude_ranges" label="排除范围" extra="逗号分隔的 CIDR，不分配">
            <Input placeholder="如 10.1.1.0/26" />
          </Form.Item>
        </Form>
      </Modal>
    </PageContainer>
  );
}
