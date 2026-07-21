import { useMemo, useState } from 'react';
import { Button, Card, Collapse, Descriptions, Modal, Space, Steps, Tabs, Tag, Typography, App } from 'antd';
import {
  ArrowLeftOutlined,
  CloseCircleOutlined,
  ReloadOutlined,
  ClockCircleOutlined,
  DeploymentUnitOutlined,
  RobotOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { LogViewer } from '@/components/LogViewer';
import { PublishModal } from '@/components/PublishModal';
import { DiagnosisDrawer } from '@/components/DiagnosisDrawer';
import type { LogLine } from '@/components/LogViewer';
import { buildApi } from '@/api/builds';
import type { LogAnalyzeInput } from '@/api/diagnosis';
import type { Build, BuildStep } from '@/types';
import { formatTime, formatRelative, formatDuration, shortSha } from '@/utils/format';
import { confirmDanger } from '@/utils/action';

const STEP_ICON: Record<string, 'wait' | 'process' | 'finish' | 'error'> = {
  pending: 'wait',
  running: 'process',
  success: 'finish',
  failed: 'error',
  cancelled: 'wait',
  skipped: 'wait',
};

export default function BuildDetailPage() {
  const { id } = useParams<{ id: string }>();
  const buildId = Number(id);
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const { data: build, isLoading } = useQuery({
    queryKey: ['build', buildId],
    queryFn: () => buildApi.get(buildId),
    enabled: !!buildId,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s === 'running' || s === 'pending' ? 3000 : false;
    },
  });

  const { data: steps } = useQuery({
    queryKey: ['build', buildId, 'steps'],
    queryFn: () => buildApi.listSteps(buildId),
    enabled: !!buildId,
    refetchInterval: (q) => {
      const b = q.state.data;
      if (!b || b.length === 0) return 3000;
      return b.some((s) => s.status === 'running' || s.status === 'pending') ? 3000 : false;
    },
  });

  const isLive = build?.status === 'running' || build?.status === 'pending';

  const { data: rawLogs } = useQuery({
    queryKey: ['build', buildId, 'logs'],
    queryFn: () => buildApi.logs(buildId),
    enabled: !!buildId,
    refetchInterval: isLive ? 2000 : false,
  });

  // 后端 /logs 返回纯文本（text/plain），X-Log-Source 标识来源（archive/jenkins/tekton/queued）。
  // axios 已将 text/plain 响应体解析为字符串；这里按行切分为 LogLine[]。
  const logs: LogLine[] = useMemo(() => {
    if (!rawLogs || typeof rawLogs !== 'string') return [];
    return rawLogs
      .split('\n')
      .map((message, idx) => ({ sequence: idx, message } as LogLine));
  }, [rawLogs]);

  const logEmpty = logs.length === 0 || (logs.length === 1 && logs[0].message.trim() === '');
  const logQueued = isLive && logEmpty;

  // 按步骤分组日志，用于分步展示（Tekton 模式每 TaskRun 一步）。
  const logsByStep = useMemo(() => {
    const groups: Record<string, LogLine[]> = {};
    for (const l of logs) {
      const key = l.step ?? '__all__';
      if (!groups[key]) groups[key] = [];
      groups[key].push(l);
    }
    return groups;
  }, [logs]);

  const [activeLogStep, setActiveLogStep] = useState<string>('__all__');
  const visibleLogs = activeLogStep === '__all__' ? logs : (logsByStep[activeLogStep] ?? []);
  const hasStepLogs = Object.keys(logsByStep).some((k) => k !== '__all__');

  const cancelMutation = useMutation({
    mutationFn: () => buildApi.cancel(buildId),
    onSuccess: () => {
      message.success('已发送取消指令');
      void queryClient.invalidateQueries({ queryKey: ['build', buildId] });
    },
    onError: (e: any) => message.error(e?.message || '取消失败'),
  });

  const retryMutation = useMutation({
    mutationFn: () => buildApi.rebuild(buildId),
    onSuccess: () => {
      message.success('构建已重新触发，正在拉取代码');
      void queryClient.invalidateQueries({ queryKey: ['build', buildId] });
      void queryClient.invalidateQueries({ queryKey: ['build', buildId, 'steps'] });
      void queryClient.invalidateQueries({ queryKey: ['build', buildId, 'logs'] });
    },
    onError: (e: any) => message.error(e?.message || '重新触发失败'),
  });

  // 发布：构建成功后弹 PublishModal 选分组+策略触发发布。
  const [publishOpen, setPublishOpen] = useState(false);
  const appId = build?.application_id;
  const canPublish = build?.status === 'success' && !!build.output_image_id;

  // AI 诊断：仅在构建失败/取消时展示按钮，点击后收集日志并打开诊断抽屉。
  const [diagOpen, setDiagOpen] = useState(false);
  const [diagInput, setDiagInput] = useState<LogAnalyzeInput | null>(null);
  const diagEnabled = build?.status === 'failed' || build?.status === 'cancelled';

  const openDiagnosis = async () => {
    if (!build) return;
    // 复用已拉取的日志（rawLogs）；若为空则即时拉取一次。
    let logs = typeof rawLogs === 'string' ? rawLogs : '';
    if (!logs) {
      try {
        logs = await buildApi.logs(buildId);
      } catch {
        logs = '';
      }
    }
    setDiagInput({
      source: 'build',
      title: `构建 #${build.id} 失败诊断`,
      build_id: build.id,
      name: `build-${build.id}`,
      error_reason: build.error_message || build.failure_reason || '',
      logs,
    });
    setDiagOpen(true);
  };

  const stepItems = useMemo(() => {
    return (steps ?? []).map((s: BuildStep) => ({
      title: s.step_name,
      description: (
        <Space size={4} wrap>
          {s.started_at && <span style={{ fontSize: 12, color: '#8c8c8c' }}>开始 {formatTime(s.started_at)}</span>}
          {s.duration_ms != null && (
            <span style={{ fontSize: 12, color: '#8c8c8c' }}>· 耗时 {formatDuration(s.duration_ms)}</span>
          )}
          <ResourceStatus status={s.status} />
        </Space>
      ),
      status: STEP_ICON[s.status] ?? 'wait',
    }));
  }, [steps]);

  return (
    <PageContainer
      title={`构建 #${build?.id ?? id}`}
      subtitle={build ? `${build.branch} · ${shortSha(build.commit_sha)}` : '加载中...'}
      breadcrumb={[
        ...(build?.application_id
          ? [
              { title: '应用', path: `/applications/${build.application_id}` },
              { title: '构建列表', path: `/applications/${build.application_id}?tab=builds` },
            ]
          : [{ title: '构建中心', path: '/builds' }]),
        { title: `#${build?.id ?? id}` },
      ]}
      extra={
        <Space>
          <Button
            icon={<ArrowLeftOutlined />}
            onClick={() => navigate(build?.application_id ? `/applications/${build.application_id}?tab=builds` : '/builds')}
          >
            返回
          </Button>
          {canPublish && (
            <Button
              type="primary"
              icon={<DeploymentUnitOutlined />}
              onClick={() => setPublishOpen(true)}
            >
              发布
            </Button>
          )}
          {build?.status === 'running' && (
            <Button
              danger
              icon={<CloseCircleOutlined />}
              loading={cancelMutation.isPending}
              onClick={() =>
                confirmDanger({
                  title: '取消构建',
                  content: `确定取消构建 #${build.id}？此操作不可撤销。`,
                  okText: '取消构建',
                  onOk: () => cancelMutation.mutateAsync(),
                })
              }
            >
              取消构建
            </Button>
          )}
          {(build?.status === 'failed' || build?.status === 'cancelled') && (
            <Button
              icon={<ReloadOutlined />}
              loading={retryMutation.isPending}
              onClick={() =>
                confirmDanger({
                  title: '构建',
                  content: `将在原构建记录上重新拉取代码并构建，不会生成新记录。`,
                  okText: '构建',
                  onOk: () => retryMutation.mutateAsync(),
                })
              }
            >
              构建
            </Button>
          )}
          {diagEnabled && (
            <Button
              icon={<RobotOutlined />}
              onClick={openDiagnosis}
            >
              AI 诊断
            </Button>
          )}
        </Space>
      }
    >
      <Card loading={isLoading} style={{ marginBottom: 16 }}>
        <Descriptions column={3} size="small" labelStyle={{ width: 100 }}>
          <Descriptions.Item label="状态">
            <ResourceStatus status={build?.status ?? ''} />
          </Descriptions.Item>
          <Descriptions.Item label="分支">{build?.branch ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="Commit">
            <Typography.Text code>{shortSha(build?.commit_sha)}</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="提交信息" span={3}>
            {build?.commit_message ?? '-'}
          </Descriptions.Item>
          <Descriptions.Item label="触发者">{build?.triggered_by_name ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="开始时间">
            <Space size={4}>
              <ClockCircleOutlined />
              {formatTime(build?.started_at)} ({formatRelative(build?.started_at)})
            </Space>
          </Descriptions.Item>
          <Descriptions.Item label="耗时">{formatDuration(build?.duration_ms)}</Descriptions.Item>
          <Descriptions.Item label="镜像" span={3}>
            {build?.image_repository ? (
              <Tag color="blue">
                {build.image_repository}:{build.image_tag ?? 'latest'}
              </Tag>
            ) : (
              '-'
            )}
          </Descriptions.Item>
          {build?.error_message && (
            <Descriptions.Item label="错误信息" span={3}>
              <Typography.Text type="danger">{build.error_message}</Typography.Text>
            </Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      {build?.dockerfile_content && (
        <Card title="构建配置" style={{ marginBottom: 16 }}>
          <Descriptions column={2} size="small" labelStyle={{ width: 100 }}>
            <Descriptions.Item label="构建模式">
              {build.dockerfile_source === 'template' ? '模板渲染' :
               build.dockerfile_source === 'repo' ? '仓库自带 Dockerfile' :
               build.dockerfile_source || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="制品路径">
              <Typography.Text code>{build.artifact_path || '-'}</Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="构建命令" span={2}>
              <Typography.Text code>{build.build_command || '-'}</Typography.Text>
            </Descriptions.Item>
            <Descriptions.Item label="Builder 镜像" span={2}>
              <Typography.Text code>{build.builder_image || '-'}</Typography.Text>
            </Descriptions.Item>
          </Descriptions>
          <Collapse
            size="small"
            style={{ marginTop: 12 }}
            items={[
              {
                key: 'dockerfile',
                label: '渲染后的 Dockerfile',
                children: (
                  <pre style={{ background: '#0b0b0b', color: '#e6e6e6', padding: 12, borderRadius: 6, overflow: 'auto', fontSize: 12, margin: 0 }}>
                    {build.dockerfile_content}
                  </pre>
                ),
              },
            ]}
          />
        </Card>
      )}

      <Card title="构建步骤" style={{ marginBottom: 16 }}>
        {stepItems.length === 0 ? (
          <Typography.Text type="secondary">暂无步骤信息</Typography.Text>
        ) : (
          <Steps current={stepItems.findIndex((s) => s.status === 'process')} items={stepItems} direction="vertical" size="small" />
        )}
      </Card>

      <Card title="构建日志" extra={isLive ? <Tag color="processing">实时</Tag> : undefined}>
        {logQueued ? (
          <div style={{ padding: '24px 0', textAlign: 'center', color: '#8c8c8c', fontSize: 13 }}>
            构建已触发，正在等待 Jenkins 分配执行器，日志就绪后自动展示…
          </div>
        ) : hasStepLogs ? (
          <Tabs
            activeKey={activeLogStep}
            onChange={setActiveLogStep}
            items={[
              { key: '__all__', label: '全部', children: <LogViewer lines={logs} height={500} downloadName={`build-${buildId}.log`} /> },
              ...Object.keys(logsByStep)
                .filter((k) => k !== '__all__')
                .sort()
                .map((k) => ({
                  key: k,
                  label: k,
                  children: <LogViewer lines={logsByStep[k]} height={500} downloadName={`build-${buildId}-${k}.log`} />,
                })),
            ]}
          />
        ) : (
          <LogViewer lines={visibleLogs} height={500} downloadName={`build-${buildId}.log`} />
        )}
      </Card>

      <PublishModal
        open={publishOpen}
        onClose={() => setPublishOpen(false)}
        applicationId={appId!}
        fixedImageId={build?.output_image_id}
        onPublished={(relId) => {
          void queryClient.invalidateQueries({ queryKey: ['build', buildId] });
          navigate(`/releases/${relId}`);
        }}
      />

      <DiagnosisDrawer open={diagOpen} onClose={() => setDiagOpen(false)} input={diagInput} />
    </PageContainer>
  );
}
