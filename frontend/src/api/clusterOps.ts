import { get, getPaged, post, del } from './client';
import type {
  ClusterNodeStatus,
  ClusterOperation,
  AbnormalPod,
  AbnormalNode,
  AffectedApp,
  NotifyResult,
  NotifyAffectedInput,
  CreateClusterOperationInput,
  NodeMetricSample,
  PodMetricSample,
  MetricRange,
  Paged,
} from '@/types';

export const clusterOpsApi = {
  // 节点状态
  listNodeStatuses: (clusterId: number) =>
    get<ClusterNodeStatus[]>(`/clusters/${clusterId}/node-statuses`),
  syncNodeStatuses: (clusterId: number) =>
    post<ClusterNodeStatus[]>(`/clusters/${clusterId}/node-statuses/sync`),

  // 异常资源
  listAbnormalPods: (clusterId: number) =>
    get<AbnormalPod[]>(`/clusters/${clusterId}/abnormal-pods`),
  listAbnormalNodes: (clusterId: number) =>
    get<AbnormalNode[]>(`/clusters/${clusterId}/abnormal-nodes`),

  // 受影响应用预览与通知分发
  previewAffected: (clusterId: number, body: NotifyAffectedInput) =>
    post<AffectedApp[]>(`/clusters/${clusterId}/affected-preview`, body),
  notifyAffected: (clusterId: number, body: NotifyAffectedInput) =>
    post<NotifyResult>(`/clusters/${clusterId}/notify-affected`, body),

  // 计划运维任务
  listOperations: (
    clusterId: number,
    params?: { page?: number; size?: number; status?: string },
  ) => getPaged<ClusterOperation>(`/clusters/${clusterId}/operations`, params),
  createOperation: (clusterId: number, body: CreateClusterOperationInput) =>
    post<ClusterOperation>(`/clusters/${clusterId}/operations`, body),
  cancelOperation: (clusterId: number, opId: number) =>
    del(`/clusters/${clusterId}/operations/${opId}`),

  // 节点/Pod 指标（趋势图）
  listLatestNodeMetrics: (clusterId: number) =>
    get<NodeMetricSample[]>(`/clusters/${clusterId}/node-metrics/latest`),
  getNodeMetricSeries: (clusterId: number, nodeName: string, range: MetricRange = '1h') =>
    get<NodeMetricSample[]>(`/clusters/${clusterId}/node-metrics/series`, { nodeName, range }),
  collectNodeMetrics: (clusterId: number) =>
    post<NodeMetricSample[]>(`/clusters/${clusterId}/node-metrics/collect`),
  listLatestPodMetrics: (clusterId: number, nodeName?: string) =>
    get<PodMetricSample[]>(`/clusters/${clusterId}/pod-metrics/latest`, { nodeName }),
  getPodMetricSeries: (
    clusterId: number,
    namespace: string,
    podName: string,
    range: MetricRange = '1h',
  ) =>
    get<PodMetricSample[]>(`/clusters/${clusterId}/pod-metrics/series`, {
      namespace,
      pod: podName,
      range,
    }),
};

// 复用类型导出，便于组件 import。
export type {
  ClusterNodeStatus,
  ClusterOperation,
  AbnormalPod,
  AbnormalNode,
  AffectedApp,
  NotifyResult,
  NotifyAffectedInput,
  CreateClusterOperationInput,
  NodeMetricSample,
  PodMetricSample,
  MetricRange,
  Paged,
};
