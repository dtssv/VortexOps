import { get, getPaged, post, put, del } from './client';
import type { AlertRule, AlertEvent, Paged } from '@/types';

// ---------- Alerts ----------
export interface AlertRuleQuery {
  page?: number;
  size?: number;
  scope?: string;
  scope_id?: number;
}

export interface AlertEventQuery {
  page?: number;
  size?: number;
  scope?: string;
  scope_id?: number;
  rule_id?: number;
  status?: string;
}

// ---------- Ops sessions (WebSSH) ----------
export interface OpsSession {
  id: number;
  uuid: string;
  workspace_id: number;
  cluster_id: number;
  namespace: string;
  pod: string;
  container: string;
  type: string;
  status: string;
  user_id: number;
  user_name: string;
  client_ip: string;
  recording_key: string;
  started_at: string;
  ended_at?: string;
  duration_ms: number;
}

export interface BehaviorAuditLog {
  id: number;
  uuid: string;
  workspace_id: number;
  session_id: number;
  cluster_id: number;
  namespace: string;
  pod: string;
  user_id: number;
  user_name: string;
  command: string;
  risk_level: 'info' | 'warn' | 'danger';
  created_at: string;
}

export interface PortForwardResult {
  session_id: string;
  local_port: number;
  remote_port: number;
  local_addr: string;
}

export const opsApi = {
  // Alerts
  listAlertRules: (params?: AlertRuleQuery) => getPaged<AlertRule>('/alert-rules', params),
  listAlertEvents: (params?: AlertEventQuery) => getPaged<AlertEvent>('/alert-events', params),
  createAlertRule: (body: Partial<AlertRule>) => post<AlertRule>('/alert-rules', body),
  updateAlertRule: (id: number, body: Partial<AlertRule>) => put<AlertRule>(`/alert-rules/${id}`, body),
  deleteAlertRule: (id: number) => del(`/alert-rules/${id}`),

  // Logs
  searchLogs: (params: Record<string, any>) => get<Paged<any>>('/logs/search', params),

  // Ops sessions (WebSSH 录像元数据)
  listSessions: (params?: {
    workspace_id?: number;
    cluster_id?: number;
    user_id?: number;
    status?: string;
    page?: number;
    size?: number;
  }) => getPaged<OpsSession>('/ops/sessions/history', params),
  getSession: (id: number) => get<OpsSession>(`/ops/sessions/history/${id}`),
  getReplay: (id: number) => get<{ replay_url: string }>(`/ops/sessions/history/${id}/replay`),

  // 活跃运维会话（实时，含 portforward）
  listActiveSessions: () => get<any[]>('/ops/sessions'),

  // 端口转发（非阻塞，返回分配的本地端口）
  startPortForward: (body: { cluster_id: number; namespace: string; pod: string; port: number; local_port?: number }) =>
    post<PortForwardResult>('/ops/port-forward', body),
  closeSession: (sessionId: string) => del(`/ops/sessions/${sessionId}`),

  // 行为审计（WebSSH 命令捕获）
  listBehavior: (params?: {
    workspace_id?: number;
    session_id?: number;
    user_id?: number;
    page?: number;
    size?: number;
  }) => getPaged<BehaviorAuditLog>('/audit/behavior', params),
};

