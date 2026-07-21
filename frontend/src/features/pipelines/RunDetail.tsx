import { useParams, useNavigate } from 'react-router-dom';
import { Button, Descriptions, Space, Spin, Steps, Typography, App } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { EmptyState } from '@/components/EmptyState';
import { pipelineApi } from '@/api/pipelines';
import type { PipelineRun } from '@/types';
import { formatTime, formatDuration, shortSha } from '@/utils/format';

function stageStatus(status?: string): 'finish' | 'process' | 'wait' | 'error' {
  switch (status) {
    case 'success':
    case 'succeeded':
      return 'finish';
    case 'running':
      return 'process';
    case 'failed':
      return 'error';
    default:
      return 'wait';
  }
}

export default function PipelineRunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();
  const runId = Number(id);

  const { data: run, isLoading } = useQuery({
    queryKey: ['pipeline-run', runId],
    queryFn: () => pipelineApi.getRun(runId),
    enabled: !!runId && !Number.isNaN(runId),
  });

  const cancelMutation = useMutation({
    mutationFn: () => pipelineApi.cancelRun(runId),
    onSuccess: () => {
      message.success('已请求取消');
      queryClient.invalidateQueries({ queryKey: ['pipeline-run', runId] });
    },
    onError: (e: any) => message.error(e?.message || '取消失败'),
  });

  if (isLoading) {
    return (
      <PageContainer title="流水线运行详情">
        <Spin />
      </PageContainer>
    );
  }

  if (!run) {
    return (
      <PageContainer title="流水线运行详情">
        <EmptyState title="运行不存在" description="可能已被删除或 ID 错误" />
      </PageContainer>
    );
  }

  const stages = (run.stages || []) as Array<Record<string, any>>;
  const isCancelable = run.status === 'running' || run.status === 'pending' || run.status === 'paused';

  return (
    <PageContainer
      title={`运行 #${run.run_number}`}
      subtitle={run.uuid}
      breadcrumb={[{ title: '流水线', path: '/pipelines' }, { title: `#${run.run_number}` }]}
      extra={
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/pipelines')}>
            返回列表
          </Button>
          {isCancelable && (
            <Button danger loading={cancelMutation.isPending} onClick={() => cancelMutation.mutate()}>
              取消运行
            </Button>
          )}
        </Space>
      }
    >
      <Descriptions bordered column={3} size="small" style={{ marginBottom: 24 }}>
        <Descriptions.Item label="状态">
          <ResourceStatus status={run.status} />
        </Descriptions.Item>
        <Descriptions.Item label="运行号">#{run.run_number}</Descriptions.Item>
        <Descriptions.Item label="触发源">{run.trigger_source || '-'}</Descriptions.Item>
        <Descriptions.Item label="触发者">{run.triggered_by_name || '-'}</Descriptions.Item>
        <Descriptions.Item label="Commit">
          <span title={run.commit_sha}>{shortSha(run.commit_sha)}</span>
        </Descriptions.Item>
        <Descriptions.Item label="开始时间">{formatTime(run.started_at)}</Descriptions.Item>
        <Descriptions.Item label="结束时间">{formatTime(run.finished_at)}</Descriptions.Item>
        <Descriptions.Item label="耗时">{formatDuration(run.duration_ms)}</Descriptions.Item>
        <Descriptions.Item label="流水线 ID">{run.pipeline_id}</Descriptions.Item>
      </Descriptions>

      <Typography.Title level={5}>阶段</Typography.Title>
      {stages.length > 0 ? (
        <Steps
          direction="vertical"
          current={stages.findIndex((s) => s.status === 'running')}
          items={stages.map((s, idx) => ({
            title: s.name || s.stage || `阶段 ${idx + 1}`,
            description: (
              <Space direction="vertical" size={0}>
                {s.status && <ResourceStatus status={s.status} />}
                {s.started_at && <span>开始：{formatTime(s.started_at)}</span>}
                {s.finished_at && <span>结束：{formatTime(s.finished_at)}</span>}
                {s.duration_ms != null && <span>耗时：{formatDuration(s.duration_ms)}</span>}
                {s.message && <Typography.Text type="secondary">{s.message}</Typography.Text>}
              </Space>
            ),
            status: stageStatus(s.status),
          }))}
        />
      ) : (
        <EmptyState title="暂无阶段信息" description="此运行未记录阶段详情" />
      )}
    </PageContainer>
  );
}
