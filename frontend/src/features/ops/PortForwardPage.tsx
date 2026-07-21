import { useState } from 'react';
import { App, Button, Card, Form, Input, InputNumber, Select, Space, Table, Tag, Typography } from 'antd';
import { ApiOutlined, CopyOutlined, LinkOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { EmptyState } from '@/components/EmptyState';
import { clusterApi } from '@/api/clusters';
import { k8sApi } from '@/api/k8s';
import { opsApi, type PortForwardResult } from '@/api/ops';

const { Paragraph, Text } = Typography;

/**
 * PortForwardPage 端口转发（运维工具）：
 * 选择集群/Pod/端口 → apiserver 分配本地端口 → 后台 SPDY 转发 → 展示连接命令。
 * 转发在 apiserver 进程运行，用户可通过 kubectl/curl 访问 apiserver 节点的本地端口。
 */
export function PortForwardPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [clusterId, setClusterId] = useState<number>();
  const [latest, setLatest] = useState<PortForwardResult | null>(null);

  const { data: clustersPage } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
  });
  const clusters = clustersPage?.items ?? [];

  const { data: podsData, isLoading: podsLoading } = useQuery({
    queryKey: ['pf-pods', clusterId],
    queryFn: () => k8sApi.listPods(clusterId!),
    enabled: !!clusterId,
  });
  const pods = Array.isArray(podsData) ? podsData : [];

  // 拉取服务端活跃会话（合并 portforward）。
  const { data: liveSessions } = useQuery({
    queryKey: ['ops-active-sessions'],
    queryFn: () => opsApi.listActiveSessions(),
    refetchInterval: 10000,
  });
  const liveForwards = (liveSessions || []).filter((s: any) => s.type === 'portforward');

  const startMutation = useMutation({
    mutationFn: (v: any) =>
      opsApi.startPortForward({
        cluster_id: v.cluster_id,
        namespace: v.namespace,
        pod: v.pod,
        port: v.port,
        local_port: v.local_port,
      }),
    onSuccess: (res) => {
      setLatest(res);
      message.success(`端口转发已建立：${res.local_addr} → ${res.remote_port}`);
      queryClient.invalidateQueries({ queryKey: ['ops-active-sessions'] });
    },
    onError: (e: any) => message.error(e?.message || '启动端口转发失败'),
  });

  const closeMutation = useMutation({
    mutationFn: (sessionId: string) => opsApi.closeSession(sessionId),
    onSuccess: () => {
      message.success('已关闭端口转发');
      queryClient.invalidateQueries({ queryKey: ['ops-active-sessions'] });
    },
    onError: (e: any) => message.error(e?.message || '关闭失败'),
  });

  const columns: ColumnsType<any> = [
    { title: '会话ID', dataIndex: 'id', key: 'id', width: 200, render: (v: string) => <Text code copyable style={{ fontSize: 12 }}>{v}</Text> },
    { title: '集群', dataIndex: 'cluster_id', key: 'cluster_id', width: 90 },
    { title: '命名空间', dataIndex: 'namespace', key: 'namespace', width: 140 },
    { title: 'Pod', dataIndex: 'pod', key: 'pod', width: 200 },
    { title: '类型', dataIndex: 'type', key: 'type', width: 100, render: (v: string) => <Tag color={v === 'portforward' ? 'blue' : 'default'}>{v}</Tag> },
    { title: '启动时间', dataIndex: 'started_at', key: 'started_at', width: 170 },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_: any, r: any) => (
        <a style={{ color: '#ff4d4f' }} onClick={() => closeMutation.mutate(r.id)}>关闭</a>
      ),
    },
  ];

  return (
    <PageContainer title="端口转发" extra={<Tag icon={<ApiOutlined />} color="blue">运维工具</Tag>}>
      <Card title="新建端口转发">
        <Form form={form} layout="inline" onFinish={(v) => startMutation.mutate(v)}>
          <Form.Item label="集群" name="cluster_id" rules={[{ required: true, message: '选择集群' }]}>
            <Select
              style={{ width: 180 }}
              placeholder="选择集群"
              options={clusters.map((c: any) => ({ label: c.name, value: c.id }))}
              onChange={(v) => { setClusterId(v); form.setFieldValue('pod', undefined); }}
            />
          </Form.Item>
          <Form.Item label="命名空间" name="namespace">
            <Input style={{ width: 140 }} placeholder="命名空间" />
          </Form.Item>
          <Form.Item label="Pod" name="pod" rules={[{ required: true, message: '选择 Pod' }]}>
            <Select
              showSearch
              style={{ width: 260 }}
              loading={podsLoading}
              placeholder="选择 Pod"
              options={pods.map((p: any) => ({ label: `${p.metadata.namespace}/${p.metadata.name}`, value: p.metadata.name }))}
            />
          </Form.Item>
          <Form.Item label="容器端口" name="port" rules={[{ required: true, message: '输入容器端口' }]}>
            <InputNumber min={1} max={65535} style={{ width: 120 }} placeholder="如 8080" />
          </Form.Item>
          <Form.Item label="本地端口" name="local_port" tooltip="留空自动分配">
            <InputNumber min={0} max={65535} style={{ width: 120 }} placeholder="自动" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" icon={<LinkOutlined />} loading={startMutation.isPending}>
              建立转发
            </Button>
          </Form.Item>
        </Form>
      </Card>

      {latest && (
        <Card style={{ marginTop: 16 }} title="转发已建立" extra={<Tag color="success">活跃</Tag>}>
          <Space direction="vertical" style={{ width: '100%' }}>
            <Paragraph>
              <Text strong>本地地址：</Text>
              <Text code copyable>{latest.local_addr}</Text>
              <Button size="small" type="link" icon={<CopyOutlined />} onClick={() => { navigator.clipboard?.writeText(latest.local_addr); message.success('已复制'); }}>
                复制
              </Button>
            </Paragraph>
            <Paragraph>
              <Text strong>连接命令：</Text>
              <Text code copyable style={{ display: 'block', marginTop: 4 }}>
                curl http://{latest.local_addr}
              </Text>
            </Paragraph>
            <Paragraph type="secondary" style={{ fontSize: 12 }}>
              转发运行在 apiserver 节点。如需从浏览器或外部访问，请确保 apiserver 节点该端口可达，或通过 SSH 隧道访问。
            </Paragraph>
          </Space>
        </Card>
      )}

      <Card
        style={{ marginTop: 16 }}
        title="活跃转发"
        extra={
          <Button size="small" onClick={() => queryClient.invalidateQueries({ queryKey: ['ops-active-sessions'] })}>
            刷新
          </Button>
        }
      >
        <Table
          rowKey="id"
          columns={columns}
          dataSource={liveForwards}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无活跃端口转发" description="新建转发后会在此显示，可随时关闭" /> }}
        />
      </Card>
    </PageContainer>
  );
}

export default PortForwardPage;
