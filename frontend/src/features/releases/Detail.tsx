import { useMemo } from 'react';
import { Button, Card, Descriptions, Progress, Space, Tag, Timeline, Typography, App } from 'antd';
import {
  ArrowLeftOutlined,
  CloseCircleOutlined,
  RollbackOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { PageContainer } from '@/components/PageContainer';
import { ResourceStatus } from '@/components/ResourceStatus';
import { releaseApi } from '@/api/releases';
import { groupApi } from '@/api/applications';
import type { ReleaseEvent } from '@/types';
import { formatTime, formatRelative, formatDuration } from '@/utils/format';
import { confirmDanger } from '@/utils/action';
import { strategyLabel } from './labels';

const EVENT_COLOR: Record<string, string> = {
  error: 'red',
  warn: 'orange',
  warning: 'orange',
  info: 'blue',
  debug: 'gray',
};

export default function ReleaseDetailPage() {
  const { id } = useParams<{ id: string }>();
  const releaseId = Number(id);
  const navigate = useNavigate();
  const { message } = App.useApp();
  const queryClient = useQueryClient();

  const { data: release, isLoading } = useQuery({
    queryKey: ['release', releaseId],
    queryFn: () => releaseApi.get(releaseId),
    enabled: !!releaseId,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      return s === 'running' || s === 'pending' || s === 'paused' ? 3000 : false;
    },
  });

  const { data: group } = useQuery({
    queryKey: ['group', release?.group_id],
    queryFn: () => groupApi.get(release!.group_id),
    enabled: !!release?.group_id,
  });

  const { data: events } = useQuery({
    queryKey: ['release', releaseId, 'events'],
    queryFn: () => releaseApi.listEvents(releaseId),
    enabled: !!releaseId,
    refetchInterval: (q) => {
      const r = q.state.data;
      return r && r.some((e) => e.level === 'info' && e.event_type.includes('progress')) ? 3000 : false;
    },
  });

  const abortMutation = useMutation({
    mutationFn: () => releaseApi.abort(releaseId),
    onSuccess: () => {
      message.success('已发送终止指令');
      void queryClient.invalidateQueries({ queryKey: ['release', releaseId] });
    },
    onError: (e: any) => message.error(e?.message || '终止失败'),
  });
  const pauseMutation = useMutation({
    mutationFn: () => releaseApi.pause(releaseId),
    onSuccess: () => {
      message.success('已暂停发布');
      void queryClient.invalidateQueries({ queryKey: ['release', releaseId] });
    },
    onError: (e: any) => message.error(e?.message || '暂停失败'),
  });
  const resumeMutation = useMutation({
    mutationFn: () => releaseApi.resume(releaseId),
    onSuccess: () => {
      message.success('已恢复发布');
      void queryClient.invalidateQueries({ queryKey: ['release', releaseId] });
    },
    onError: (e: any) => message.error(e?.message || '恢复失败'),
  });

  const rollbackMutation = useMutation({
    mutationFn: () => releaseApi.rollback(release!.group_id, { release_id: releaseId }),
    onSuccess: (data) => {
      message.success(`已触发回滚，新发布 #${data.release_number}`);
      void queryClient.invalidateQueries({ queryKey: ['release', releaseId] });
      navigate(`/releases/${data.id}`);
    },
    onError: (e: any) => message.error(e?.message || '回滚失败'),
  });

  const timelineItems = useMemo(() => {
    return (events ?? []).map((e: ReleaseEvent) => ({
      color: EVENT_COLOR[e.level?.toLowerCase()] ?? 'blue',
      children: (
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          <Space size={6}>
            <Typography.Text strong>{e.event_type}</Typography.Text>
            {e.level && e.level !== 'info' && (
              <Tag color={EVENT_COLOR[e.level.toLowerCase()] ?? 'default'}>{e.level}</Tag>
            )}
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {formatTime(e.occurred_at)}
            </Typography.Text>
          </Space>
          <Typography.Text>{e.message}</Typography.Text>
        </Space>
      ),
    }));
  }, [events]);

  const isRunning = release?.status === 'running' || release?.status === 'pending' || release?.status === 'paused';
  const canRollback = release?.status === 'succeeded' && !release?.rollback_of_release_id;

  return (
    <PageContainer
      title={`发布 #${release?.release_number ?? id}`}
      subtitle={group ? `${group.display_name || group.name} · ${group.environment}` : '加载中...'}
      breadcrumb={[
        { title: '发布中心', path: '/releases' },
        { title: `#${release?.release_number ?? id}` },
      ]}
      extra={
        <Space>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)}>
            返回
          </Button>
          {isRunning && (
            <Button
              danger
              icon={<CloseCircleOutlined />}
              loading={abortMutation.isPending}
              onClick={() =>
                confirmDanger({
                  title: '终止发布',
                  content: `确定终止发布 #${release?.release_number}？可能导致服务处于中间状态。`,
                  okText: '终止发布',
                  onOk: () => abortMutation.mutateAsync(),
                })
              }
            >
              终止发布
            </Button>
          )}
          {release?.status === 'running' && (
            <Button icon={<ClockCircleOutlined />} loading={pauseMutation.isPending} onClick={() => pauseMutation.mutateAsync()}>
              暂停
            </Button>
          )}
          {release?.status === 'paused' && (
            <Button type="primary" icon={<ClockCircleOutlined />} loading={resumeMutation.isPending} onClick={() => resumeMutation.mutateAsync()}>
              继续
            </Button>
          )}
          {canRollback && (
            <Button
              icon={<RollbackOutlined />}
              loading={rollbackMutation.isPending}
              onClick={() =>
                confirmDanger({
                  title: '回滚到此版本',
                  content: `将创建一个新发布，把分组回滚到当前版本 #${release?.release_number} 的镜像与配置。`,
                  okText: '确认回滚',
                  onOk: () => rollbackMutation.mutateAsync(),
                })
              }
            >
              回滚到此版本
            </Button>
          )}
        </Space>
      }
    >
      <Card loading={isLoading} style={{ marginBottom: 16 }}>
        <Descriptions column={3} size="small" labelStyle={{ width: 100 }}>
          <Descriptions.Item label="状态">
            <ResourceStatus status={release?.status ?? ''} />
          </Descriptions.Item>
          <Descriptions.Item label="策略">{strategyLabel(release?.strategy)}</Descriptions.Item>
          <Descriptions.Item label="配置版本">v{release?.config_version ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="镜像" span={3}>
            {release?.image_ref ? <Tag color="blue">{release.image_ref}</Tag> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="触发者">{release?.triggered_by_name ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="开始时间">
            <Space size={4}>
              <ClockCircleOutlined />
              {formatTime(release?.started_at)} ({formatRelative(release?.started_at)})
            </Space>
          </Descriptions.Item>
          <Descriptions.Item label="耗时">{formatDuration(release?.duration_ms)}</Descriptions.Item>
          <Descriptions.Item label="副本数">{release?.replicas ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="批次大小">{release?.batch_size ?? '-'}</Descriptions.Item>
          <Descriptions.Item label="批次间隔">
            {release?.batch_interval_sec != null ? `${release.batch_interval_sec}s` : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="进度" span={3}>
            <Progress
              percent={release?.progress_percent ?? 0}
              status={
                release?.status === 'failed' || release?.status === 'aborted'
                  ? 'exception'
                  : release?.status === 'succeeded'
                  ? 'success'
                  : 'active'
              }
            />
          </Descriptions.Item>
          {release?.failure_reason && (
            <Descriptions.Item label="失败原因" span={3}>
              <Typography.Text type="danger">{release.failure_reason}</Typography.Text>
            </Descriptions.Item>
          )}
        </Descriptions>
      </Card>

      <Card title="事件时间线">
        {timelineItems.length === 0 ? (
          <Typography.Text type="secondary">暂无事件</Typography.Text>
        ) : (
          <Timeline items={timelineItems} />
        )}
      </Card>
    </PageContainer>
  );
}
