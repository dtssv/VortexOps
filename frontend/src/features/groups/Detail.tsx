import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Alert, App, Button, Card, Checkbox, Descriptions, Drawer, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs, Tag, Tooltip, Typography } from 'antd';
import { CodeOutlined, CopyOutlined, DeleteOutlined, DeploymentUnitOutlined, DesktopOutlined, DiffOutlined, EditOutlined, FileTextOutlined, HistoryOutlined, PlusOutlined, ReloadOutlined, RobotOutlined, ToolOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { BreadcrumbSwitcher } from '@/components/BreadcrumbSwitcher';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { LogViewer } from '@/components/LogViewer';
import { PodLogPanel } from '@/components/PodLogPanel';
import { PublishModal } from '@/components/PublishModal';
import { DiagnosisDrawer } from '@/components/DiagnosisDrawer';
import { ConfigContentEditor, ConfigContentPreview, buildConfigContentFromForm, parseConfigContent, populateFormFromContent } from '@/components/ConfigContentEditor';
import { DiffViewer } from '@/components/DiffViewer';
import { PodFileBrowser } from '@/components/PodFileBrowser';
import { PodNetCmd } from '@/components/PodNetCmd';
import { PodTerminal } from '@/features/ops/PodTerminal';
import type { LogLine } from '@/components/LogViewer';
import { applicationApi, groupApi, type GroupEvent, type RenderedResource } from '@/api/applications';
import { workspaceApi } from '@/api/workspaces';
import { configApi } from '@/api/configs';
import { releaseApi } from '@/api/releases';
import type { LogAnalyzeInput } from '@/api/diagnosis';
import { PermissionGate } from '@/components/PermissionGate';
import { usePermission } from '@/hooks/usePermission';
import { confirmDanger } from '@/utils/action';
import { formatTime, formatRelative, formatDuration } from '@/utils/format';
import { strategyLabel } from '@/features/releases/labels';
import type { Group, PodSummary, Release, ConfigSet, ConfigContentSnapshot } from '@/types';
import { clusterApi, type ClusterCapacity } from '@/api/clusters';
import { getNetworkProfileOption, resolveClusterNetworkMeta, requiresUnderlaySecondary } from '@/features/clusters/networkProfiles';
import type { GroupStableIP } from '@/types';

export default function GroupDetailPage() {
  const params = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const groupId = Number(params.groupId);
  const canManageGroup = usePermission('group:manage').can;
  const canScale = usePermission('group:scale').can;
  const canRelease = usePermission('release:trigger').can;
  const canViewConfig = usePermission('menu:config:view').can;
  const canViewRelease = usePermission('menu:release:view').can;
  const canExec = usePermission('ops:exec').can;
  const canDiagnose = usePermission('menu:diagnosis:view').can;
  const canViewK8s = usePermission('menu:k8s:view').can;
  const canViewOps = canExec || canViewK8s;

  const [opsOpen, setOpsOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editForm] = Form.useForm();
  const [scaleUpOpen, setScaleUpOpen] = useState(false);
  const [scaleUpForm] = Form.useForm();
  const [scaleDownOpen, setScaleDownOpen] = useState(false);
  const [scaleDownForm] = Form.useForm();
  const [selectedRemovePods, setSelectedRemovePods] = useState<string[]>([]);
  // 分组头部发布入口：打开 PublishModal（固定到当前分组，排除当前镜像）。
  const [headerPublishOpen, setHeaderPublishOpen] = useState(false);

  const { data: group, isLoading } = useQuery({
    queryKey: ['group', groupId],
    queryFn: () => groupApi.get(groupId),
    enabled: !!groupId,
  });

  const applicationId = group?.application_id;

  const { data: ws } = useQuery({
    queryKey: ['workspace', group?.workspace_id],
    queryFn: () => workspaceApi.get(group!.workspace_id!),
    enabled: !!group?.workspace_id,
  });

  const { data: app } = useQuery({
    queryKey: ['application', applicationId],
    queryFn: () => applicationApi.get(applicationId!),
    enabled: !!applicationId,
  });

  // 后端可能在 group 上不返回 workspace_id，回退从 app 取（app.workspace_id 为必填）。
  const workspaceId = group?.workspace_id ?? app?.workspace_id;

  const updateMutation = useMutation({
    mutationFn: (body: Record<string, any>) => groupApi.update(groupId, body),
    onSuccess: () => {
      message.success('分组已更新');
      setEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '更新失败'),
  });

  // 扩缩容：调用专用 scale 端点（同步 K8s）。
  // scaleUpMutation：扩容，传入新目标副本数。
  const scaleUpMutation = useMutation({
    mutationFn: (replicas: number) => groupApi.scale(groupId, { replicas, version: group?.version }),
    onSuccess: () => {
      message.success(`已扩容到 ${scaleUpForm.getFieldValue('replicas')} 副本`);
      setScaleUpOpen(false);
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '扩容失败'),
  });
  // scaleDownMutation：缩容所选 Pod，传 remove_pod_names（后端推导目标副本数）。
  const scaleDownMutation = useMutation({
    mutationFn: (removePods: string[]) => groupApi.scale(groupId, { replicas: 0, version: group?.version, remove_pod_names: removePods }),
    onSuccess: () => {
      message.success(`已缩容 ${selectedRemovePods.length} 个 Pod`);
      setScaleDownOpen(false);
      setSelectedRemovePods([]);
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '缩容失败'),
  });
  // 缩容 Modal：拉取当前 Pod 列表供勾选。
  const { data: scaleDownPods, isLoading: scaleDownPodsLoading } = useQuery({
    queryKey: ['group', groupId, 'pods-for-scaledown'],
    queryFn: () => groupApi.listPods(groupId),
    enabled: scaleDownOpen,
  });

  // 机器运维：重启/关机/开机。
  const restartMutation = useMutation({
    mutationFn: () => groupApi.restart(groupId),
    onSuccess: () => {
      message.success('已触发分组重启');
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '重启失败'),
  });
  const shutdownMutation = useMutation({
    mutationFn: () => groupApi.shutdown(groupId),
    onSuccess: () => {
      message.success('分组已关机（副本数缩为 0）');
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '关机失败'),
  });
  const startupMutation = useMutation({
    mutationFn: () => groupApi.startup(groupId),
    onSuccess: () => {
      message.success('分组已开机');
      queryClient.invalidateQueries({ queryKey: ['group', groupId] });
    },
    onError: (e: any) => message.error(e?.message || '开机失败'),
  });

  const deleteMutation = useMutation({
    mutationFn: () => groupApi.delete(groupId),
    onSuccess: () => {
      message.success('分组已删除');
      if (applicationId) {
        navigate(`/applications/${applicationId}`);
      } else {
        navigate('/workspaces');
      }
    },
    onError: (e: any) => message.error(e?.message || '删除失败'),
  });

  const workload = group?.workload;
  const resources = group?.resources;

  // 应用状态：
  // - 未配置探活：容器起来（Running + started）即就绪
  // - 已配置探活：以主动拨测 app_ready 为准；结果暂缺时回退容器状态，避免一直「检测中」
  const probeEnabled = !!(app?.probe?.enabled ?? (app?.metadata?.probe as { enabled?: boolean } | undefined)?.enabled);

  function containerUpStatus(r: PodSummary): 'ready' | 'starting' | 'not_ready' {
    const phase = r.phase || r.status;
    // 未配置探活：Pod Running/Succeeded 即视为容器已起来（就绪）。
    if (phase === 'Running' || phase === 'Succeeded') return 'ready';
    if (phase === 'Pending') return 'starting';
    if (phase) return 'not_ready';
    const cs = r.containers || [];
    if (cs.length > 0 && cs.every((c) => c.started !== false)) return 'ready';
    return 'starting';
  }

  function resolveAppReady(r: PodSummary): { status: 'ready' | 'starting' | 'not_ready'; text: string; detail?: string } {
    if (probeEnabled) {
      if (r.app_ready === true) return { status: 'ready', text: '就绪' };
      if (r.app_ready === false) {
        return { status: 'not_ready', text: '未就绪', detail: r.app_ready_detail };
      }
      // 拨测尚未回传：按容器状态降级，避免永久「检测中」
    }
    const s = containerUpStatus(r);
    return {
      status: s,
      text: s === 'ready' ? '就绪' : s === 'starting' ? '启动中' : '未就绪',
    };
  }

  function renderAppReadyText(r: PodSummary): string {
    return resolveAppReady(r).text;
  }

  function renderAppReadyStatus(r: PodSummary) {
    const { status, text, detail } = resolveAppReady(r);
    const tag = <ResourceStatus status={status} text={text} />;
    return detail ? <Tooltip title={detail}>{tag}</Tooltip> : tag;
  }

  const tabItems = [
    { key: 'overview', label: '概览', children: <OverviewTab /> },
    { key: 'pods', label: 'Pod', children: <PodsTab groupId={groupId} /> },
    ...(canViewConfig
      ? [{ key: 'configs', label: '配置', children: <ConfigsTab groupId={groupId} applicationId={group?.application_id} /> }]
      : []),
    ...(canViewRelease
      ? [{ key: 'releases', label: '发布', children: <ReleasesTab groupId={groupId} applicationId={group?.application_id} currentImageId={group?.current_image_id} /> }]
      : []),
  ];
  const allowedTabKeys = tabItems.map((t) => t.key);
  const activeTab = allowedTabKeys.includes(searchParams.get('tab') || '')
    ? (searchParams.get('tab') as string)
    : 'overview';
  const onTabChange = (key: string) => {
    const next = new URLSearchParams(searchParams);
    next.set('tab', key);
    setSearchParams(next, { replace: true });
  };

  return (
    <PageContainer
      title={group?.display_name || group?.name || '分组'}
      subtitle={group?.description}
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
              currentValue={applicationId}
              currentPath={applicationId ? `/applications/${applicationId}` : undefined}
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
        {
          switcher: (
            <BreadcrumbSwitcher
              currentLabel={group?.display_name || group?.name}
              currentValue={groupId}
              currentPath={groupId ? `/groups/${groupId}` : undefined}
              queryKeyPrefix={['groups', 'app', applicationId ?? 0]}
              loadOptions={(search) =>
                applicationId
                  ? groupApi
                      .list(applicationId, { search: search || undefined, page: 1, size: 50 })
                      .then((p) =>
                        p.items.map((g) => ({
                          label: g.display_name || g.name,
                          value: g.id,
                          path: `/groups/${g.id}`,
                        })),
                      )
                  : Promise.resolve([])
              }
            />
          ),
        },
      ]}
      extra={
        group && (
          <Space>
            <Tag icon={<DesktopOutlined />}>{group.cluster_name || `集群 ${group.cluster_id}`}</Tag>
            <Tag>{group.namespace}</Tag>
            <Tag color="blue" style={{ margin: '0 4px', lineHeight: '24px' }}>
              {workload?.replicas ?? 0} 副本
            </Tag>
            <PermissionGate code="group:scale">
              <Button
                size="small"
                icon={<PlusOutlined />}
                loading={scaleUpMutation.isPending}
                onClick={() => {
                  scaleUpForm.setFieldsValue({ replicas: (workload?.replicas ?? 0) + 1 });
                  setScaleUpOpen(true);
                }}
              >
                扩容
              </Button>
              <Button
                size="small"
                icon={<DeleteOutlined />}
                loading={scaleDownMutation.isPending}
                disabled={(workload?.replicas ?? 0) === 0}
                onClick={() => {
                  setSelectedRemovePods([]);
                  setScaleDownOpen(true);
                }}
              >
                缩容
              </Button>
            </PermissionGate>
            {group.candidate_release_id ? (
              <Tag color="orange">候选发布中 #{group.candidate_release_id} ({group.candidate_replicas ?? 0})</Tag>
            ) : null}
            <PermissionGate code="group:scale">
              <Button icon={<ReloadOutlined />} loading={restartMutation.isPending} onClick={() => restartMutation.mutate()}>
                重启
              </Button>
              <Button
                loading={shutdownMutation.isPending}
                disabled={(workload?.replicas ?? 0) === 0}
                onClick={() =>
                  confirmDanger({
                    title: '关机分组',
                    content: `确定关机分组「${group.display_name || group.name}」？所有副本将缩为 0（关机前副本数会记录，开机时恢复）。`,
                    okText: '关机',
                    onOk: () => shutdownMutation.mutateAsync(),
                  })
                }
              >
                关机
              </Button>
              <Button
                loading={startupMutation.isPending}
                disabled={(workload?.replicas ?? 0) > 0}
                onClick={() => startupMutation.mutate()}
              >
                开机
              </Button>
            </PermissionGate>
            <PermissionGate code="group:manage">
              <Button
                icon={<EditOutlined />}
                onClick={() => {
                  setEditOpen(true);
                  editForm.resetFields();
                }}
              >
                编辑
              </Button>
            </PermissionGate>
            <PermissionGate code="release:trigger">
              <Button
                type="primary"
                icon={<DeploymentUnitOutlined />}
                disabled={!group?.application_id}
                onClick={() => setHeaderPublishOpen(true)}
              >
                发布
              </Button>
            </PermissionGate>
            {canViewOps && (
              <Button icon={<ToolOutlined />} onClick={() => setOpsOpen(true)}>
                运维
              </Button>
            )}
            <PermissionGate code="group:manage">
              <Button
                danger
                icon={<DeleteOutlined />}
                onClick={() =>
                  confirmDanger({
                    title: '删除分组',
                    content: `确定删除分组「${group.display_name || group.name}」吗？删除后该分组下的发布历史将一并清除，此操作不可恢复。`,
                    okText: '删除',
                    onOk: () => deleteMutation.mutateAsync(),
                  })
                }
              >
                删除
              </Button>
            </PermissionGate>
          </Space>
        )
      }
    >
      <Card loading={isLoading}>
        <Tabs
          activeKey={activeTab}
          onChange={onTabChange}
          items={tabItems}
        />
      </Card>

      <Drawer
        title="运维"
        open={opsOpen}
        onClose={() => setOpsOpen(false)}
        width={880}
        destroyOnHidden
      >
        <Tabs
          items={[
            { key: 'events', label: '事件', children: <EventsTab groupId={groupId} /> },
            { key: 'yaml', label: 'YAML', children: <YAMLTab groupId={groupId} /> },
          ]}
        />
      </Drawer>

      <Modal
        title={`编辑分组 - ${group?.display_name || group?.name || ''}`}
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        destroyOnHidden
        width={620}
      >
        <Form
          layout="vertical"
          form={editForm}
          initialValues={
            group
              ? {
                  display_name: group.display_name,
                  description: group.description,
                  replicas: group.workload?.replicas,
                  version: group.version,
                }
              : {}
          }
          onFinish={(v) => {
            const replicasChanged = group && v.replicas !== group.workload?.replicas;
            // 副本数变更走专用 scale 端点（同步 K8s）；其余字段走 update。
            if (replicasChanged) {
              scaleUpMutation.mutateAsync(v.replicas).then(() => {
                // 副本数之外的字段若也变更，再走 update。
                if (v.display_name !== group?.display_name || v.description !== group?.description) {
                  updateMutation.mutate({
                    display_name: v.display_name,
                    description: v.description,
                    version: v.version,
                  });
                }
              });
            } else {
              updateMutation.mutate({
                display_name: v.display_name,
                description: v.description,
                version: v.version,
              });
            }
          }}
        >
          <Form.Item label="标识名" extra="创建后不可修改">
            <Input value={group?.name} disabled />
          </Form.Item>
          <Space style={{ width: '100%' }} size="middle">
            <Form.Item label="环境" extra="创建后不可修改" style={{ flex: 1 }}>
              <Input value={group?.environment} disabled />
            </Form.Item>
            <Form.Item label="命名空间" extra="创建后不可修改" style={{ flex: 1 }}>
              <Input value={group?.namespace} disabled />
            </Form.Item>
          </Space>
          <Form.Item label="集群" extra="创建后不可更换，如需迁移请新建分组">
            <Input value={group?.cluster_name || `集群 ${group?.cluster_id}`} disabled />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称">
            <Input placeholder="例如 生产分组" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="可选" />
          </Form.Item>
          <Form.Item name="replicas" label="副本数" rules={[{ required: true }]}>
            <InputNumber min={0} style={{ width: 160 }} />
          </Form.Item>
          <Form.Item name="version" hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`扩容 - ${group?.display_name || group?.name || ''}`}
        open={scaleUpOpen}
        onCancel={() => setScaleUpOpen(false)}
        onOk={() => scaleUpForm.submit()}
        confirmLoading={scaleUpMutation.isPending}
        destroyOnHidden
        width={420}
      >
        <Form
          layout="vertical"
          form={scaleUpForm}
          onFinish={(v) => scaleUpMutation.mutate(v.replicas)}
        >
          <Form.Item label="当前副本数">
            <Tag color="blue">{workload?.replicas ?? 0}</Tag>
          </Form.Item>
          <Form.Item
            name="replicas"
            label="扩容后副本总数"
            rules={[{ required: true, message: '请输入副本数' }]}
            extra={`新增 ${(scaleUpForm.getFieldValue('replicas') ?? 0) - (workload?.replicas ?? 0)} 个副本`}
          >
            <InputNumber min={(workload?.replicas ?? 0)} style={{ width: 200 }} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={`缩容（选择要移除的 Pod）- ${group?.display_name || group?.name || ''}`}
        open={scaleDownOpen}
        onCancel={() => {
          setScaleDownOpen(false);
          setSelectedRemovePods([]);
        }}
        onOk={() => {
          if (selectedRemovePods.length === 0) {
            message.warning('请至少选择一个 Pod');
            return;
          }
          scaleDownMutation.mutate(selectedRemovePods);
        }}
        confirmLoading={scaleDownMutation.isPending}
        destroyOnHidden
        width={620}
        okText={`缩容 ${selectedRemovePods.length} 个`}
        okButtonProps={{ disabled: selectedRemovePods.length === 0 }}
      >
        <p style={{ color: '#888', marginBottom: 12 }}>
          当前 {workload?.replicas ?? 0} 副本，勾选要移除的 Pod，缩容后副本数将为 {(workload?.replicas ?? 0) - selectedRemovePods.length}。
        </p>
        <Table<PodSummary>
          rowKey="name"
          size="small"
          loading={scaleDownPodsLoading}
          dataSource={scaleDownPods}
          pagination={false}
          scroll={{ y: 320 }}
          rowSelection={{
            selectedRowKeys: selectedRemovePods,
            onChange: (keys) => setSelectedRemovePods(keys.map(String)),
          }}
          columns={[
            { title: 'Pod 名称', dataIndex: 'name' },
            { title: '节点', dataIndex: 'node_name', width: 140, render: (v?: string) => v || '-' },
            { title: 'IP', dataIndex: 'pod_ip', width: 130, render: (v?: string) => v || '-' },
            {
              title: 'Pod 状态', dataIndex: 'phase', width: 100,
              render: (phase: string, r: PodSummary) => phase || r.status || '-',
            },
            {
              title: '应用', key: 'app_ready', width: 80,
              render: (_: unknown, r: PodSummary) => renderAppReadyText(r),
            },
          ]}
        />
      </Modal>

      <PublishModal
        open={headerPublishOpen}
        onClose={() => setHeaderPublishOpen(false)}
        applicationId={group?.application_id ?? 0}
        fixedGroupId={groupId}
        excludeImageId={group?.current_image_id}
        onPublished={(relId) => {
          void queryClient.invalidateQueries({ queryKey: ['group', groupId] });
          void queryClient.invalidateQueries({ queryKey: ['group', groupId, 'releases'] });
          navigate(`/releases/${relId}`);
        }}
      />
    </PageContainer>
  );

  // Mesh 配置卡片：分组维度是否启用 Cilium L7 治理（mTLS/流量切分/熔断）。
  // 默认关闭；Phase 5 生效。开启后渲染 CiliumNetworkPolicy 等 CRD。
  function MeshConfigCard({ group, onSaved }: { group: Group; onSaved: () => void }) {
    const { message } = App.useApp();
    const [form] = Form.useForm<{ mesh_enabled: boolean }>();

    const saveMutation = useMutation({
      mutationFn: (v: { mesh_enabled: boolean }) => {
        return groupApi.update(group.id, {
          mesh_enabled: v.mesh_enabled,
          version: group.version,
        });
      },
      onSuccess: () => {
        message.success('Mesh 配置已保存');
        onSaved();
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    useEffect(() => {
      form.setFieldsValue({ mesh_enabled: group.mesh_enabled ?? false });
    }, [form, group]);

    const onFinish = (v: { mesh_enabled: boolean }) => {
      saveMutation.mutate({ mesh_enabled: v.mesh_enabled });
    };

    return (
      <Form form={form} layout="vertical" initialValues={{ mesh_enabled: false }} onFinish={onFinish}>
        <Form.Item
          name="mesh_enabled"
          label="启用 Service Mesh"
          valuePropName="checked"
          extra="开启后为该分组注入 CiliumNetworkPolicy 等 CRD，启用 mTLS、流量切分、熔断等 L7 治理能力。默认关闭。需集群已安装 Cilium（Phase 3）。"
        >
          <Switch checkedChildren="开" unCheckedChildren="关" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={saveMutation.isPending} style={{ marginTop: 12 }}>
          保存 Mesh 配置
        </Button>
      </Form>
    );
  }

  function NetworkAccessCard({ group, probePort }: { group: Group; probePort?: number }) {
    const { message } = App.useApp();
    const { data: cluster } = useQuery({
      queryKey: ['cluster', group.cluster_id],
      queryFn: () => clusterApi.get(group.cluster_id),
      enabled: !!group.cluster_id,
    });
    const { data: stable, isLoading } = useQuery({
      queryKey: ['group', group.id, 'stable-ips'],
      queryFn: () => groupApi.listStableIPs(group.id),
      enabled: !!group.id,
    });
    const { profile, cni, npObj } = resolveClusterNetworkMeta(cluster?.metadata);
    const profileOpt = profile ? getNetworkProfileOption(profile) : undefined;
    const items = stable?.items ?? [];
    const capOk = stable?.capability?.ok ?? true;
    const overlaySecondary = requiresUnderlaySecondary(profile);
    const accessHint = overlaySecondary
      ? 'Overlay 网段不可从集群外直连，访问入口为 Multus 副网卡固定 IP。'
      : profile === 'xlarge-bgp'
        ? '固定 IP 经 BGP 宣告后三层直连。'
        : '固定 IP 与办公网同网段直连。';

    const copyAddr = (ip: string) => {
      const addr = probePort ? `${ip}:${probePort}` : ip;
      void navigator.clipboard.writeText(addr).then(
        () => message.success(`已复制 ${addr}`),
        () => message.error('复制失败'),
      );
    };

    return (
      <Card title="网络与访问" size="small" loading={isLoading}>
        {!capOk && (
          <Alert
            type="error"
            showIcon
            style={{ marginBottom: 12 }}
            message="当前集群无法满足固定 IP 直连"
            description={stable?.capability?.message || '请检查 Multus、CNI 与 Underlay IP 池配置。'}
          />
        )}
        <Descriptions bordered column={2} size="small">
          <Descriptions.Item label="网络方案">{profileOpt?.label || profile || '未配置（默认开发环境）'}</Descriptions.Item>
          <Descriptions.Item label="CNI">{cni || '-'}</Descriptions.Item>
          <Descriptions.Item label="Multus">{npObj?.multus_enabled ? '已开启' : '未开启'}</Descriptions.Item>
          <Descriptions.Item label="固定 IP">{items.length > 0 ? <Tag color="green">已分配 {items.length}</Tag> : <Tag>未分配</Tag>}</Descriptions.Item>
          <Descriptions.Item label="访问方式" span={2}>{accessHint}不经 Service / NodePort / LB。</Descriptions.Item>
          <Descriptions.Item label="访问入口" span={2}>
            {items.length === 0 ? (
              <span style={{ color: '#888' }}>{probePort ? '发布后显示 稳定IP:' + probePort : '发布后显示稳定 IP；应用未配置探活端口时仅显示 IP'}</span>
            ) : (
              <Space wrap>
                {items.map((it: GroupStableIP) => {
                  const addr = probePort ? `${it.ip}:${probePort}` : it.ip;
                  return (
                    <Space key={`${it.replica_index}-${it.ip}`} size={4}>
                      <Typography.Text copyable={{ text: addr }} code>
                        {addr}
                      </Typography.Text>
                      <Typography.Text type="secondary">#{it.replica_index}</Typography.Text>
                    </Space>
                  );
                })}
              </Space>
            )}
          </Descriptions.Item>
        </Descriptions>
        {items.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <Button size="small" icon={<CopyOutlined />} onClick={() => copyAddr(items[0].ip)}>
              复制首个访问地址
            </Button>
          </div>
        )}
      </Card>
    );
  }

  // 资源配置卡片：编辑 CPU / 内存 / GPU / 临时磁盘，保存为 group.resources / group.storage。
  // 后端 groupApi.update 为整字段替换，需合并现有 resources / storage 保留其它字段。
  function ResourceConfigCard({ group, onSaved }: { group: Group; onSaved: () => void }) {
    const { message } = App.useApp();
    const [form] = Form.useForm<{
      cpu_cores: number;
      cpu_limit_cores?: number;
      memory_gb: number;
      memory_limit_gb?: number;
      gpu: number;
      gpu_type?: string;
      ephemeral_storage_gb?: number;
      ephemeral_storage_limit_gb?: number;
    }>();

    const cpuCores = Form.useWatch('cpu_cores', form);
    const memGb = Form.useWatch('memory_gb', form);
    const gpu = Form.useWatch('gpu', form);

    // 集群容量校验：以当前所选资源（按单副本）查询可部署副本数。
    const cpuM = cpuCores ? Math.round(cpuCores * 1000) : undefined;
    const memBytes = memGb ? Math.round(memGb * 1024 * 1024 * 1024) : undefined;
    const { data: capacity, isFetching: capLoading } = useQuery<ClusterCapacity>({
      queryKey: ['cluster-capacity', group.cluster_id, cpuM, memBytes, gpu ?? 0],
      queryFn: () => clusterApi.getCapacity(group.cluster_id, { cpu_m: cpuM!, memory_bytes: memBytes!, gpu: gpu ?? 0 }),
      enabled: !!group.cluster_id && cpuM != null && memBytes != null,
      staleTime: 30_000,
    });

    const saveMutation = useMutation({
      mutationFn: (v: {
        cpu_cores: number;
        cpu_limit_cores?: number;
        memory_gb: number;
        memory_limit_gb?: number;
        gpu: number;
        gpu_type?: string;
        ephemeral_storage_gb?: number;
        ephemeral_storage_limit_gb?: number;
      }) => {
        const resources = {
          ...group.resources,
          cpu_m: Math.round(v.cpu_cores * 1000),
          cpu_limit_m: v.cpu_limit_cores != null ? Math.round(v.cpu_limit_cores * 1000) : group.resources.cpu_limit_m,
          memory_bytes: Math.round(v.memory_gb * 1024 * 1024 * 1024),
          memory_limit_bytes:
            v.memory_limit_gb != null
              ? Math.round(v.memory_limit_gb * 1024 * 1024 * 1024)
              : group.resources.memory_limit_bytes,
          gpu: v.gpu ?? 0,
          gpu_type: v.gpu_type || group.resources.gpu_type,
          gpu_resource_name: group.resources.gpu_resource_name,
        };
        const storage = {
          ...group.storage,
          ephemeral_storage_request_bytes:
            v.ephemeral_storage_gb != null
              ? Math.round(v.ephemeral_storage_gb * 1024 * 1024 * 1024)
              : group.storage?.ephemeral_storage_request_bytes,
          ephemeral_storage_limit_bytes:
            v.ephemeral_storage_limit_gb != null
              ? Math.round(v.ephemeral_storage_limit_gb * 1024 * 1024 * 1024)
              : group.storage?.ephemeral_storage_limit_bytes,
        };
        return groupApi.update(group.id, { resources, storage, version: group.version });
      },
      onSuccess: () => {
        message.success('资源配置已保存');
        onSaved();
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    // 同步表单初始值（group 变化或刷新后重置）。
    useEffect(() => {
      form.setFieldsValue({
        cpu_cores: (group.resources.cpu_m ?? 0) / 1000,
        cpu_limit_cores: group.resources.cpu_limit_m != null ? group.resources.cpu_limit_m / 1000 : undefined,
        memory_gb: (group.resources.memory_bytes ?? 0) / 1024 / 1024 / 1024,
        memory_limit_gb:
          group.resources.memory_limit_bytes != null ? group.resources.memory_limit_bytes / 1024 / 1024 / 1024 : undefined,
        gpu: group.resources.gpu ?? 0,
        gpu_type: group.resources.gpu_type,
        ephemeral_storage_gb:
          group.storage?.ephemeral_storage_request_bytes != null
            ? group.storage.ephemeral_storage_request_bytes / 1024 / 1024 / 1024
            : undefined,
        ephemeral_storage_limit_gb:
          group.storage?.ephemeral_storage_limit_bytes != null
            ? group.storage.ephemeral_storage_limit_bytes / 1024 / 1024 / 1024
            : undefined,
      });
    }, [form, group]);

    const onFinish = (v: Parameters<typeof saveMutation.mutate>[0]) => saveMutation.mutate(v);

    return (
      <Form form={form} layout="vertical" onFinish={onFinish}>
        <Card type="inner" size="small" title="CPU / 内存" style={{ marginBottom: 12 }}>
          <Space style={{ display: 'flex' }} size="middle" wrap>
            <Form.Item
              name="cpu_cores"
              label="CPU 请求 (核)"
              rules={[{ required: true, message: '请输入 CPU' }]}
              extra="1 核 = 1000m"
            >
              <InputNumber min={0.01} step={0.1} style={{ width: 140 }} />
            </Form.Item>
            <Form.Item name="cpu_limit_cores" label="CPU 上限 (核)" extra="留空则不设上限">
              <InputNumber min={0.01} step={0.1} style={{ width: 140 }} />
            </Form.Item>
            <Form.Item
              name="memory_gb"
              label="内存请求 (GB)"
              rules={[{ required: true, message: '请输入内存' }]}
              extra="1 GB = 1024 Mi"
            >
              <InputNumber min={0.1} step={0.5} style={{ width: 160 }} />
            </Form.Item>
            <Form.Item name="memory_limit_gb" label="内存上限 (GB)" extra="留空则不设上限">
              <InputNumber min={0.1} step={0.5} style={{ width: 160 }} />
            </Form.Item>
          </Space>
        </Card>
        <Card type="inner" size="small" title="GPU" style={{ marginBottom: 12 }}>
          <Space style={{ display: 'flex' }} size="middle" wrap>
            <Form.Item name="gpu" label="GPU 数量" extra="0 表示不申请 GPU">
              <InputNumber min={0} step={1} style={{ width: 120 }} />
            </Form.Item>
            <Form.Item name="gpu_type" label="GPU 类型" extra="如 nvidia.com/gpu，留空使用集群默认">
              <Input placeholder="例如 nvidia-tesla-t4" style={{ width: 220 }} />
            </Form.Item>
          </Space>
        </Card>
        <Card type="inner" size="small" title="临时磁盘 (Ephemeral Storage)" style={{ marginBottom: 12 }}>
          <Space style={{ display: 'flex' }} size="middle" wrap>
            <Form.Item name="ephemeral_storage_gb" label="磁盘请求 (GB)" extra="留空则不显式申请">
              <InputNumber min={0} step={1} style={{ width: 160 }} />
            </Form.Item>
            <Form.Item name="ephemeral_storage_limit_gb" label="磁盘上限 (GB)" extra="留空则不设上限">
              <InputNumber min={0} step={1} style={{ width: 160 }} />
            </Form.Item>
          </Space>
        </Card>
        {capacity && (
          <Alert
            type={capacity.max_replicas >= (group.workload?.replicas ?? 1) ? 'success' : 'warning'}
            showIcon
            style={{ marginBottom: 12 }}
            message={`按当前单副本资源，集群可部署副本数：${capacity.max_replicas}（数据来源：${
              capacity.source === 'k8s_api' ? 'K8s API 实时' : '缓存'
            }）${capLoading ? '（刷新中…）' : ''}`}
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
        <Tooltip title="保存后将更新分组资源配置；下次发布生效到 K8s。">
          <Button type="primary" htmlType="submit" loading={saveMutation.isPending}>
            保存资源配置
          </Button>
        </Tooltip>
      </Form>
    );
  }

  function OverviewTab() {
    // 当前发布：用于展示发布时间与运行时长（运行时长 = 当前时间 − 发布完成时间）。
    const { data: currentRelease } = useQuery({
      queryKey: ['release', group?.current_release_id],
      queryFn: () => releaseApi.get(group!.current_release_id!),
      enabled: !!group?.current_release_id,
    });
    // 每秒刷新运行时长（无需触达后端，纯前端计算）。
    const [, setTick] = useState(0);
    useEffect(() => {
      const id = setInterval(() => setTick((t) => t + 1), 1000);
      return () => clearInterval(id);
    }, []);

    if (!group) return null;
    // 发布时间：优先用 finished_at（发布完成才视为生效版本），否则回退到 started_at。
    const releasedAt = currentRelease?.finished_at || currentRelease?.started_at;
    const runtimeMs = releasedAt ? Date.now() - new Date(releasedAt).getTime() : null;
    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Descriptions title="基本信息" bordered column={2} size="small">
          <Descriptions.Item label="状态">
            <ResourceStatus status={group.status || 'unknown'} />
          </Descriptions.Item>
          <Descriptions.Item label="环境">{group.environment}</Descriptions.Item>
          <Descriptions.Item label="工作负载类型">{workload?.type}</Descriptions.Item>
          <Descriptions.Item label="副本数">{workload?.replicas ?? 0}</Descriptions.Item>
          <Descriptions.Item label="镜像" span={2}>
            <code>{group.current_image_ref || workload?.image_ref || '-'}</code>
          </Descriptions.Item>
          <Descriptions.Item label="Mesh">
            {group.mesh_enabled ? <Tag color="blue">已启用</Tag> : <Tag color="default">未启用</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="配置版本">{group.config_version ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="发布时间">
            {currentRelease ? formatTime(releasedAt) : '-'}
            {currentRelease ? <span style={{ color: '#888', marginLeft: 8 }}>#{currentRelease.release_number}</span> : null}
          </Descriptions.Item>
          <Descriptions.Item label="运行时长">
            {runtimeMs != null && runtimeMs >= 0 ? formatDuration(runtimeMs) : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatTime(group.created_at)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatRelative(group.updated_at)}</Descriptions.Item>
        </Descriptions>

        <Descriptions title="资源配置" bordered column={2} size="small">
          <Descriptions.Item label="CPU 请求">{resources?.cpu_m ? `${(resources.cpu_m / 1000).toFixed(2)} 核` : '-'}</Descriptions.Item>
          <Descriptions.Item label="CPU 上限">{resources?.cpu_limit_m ? `${(resources.cpu_limit_m / 1000).toFixed(2)} 核` : '-'}</Descriptions.Item>
          <Descriptions.Item label="内存请求">{resources?.memory_bytes ? `${(resources.memory_bytes / 1024 / 1024 / 1024).toFixed(2)} GiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="内存上限">{resources?.memory_limit_bytes ? `${(resources.memory_limit_bytes / 1024 / 1024 / 1024).toFixed(2)} GiB` : '-'}</Descriptions.Item>
          <Descriptions.Item label="GPU 数量">{resources?.gpu ?? 0}</Descriptions.Item>
          <Descriptions.Item label="GPU 类型">{resources?.gpu_type || '-'}</Descriptions.Item>
        </Descriptions>

        <NetworkAccessCard group={group} probePort={app?.probe?.port} />

        {canManageGroup && (
          <>
            <Card title="资源配置" size="small">
              <ResourceConfigCard
                group={group}
                onSaved={() => queryClient.invalidateQueries({ queryKey: ['group', group.id] })}
              />
            </Card>

            <Card title="Mesh 配置" size="small">
              <MeshConfigCard
                group={group}
                onSaved={() => queryClient.invalidateQueries({ queryKey: ['group', group.id] })}
              />
            </Card>
          </>
        )}
      </Space>
    );
  }

  function podContainersFor(podName: string, pods?: PodSummary[]): Array<{ name: string }> {
    const pod = pods?.find((p) => p.name === podName);
    return (pod?.containers || []).map((c) => ({ name: c.name }));
  }

  function PodsTab({ groupId }: { groupId: number }) {
    const { message } = App.useApp();
    const [logDrawer, setLogDrawer] = useState<{ open: boolean; pod?: string; container?: string }>({ open: false });
    const [execModal, setExecModal] = useState<{ open: boolean; pod?: PodSummary; container?: string }>({ open: false });
    const [execCmd, setExecCmd] = useState<string>('sh');
    const [execOutput, setExecOutput] = useState<string>('');
    const [termDrawer, setTermDrawer] = useState<{ open: boolean; pod?: string; container?: string }>({ open: false });
    const [fileDrawer, setFileDrawer] = useState<{ open: boolean; pod?: string; container?: string }>({ open: false });
    const [netDrawer, setNetDrawer] = useState<{ open: boolean; pod?: string; container?: string }>({ open: false });
    // AI 诊断：Pod 启动失败/崩溃时收集日志并调用诊断。
    const [diagOpen, setDiagOpen] = useState(false);
    const [diagInput, setDiagInput] = useState<LogAnalyzeInput | null>(null);

    const restartPodMutation = useMutation({
      mutationFn: (pod: string) => groupApi.restartPod(groupId, pod),
      onSuccess: () => {
        message.success('已触发 Pod 重启');
        queryClient.invalidateQueries({ queryKey: ['group', groupId, 'pods'] });
      },
      onError: (e: any) => message.error(e?.message || '重启失败'),
    });

    const { data, isLoading, refetch, isFetching } = useQuery({
      queryKey: ['group', groupId, 'pods'],
      queryFn: () => groupApi.listPods(groupId),
      enabled: !!groupId,
      refetchInterval: 5000,
    });
    const { data: cluster } = useQuery({
      queryKey: ['cluster', group?.cluster_id],
      queryFn: () => clusterApi.get(group!.cluster_id),
      enabled: !!group?.cluster_id,
    });
    const { data: stable } = useQuery({
      queryKey: ['group', groupId, 'stable-ips'],
      queryFn: () => groupApi.listStableIPs(groupId),
      enabled: !!groupId,
    });
    const { profile } = resolveClusterNetworkMeta(cluster?.metadata);
    const overlaySecondary = requiresUnderlaySecondary(profile);
    const stableIPSet = new Set((stable?.items ?? []).map((it) => it.ip));
    const probePort = app?.probe?.port;

    const { data: logs } = useQuery({
      queryKey: ['group', groupId, 'pod-logs', logDrawer.pod, logDrawer.container],
      queryFn: () => groupApi.podLogs(groupId, logDrawer.pod!, { container: logDrawer.container, tail: 1000 }),
      enabled: !!logDrawer.open && !!logDrawer.pod,
    });

    // 启动 AI 诊断：收集 Pod 日志 → 构造 LogAnalyzeInput → 打开抽屉。
    const openPodDiagnosis = async (pod: PodSummary) => {
      const containerName = pod.containers?.[0]?.name;
      let podLogs = '';
      try {
        podLogs = await groupApi.podLogs(groupId, pod.name, { container: containerName, tail: 2000 });
      } catch {
        podLogs = '';
      }
      // 根据状态选择诊断场景：CrashLoopBackOff/频繁重启 → pod_crash；其它未就绪 → pod_startup。
      const isCrash = (pod.restart_count ?? 0) >= 3 || /CrashLoopBackOff|Error/i.test(pod.phase || pod.status || '');
      setDiagInput({
        source: isCrash ? 'pod_crash' : 'pod_startup',
        title: `Pod ${pod.name} ${isCrash ? '崩溃诊断' : '启动诊断'}`,
        cluster_id: group?.cluster_id,
        namespace: pod.namespace || group?.namespace || '',
        name: pod.name,
        container: containerName,
        error_reason: `Pod phase=${pod.phase || pod.status || 'Unknown'}, restarts=${pod.restart_count ?? 0}, ready=${pod.ready}`,
        logs: podLogs,
      });
      setDiagOpen(true);
    };

    const execMutation = useMutation({
      mutationFn: async () => {
        // exec 通过 /ops/exec 流式接口执行；此处用 fetch 直接读取文本输出。
        const token = localStorage.getItem('access_token') || '';
        const res = await fetch('/api/v1/ops/exec', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
          body: JSON.stringify({
            cluster_id: group?.cluster_id,
            namespace: group?.namespace,
            pod: execModal.pod?.name,
            container: execModal.container,
            command: execCmd.split(/\s+/).filter(Boolean),
            tty: false,
          }),
        });
        if (!res.ok) throw new Error(`exec 失败: ${res.status}`);
        return res.text();
      },
      onSuccess: (out) => {
        setExecOutput(out || '(无输出)');
      },
      onError: (e: any) => message.error(e?.message || 'exec 失败'),
    });

    const columns: ColumnsType<PodSummary> = [
      { title: '名称', dataIndex: 'name' },
      {
        // 容器/Pod 状态：K8s Pod Phase（Running/Pending/Succeeded/Failed/Unknown）。
        title: 'Pod 状态',
        dataIndex: 'phase',
        width: 110,
        render: (phase: string, r) => {
          const p = phase || r.status || 'Unknown';
          // Pod Phase → ResourceStatus 颜色映射。
          const statusKey = p === 'Running' ? 'running'
            : p === 'Pending' ? 'pending'
            : p === 'Succeeded' ? 'success'
            : p === 'Failed' ? 'failed'
            
            : 'unknown';
          return <ResourceStatus status={statusKey} text={p} />;
        },
      },
      {
        // 应用状态：未配置探活时容器起来即就绪；已配置则看 app_ready。
        title: '应用状态',
        key: 'app_ready',
        width: 110,
        render: (_: unknown, r) => renderAppReadyStatus(r),
      },
      { title: '节点', dataIndex: 'node_name', width: 160, render: (v?: string) => v || '-' },
      {
        title: 'IP',
        dataIndex: 'pod_ip',
        width: 200,
        render: (v?: string) => {
          const ip = v || '';
          const pinned = ip && stableIPSet.has(ip);
          return (
            <Space size={4} wrap>
              <span>{ip || '-'}</span>
              {pinned ? <Tag color="green">固定</Tag> : null}
              {!pinned && overlaySecondary && stableIPSet.size > 0 ? (
                <Tooltip title="此列为集群默认网卡 IP；对外直连请用「网络与访问」中的副网卡固定 IP">
                  <Tag>集群网卡</Tag>
                </Tooltip>
              ) : null}
              {!pinned && !overlaySecondary && stableIPSet.size > 0 && ip ? <Tag color="orange">非固定</Tag> : null}
            </Space>
          );
        },
      },
      { title: '重启', dataIndex: 'restart_count', width: 80, align: 'center' },
      {
        title: '运行时长',
        dataIndex: 'age_seconds',
        width: 100,
        render: (s?: number) => (s != null ? formatDuration(s * 1000) : '-'),
      },
      {
        title: '操作',
        key: 'actions',
        width: 440,
        render: (_, r) => {
          const containers = (r as any).containers as Array<{ name: string; image: string }> | undefined;
          const containerName = containers?.[0]?.name;
          return (
            <Space size="small" wrap>
              {canScale && (
                <Button
                  type="link"
                  size="small"
                  icon={<ReloadOutlined />}
                  disabled={!r.name}
                  loading={restartPodMutation.isPending}
                  onClick={() => restartPodMutation.mutate(r.name)}
                >
                  重启
                </Button>
              )}
              <Button
                type="link"
                size="small"
                icon={<FileTextOutlined />}
                disabled={!r.name}
                onClick={() => setLogDrawer({ open: true, pod: r.name, container: containerName })}
              >
                日志
              </Button>
              {canExec && (
                <>
                  <Button
                    type="link"
                    size="small"
                    icon={<CodeOutlined />}
                    disabled={!r.name}
                    onClick={() => setTermDrawer({ open: true, pod: r.name, container: containerName })}
                  >
                    WebSSH
                  </Button>
                  <Button
                    type="link"
                    size="small"
                    icon={<DeploymentUnitOutlined />}
                    disabled={!r.name}
                    onClick={() => setFileDrawer({ open: true, pod: r.name, container: containerName })}
                  >
                    文件
                  </Button>
                  <Button
                    type="link"
                    size="small"
                    icon={<DesktopOutlined />}
                    disabled={!r.name}
                    onClick={() => setNetDrawer({ open: true, pod: r.name, container: containerName })}
                  >
                    网络命令
                  </Button>
                </>
              )}
              <Button
                type="link"
                size="small"
                icon={<CopyOutlined />}
                disabled={!r.pod_ip && stableIPSet.size === 0}
                onClick={() => {
                  const direct = r.pod_ip && stableIPSet.has(r.pod_ip) ? r.pod_ip : (stable?.items?.[0]?.ip || r.pod_ip);
                  if (!direct) {
                    message.warning('暂无访问地址');
                    return;
                  }
                  const addr = probePort ? `${direct}:${probePort}` : direct;
                  void navigator.clipboard.writeText(addr).then(
                    () => message.success(`已复制 ${addr}`),
                    () => message.error('复制失败'),
                  );
                }}
              >
                复制访问地址
              </Button>
              {/* AI 诊断：仅在 Pod 未就绪时展示（ready=false 或 phase 非 Running/Succeeded 或频繁重启）。 */}
              {canDiagnose && (!r.ready || (r.restart_count && r.restart_count >= 3) || (r.phase && !['Running', 'Succeeded'].includes(r.phase))) && (
                <Button
                  type="link"
                  size="small"
                  icon={<RobotOutlined />}
                  disabled={!r.name}
                  onClick={() => openPodDiagnosis(r)}
                >
                  AI 诊断
                </Button>
              )}
            </Space>
          );
        },
      },
    ];

    const logLines: LogLine[] = (logs || '')
      .split('\n')
      .filter(Boolean)
      .map((line, i) => ({ sequence: i, timestamp: '', stream: 'stdout', message: line }));

    return (
      <>
        <div style={{ marginBottom: 16, textAlign: 'right' }}>
          <Button icon={<ReloadOutlined />} loading={isFetching} onClick={() => refetch()}>
            刷新
          </Button>
        </div>
        <Table<PodSummary>
          rowKey="name"
          loading={isLoading}
          columns={columns}
          dataSource={data}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无 Pod" description="分组尚未部署或集群不可达" /> }}
        />

        <Drawer
          title={logDrawer.pod ? `日志 - ${logDrawer.pod}` : '日志'}
          open={logDrawer.open}
          onClose={() => setLogDrawer({ open: false })}
          width={900}
          styles={{ body: { padding: 12 } }}
        >
          <PodLogPanel
            groupId={groupId}
            pod={logDrawer.pod || ''}
            container={logDrawer.container}
            clusterId={group?.cluster_id || 0}
            namespace={group?.namespace || ''}
            logLines={logLines}
          />
        </Drawer>

        <Modal
          title={execModal.pod ? `exec - ${execModal.pod.name}` : 'exec'}
          open={execModal.open}
          onCancel={() => setExecModal({ open: false })}
          footer={[
            <Button key="cancel" onClick={() => setExecModal({ open: false })}>关闭</Button>,
            <Button key="run" type="primary" loading={execMutation.isPending} onClick={() => execMutation.mutate()}>
              执行
            </Button>,
          ]}
          width={760}
        >
          <Space direction="vertical" style={{ width: '100%' }} size="middle">
            <Space>
              <Typography.Text>命令:</Typography.Text>
              <Input value={execCmd} onChange={(e) => setExecCmd(e.target.value)} style={{ width: 300 }} placeholder="例如 sh 或 ls -la" />
            </Space>
            <Typography.Text>输出:</Typography.Text>
            <pre style={{ background: '#0b0b0b', color: '#e6e6e6', padding: 12, borderRadius: 6, maxHeight: 360, overflow: 'auto', fontSize: 12 }}>
              {execOutput || '(点击「执行」运行命令)'}
            </pre>
          </Space>
        </Modal>

        <Drawer
          title={termDrawer.pod ? `WebSSH - ${termDrawer.pod}` : 'WebSSH'}
          open={termDrawer.open}
          onClose={() => setTermDrawer({ open: false })}
          width={820}
          styles={{ body: { padding: 12, height: '100%' } }}
          destroyOnHidden
        >
          {termDrawer.open && termDrawer.pod && group?.cluster_id && group?.namespace && (
            <PodTerminal
              clusterId={group.cluster_id}
              namespace={group.namespace}
              pod={termDrawer.pod}
              container={termDrawer.container}
              onClose={() => setTermDrawer({ open: false })}
            />
          )}
        </Drawer>

        <Drawer
          title={fileDrawer.pod ? `文件浏览器 - ${fileDrawer.pod}` : '文件浏览器'}
          open={fileDrawer.open}
          onClose={() => setFileDrawer({ open: false })}
          width={820}
          styles={{ body: { padding: 12, height: '100%' } }}
          destroyOnHidden
        >
          {fileDrawer.open && fileDrawer.pod && (
            <PodFileBrowser
              groupId={groupId}
              pod={fileDrawer.pod}
              containers={podContainersFor(fileDrawer.pod, data)}
              defaultContainer={fileDrawer.container}
            />
          )}
        </Drawer>

        <Drawer
          title={netDrawer.pod ? `网络命令 - ${netDrawer.pod}` : '网络命令'}
          open={netDrawer.open}
          onClose={() => setNetDrawer({ open: false })}
          width={820}
          styles={{ body: { padding: 12, height: '100%' } }}
          destroyOnHidden
        >
          {netDrawer.open && netDrawer.pod && (
            <PodNetCmd
              groupId={groupId}
              pod={netDrawer.pod}
              containers={podContainersFor(netDrawer.pod, data)}
              defaultContainer={netDrawer.container}
            />
          )}
        </Drawer>

        <DiagnosisDrawer open={diagOpen} onClose={() => setDiagOpen(false)} input={diagInput} />
      </>
    );
  }

  function ConfigsTab({ groupId, applicationId }: { groupId: number; applicationId?: number }) {
    const { message } = App.useApp();
    const queryClient = useQueryClient();
    const [bindOpen, setBindOpen] = useState(false);
    const [bindForm] = Form.useForm<{ config_set_id: number; priority: number; pinned_version?: number }>();
    const [localEditOpen, setLocalEditOpen] = useState(false);
    const [localForm] = Form.useForm();
    const [diffOpen, setDiffOpen] = useState(false);
    const [diffSnapshotId, setDiffSnapshotId] = useState<number>();
    const [diffFilePath, setDiffFilePath] = useState<string>();
    const [diffTarget, setDiffTarget] = useState<{ target_type: string; target_id: number }>();
    const [cloneOpen, setCloneOpen] = useState(false);
    const [cloneForm] = Form.useForm<{
      source_group_id: number;
      file_paths: string[];
      include_env: boolean;
      include_command: boolean;
      include_args: boolean;
    }>();

    const SNAPSHOT_REASON: Record<string, string> = {
      update: '配置更新',
      bind: '绑定配置',
      unbind: '解绑配置',
    };

    const { data: bindings } = useQuery({
      queryKey: ['group', groupId, 'config-bindings'],
      queryFn: () => configApi.listBindings(groupId),
      enabled: !!groupId,
    });
    const currentBinding = bindings && bindings.length > 0 ? bindings[0] : undefined;

    const { data: appConfigSets } = useQuery({
      queryKey: ['application', applicationId, 'config-sets'],
      queryFn: () => configApi.listAppConfigSets(applicationId!),
      enabled: !!applicationId,
    });

    const { data: boundConfigSet } = useQuery({
      queryKey: ['config-set', currentBinding?.config_set_id],
      queryFn: () => configApi.getConfigSet(currentBinding!.config_set_id),
      enabled: !!currentBinding?.config_set_id,
    });

    const { data: localConfig } = useQuery({
      queryKey: ['group', groupId, 'local-config'],
      queryFn: () => configApi.getLocalConfig(groupId),
      enabled: !!groupId && !currentBinding,
    });

    const { data: contentSnapshots = [] } = useQuery({
      queryKey: ['config-snapshots', currentBinding ? 'config_set' : 'group_local', currentBinding?.config_set_id ?? groupId],
      queryFn: () =>
        currentBinding
          ? configApi.listConfigSetSnapshots(currentBinding!.config_set_id)
          : configApi.listLocalConfigSnapshots(groupId),
      enabled: !!groupId && (currentBinding ? !!currentBinding.config_set_id : true),
    });

    const { data: bindSnapshots = [] } = useQuery({
      queryKey: ['group', groupId, 'config-bind-snapshots'],
      queryFn: () => configApi.listGroupBindSnapshots(groupId),
      enabled: !!groupId,
    });

    const allSnapshots: ConfigContentSnapshot[] = useMemo(() => {
      const merged = [...contentSnapshots, ...bindSnapshots];
      return merged.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());
    }, [contentSnapshots, bindSnapshots]);

    const effectiveContent = useMemo(() => {
      if (currentBinding && boundConfigSet) {
        return parseConfigContent(boundConfigSet.content);
      }
      if (localConfig) {
        return parseConfigContent(localConfig.content);
      }
      return parseConfigContent(null);
    }, [currentBinding, boundConfigSet, localConfig]);

    const filePathOptions = useMemo(
      () => (effectiveContent.files || []).map((f) => f.path).filter(Boolean).map((p) => ({ label: p, value: p })),
      [effectiveContent],
    );

    const diffTargetForSnapshot = (snap: ConfigContentSnapshot) => {
      if (snap.target_type === 'config_set') {
        return { target_type: 'config_set', target_id: snap.target_id };
      }
      if (snap.target_type === 'group_local') {
        return { target_type: 'group_local', target_id: snap.target_id };
      }
      if (currentBinding?.config_set_id) {
        return { target_type: 'config_set', target_id: currentBinding.config_set_id };
      }
      return { target_type: 'group_local', target_id: groupId };
    };

    const { data: fileDiff, isLoading: diffLoading } = useQuery({
      queryKey: ['config-file-diff', diffSnapshotId, diffFilePath, diffTarget?.target_type, diffTarget?.target_id],
      queryFn: () =>
        configApi.diffConfigFile(diffSnapshotId!, {
          file_path: diffFilePath!,
          target_type: diffTarget!.target_type,
          target_id: diffTarget!.target_id,
        }),
      enabled: !!diffOpen && !!diffSnapshotId && !!diffFilePath && !!diffTarget,
    });

    const { data: siblingGroups } = useQuery({
      queryKey: ['application', applicationId, 'groups', 'clone-source'],
      queryFn: () => groupApi.list(applicationId!, { size: 200 }).then((r) => r.items),
      enabled: !!applicationId && cloneOpen,
    });

    const cloneSourceId = Form.useWatch('source_group_id', cloneForm) as number | undefined;

    const { data: cloneSourceFiles = [] } = useQuery({
      queryKey: ['group', cloneSourceId, 'config-files'],
      queryFn: () => configApi.listGroupConfigFiles(cloneSourceId!),
      enabled: !!cloneSourceId && cloneOpen,
    });

    const invalidateConfigQueries = () => {
      queryClient.invalidateQueries({ queryKey: ['group', groupId, 'config-bindings'] });
      queryClient.invalidateQueries({ queryKey: ['group', groupId, 'local-config'] });
      queryClient.invalidateQueries({ queryKey: ['config-snapshots'] });
      queryClient.invalidateQueries({ queryKey: ['group', groupId, 'config-bind-snapshots'] });
      if (currentBinding?.config_set_id) {
        queryClient.invalidateQueries({ queryKey: ['config-set', currentBinding.config_set_id] });
      }
    };

    const bindMutation = useMutation({
      mutationFn: (v: { config_set_id: number; priority: number; pinned_version?: number }) =>
        configApi.createBinding(groupId, v),
      onSuccess: () => {
        message.success('配置已绑定，分组配置已被覆盖');
        setBindOpen(false);
        bindForm.resetFields();
        invalidateConfigQueries();
      },
      onError: (e: any) => message.error(e?.message || '绑定失败'),
    });

    const saveLocalMutation = useMutation({
      mutationFn: (v: { content: Record<string, any>; version: number }) =>
        configApi.upsertLocalConfig(groupId, { content: v.content, version: v.version }),
      onSuccess: () => {
        message.success('配置已保存');
        setLocalEditOpen(false);
        invalidateConfigQueries();
      },
      onError: (e: any) => message.error(e?.message || '保存失败'),
    });

    const cloneMutation = useMutation({
      mutationFn: (v: {
        source_group_id: number;
        file_paths: string[];
        include_env: boolean;
        include_command: boolean;
        include_args: boolean;
      }) => configApi.cloneLocalConfigFromGroup(groupId, v),
      onSuccess: () => {
        message.success('配置已克隆');
        setCloneOpen(false);
        cloneForm.resetFields();
        invalidateConfigQueries();
      },
      onError: (e: any) => message.error(e?.message || '克隆失败'),
    });

    const unbind = (id: number) =>
      confirmDanger({
        title: '解绑配置',
        content: '确认解除配置绑定？解绑后分组将沿用解绑时的配置内容，不再随原配置变更。',
        onOk: async () => {
          await configApi.deleteBinding(id);
          message.success('已解绑');
          invalidateConfigQueries();
        },
      });

    const deleteLocal = () =>
      confirmDanger({
        title: '删除配置',
        content: '确认删除分组的当前配置？',
        onOk: async () => {
          await configApi.deleteLocalConfig(groupId);
          message.success('配置已删除');
          invalidateConfigQueries();
        },
      });

    const openDiff = (snap: ConfigContentSnapshot) => {
      setDiffSnapshotId(snap.id);
      setDiffTarget(diffTargetForSnapshot(snap));
      setDiffFilePath(undefined);
      setDiffOpen(true);
    };

    const snapshotColumns: ColumnsType<ConfigContentSnapshot> = [
      { title: '版本', dataIndex: 'snapshot_no', width: 72, align: 'center' },
      {
        title: '类型',
        dataIndex: 'target_type',
        width: 110,
        render: (v: string) => (
          <Tag color={v === 'group_bind' ? 'purple' : v === 'config_set' ? 'blue' : 'green'}>
            {v === 'group_bind' ? '绑定快照' : v === 'config_set' ? '配置集' : '本地配置'}
          </Tag>
        ),
      },
      {
        title: '原因',
        dataIndex: 'change_reason',
        width: 100,
        render: (v: string) => SNAPSHOT_REASON[v] || v,
      },
      { title: '文件数', dataIndex: 'file_count', width: 72, align: 'center' },
      { title: '时间', dataIndex: 'created_at', width: 180, render: formatTime },
      {
        title: '操作',
        width: 88,
        render: (_, row) => (
          <Button type="link" size="small" icon={<DiffOutlined />} onClick={() => openDiff(row)}>
            比对
          </Button>
        ),
      },
    ];

    const setsForBinding: ConfigSet[] = appConfigSets || [];
    const cloneGroupOptions = (siblingGroups || [])
      .filter((g) => g.id !== groupId)
      .map((g) => ({ label: g.display_name || g.name, value: g.id }));

    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          分组可绑定 <b>一个</b> 应用配置，绑定后该配置内容覆盖分组的当前配置（只读）；解绑后分组沿用解绑时的配置内容，不再随原配置变更。未绑定时可直接在分组内维护配置，并支持从其他分组克隆选定文件。
        </Typography.Paragraph>

        <Card
          size="small"
          title={currentBinding ? `配置（来自绑定：${boundConfigSet?.name ?? `#${currentBinding.config_set_id}`}）` : '配置'}
          extra={
            <PermissionGate code="config:manage">
            <Space>
              {currentBinding ? (
                <>
                  <Button size="small" type="primary" onClick={() => { setBindOpen(true); bindForm.resetFields(); }} disabled={!applicationId}>
                    重新绑定
                  </Button>
                  <Button size="small" danger onClick={() => unbind(currentBinding.id)}>解绑</Button>
                </>
              ) : (
                <>
                  <Button size="small" type="primary" onClick={() => { setBindOpen(true); bindForm.resetFields(); }} disabled={!applicationId}>
                    绑定配置
                  </Button>
                  <Button
                    size="small"
                    icon={<CopyOutlined />}
                    onClick={() => {
                      cloneForm.resetFields();
                      cloneForm.setFieldsValue({
                        file_paths: [],
                        include_env: false,
                        include_command: false,
                        include_args: false,
                      });
                      setCloneOpen(true);
                    }}
                    disabled={!applicationId}
                  >
                    从其他分组克隆
                  </Button>
                  {localConfig && (
                    <Button size="small" onClick={() => deleteLocal()} danger>删除</Button>
                  )}
                  <Button
                    size="small"
                    onClick={() => {
                      if (localConfig) {
                        populateFormFromContent(localForm, parseConfigContent(localConfig.content));
                        localForm.setFieldsValue({ version: localConfig.version });
                      } else {
                        populateFormFromContent(localForm, parseConfigContent(null));
                        localForm.setFieldsValue({ version: 1 });
                      }
                      setLocalEditOpen(true);
                    }}
                  >
                    {localConfig ? '编辑配置' : '新建配置'}
                  </Button>
                </>
              )}
            </Space>
            </PermissionGate>
          }
        >
          {currentBinding ? (
            boundConfigSet ? (
              <ConfigContentPreview content={effectiveContent} />
            ) : (
              <EmptyState title="加载绑定配置中..." />
            )
          ) : localConfig ? (
            <ConfigContentPreview content={effectiveContent} />
          ) : (
            <EmptyState
              title="未配置"
              description="可绑定应用配置，或直接在分组内维护配置文件、环境变量与启动参数。"
            />
          )}
        </Card>

        <Card
          size="small"
          title={
            <Space>
              <HistoryOutlined />
              <span>配置历史快照</span>
            </Space>
          }
          extra={
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              文件内容变更或绑定/解绑时自动留存，可与当前版本逐文件比对
            </Typography.Text>
          }
        >
          <Table<ConfigContentSnapshot>
            rowKey="id"
            size="small"
            columns={snapshotColumns}
            dataSource={allSnapshots}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            locale={{ emptyText: <EmptyState title="暂无历史快照" description="保存配置或绑定变更后将自动生成" /> }}
          />
        </Card>

        <Modal
          title="绑定配置"
          open={bindOpen}
          onCancel={() => setBindOpen(false)}
          onOk={() => bindForm.submit()}
          confirmLoading={bindMutation.isPending}
          destroyOnHidden
        >
          <Form layout="vertical" form={bindForm} onFinish={(v) => bindMutation.mutate(v)} initialValues={{ priority: 100 }}>
            <Form.Item name="config_set_id" label="配置" rules={[{ required: true, message: '请选择配置' }]}>
              <Select
                placeholder="选择当前应用下的配置"
                options={setsForBinding.map((c) => ({ label: c.name, value: c.id }))}
              />
            </Form.Item>
            <Form.Item name="priority" label="优先级" rules={[{ required: true }]} extra="数值越小优先级越高">
              <InputNumber min={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="pinned_version" label="固定版本（可选）">
              <InputNumber style={{ width: '100%' }} placeholder="留空使用最新版本" />
            </Form.Item>
          </Form>
        </Modal>

        <Modal
          title="编辑分组配置"
          open={localEditOpen}
          onCancel={() => setLocalEditOpen(false)}
          onOk={() => localForm.submit()}
          confirmLoading={saveLocalMutation.isPending}
          destroyOnHidden
          width={780}
        >
          <Form
            layout="vertical"
            form={localForm}
            onFinish={(v) => {
              saveLocalMutation.mutate({
                content: buildConfigContentFromForm(v) as unknown as Record<string, any>,
                version: v.version,
              });
            }}
          >
            <Form.Item name="version" hidden><Input /></Form.Item>
            <ConfigContentEditor />
          </Form>
        </Modal>

        <Modal
          title="从其他分组克隆配置"
          open={cloneOpen}
          onCancel={() => setCloneOpen(false)}
          onOk={() => cloneForm.submit()}
          confirmLoading={cloneMutation.isPending}
          destroyOnHidden
          width={640}
        >
          <Typography.Paragraph type="secondary" style={{ marginBottom: 12 }}>
            仅未绑定配置集的分组可使用。将源分组生效配置中的选定文件（及可选的环境变量、启动参数）合并到本分组本地配置。
          </Typography.Paragraph>
          <Form
            layout="vertical"
            form={cloneForm}
            onFinish={(v) => cloneMutation.mutate(v)}
            initialValues={{ file_paths: [], include_env: false, include_command: false, include_args: false }}
          >
            <Form.Item name="source_group_id" label="源分组" rules={[{ required: true, message: '请选择源分组' }]}>
              <Select placeholder="选择同应用下的分组" options={cloneGroupOptions} showSearch optionFilterProp="label" />
            </Form.Item>
            <Form.Item name="file_paths" label="克隆文件" extra="不选文件时可仅克隆环境变量或启动参数">
              <Select
                mode="multiple"
                placeholder={cloneSourceId ? '选择要克隆的配置文件' : '请先选择源分组'}
                options={cloneSourceFiles.map((p) => ({ label: p, value: p }))}
                disabled={!cloneSourceId}
                allowClear
              />
            </Form.Item>
            <Form.Item label="其他项">
              <Space direction="vertical">
                <Form.Item name="include_env" valuePropName="checked" noStyle>
                  <Checkbox>环境变量</Checkbox>
                </Form.Item>
                <Form.Item name="include_command" valuePropName="checked" noStyle>
                  <Checkbox>启动命令</Checkbox>
                </Form.Item>
                <Form.Item name="include_args" valuePropName="checked" noStyle>
                  <Checkbox>启动参数</Checkbox>
                </Form.Item>
              </Space>
            </Form.Item>
          </Form>
        </Modal>

        <Modal
          title="配置文件比对"
          open={diffOpen}
          onCancel={() => {
            setDiffOpen(false);
            setDiffSnapshotId(undefined);
            setDiffFilePath(undefined);
            setDiffTarget(undefined);
          }}
          footer={null}
          width={920}
          destroyOnHidden
        >
          <Space style={{ marginBottom: 12 }} wrap>
            <Select
              placeholder="历史快照"
              style={{ width: 280 }}
              value={diffSnapshotId}
              onChange={(id) => {
                const snap = allSnapshots.find((s) => s.id === id);
                setDiffSnapshotId(id);
                if (snap) setDiffTarget(diffTargetForSnapshot(snap));
              }}
              options={allSnapshots.map((s) => ({
                label: `#${s.snapshot_no} ${SNAPSHOT_REASON[s.change_reason] || s.change_reason} (${formatTime(s.created_at)})`,
                value: s.id,
              }))}
            />
            <Select
              placeholder="选择文件"
              style={{ width: 320 }}
              value={diffFilePath}
              onChange={setDiffFilePath}
              options={filePathOptions}
              showSearch
              optionFilterProp="label"
            />
          </Space>
          {diffSnapshotId && diffFilePath ? (
            diffLoading ? (
              <EmptyState title="加载比对中..." />
            ) : fileDiff ? (
              <div>
                <Descriptions size="small" column={2} style={{ marginBottom: 8 }}>
                  <Descriptions.Item label="文件">{fileDiff.file_path}</Descriptions.Item>
                  <Descriptions.Item label="左侧">历史快照</Descriptions.Item>
                  <Descriptions.Item label="右侧">当前版本</Descriptions.Item>
                </Descriptions>
                <DiffViewer
                  original={fileDiff.original}
                  modified={fileDiff.modified}
                  language={fileDiff.language}
                  height={480}
                />
              </div>
            ) : (
              <EmptyState title="无法加载比对结果" />
            )
          ) : (
            <EmptyState title="请选择快照与文件" description="左侧为历史快照内容，右侧为当前生效配置" />
          )}
        </Modal>
      </Space>
    );
  }

  function EventsTab({ groupId }: { groupId: number }) {
    const { data, isLoading } = useQuery({
      queryKey: ['group', groupId, 'events'],
      queryFn: () => groupApi.listEvents(groupId),
      enabled: !!groupId,
    });

    const columns: ColumnsType<GroupEvent> = [
      {
        title: '类型',
        dataIndex: 'type',
        width: 80,
        render: (v: string) => <Tag color={v === 'Warning' ? 'orange' : v === 'Error' ? 'red' : 'blue'}>{v}</Tag>,
      },
      { title: '原因', dataIndex: 'reason', width: 140 },
      { title: '消息', dataIndex: 'message' },
      { title: '次数', dataIndex: 'count', width: 80, align: 'center' },
      { title: '最近时间', dataIndex: 'last_time', width: 180, render: formatTime },
      { title: '首次时间', dataIndex: 'first_time', width: 180, render: formatTime },
    ];

    return (
      <Table<GroupEvent>
        rowKey={(r, i) => `${r.type}-${r.reason}-${r.first_time}-${r.last_time}-${r.count}-${i}`}
        loading={isLoading}
        columns={columns}
        dataSource={data}
        pagination={{ pageSize: 50 }}
        locale={{ emptyText: <EmptyState title="暂无事件" description="集群不可达或无相关事件" /> }}
      />
    );
  }

  function YAMLTab({ groupId }: { groupId: number }) {
    const { data, isLoading } = useQuery({
      queryKey: ['group', groupId, 'yaml'],
      queryFn: () => groupApi.getYAML(groupId),
      enabled: !!groupId,
    });

    if (isLoading) return <EmptyState title="加载中..." />;
    if (!data || data.length === 0) return <EmptyState title="暂无 YAML" description="分组尚未部署到集群" />;

    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {data.map((r: RenderedResource) => (
          <Card key={`${r.kind}-${r.name}`} size="small" title={`${r.kind}: ${r.name}`}>
            <pre style={{ background: '#0b0b0b', color: '#e6e6e6', padding: 12, borderRadius: 6, overflow: 'auto', fontSize: 12, margin: 0 }}>
              {r.yaml}
            </pre>
          </Card>
        ))}
      </Space>
    );
  }

  function ReleasesTab({ groupId, applicationId, currentImageId }: { groupId: number; applicationId?: number; currentImageId?: number }) {
    const { data, isLoading } = useQuery({
      queryKey: ['group', groupId, 'releases'],
      queryFn: () => releaseApi.list(groupId, { page: 1, size: 20 }),
      enabled: !!groupId,
    });

    // 新建发布：PublishModal 选镜像（排除当前在跑版本）+ 策略 + 目标 Pod。
    const [newOpen, setNewOpen] = useState(false);

    const columns: ColumnsType<Release> = [
      {
        title: '发布',
        dataIndex: 'release_number',
        width: 80,
        render: (n: number, r) => (
          <a onClick={() => navigate(`/releases/${r.id}`)}>#{n}</a>
        ),
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 110,
        render: (s: Release['status']) => <ResourceStatus status={s} />,
      },
      { title: '策略', dataIndex: 'strategy', width: 120, render: (v: string) => strategyLabel(v) },
      { title: '副本', dataIndex: 'replicas', width: 80, align: 'center' },
      { title: '进度', dataIndex: 'progress_percent', width: 100, render: (v: number) => `${v}%` },
      { title: '镜像', dataIndex: 'image_ref', render: (v?: string) => v ? <code>{v}</code> : '-' },
      { title: '耗时', dataIndex: 'duration_ms', width: 100, render: formatDuration },
      { title: '时间', dataIndex: 'created_at', width: 140, render: formatRelative },
    ];

    return (
      <>
        {canRelease && (
          <div style={{ marginBottom: 16, textAlign: 'right' }}>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={!applicationId}
              onClick={() => setNewOpen(true)}
            >
              新建发布
            </Button>
          </div>
        )}
        <Table<Release>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={data?.items}
          pagination={false}
          locale={{ emptyText: <EmptyState title="暂无发布历史" /> }}
        />

        <PublishModal
          open={newOpen}
          onClose={() => setNewOpen(false)}
          applicationId={applicationId!}
          fixedGroupId={groupId}
          excludeImageId={currentImageId}
          onPublished={(relId) => {
            void queryClient.invalidateQueries({ queryKey: ['group', groupId, 'releases'] });
            navigate(`/releases/${relId}`);
          }}
        />
      </>
    );
  }
}
