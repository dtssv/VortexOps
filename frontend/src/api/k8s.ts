import { get, post, del } from './client';

// K8s 资源类型（精简版，仅保留前端展示所需字段）

export interface K8sNode {
  metadata: { name: string; creationTimestamp: string; labels?: Record<string, string> };
  status: {
    conditions: Array<{ type: string; status: string }>;
    addresses: Array<{ type: string; address: string }>;
    capacity: Record<string, string>;
    nodeInfo: { kubeletVersion: string; osImage: string; architecture: string };
  };
  spec: { unschedulable?: boolean; taints?: Array<{ key: string; effect: string }> };
}

export interface K8sDeployment {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { replicas: number };
  status: { replicas: number; readyReplicas: number; updatedReplicas: number; availableReplicas: number };
}

export interface K8sStatefulSet {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { replicas: number };
  status: { replicas: number; readyReplicas: number; updatedReplicas: number };
}

export interface K8sDaemonSet {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  status: { desiredNumberScheduled: number; numberReady: number; numberAvailable: number };
}

export interface K8sPod {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { nodeName: string; phase: string };
  status: { phase: string; podIP: string; hostIP: string; containerStatuses: any[] };
}

export interface K8sService {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { type: string; clusterIP: string; externalIPs?: string[]; ports: any[] };
}

export interface K8sIngress {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { rules: any[]; tls?: any[] };
  status: { loadBalancer: { ingress?: any[] } };
}

export interface K8sPersistentVolume {
  metadata: { name: string; creationTimestamp: string };
  spec: { capacity: Record<string, string>; accessModes: string[]; storageClassName: string };
  status: { phase: string };
}

export interface K8sPVC {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: { accessModes: string[]; storageClassName: string; resources: { requests: Record<string, string> } };
  status: { phase: string };
}

export interface K8sStorageClass {
  metadata: { name: string; creationTimestamp: string };
  provisioner: string;
  parameters?: Record<string, string>;
}

export interface K8sConfigMap {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  data?: Record<string, string>;
}

export interface K8sSecret {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  type: string;
}

export interface K8sEvent {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  involvedObject: { kind: string; name: string; namespace: string };
  reason: string;
  message: string;
  type: string;
  lastTimestamp: string;
  count: number;
}

export const k8sApi = {
  // 节点
  listNodes: (clusterId: number) => get<K8sNode[]>(`/k8s/clusters/${clusterId}/nodes`),
  cordonNode: (clusterId: number, nodeName: string) =>
    post(`/k8s/clusters/${clusterId}/nodes/${nodeName}/cordon`),
  uncordonNode: (clusterId: number, nodeName: string) =>
    post(`/k8s/clusters/${clusterId}/nodes/${nodeName}/uncordon`),
  drainNode: (clusterId: number, nodeName: string) =>
    post(`/k8s/clusters/${clusterId}/nodes/${nodeName}/drain`),

  // 云节点池
  getNodePool: (clusterId: number, nodePoolId: string) =>
    get<{ node_pool_id: string; status: string; desired_size?: number; current_size?: number }>(
      `/k8s/clusters/${clusterId}/node-pools/${nodePoolId}`,
    ),
  scaleNodePool: (clusterId: number, nodePoolId: string, desiredSize: number) =>
    post<{ operation_id: string }>(`/k8s/clusters/${clusterId}/node-pools/${nodePoolId}/scale`, {
      desired_size: desiredSize,
    }),

  // 工作负载
  listDeployments: (clusterId: number, namespace?: string) =>
    get<K8sDeployment[]>(`/k8s/clusters/${clusterId}/deployments`, { namespace }),
  listStatefulSets: (clusterId: number, namespace?: string) =>
    get<K8sStatefulSet[]>(`/k8s/clusters/${clusterId}/statefulsets`, { namespace }),
  listDaemonSets: (clusterId: number, namespace?: string) =>
    get<K8sDaemonSet[]>(`/k8s/clusters/${clusterId}/daemonsets`, { namespace }),
  listPods: (clusterId: number, namespace?: string, fieldSelector?: string) =>
    get<K8sPod[]>(`/k8s/clusters/${clusterId}/pods`, { namespace, fieldSelector }),
  scaleDeployment: (clusterId: number, namespace: string, name: string, replicas: number) =>
    post(`/k8s/clusters/${clusterId}/namespaces/${namespace}/deployments/${name}/scale`, { replicas }),
  scaleStatefulSet: (clusterId: number, namespace: string, name: string, replicas: number) =>
    post(`/k8s/clusters/${clusterId}/namespaces/${namespace}/statefulsets/${name}/scale`, { replicas }),
  deletePod: (clusterId: number, namespace: string, name: string) =>
    del(`/k8s/clusters/${clusterId}/namespaces/${namespace}/pods/${name}`),

  // 存储
  listPersistentVolumes: (clusterId: number) =>
    get<K8sPersistentVolume[]>(`/k8s/clusters/${clusterId}/persistentvolumes`),
  listPersistentVolumeClaims: (clusterId: number, namespace?: string) =>
    get<K8sPVC[]>(`/k8s/clusters/${clusterId}/persistentvolumeclaims`, { namespace }),
  listStorageClasses: (clusterId: number) =>
    get<K8sStorageClass[]>(`/k8s/clusters/${clusterId}/storageclasses`),

  // 网络
  listServices: (clusterId: number, namespace?: string) =>
    get<K8sService[]>(`/k8s/clusters/${clusterId}/services`, { namespace }),
  listIngresses: (clusterId: number, namespace?: string) =>
    get<K8sIngress[]>(`/k8s/clusters/${clusterId}/ingresses`, { namespace }),
  listNetworkPolicies: (clusterId: number, namespace?: string) =>
    get<any[]>(`/k8s/clusters/${clusterId}/networkpolicies`, { namespace }),

  // 配置
  listConfigMaps: (clusterId: number, namespace?: string) =>
    get<K8sConfigMap[]>(`/k8s/clusters/${clusterId}/configmaps`, { namespace }),
  listSecrets: (clusterId: number, namespace?: string) =>
    get<K8sSecret[]>(`/k8s/clusters/${clusterId}/secrets`, { namespace }),

  // 事件
  listEvents: (clusterId: number, namespace?: string, fieldSelector?: string) =>
    get<K8sEvent[]>(`/k8s/clusters/${clusterId}/events`, { namespace, fieldSelector }),
};
