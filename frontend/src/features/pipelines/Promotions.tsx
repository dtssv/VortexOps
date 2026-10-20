import { useState } from 'react';
import { Button, Drawer, Form, Input, Modal, Select, Space, Table, Tag, Steps, App, Tooltip } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ColumnsType } from 'antd/es/table';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { pipelineApi } from '@/api/pipelines';
import { applicationApi, groupApi } from '@/api/applications';
import { buildApi } from '@/api/builds';
import type { Promotion } from '@/types';
import { confirmDanger } from '@/utils/action';
import { formatTime } from '@/utils/format';

const ENV_OPTIONS = [
  { label: 'dev', value: 'dev' },
  { label: 'staging', value: 'staging' },
  { label: 'prod', value: 'prod' },
];

const STRATEGY_OPTIONS = [
  { label: 'Rolling（滚动）', value: 'rolling' },
  { label: 'Blue-Green（蓝绿）', value: 'blue_green' },
  { label: 'Canary（金丝雀）', value: 'canary' },
];

// 晋升状态 → Steps 当前步索引
function statusStepIndex(status: string): number {
  switch (status) {
    case 'pending':
      return 0;
    case 'deploying':
      return 1;
    case 'verifying':
      return 2;
    case 'succeeded':
      return 3;
    default: // failed | aborted
      return 3;
  }
}

function statusStepStatus(status: string): 'process' | 'finish' | 'error' | 'wait' {
  switch (status) {
    case 'succeeded':
      return 'finish';
    case 'failed':
    case 'aborted':
      return 'error';
    case 'pending':
      return 'wait';
    default:
      return 'process';
  }
}

const STATUS_COLOR: Record<string, string> = {
  pending: 'default',
  deploying: 'processing',
  verifying: 'processing',
  succeeded: 'success',
  failed: 'error',
  aborted: 'warning',
};

export default function PromotionsPage() {
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [statusFilter, setStatusFilter] = useState<string>();
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm] = Form.useForm();
  const [detailTarget, setDetailTarget] = useState<Promotion | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['promotions', { page, size, status: statusFilter }],
    queryFn: () => pipelineApi.listPromotions({ page, size, status: statusFilter }),
  });

  const createMutation = useMutation({
    mutationFn: (body: Record<string, any>) => pipelineApi.createPromotion(body),
    onSuccess: () => {
      message.success('晋升已创建');
      setCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['promotions'] });
    },
    onError: (e: any) => message.error(e?.message || '创建失败'),
  });

  const invalidateDetail = (id: number) => {
    queryClient.invalidateQueries({ queryKey: ['promotions'] });
    queryClient.invalidateQueries({ queryKey: ['promotion', id] });
    setDetailTarget(null);
  };

  const startMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.startPromotion(id),
    onSuccess: () => { message.success('晋升已启动'); invalidateDetail; },
    onError: (e: any) => message.error(e?.message || '启动失败'),
  });

  const completeDeployMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.completePromotionDeployment(id),
    onSuccess: () => { message.success('已标记部署完成'); invalidateDetail; },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const completeVerifyPassMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.completePromotionVerify(id, { passed: true }),
    onSuccess: () => { message.success('验证通过，晋升成功'); invalidateDetail; },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const completeVerifyFailMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.completePromotionVerify(id, { passed: false }),
    onSuccess: () => { message.warning('验证未通过，晋升失败'); invalidateDetail; },
    onError: (e: any) => message.error(e?.message || '操作失败'),
  });

  const abortMutation = useMutation({
    mutationFn: (id: number) => pipelineApi.abortPromotion(id),
    onSuccess: () => { message.success('晋升已中止'); invalidateDetail; },
    onError: (e: any) => message.error(e?.message || '中止失败'),
  });

  const isTerminal = (s: string) => s === 'succeeded' || s === 'failed' || s === 'aborted';

  const columns: ColumnsType<Promotion> = [
    { title: 'ID', dataIndex: 'id', width: 70 },
    { title: '应用', dataIndex: 'application_id', width: 90 },
    {
      title: '环境',
      key: 'env',
      width: 160,
      render: (_, r) => (
        <Space size="small">
          <Tag color="blue">{r.source_env}</Tag>
          <span>→</span>
          <Tag color={r.target_env === 'prod' ? 'red' : 'gold'}>{r.target_env}</Tag>
        </Space>
      ),
    },
    { title: '策略', dataIndex: 'strategy', width: 120, render: (s: string) => <Tag>{s}</Tag> },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (s: string) => <ResourceStatus status={s} />,
    },
    { title: '镜像ID', dataIndex: 'artifact_image_id', width: 90 },
    {
      title: '自动晋升',
      dataIndex: 'auto_promote_on_verify',
      width: 90,
      render: (v: boolean) => (v ? <Tag color="green">是</Tag> : '-'),
    },
    { title: '开始时间', dataIndex: 'started_at', width: 180, render: (t: string) => (t ? formatTime(t) : '-') },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_, r) => (
        <Button type="link" size="small" onClick={() => setDetailTarget(r)}>
          详情 / 操作
        </Button>
      ),
    },
  ];

  return (
    <PageContainer
      title="环境晋升"
      subtitle="跨环境晋升状态机：dev → staging → prod，含签名/发布窗口/审批校验"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          新建晋升
        </Button>
      }
    >
      <Space style={{ marginBottom: 16 }}>
        <Select
          allowClear
          placeholder="按状态筛选"
          style={{ width: 160 }}
          options={Object.keys(STATUS_COLOR).map((s) => ({ label: s, value: s }))}
          value={statusFilter}
          onChange={(v) => { setStatusFilter(v); setPage(1); }}
        />
      </Space>
      <Table
        rowKey="id"
        loading={isLoading}
        columns={columns}
        dataSource={data?.items || []}
        locale={{ emptyText: <EmptyState title="暂无晋升记录" /> }}
        pagination={{
          current: page,
          pageSize: size,
          total: data?.total || 0,
          showSizeChanger: true,
          showTotal: (t) => `共 ${t} 条`,
          onChange: (p, s) => { setPage(p); setSize(s); },
        }}
      />

      <PromotionCreateModal
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        form={createForm}
        onSubmit={(v) => createMutation.mutate(v)}
        submitting={createMutation.isPending}
      />

      <Drawer
        title={detailTarget ? `晋升 #${detailTarget.id}` : '晋升详情'}
        open={!!detailTarget}
        onClose={() => setDetailTarget(null)}
        width={680}
        destroyOnClose
      >
        {detailTarget && (
          <PromotionDetail
            promotion={detailTarget}
            isTerminal={isTerminal(detailTarget.status)}
            onStart={() => startMutation.mutate(detailTarget.id)}
            onCompleteDeploy={() => completeDeployMutation.mutate(detailTarget.id)}
            onCompleteVerifyPass={() => completeVerifyPassMutation.mutate(detailTarget.id)}
            onCompleteVerifyFail={() => completeVerifyFailMutation.mutate(detailTarget.id)}
            onAbort={() =>
              confirmDanger({
                title: '中止晋升',
                content: '确定中止此次晋升？中止后不可恢复。',
                okText: '中止',
                onOk: () => abortMutation.mutateAsync(detailTarget.id),
              })
            }
            loading={
              startMutation.isPending ||
              completeDeployMutation.isPending ||
              completeVerifyPassMutation.isPending ||
              completeVerifyFailMutation.isPending ||
              abortMutation.isPending
            }
          />
        )}
      </Drawer>
    </PageContainer>
  );
}

