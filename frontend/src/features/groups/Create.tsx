import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import {
  App,
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
} from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { PageContainer } from '@/components/PageContainer';
import { BreadcrumbSwitcher } from '@/components/BreadcrumbSwitcher';
import { clusterApi, type ClusterCapacity } from '@/api/clusters';
import { applicationApi, groupApi } from '@/api/applications';
import { workspaceApi } from '@/api/workspaces';
import type { Group } from '@/types';

export default function GroupCreatePage() {
  const { message } = App.useApp();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const appId = Number(searchParams.get('appId') || 0);
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [capacity, setCapacity] = useState<ClusterCapacity | null>(null);

  const { data: clustersData } = useQuery({
    queryKey: ['clusters', { page: 1, size: 200 }],
    queryFn: () => clusterApi.list({ page: 1, size: 200 }),
  });
  const clusters = clustersData?.items ?? [];

  const { data: app } = useQuery({
    queryKey: ['application', appId],
    queryFn: () => applicationApi.get(appId),
    enabled: !!appId,
  });
  const workspaceId = app?.workspace_id;
  const { data: ws } = useQuery({
    queryKey: ['workspace', workspaceId],
    queryFn: () => workspaceApi.get(workspaceId!),
    enabled: !!workspaceId,
  });

  const selectedClusterId = Form.useWatch('cluster_id', form);
  const cpuCores = Form.useWatch(['resources', 'cpu_cores'], form);
  const memGB = Form.useWatch(['resources', 'memory_gb'], form);
  const gpu = Form.useWatch(['resources', 'gpu'], form);
  const replicas = Form.useWatch('replicas', form);

  // 选择集群 + 资源后实时查询可调度副本数。
  const cpuM = cpuCores ? Math.round(cpuCores * 1000) : undefined;
  const memBytes = memGB ? Math.round(memGB * 1024 * 1024 * 1024) : undefined;
  const { data: capacityData, refetch } = useQuery({
    queryKey: ['cluster-capacity', selectedClusterId, cpuM, memBytes, gpu],
    queryFn: () =>
      clusterApi.getCapacity(selectedClusterId, {
        cpu_m: cpuM,
        memory_bytes: memBytes,
        gpu: gpu ?? 0,
      }),
    enabled: false, // 手动触发，避免输入过程中频繁请求
  });

  useEffect(() => {
    if (capacityData) setCapacity(capacityData);
  }, [capacityData]);

  useEffect(() => {
    if (selectedClusterId && cpuM && memBytes) {
      refetch();
      setCapacity(null);
    }
  }, [selectedClusterId, cpuM, memBytes, gpu, refetch]);

  const createMutation = useMutation({
    mutationFn: (v: any) =>
      groupApi.create({
        application_id: appId,
        name: v.name,
        display_name: v.display_name,
        description: v.description,
        environment: v.environment,
        cluster_id: v.cluster_id,
        namespace: v.namespace,
        replicas: v.replicas ?? 1,
        workload: {
          type: v.workload_type ?? 'deployment',
        },
        resources: {
          cpu_m: Math.round((v.resources?.cpu_cores ?? 0.1) * 1000),
          memory_bytes: Math.round((v.resources?.memory_gb ?? 0.5) * 1024 * 1024 * 1024),
          gpu: v.resources?.gpu ?? 0,
        },
        mesh_enabled: false,
      } as any),
    onSuccess: (g: Group) => {
      message.success('分组已创建');
      queryClient.invalidateQueries({ queryKey: ['application', appId, 'groups'] });
      navigate(`/groups/${g.id}`);
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  if (!appId) {
    return (
      <PageContainer title="新建分组">
        <Alert type="error" message="缺少 appId 参数" />
      </PageContainer>
    );
  }

  return (
    <PageContainer
      title="新建分组"
      breadcrumb={[
        { title: '空间', path: '/workspaces' },
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={ws?.display_name || ws?.name}
              currentValue={workspaceId}
              currentPath={workspaceId ? `/workspaces/${workspaceId}` : undefined}
              queryKeyPrefix={['workspaces']}
              loadOptions={(search) =>
                workspaceApi
                  .list({ search: search || undefined, page: 1, size: 50 })
                  .then((p) =>
                    p.items.map((w) => ({
                      label: w.display_name || w.name,
                      value: w.id,
                      path: `/workspaces/${w.id}`,
                    })),
                  )
              }
            />
          ),
        },
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={app?.display_name || app?.name}
              currentValue={appId || undefined}
              currentPath={appId ? `/applications/${appId}` : undefined}
              queryKeyPrefix={['applications', 'ws', workspaceId ?? 0]}
              loadOptions={(search) =>
                workspaceId
                  ? applicationApi
                      .list(workspaceId, { search: search || undefined, page: 1, size: 50 })
                      .then((p) =>
                        p.items.map((a) => ({
                          label: a.display_name || a.name,
                          value: a.id,
                          path: `/applications/${a.id}`,
                        })),
                      )
                  : Promise.resolve([])
              }
            />
          ),
        },
        { title: '新建' },
      ]}
    >
      <Card>
        <Form
          layout="vertical"
          form={form}
          onFinish={(v) => createMutation.mutate(v)}
          initialValues={{
            environment: 'prod',
            replicas: 1,
            workload_type: 'deployment',
            resources: { cpu_cores: 0.1, memory_gb: 0.5, gpu: 0 },
          }}
          style={{ maxWidth: 720 }}
        >
          <Form.Item name="name" label="标识名" rules={[{ required: true, message: '请输入标识名' }]}>
            <Input placeholder="例如 prod-cluster" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="例如 生产分组" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Space style={{ display: 'flex' }} size="middle">
            <Form.Item name="environment" label="环境" rules={[{ required: true }]}>
              <Select
                style={{ width: 160 }}
                options={[
                  { label: '生产', value: 'prod' },
                  { label: '预发', value: 'staging' },
                  { label: '测试', value: 'test' },
                  { label: '开发', value: 'dev' },
                ]}
              />
            </Form.Item>
            <Form.Item name="workload_type" label="工作负载类型">
              <Select
                style={{ width: 160 }}
                options={[
                  { label: 'Deployment', value: 'deployment' },
                  { label: 'StatefulSet', value: 'statefulset' },
                  { label: 'CronJob', value: 'cronjob' },
                ]}
              />
            </Form.Item>
          </Space>
          <Form.Item
            name="cluster_id"
            label="K8s 集群"
            rules={[{ required: true, message: '请选择集群（创建后不可更换）' }]}
            extra="集群一旦选定，该分组后续所有发布都在此集群上，不可更换。"
          >
            <Select
              placeholder="选择集群"
              options={clusters.map((c) => ({ label: c.display_name || c.name, value: c.id }))}
            />
          </Form.Item>
          <Form.Item name="namespace" label="命名空间" rules={[{ required: true, message: '请输入命名空间' }]}>
            <Input placeholder="例如 vortexops-prod" />
          </Form.Item>
          <Form.Item name="replicas" label="副本数" rules={[{ required: true }]}>
            <InputNumber min={0} style={{ width: 160 }} />
          </Form.Item>
          <Card type="inner" title="单副本资源需求" size="small" style={{ marginBottom: 16 }}>
            <Space style={{ display: 'flex' }} size="middle">
              <Form.Item
                name={['resources', 'cpu_cores']}
                label="CPU (核)"
                rules={[{ required: true, message: '请输入 CPU' }]}
                extra="1 核 = 1000m"
              >
                <InputNumber min={0.01} step={0.1} style={{ width: 140 }} />
              </Form.Item>
              <Form.Item
                name={['resources', 'memory_gb']}
                label="内存 (GB)"
                rules={[{ required: true, message: '请输入内存' }]}
                extra="1 GB = 1024 Mi"
              >
                <InputNumber min={0.1} step={0.5} style={{ width: 160 }} />
              </Form.Item>
              <Form.Item name={['resources', 'gpu']} label="GPU">
                <InputNumber min={0} style={{ width: 120 }} />
              </Form.Item>
            </Space>
          </Card>

          {capacity && (
            <Alert
              type={capacity.max_replicas >= (replicas ?? 1) ? 'success' : 'warning'}
              showIcon
              style={{ marginBottom: 16 }}
              message={`当前集群可部署副本数：${capacity.max_replicas}（数据来源：${
                capacity.source === 'k8s_api' ? 'K8s API 实时' : '缓存'
              }）`}
              description={
                <span>
                  可调度：CPU {(capacity.allocatable_cpu_m / 1000).toFixed(2)} 核 / 内存{' '}
                  {(capacity.allocatable_memory_bytes / (1024 * 1024 * 1024)).toFixed(2)} GB / GPU{' '}
                  {capacity.allocatable_gpu}；已用：CPU {(capacity.used_cpu_m / 1000).toFixed(2)} 核 / 内存{' '}
                  {(capacity.used_memory_bytes / (1024 * 1024 * 1024)).toFixed(2)} GB / GPU {capacity.used_gpu}
                </span>
              }
            />
          )}

          <Form.Item>
            <Space>
              <Button type="primary" htmlType="submit" loading={createMutation.isPending}>
                创建
              </Button>
              <Button onClick={() => navigate(-1)}>取消</Button>
            </Space>
          </Form.Item>
        </Form>
      </Card>
    </PageContainer>
  );
}
