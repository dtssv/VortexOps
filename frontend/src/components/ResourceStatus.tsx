import { Tag } from 'antd';
import type { ReactNode } from 'react';

const STATUS_COLOR: Record<string, string> = {
  // build/release/pipeline
  running: 'processing',
  pending: 'default',
  pending_approval: 'warning',
  paused: 'warning',
  success: 'success',
  succeeded: 'success',
  failed: 'error',
  cancelled: 'default',
  aborted: 'default',
  interrupted: 'default',
  rolled_back: 'default',
  // cluster / instance / service
  active: 'success',
  stopped: 'default',
  starting: 'processing',
  updating: 'processing',
  archived: 'default',
  ready: 'success',
  not_ready: 'error',
  partial_ready: 'warning',
  unknown: 'default',
  // severity
  info: 'blue',
  warning: 'orange',
  critical: 'red',
  // alert event
  firing: 'error',
  resolved: 'success',
  suppressed: 'default',
  // generic
  healthy: 'success',
  unhealthy: 'error',
};

const STATUS_TEXT: Record<string, string> = {
  running: '运行中',
  pending: '等待中',
  pending_approval: '待审批',
  paused: '已暂停',
  success: '成功',
  succeeded: '成功',
  failed: '失败',
  cancelled: '已取消',
  aborted: '已终止',
  interrupted: '已中断',
  rolled_back: '已回滚',
  active: '活跃',
  stopped: '已停止',
  starting: '启动中',
  updating: '更新中',
  archived: '已归档',
  ready: '就绪',
  not_ready: '未就绪',
  partial_ready: '部分就绪',
  unknown: '未知',
  healthy: '健康',
  unhealthy: '不健康',
  firing: '告警中',
  resolved: '已恢复',
  suppressed: '已抑制',
};

export function ResourceStatus({ status, text }: { status: string; text?: string }) {
  const color = STATUS_COLOR[status] || 'default';
  const label = text || STATUS_TEXT[status] || status;
  return <Tag color={color}>{label}</Tag>;
}

export function StatusDot({ status }: { status: string }): ReactNode {
  const color = STATUS_COLOR[status] === 'success'
    ? '#52c41a'
    : STATUS_COLOR[status] === 'error'
    ? '#ff4d4f'
    : STATUS_COLOR[status] === 'processing'
    ? '#1677ff'
    : STATUS_COLOR[status] === 'warning'
    ? '#faad14'
    : '#d9d9d9';
  return <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: color, marginRight: 6 }} />;
}