// 创建晋升 Modal（选择应用 → 镜像 → 源/目标环境 → 策略）
function PromotionCreateModal({
  open,
  onCancel,
  form,
  onSubmit,
  submitting,
}: {
  open: boolean;
  onCancel: () => void;
  form: ReturnType<typeof Form.useForm>[0];
  onSubmit: (v: any) => void;
  submitting: boolean;
}) {
  const appId = Form.useWatch('application_id', form);
  const sourceEnv = Form.useWatch('source_env', form);

  const { data: appsPage } = useQuery({
    queryKey: ['applications', 'all', { page: 1, size: 200 }],
    queryFn: () => applicationApi.listAll({ page: 1, size: 200 }),
  });
  const apps = appsPage?.items ?? [];

  const { data: imagesPage } = useQuery({
    queryKey: ['build-images', appId],
    queryFn: () => buildApi.listImages(appId!, { page: 1, size: 100 }),
    enabled: !!appId,
  });
  const images = imagesPage?.items ?? [];

  // 选中应用的分组列表（用于目标分组下拉）
  const { data: groupsPage } = useQuery({
    queryKey: ['application-groups', appId],
    queryFn: () => groupApi.list(appId!, { page: 1, size: 200 }),
    enabled: !!appId,
  });
  const groups = groupsPage?.items ?? [];

  // 目标环境选项：根据源环境递进
  const targetOptions = (() => {
    if (sourceEnv === 'dev') return [{ label: 'staging', value: 'staging' }];
    if (sourceEnv === 'staging') return [{ label: 'prod', value: 'prod' }];
    return ENV_OPTIONS.filter((o) => o.value !== sourceEnv);
  })();

  return (
    <Modal
      title="新建环境晋升"
      open={open}
      onCancel={onCancel}
      onOk={() => form.submit()}
      confirmLoading={submitting}
      destroyOnClose
      width={560}
    >
      <Form layout="vertical" form={form} onFinish={onSubmit} initialValues={{ strategy: 'rolling', auto_promote_on_verify: false }}>
        <Form.Item name="application_id" label="应用" rules={[{ required: true, message: '请选择应用' }]}>
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="选择应用"
            options={apps.map((a) => ({ label: a.display_name || a.name, value: a.id }))}
          />
        </Form.Item>
        <Form.Item name="artifact_image_id" label="镜像" rules={[{ required: true, message: '请选择镜像' }]}>
          <Select
            placeholder={appId ? '选择镜像' : '请先选择应用'}
            disabled={!appId}
            options={images.map((img: any) => ({
              label: `${img.tag || img.digest?.slice(0, 12) || img.id} (${formatTime(img.created_at)})`,
              value: img.id,
            }))}
          />
        </Form.Item>
        <div style={{ display: 'flex', gap: 12, width: '100%' }}>
          <Form.Item name="source_env" label="源环境" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select options={ENV_OPTIONS} placeholder="源环境" />
          </Form.Item>
          <Form.Item name="target_env" label="目标环境" rules={[{ required: true, message: '请选择目标环境' }]} style={{ flex: 1 }}>
            <Select options={targetOptions} placeholder="目标环境" disabled={!sourceEnv} />
          </Form.Item>
        </div>
        <Form.Item name="strategy" label="发布策略" rules={[{ required: true }]}>
          <Select options={STRATEGY_OPTIONS} />
        </Form.Item>
        <Form.Item
          name="target_group_ids"
          label="目标分组（可选）"
          tooltip="多分组灰度时可指定目标分组；按目标环境过滤，留空表示该环境全部分组"
        >
          <Select
            mode="multiple"
            placeholder={appId ? '留空表示目标环境全部分组' : '请先选择应用'}
            disabled={!appId}
            allowClear
            options={groups
              .filter((g) => !sourceEnv || g.environment === sourceEnv)
              .map((g) => ({
                label: `${g.display_name || g.name}${g.environment ? ` (${g.environment})` : ''}`,
                value: g.id,
              }))}
          />
        </Form.Item>
        <Form.Item name="auto_promote_on_verify" label="验证通过后自动完成晋升" valuePropName="checked">
          <Select
            options={[
              { label: '否（需人工确认）', value: false },
              { label: '是', value: true },
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}

// 晋升详情 + 状态机操作
function PromotionDetail({
  promotion,
  isTerminal,
  onStart,
  onCompleteDeploy,
  onCompleteVerifyPass,
  onCompleteVerifyFail,
  onAbort,
  loading,
}: {
  promotion: Promotion;
  isTerminal: boolean;
  onStart: () => void;
  onCompleteDeploy: () => void;
  onCompleteVerifyPass: () => void;
  onCompleteVerifyFail: () => void;
  onAbort: () => void;
  loading: boolean;
}) {
  const p = promotion;
  return (
    <div>
      <Steps
        size="small"
        current={statusStepIndex(p.status)}
        status={statusStepStatus(p.status)}
        items={[
          { title: '待启动' },
          { title: '部署中' },
          { title: '验证中' },
          { title: p.status === 'aborted' ? '已中止' : p.status === 'failed' ? '已失败' : '已完成' },
        ]}
      />

      <div style={{ marginTop: 24 }}>
        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <Row label="应用ID" value={p.application_id} />
          <Row label="源环境 → 目标环境" value={`${p.source_env} → ${p.target_env}`} />
          <Row label="镜像ID" value={p.artifact_image_id} />
          <Row label="发布策略" value={p.strategy} />
          <Row label="自动晋升" value={p.auto_promote_on_verify ? '是' : '否'} />
          <Row label="开始时间" value={p.started_at ? formatTime(p.started_at) : '-'} />
          <Row label="完成时间" value={p.finished_at ? formatTime(p.finished_at) : '-'} />
          <Row
            label="状态"
            value={<Tag color={STATUS_COLOR[p.status] ?? 'default'}>{p.status}</Tag>}
          />
        </Space>
      </div>

      {!isTerminal && (
        <div style={{ marginTop: 24 }}>
          <Space wrap>
            {p.status === 'pending' && (
              <Button type="primary" loading={loading} onClick={onStart}>
                启动晋升
              </Button>
            )}
            {p.status === 'deploying' && (
              <Button type="primary" loading={loading} onClick={onCompleteDeploy}>
                标记部署完成
              </Button>
            )}
            {p.status === 'verifying' && (
              <>
                <Tooltip title="验证通过，完成晋升">
                  <Button type="primary" loading={loading} onClick={onCompleteVerifyPass}>
                    验证通过
                  </Button>
                </Tooltip>
                <Button danger loading={loading} onClick={onCompleteVerifyFail}>
                  验证失败
                </Button>
              </>
            )}
            <Button danger loading={loading} onClick={onAbort}>
              中止晋升
            </Button>
          </Space>
        </div>
      )}

      {isTerminal && (
        <div style={{ marginTop: 24, color: '#888' }}>
          晋升已结束，状态不可变更。
        </div>
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div style={{ display: 'flex', gap: 12 }}>
      <span style={{ width: 120, color: '#888', flexShrink: 0 }}>{label}</span>
      <span style={{ flex: 1 }}>{value}</span>
    </div>
  );
}
