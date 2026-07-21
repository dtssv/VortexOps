import { useState } from 'react';
import { Card, Form, Select, Input, Button, Space, App, Modal, Tag } from 'antd';
import { CodeOutlined, ReloadOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';
import { PageContainer } from '@/components/PageContainer';
import { clusterApi } from '@/api/clusters';
import { k8sApi, type K8sPod } from '@/api/k8s';
import { PodTerminal } from './PodTerminal';

interface TerminalSession {
  clusterId: number;
  clusterName: string;
  namespace: string;
  pod: string;
  container: string;
}

function podName(p: K8sPod) { return p.metadata?.name ?? ''; }
function podNs(p: K8sPod) { return p.metadata?.namespace ?? ''; }
function podPhase(p: K8sPod) { return p.status?.phase ?? p.spec?.phase ?? ''; }
function podNode(p: K8sPod) { return p.spec?.nodeName ?? ''; }
function podCreated(p: K8sPod) { return p.metadata?.creationTimestamp ?? ''; }
function podContainers(p: K8sPod): string[] {
  const spec = (p as any).spec?.containers as Array<{ name: string }> | undefined;
  return spec ? spec.map((c) => c.name) : [];
}

/**
 * WebSSH 终端页：选择集群/命名空间/Pod/容器 → 打开交互式 xterm 终端。
 * 连接由后端 /api/v1/ops/exec/ws 建立，录像与行为审计在后端自动落库。
 */
export function WebTerminalPage() {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [clusterId, setClusterId] = useState<number>();
  const [namespace, setNamespace] = useState<string>('');
  const [session, setSession] = useState<TerminalSession | null>(null);
  const [podPickerOpen, setPodPickerOpen] = useState(false);

  const { data: clustersPage } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
  });
  const clusters = clustersPage?.items ?? [];

  const { data: podsData, isLoading: podsLoading } = useQuery({
    queryKey: ['k8s-pods', clusterId, namespace],
    queryFn: () => k8sApi.listPods(clusterId!, namespace || undefined),
    enabled: !!clusterId,
  });
  const pods: K8sPod[] = Array.isArray(podsData) ? podsData : [];

  const launch = () => {
    if (!clusterId || !namespace || !session?.pod) {
      message.warning('请选择集群、命名空间与 Pod');
      return;
    }
    setPodPickerOpen(false);
  };

  return (
    <PageContainer
      title="Pod 终端 (WebSSH)"
      extra={
        <Button icon={<ReloadOutlined />} onClick={() => setSession(null)}>
          新建会话
        </Button>
      }
    >
      <Card>
        <Form form={form} layout="inline" initialValues={{ command: '/bin/sh' }}>
          <Form.Item label="集群" name="clusterId" rules={[{ required: true }]}>
            <Select
              style={{ width: 200 }}
              placeholder="选择集群"
              options={clusters.map((c: any) => ({ label: c.name, value: c.id }))}
              onChange={(v) => { setClusterId(v); setNamespace(''); setSession(null); }}
            />
          </Form.Item>
          <Form.Item label="命名空间" name="namespace">
            <Input
              style={{ width: 160 }}
              placeholder="留空查全部"
              onPressEnter={(e) => setNamespace((e.target as HTMLInputElement).value)}
            />
          </Form.Item>
          <Form.Item label="Pod" name="pod">
            <Button
              icon={<CodeOutlined />}
              disabled={!clusterId}
              onClick={() => setPodPickerOpen(true)}
            >
              {session ? `${session.namespace}/${session.pod}` : '选择 Pod'}
            </Button>
          </Form.Item>
          <Form.Item label="命令" name="command">
            <Input style={{ width: 180 }} placeholder="/bin/sh" />
          </Form.Item>
        </Form>
      </Card>

      <Card style={{ marginTop: 16, minHeight: 500 }} bodyStyle={{ padding: 12, height: 'calc(100vh - 360px)' }}>
        {session ? (
          <PodTerminal
            clusterId={session.clusterId}
            namespace={session.namespace}
            pod={session.pod}
            container={session.container}
            command={form.getFieldValue('command') || '/bin/sh'}
            onClose={() => setSession(null)}
          />
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#888' }}>
            请选择集群与 Pod 后打开终端会话。所有操作将被录像与审计。
          </div>
        )}
      </Card>

      <Modal
        title="选择 Pod"
        open={podPickerOpen}
        onCancel={() => setPodPickerOpen(false)}
        onOk={launch}
        width={680}
        okText="打开终端"
        cancelText="取消"
      >
        <PodPicker
          pods={pods}
          loading={podsLoading}
          onPick={(p, container) => {
            const c = clusters.find((x: any) => x.id === clusterId);
            setSession({
              clusterId: clusterId!,
              clusterName: c?.name ?? '',
              namespace: podNs(p),
              pod: podName(p),
              container: container || podContainers(p)[0] || '',
            });
          }}
        />
      </Modal>
    </PageContainer>
  );
}

function PodPicker({
  pods,
  loading,
  onPick,
}: {
  pods: K8sPod[];
  loading: boolean;
  onPick: (pod: K8sPod, container?: string) => void;
}) {
  const [selected, setSelected] = useState<{ pod?: K8sPod; container?: string }>({});
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Select
        loading={loading}
        placeholder="选择 Pod"
        style={{ width: '100%' }}
        showSearch
        optionFilterProp="label"
        options={pods.map((p) => ({ label: `${podNs(p)}/${podName(p)}`, value: podName(p) }))}
        onChange={(v) => {
          const pod = pods.find((p) => podName(p) === v);
          setSelected({ pod, container: pod ? podContainers(pod)[0] : undefined });
        }}
      />
      {selected.pod && (
        <>
          <div style={{ fontSize: 12, color: '#888' }}>
            状态: <Tag color={podPhase(selected.pod) === 'Running' ? 'green' : 'default'}>{podPhase(selected.pod)}</Tag>
            节点: {podNode(selected.pod)} · 创建: {podCreated(selected.pod)}
          </div>
          <Select
            placeholder="容器"
            style={{ width: '100%' }}
            value={selected.container}
            onChange={(v) => setSelected((s) => ({ ...s, container: v }))}
            options={podContainers(selected.pod).map((name) => ({ label: name, value: name }))}
          />
          <Button type="primary" onClick={() => selected.pod && onPick(selected.pod, selected.container)}>
            确认选择
          </Button>
        </>
      )}
    </Space>
  );
}

export default WebTerminalPage;
